package account

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
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
	Total     types.Currency `json:"total"`
	Locked    types.Currency `json:"locked"`
	Avaliable types.Currency `json:"available"`
}

// PaymentPlan can be either "pre-payment" or "invoicing".
type PaymentPlan int

const (
	PaymentPlanPrePayment PaymentPlan = iota
	PaymentPlanInvoicing
)

var PredefinedPaymentPlans = []string{"pre-payment", "invoicing"}

// AccountManager manages the user accounts.
type AccountManager struct {
	accounts map[string]*Account
	key      types.PrivateKey
	db       *sql.DB
	mu       sync.Mutex
}

// New returns an initialized account manager.
func New(db *sql.DB) (*AccountManager, error) {
	am := &AccountManager{
		db:       db,
		accounts: make(map[string]*Account),
	}

	if err := am.load(); err != nil {
		return nil, utils.AddContext(err, "couldn't load account manager")
	}

	return am, nil
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
