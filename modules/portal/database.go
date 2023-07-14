package portal

import (
	"bytes"
	"database/sql"
	"errors"
	"runtime"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"golang.org/x/crypto/argon2"

	"lukechampine.com/frand"
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

// userExists checks if there is an account with the given email
// address.
func (p *Portal) userExists(email string) (bool, error) {
	var count int
	err := p.db.QueryRow("SELECT COUNT(*) FROM pt_accounts WHERE email = ?", email).Scan(&count)
	return count > 0, err
}

// isVerified checks if the user account is verified. If password
// is not empty, it also checks if the password matches the one
// in the database.
func (p *Portal) isVerified(email, password string) (verified bool, ok bool, err error) {
	pwHash := make([]byte, 32)
	if password != "" {
		pwh := passwordHash(password)
		copy(pwHash, pwh[:])
	}
	ph := make([]byte, 32)
	var v bool
	err = p.db.QueryRow("SELECT password_hash, verified FROM pt_accounts WHERE email = ?", email).Scan(&ph, &v)
	return v, bytes.Equal(ph, pwHash), err
}

// updateAccount updates the user account in the database.
// If the account does not exist yet, it is created.
func (p *Portal) updateAccount(email, password string, verified bool) error {
	exists, err := p.userExists(email)
	if err != nil {
		return err
	}

	// No entries, create a new account.
	if !exists {
		if password == "" {
			return errors.New("password may not be empty")
		}
		pwHash := passwordHash(password)
		_, err := p.db.Exec(`
			INSERT INTO pt_accounts (email, password_hash, verified, time, nonce)
			VALUES (?, ?, ?, ?, ?)`, email, pwHash[:], false, time.Now().Unix(), []byte{})
		return err
	}

	// An entry found, update it.
	if password == "" {
		_, err := p.db.Exec("UPDATE pt_accounts SET verified = ? WHERE email = ?", verified, email)
		return err
	}
	pwHash := passwordHash(password)
	_, err = p.db.Exec("UPDATE pt_accounts SET password_hash = ?, verified = ? WHERE email = ?", pwHash[:], verified, email)
	return err
}

// passwordHash implements the Argon2id hashing mechanism.
func passwordHash(password string) (pwh types.Hash256) {
	t := uint8(runtime.NumCPU())
	hash := argon2.IDKey([]byte(password), []byte(argon2Salt), 1, 64 * 1024, t, 32)
	copy(pwh[:], hash)
	return
}

// threadedPruneUnverifiedAccounts deletes unverified user accounts
// from the database.
func (p *Portal) threadedPruneUnverifiedAccounts() {
	for {
		select {
		case <-p.tg.StopChan():
			return
		case <-time.After(pruneUnverifiedAccountsFrequency):
		}

		func() {
			err := p.tg.Add()
			if err != nil {
				return
			}
			defer p.tg.Done()

			p.mu.Lock()
			defer p.mu.Unlock()

			now := time.Now().Unix()
			_, err = p.db.Exec("DELETE FROM pt_accounts WHERE verified = FALSE AND time < ?", now - pruneUnverifiedAccountsThreshold.Milliseconds() / 1000)
			if err != nil {
				p.log.Printf("ERROR: error querying database: %v\n", err)
			}
		}()
	}
}

// deleteAccount deletes the user account from the database.
func (p *Portal) deleteAccount(email string) error {
	var errs []error
	var err error

	// Search for the renter public key.
	pk := make([]byte, 32)
	err = p.db.QueryRow("SELECT public_key FROM ctr_renters WHERE email = ?", email).Scan(&pk)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Delete contracts.
	_, err = p.db.Exec("DELETE FROM ctr_contracts WHERE renter_pk = ?", pk)
	errs = append(errs, err)

	// Delete from other tables.
	_, err = p.db.Exec("DELETE FROM ctr_renters WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM mg_spendings WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM pt_payments WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM mg_balances WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM pt_accounts WHERE email = ?", email)
	errs = append(errs, err)

	return modules.ComposeErrors(errs...)
}

// putPayment inserts a payment into the database.
func (p *Portal) putPayment(email string, amount float64, currency string) error {
	// Convert the amount to SC.
	rate, err := p.manager.GetSiacoinRate(currency)
	if err != nil {
		return err
	}
	if rate == 0 {
		return errors.New("unable to get SC exchange rate")
	}

	// Insert the payment.
	amountSC := amount / rate
	timestamp := time.Now().Unix()
	_, err = p.db.Exec(`
		INSERT INTO pt_payments (email, amount, currency, amount_sc, made_at)
		VALUES (?, ?, ?, ?, ?)`, email, amount, currency, amountSC, timestamp)

	return err
}

// addPayment updates the payments and balances tables with a new payment.
func (p *Portal) addPayment(id string, amount float64, currency string) error {
	// Sanity checks.
	if id == "" || currency == "" || amount == 0 {
		return errors.New("one or more empty parameters provided")
	}
	rate, err := p.manager.GetSiacoinRate(currency)
	if err != nil {
		return err
	}
	if rate == 0 {
		return errors.New("unable to get SC exchange rate")
	}

	// Fetch the account.
	var email string
	var s bool
	var b, l float64
	var c string
	err = p.db.QueryRow(`
		SELECT email, subscribed, sc_balance, sc_locked, currency
		FROM mg_balances WHERE stripe_id = ?
	`, id).Scan(&email, &s, &b, &l, &c)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// No record found. Check for a special case when the account is being
	// credited. In this case, id is the user's email.
	credit := false
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		exists, err := p.userExists(id)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("trying to credit a non-existing account")
		}
		email = id
		credit = true
	}

	// Update the payments table.
	if err = p.putPayment(email, amount, currency); err != nil {
		return err
	}

	// Calculate the new balance.
	ub := modules.UserBalance{
		IsUser:     true,
		Subscribed: s,
		Balance:    b,
		Locked:     l,
		Currency:   c,
		StripeID:   id,
	}
	if ub.Currency == "" {
		// New renter, need to create a new record.
		seed, err := p.manager.GetWalletSeed()
		defer frand.Read(seed[:])
		if err != nil {
			return err
		}
		renterSeed := modules.DeriveRenterSeed(seed, email)
		defer frand.Read(renterSeed)
		pk := types.NewPrivateKeyFromSeed(renterSeed).PublicKey()
		if err = p.createNewRenter(email, pk); err != nil {
			return err
		}
		ub.Currency = currency
	}
	if credit {
		ub.StripeID = ""
	}
	ub.Balance += amount / rate

	// Update the balances table.
	err = p.manager.UpdateBalance(email, ub)

	return err
}

// getPayments retrieves the payments from the account payment
// history.
func (p *Portal) getPayments(email string) ([]userPayment, error) {
	rows, err := p.db.Query(`
		SELECT amount, currency, amount_sc, made_at FROM pt_payments
		WHERE email = ?
	`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := make([]userPayment, 0)
	var payment userPayment

	for rows.Next() {
		err := rows.Scan(&payment.Amount, &payment.Currency, &payment.AmountSC, &payment.Timestamp)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}

	return payments, nil
}

// createNewRenter creates a new renter record in the database.
func (p *Portal) createNewRenter(email string, pk types.PublicKey) error {
	_, err := p.db.Exec(`
		INSERT INTO ctr_renters
		(email, public_key, current_period, allowance,
		private_key, auto_renew_contracts)
		VALUES (?, ?, ?, ?, ?, ?)
	`, email, pk[:], 0, []byte{}, []byte{}, false)
	if err != nil {
		return err
	}
	p.manager.CreateNewRenter(email, pk)

	return nil
}

// saveNonce updates a user account with the nonce value.
func (p* Portal) saveNonce(email string, nonce []byte) error {
	_, err := p.db.Exec("UPDATE pt_accounts SET nonce = ? WHERE email = ?", nonce, email)
	return err
}

// verifyNonce verifies the nonce value against the user account.
func (p* Portal) verifyNonce(email string, nonce []byte) (bool, error) {
	n := make([]byte, 16)
	err := p.db.QueryRow("SELECT nonce FROM pt_accounts WHERE email = ?", email).Scan(&n)
	if err != nil {
		return false, err
	}

	return bytes.Equal(n, nonce), nil
}

// saveStats updates the authentication stats in the database.
func (p* Portal) saveStats() error {
	tx, err := p.db.Begin()
	if err != nil {
		p.log.Println("ERROR: couldn't save auth stats:", err)
		return err
	}

	_, err = tx.Exec("DELETE FROM pt_stats")
	if err != nil {
		p.log.Println("ERROR: couldn't clear auth stats:", err)
		tx.Rollback()
		return err
	}

	for ip, entry := range p.authStats {
		_, err = tx.Exec(`
			INSERT INTO pt_stats
			(remote_host, login_last, login_count, verify_last,
			verify_count, reset_last, reset_count)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, ip, entry.FailedLogins.LastAttempt, entry.FailedLogins.Count, entry.Verifications.LastAttempt, entry.Verifications.Count, entry.PasswordResets.LastAttempt, entry.PasswordResets.Count)
		if err != nil {
			p.log.Println("ERROR: couldn't save auth stats:", err)
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// loadStats loads the authentication stats from the database.
func (p* Portal) loadStats() error {
	rows, err := p.db.Query(`
		SELECT remote_host, login_last, login_count, verify_last,
		verify_count, reset_last, reset_count
		FROM pt_stats
	`)
	if err != nil {
		p.log.Println("ERROR: couldn't load auth stats:", err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var ip string
		var ll, lc, vl, vc, rl, rc int64
		if err := rows.Scan(&ip, &ll, &lc, &vl, &vc, &rl, &rc); err != nil {
			p.log.Println("ERROR: couldn't load auth stats:", err)
			return err
		}
		p.authStats[ip] = authenticationStats{
			RemoteHost: ip,
			FailedLogins: authAttempts{
				LastAttempt: ll,
				Count:       lc,
			},
			Verifications: authAttempts{
				LastAttempt: vl,
				Count:       vc,
			},
			PasswordResets: authAttempts{
				LastAttempt: rl,
				Count:       rc,
			},
		}
	}

	return nil
}

// saveCredits updates the promotion data in the database.
func (p* Portal) saveCredits() error {
	_, err := p.db.Exec(`
		REPLACE INTO pt_credits (id, amount, remaining)
		VALUES (1, ?, ?)
	`, p.credits.Amount, p.credits.Remaining)
	if err != nil {
		p.log.Println("ERROR: couldn't save credit data:", err)
		return err
	}

	return nil
}

// loadCredits loads the promotion data from the database.
func (p* Portal) loadCredits() error {
	var a float64
	var r uint64
	err := p.db.QueryRow(`
		SELECT amount, remaining
		FROM pt_credits
		WHERE id = 1
	`).Scan(&a, &r)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		p.log.Println("ERROR: couldn't load credit data:", err)
		return err
	}

	p.credits.Amount = a
	p.credits.Remaining = r

	return nil
}
