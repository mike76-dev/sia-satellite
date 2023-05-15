package portal

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"runtime"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/fastrand"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ed25519"
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
	err := p.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE email = ?", email).Scan(&count)
	return count > 0, err
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
			INSERT INTO accounts (email, password_hash, verified, created, nonce)
			VALUES (?, ?, ?, ?, ?)`, email, pwHash, false, time.Now().Unix(), "")
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
	var errs []error
	var err error

	// Search for the renter public key.
	var pk string
	err = p.db.QueryRow("SELECT public_key FROM renters WHERE email = ?", email).Scan(&pk)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Delete contracts and transactions.
	if err == nil {
		var fcid string
		rows, err := p.db.Query(`
			SELECT contract_id
			FROM transactions
			WHERE uc_renter_pk = ?
		`, pk)
		errs = append(errs, err)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				err = rows.Scan(&fcid)
				if err == nil {
					_, err = p.db.Exec("DELETE FROM transactions WHERE contract_id = ?", fcid)
					errs = append(errs, err)
					_, err = p.db.Exec("DELETE FROM contracts WHERE contract_id = ?", fcid)
					errs = append(errs, err)
				}
			}
		}
	}

	// Delete from other tables.
	_, err = p.db.Exec("DELETE FROM renters WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM spendings WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM payments WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM balances WHERE email = ?", email)
	errs = append(errs, err)
	_, err = p.db.Exec("DELETE FROM accounts WHERE email = ?", email)
	errs = append(errs, err)

	// Iterate through the errors and return the first non-nil one.
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	return nil
}

// putPayment inserts a payment into the database.
func (p *Portal) putPayment(email string, amount float64, currency string) error {
	// Convert the amount to SC.
	rate, err := p.satellite.GetSiacoinRate(currency)
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
		INSERT INTO payments (email, amount, currency, amount_sc, made_at)
		VALUES (?, ?, ?, ?, ?)`, email, amount, currency, amountSC, timestamp)

	return err
}

// addPayment updates the payments and balances tables with a new payment.
func (p *Portal) addPayment(id string, amount float64, currency string) error {
	// Sanity checks.
	if id == "" || currency == "" || amount == 0 {
		return errors.New("one or more empty parameters provided")
	}
	rate, err := p.satellite.GetSiacoinRate(currency)
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
		SELECT email, subscribed, balance, locked, currency
		FROM balances WHERE stripe_id = ?
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
	ub := &modules.UserBalance{
		IsUser:     true,
		Subscribed: s,
		Balance:    b,
		Locked:     l,
		Currency:   c,
		StripeID:   id,
	}
	if ub.Currency == "" {
		// New renter, need to create a new record.
		seed, err := p.satellite.GetWalletSeed()
		defer fastrand.Read(seed[:])
		if err != nil {
			return err
		}
		renterSeed := modules.DeriveRenterSeed(seed, email)
		defer fastrand.Read(renterSeed[:])
		var sk crypto.SecretKey
		copy(sk[:], ed25519.NewKeyFromSeed(renterSeed[:]))
		pk := types.Ed25519PublicKey(sk.PublicKey())
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
	err = p.satellite.UpdateBalance(email, ub)

	return err
}

// getPayments retrieves the payments from the account payment
// history.
func (p *Portal) getPayments(email string) ([]userPayment, error) {
	rows, err := p.db.Query(`
		SELECT amount, currency, amount_sc, made_at FROM payments
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
func (p *Portal) createNewRenter(email string, pk types.SiaPublicKey) error {
	_, err := p.db.Exec(`
		INSERT INTO renters (email, public_key, current_period, funds, hosts,
			period, renew_window, expected_storage, expected_upload,
			expected_download, min_shards, total_shards, max_rpc_price,
			max_contract_price, max_download_bandwidth_price,
			max_sector_access_price, max_storage_price,
			max_upload_bandwidth_price, min_max_collateral, blockheight_leeway)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, email, pk.String(), 0, "", 0, 0, 0, 0, 0, 0, 0, 0, "", "", "", "", "", "", "", 0)
	if err != nil {
		return err
	}
	p.satellite.CreateNewRenter(email, pk)

	return nil
}

// saveNonce updates a user account with the nonce value.
func (p* Portal) saveNonce(email string, nonce []byte) error {
	_, err := p.db.Exec("UPDATE accounts SET nonce = ? WHERE email = ?", hex.EncodeToString(nonce), email)
	return err
}

// verifyNonce verifies the nonce value against the user account.
func (p* Portal) verifyNonce(email string, nonce []byte) (bool, error) {
	var n string
	err := p.db.QueryRow("SELECT nonce FROM accounts WHERE email = ?", email).Scan(&n)
	if err != nil {
		return false, err
	}

	ns := hex.EncodeToString(nonce)
	
	return ns == n, nil
}
