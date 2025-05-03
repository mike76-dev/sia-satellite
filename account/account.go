package account

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/external"
	"github.com/mike76-dev/sia-satellite/internal/utils"
	"github.com/mike76-dev/sia-satellite/wallet"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.uber.org/zap"
	"lukechampine.com/frand"
)

var (
	// ErrUserNotFound is returned when there is no account with such email.
	ErrUserNotFound = errors.New("user not found")

	// ErrNotVerified is returned when the account email has not been verified.
	ErrNotVerified = errors.New("account not verified")

	// ErrWrongPassword is returned when a wrong password has been provided.
	ErrWrongPassword = errors.New("provided password is incorrect")

	// ErrWrongCode is returned when a wrong code has been provided.
	ErrWrongCode = errors.New("provided code is incorrect")

	// ErrCodeExpired is returned when a code has been provided that has expired.
	ErrCodeExpired = errors.New("code has expired")
)

// verificationCode defines a 6-digit code that expires after the specified time.
type verificationCode struct {
	code    string
	expires time.Time
}

// generate generates a new verification code.
func (vc *verificationCode) generate(expiration time.Time) {
	vc.code = fmt.Sprintf("%06d", frand.Intn(1e6))
	vc.expires = expiration
}

// Account holds the user account data.
type Account struct {
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"createdAt"`
	Verified    bool      `json:"verified"`
	PaymentPlan string    `json:"paymentPlan"`
	Balance     Balance   `json:"balance"`
	Currency    string    `json:"currency"`
	StripeID    string    `json:"stripeID"`

	verification verificationCode
	address      types.Address
}

func (acc *Account) GenerateCode(expiration time.Time) string {
	acc.verification.generate(expiration)
	return acc.verification.code
}

func (acc *Account) VerifyCode(code string) error {
	if code != acc.verification.code {
		return ErrWrongCode
	} else if time.Now().After(acc.verification.expires) {
		return ErrCodeExpired
	} else {
		acc.verification.code = ""
		acc.verification.expires = time.Time{}
		return nil
	}
}

// Balance is the breakdown of the user's balance, in Siacoin.
type Balance struct {
	Total    types.Currency `json:"total"`
	Locked   types.Currency `json:"locked"`
	Negative bool           `json:"negative"`
}

// PaymentPlan can be either "pre-payment" or "invoicing".
type PaymentPlan int

const (
	PaymentPlanPrePayment PaymentPlan = iota
	PaymentPlanInvoicing
)

var PredefinedPaymentPlans = []string{"pre-payment", "invoicing"}

// Payment contains the details of a payment made by a user.
type Payment struct {
	Amount            float64             `json:"amount"`
	Currency          string              `json:"currency"`
	SCRate            float64             `json:"scRate"`
	Timestamp         time.Time           `json:"timestamp"`
	ConfirmationsLeft int                 `json:"confirmationsRequired"`
	TransactionID     types.TransactionID `json:"transactionID,omitempty"`
}

// AccountManager manages the user accounts.
type AccountManager struct {
	accounts     map[string]*Account
	addresses    map[types.Address]string
	transactions map[types.TransactionID]map[types.Address]string
	rates        map[string]float64
	key          types.PrivateKey
	db           *sql.DB
	log          *zap.Logger
	chain        *chain.Manager
	wallet       *wallet.Wallet
	mu           sync.Mutex
	closeChan    chan struct{}
	tip          types.ChainIndex
}

// New returns an initialized account manager.
func New(db *sql.DB, cm *chain.Manager, w *wallet.Wallet, logger *zap.Logger) (*AccountManager, error) {
	am := &AccountManager{
		db:           db,
		chain:        cm,
		wallet:       w,
		log:          logger,
		accounts:     make(map[string]*Account),
		addresses:    make(map[types.Address]string),
		transactions: make(map[types.TransactionID]map[types.Address]string),
		rates:        make(map[string]float64),
	}

	go am.fetchSiacoinRates()
	go am.checkTransactions()

	if err := am.load(); err != nil {
		return nil, utils.AddContext(err, "couldn't load account manager")
	}

	reorgCh := make(chan struct{}, 1)
	reorgCh <- struct{}{}
	stop := cm.OnReorg(func(index types.ChainIndex) {
		select {
		case reorgCh <- struct{}{}:
		default:
		}
	})

	go func() {
		defer stop()

		for cm.Tip().Height <= am.tip.Height {
			select {
			case <-am.closeChan:
				return
			default:
				time.Sleep(5 * time.Second)
			}
		}

		for w.IsScanning() {
			select {
			case <-am.closeChan:
				return
			default:
				time.Sleep(5 * time.Second)
			}
		}

		for {
			select {
			case <-am.closeChan:
				return
			case <-reorgCh:
				if err := am.sync(am.tip); err != nil {
					am.log.Error("failed to sync", zap.Error(err))
				}
			}
		}
	}()

	return am, nil
}

// Close shuts down the account manager.
func (am *AccountManager) Close() {
	am.closeChan <- struct{}{}
}

// fetchSiacoinRates periodically fetches the SC exchange rates.
func (am *AccountManager) fetchSiacoinRates() {
	fetch := func() {
		rates, err := external.FetchSCRates()
		if err != nil {
			am.log.Error("failed to fetch SC exchange rates", zap.Error(err))
		} else {
			am.mu.Lock()
			am.rates = rates
			am.mu.Unlock()
		}
	}

	fetch()
	for {
		select {
		case <-am.closeChan:
			return
		case <-time.After(10 * time.Minute):
			fetch()
		}
	}
}

// FindAccount returns an account with the specified email.
func (am *AccountManager) FindAccount(email string) (*Account, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	acc, ok := am.accounts[email]
	if !ok {
		return nil, ErrUserNotFound
	}

	return acc, nil
}

// Accounts returns all accounts.
func (am *AccountManager) Accounts() (accs []Account) {
	am.mu.Lock()
	defer am.mu.Unlock()

	for _, acc := range am.accounts {
		accs = append(accs, *acc)
	}

	return
}

// GetSiacoinRate returns the Siacoin exchange rate for the given currency.
// If the currency is not supported, zero is returned.
func (am *AccountManager) GetSiacoinRate(currency string) float64 {
	currency = strings.ToLower(currency)
	if currency == "sc" { // edge case
		return 1
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	return am.rates[currency]
}
