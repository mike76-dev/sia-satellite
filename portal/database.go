package portal

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"runtime"
	"time"

	nerrors "gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"

	"golang.org/x/crypto/argon2"
)

const (
	// argon2Salt is the salt for the password hashing algorithm.
	argon2Salt = "SiaSatellitePasswordHashingSalt."

	// pruneUnverifiedAccountsFrequency is how often unverified
	// accounts are pruned from the database.
	pruneUnverifiedAccountsFrequency = 2 * time.Hour

	// pruneUnverifiedAccountsThreshold is how old an unverified
	// account needs to be to get pruned.
	pruneUnverifiedAccountsThreshold = 7 * 24 * time.Hour
)

// countEmails counts all accounts with the given email
// address. There should be at most one per address.
func (p *Portal) countEmails(email string) (count int, err error) {
	err = p.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE email = ?", email).Scan(&count)
	return
}

// isVerified checks if the user account is verified. If password
// is not empty, it also checks if the password matches the one
// in the database.
func (p *Portal) isVerified(email, password string) (verified bool, ok bool, err error) {
	pwHash := ""
	if password != "" {
		pwHash = passwordHash(password)
	}
	var ph string
	var v bool
	err = p.db.QueryRow("SELECT password_hash, verified FROM accounts WHERE email = ?", email).Scan(&ph, &v)
	return v, (ph == pwHash), err
}

// updateAccount updates the user account in the database.
// If the account does not exist yet, it is created.
func (p *Portal) updateAccount(email, password string, verified bool) error {
	c, err := p.countEmails(email)
	if err != nil {
		return err
	}

	// No entries, create a new account.
	if c == 0 {
		if password == "" {
			return errors.New("password may not be empty")
		}
		pwHash := passwordHash(password)
		_, err := p.db.Exec(`
			INSERT INTO accounts (email, password_hash, verified, created)
			VALUES (?, ?, ?, ?)`, email, pwHash, false, time.Now().Unix())
		return err
	}

	// An entry found, update it.
	if password == "" {
		_, err := p.db.Exec("UPDATE accounts SET verified = ? WHERE email = ?", verified, email)
		return err
	}
	pwHash := passwordHash(password)
	_, err = p.db.Exec("UPDATE accounts SET password_hash = ?, verified = ? WHERE email = ?", pwHash, verified, email)
	return err
}

// passwordHash implements the Argon2id hashing mechanism.
func passwordHash(password string) string {
	t := uint8(runtime.NumCPU())
	hash := argon2.IDKey([]byte(password), []byte(argon2Salt), 1, 64 * 1024, t, 32)
	defer fastrand.Read(hash[:])
	return hex.EncodeToString(hash)
}

// threadedPruneUnverifiedAccounts deletes unverified user accounts
// from the database.
func (p *Portal) threadedPruneUnverifiedAccounts() {
	for {
		select {
		case <-p.threads.StopChan():
			return
		case <-time.After(pruneUnverifiedAccountsFrequency):
		}

		func() {
			err := p.threads.Add()
			if err != nil {
				return
			}
			defer p.threads.Done()

			p.mu.Lock()
			defer p.mu.Unlock()

			now := time.Now().Unix()
			_, err = p.db.Exec("DELETE FROM accounts WHERE verified = FALSE AND created < ?", now - pruneUnverifiedAccountsThreshold.Milliseconds() / 1000)
			if err != nil {
				p.log.Printf("ERROR: error querying database: %v\n", err)
			}
		}()
	}
}

// deleteAccount deletes the user account from the database.
func (p *Portal) deleteAccount(email string) error {
	_, err1 := p.db.Exec("DELETE FROM payments WHERE email = ?", email)
	_, err2 := p.db.Exec("DELETE FROM balances WHERE email = ?", email)
	_, err3 := p.db.Exec("DELETE FROM accounts WHERE email = ?", email)
	return nerrors.Compose(err1, err2, err3)
}

// getBalance retrieves the balance information on the account.
// An empty struct is returned when there is no data.
func (p *Portal) getBalance(email string) (*userBalance, error) {
	var s bool
	var b float64
	var c string
	err := p.db.QueryRow("SELECT subscribed, balance, currency FROM balances WHERE email = ?", email).Scan(&s, &b, &c)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	ub := &userBalance{
		IsUser:     !errors.Is(err, sql.ErrNoRows),
		Subscribed: s,
		Balance:    b,
		Currency:   c,
	}

	return ub, nil
}

// flushPendingPayments removes any pending payments for the given
// user account.
func (p *Portal) flushPendingPayments(email string) error {
	_, err := p.db.Exec("DELETE FROM payments WHERE email = ? AND pending = ?", email, true)
	return err
}

// putPendingPayment inserts a pending payment into the database.
func (p *Portal) putPendingPayment(email string, amount float64, currency string) error {
	// Convert the amount to USD.
	p.mu.Lock()
	rate, exists := p.exchRates[currency]
	p.mu.Unlock()
	if !exists {
		return errors.New("unsupported currency")
	}
	if rate == 0 {
		return errors.New("unable to get exchange rate")
	}

	// Flush any existing pending payments first.
	if err := p.flushPendingPayments(email); err != nil {
		return err
	}

	// Insert a pending payment.
	amountUSD := amount / rate
	timestamp := time.Now().Unix()
	_, err := p.db.Exec(`
		INSERT INTO payments (email, amount, currency, amount_usd, made, pending)
		VALUES (?, ?, ?, ?, ?, ?)`, email, amount, currency, amountUSD, timestamp, true)

	return err
}

// getPendingPayment retrieves a pending payment from the database.
func (p *Portal) getPendingPayment(email string) (*userPayment, error) {
	var amount, amountUSD float64
	var currency string
	var timestamp uint64

	err := p.db.QueryRow(`
		SELECT amount, currency, amount_usd, made FROM payments
		WHERE email = ? AND pending = ?`, email, true).Scan(&amount, &currency, &amountUSD, &timestamp)

	// If there are no rows, return an error anyway.
	if err != nil {
		return nil, err
	}

	up := &userPayment{
		Amount: amount,
		Currency: currency,
		AmountUSD: amountUSD,
		Timestamp: timestamp,
	}

	return up, nil
}
