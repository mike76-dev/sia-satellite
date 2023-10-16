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
			INSERT INTO pt_accounts (email, password_hash, verified, time, nonce, sc_address)
			VALUES (?, ?, ?, ?, ?, ?)`, email, pwHash[:], false, time.Now().Unix(), []byte{}, []byte{})
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
	hash := argon2.IDKey([]byte(password), []byte(argon2Salt), 1, 64*1024, t, 32)
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
			_, err = p.db.Exec("DELETE FROM pt_accounts WHERE verified = FALSE AND time < ?", now-pruneUnverifiedAccountsThreshold.Milliseconds()/1000)
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

	// Delete file metadata.
	var objects, slabs []types.Hash256
	rows, err := p.db.Query("SELECT enc_key FROM ctr_metadata WHERE renter_pk = ?", pk)
	if err != nil {
		return err
	}

	for rows.Next() {
		id := make([]byte, 32)
		if err := rows.Scan(&id); err != nil {
			p.log.Println("ERROR: unable to retrieve metadata key:", err)
			continue
		}
		var key types.Hash256
		copy(key[:], id)
		objects = append(objects, key)

		slabRows, err := p.db.Query("SELECT enc_key FROM ctr_slabs WHERE object_id = ?", id)
		if err != nil {
			p.log.Println("ERROR: unable to retrieve slabs:", err)
			continue
		}

		for slabRows.Next() {
			slabID := make([]byte, 32)
			if err := slabRows.Scan(&slabID); err != nil {
				p.log.Println("ERROR: unable to retrieve slab:", err)
				continue
			}
			var slab types.Hash256
			copy(slab[:], slabID)
			slabs = append(slabs, slab)
		}
		slabRows.Close()
	}
	rows.Close()

	for _, slab := range slabs {
		_, err = p.db.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", slab[:])
		errs = append(errs, err)
	}

	for _, object := range objects {
		_, err = p.db.Exec("DELETE FROM ctr_slabs WHERE object_id = ?", object[:])
		errs = append(errs, err)
	}

	_, err = p.db.Exec("DELETE FROM ctr_metadata WHERE renter_pk = ?", pk)
	errs = append(errs, err)

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
func (p *Portal) putPayment(email string, amount float64, currency string, txid types.TransactionID) error {
	// Check if the currency is SC.
	var amountSC float64
	var left int
	if currency == "SC" {
		amountSC = amount
		left = 6
	} else {
		// Convert the amount to SC.
		rate, err := p.manager.GetSiacoinRate(currency)
		if err != nil {
			return err
		}
		if rate == 0 {
			return errors.New("unable to get SC exchange rate")
		}
		amountSC = amount / rate
	}

	// Insert the payment.
	timestamp := time.Now().Unix()
	_, err := p.db.Exec(`
		INSERT INTO pt_payments (email, amount, currency, amount_sc, made_at, conf_left, txid)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		email,
		amount,
		currency,
		amountSC,
		timestamp,
		left,
		txid[:],
	)

	return err
}

// addPayment updates the payments and balances tables with a new payment.
func (p *Portal) addPayment(id string, amount float64, currency string, def bool) error {
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
	err = p.db.QueryRow(`
		SELECT email, subscribed, sc_balance, sc_locked
		FROM mg_balances WHERE stripe_id = ?
	`, id).Scan(&email, &s, &b, &l)
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
	if !def {
		if err = p.putPayment(email, amount, currency, types.TransactionID{}); err != nil {
			return err
		}
	}

	// Calculate the new balance.
	ub := modules.UserBalance{
		IsUser:     true,
		IsRenter:   true,
		Subscribed: s || def,
		Balance:    b,
		Locked:     l,
		Currency:   currency,
		StripeID:   id,
	}
	if credit {
		ub.StripeID = ""
	}
	if !def {
		ub.Balance += amount / rate
	}

	// Update the balances table.
	if err = p.manager.UpdateBalance(email, ub); err != nil {
		return err
	}

	// Create a new renter if needed.
	var c int
	err = p.db.QueryRow("SELECT COUNT(*) FROM ctr_renters WHERE email = ?", email).Scan(&c)
	if err != nil {
		return err
	}
	if c == 0 {
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
	}

	return nil
}

// addSiacoinPayment adds a new payment in Siacoin.
func (p *Portal) addSiacoinPayment(email string, amount types.Currency, txid types.TransactionID) error {
	// Sanity check.
	if amount.IsZero() {
		return errors.New("zero payment amount provided")
	}

	// Update the payments table.
	amt := modules.Float64(amount) / modules.Float64(types.HastingsPerSiacoin)
	if err := p.putPayment(email, amt, "SC", txid); err != nil {
		return err
	}

	return nil
}

// confirmSiacoinPayment decrements the number of remaining payment
// confirmations.
func (p *Portal) confirmSiacoinPayment(txid types.TransactionID) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}

	// Fetch the payment.
	var email string
	var amount float64
	var left int
	err = tx.QueryRow(`
		SELECT email, amount, conf_left
		FROM pt_payments
		WHERE txid = ?
	`, txid[:]).Scan(&email, &amount, &left)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return modules.AddContext(err, "couldn't fetch payment")
	}

	// Sanity check.
	if left == 0 {
		tx.Commit()
		return nil
	}

	// Update the payments table.
	left--
	_, err = tx.Exec("UPDATE pt_payments SET conf_left = ? WHERE txid = ?", left, txid[:])
	if err != nil {
		return modules.AddContext(err, "couldn't update payment")
	}

	// If the tx is confirmed, increase the account balance.
	if left == 0 {
		// Delete from the map.
		p.mu.Lock()
		delete(p.transactions, txid)
		p.mu.Unlock()

		// Fetch the account.
		var c, id string
		var s bool
		var b, l float64
		err := tx.QueryRow(`
			SELECT subscribed, sc_balance, sc_locked, currency, stripe_id
			FROM mg_balances WHERE email = ?
		`, email).Scan(&s, &b, &l, &c, &id)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't fetch account balance")
		}

		// Calculate the new balance.
		ub := modules.UserBalance{
			IsUser:     true,
			IsRenter:   true,
			Subscribed: s,
			Balance:    b,
			Locked:     l,
			Currency:   c,
			StripeID:   id,
		}
		ub.Balance += amount
		if ub.Currency == "" {
			ub.Currency = "USD"
		}

		// Update the balances table.
		_, err = tx.Exec(`
			REPLACE INTO mg_balances
			(email, subscribed, sc_balance, sc_locked, currency, stripe_id)
			VALUES (?, ?, ?, ?, ?, ?)
		`, email, ub.Subscribed, ub.Balance, ub.Locked, ub.Currency, ub.StripeID)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't update account balance")
		}

		// Create a new renter if needed.
		var count int
		err = tx.QueryRow("SELECT COUNT(*) FROM ctr_renters WHERE email = ?", email).Scan(&count)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't fetch renters")
		}
		if count == 0 {
			// New renter, need to create a new record.
			seed, err := p.manager.GetWalletSeed()
			defer frand.Read(seed[:])
			if err != nil {
				tx.Rollback()
				return err
			}
			renterSeed := modules.DeriveRenterSeed(seed, email)
			defer frand.Read(renterSeed)
			pk := types.NewPrivateKeyFromSeed(renterSeed).PublicKey()
			if err = p.createNewRenter(email, pk); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	return tx.Commit()
}

// revertSiacoinPayment cancels a payment if the corresponding tx
// is reverted.
func (p *Portal) revertSiacoinPayment(txid types.TransactionID) error {
	_, err := p.db.Exec("DELETE FROM pt_payments WHERE txid = ?", txid[:])
	return err
}

// getPayments retrieves the payments from the account payment
// history.
func (p *Portal) getPayments(email string) ([]userPayment, error) {
	rows, err := p.db.Query(`
		SELECT amount, currency, amount_sc, made_at, conf_left FROM pt_payments
		WHERE email = ?
	`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := make([]userPayment, 0)
	var payment userPayment

	for rows.Next() {
		err := rows.Scan(&payment.Amount, &payment.Currency, &payment.AmountSC, &payment.Timestamp, &payment.ConfirmationsLeft)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}

	return payments, nil
}

// createNewRenter creates a new renter record in the database.
func (p *Portal) createNewRenter(email string, pk types.PublicKey) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	modules.DefaultAllowance.EncodeTo(e)
	e.Flush()
	_, err := p.db.Exec(`
		INSERT INTO ctr_renters
		(email, public_key, current_period, allowance,
		private_key, account_key,
		auto_renew_contracts, backup_file_metadata,
		auto_repair_files)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, email, pk[:], 0, buf.Bytes(), []byte{}, []byte{}, false, false, false)
	if err != nil {
		return err
	}
	p.manager.CreateNewRenter(email, pk)

	return nil
}

// saveNonce updates a user account with the nonce value.
func (p *Portal) saveNonce(email string, nonce []byte) error {
	_, err := p.db.Exec("UPDATE pt_accounts SET nonce = ? WHERE email = ?", nonce, email)
	return err
}

// verifyNonce verifies the nonce value against the user account.
func (p *Portal) verifyNonce(email string, nonce []byte) (bool, error) {
	n := make([]byte, 16)
	err := p.db.QueryRow("SELECT nonce FROM pt_accounts WHERE email = ?", email).Scan(&n)
	if err != nil {
		return false, err
	}

	return bytes.Equal(n, nonce), nil
}

// saveStats updates the authentication stats in the database.
func (p *Portal) saveStats() error {
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
func (p *Portal) loadStats() error {
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
func (p *Portal) saveCredits() error {
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
func (p *Portal) loadCredits() error {
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

// getFiles retrieves the information about the stored file metadata.
func (p *Portal) getFiles(pk types.PublicKey) ([]savedFile, error) {
	rows, err := p.db.Query(`
		SELECT enc_key, filepath, uploaded
		FROM ctr_metadata
		WHERE renter_pk = ?
		ORDER BY uploaded DESC
	`, pk[:])
	if err != nil {
		return nil, modules.AddContext(err, "couldn't retrieve metadata")
	}
	defer rows.Close()

	var sf []savedFile
	for rows.Next() {
		var path string
		var timestamp uint64
		id := make([]byte, 32)
		if err = rows.Scan(&id, &path, &timestamp); err != nil {
			return nil, modules.AddContext(err, "couldn't retrieve object")
		}
		slabRows, err := p.db.Query("SELECT len FROM ctr_slabs WHERE object_id = ?", id)
		if err != nil {
			return nil, modules.AddContext(err, "couldn't retrieve slabs")
		}
		var count int
		var size uint64
		for slabRows.Next() {
			var length uint64
			if err := slabRows.Scan(&length); err != nil {
				slabRows.Close()
				return nil, modules.AddContext(err, "couldn't retrieve slab")
			}
			count++
			size += length
		}
		slabRows.Close()
		sf = append(sf, savedFile{
			Path:     path,
			Size:     size,
			Slabs:    count,
			Uploaded: timestamp,
		})
	}

	return sf, nil
}

// deleteFiles deletes the specified metadata from the database.
func (p *Portal) deleteFiles(pk types.PublicKey, indices []int) error {
	sf, err := p.getFiles(pk)
	if err != nil {
		return modules.AddContext(err, "couldn't retrieve files")
	}

	for _, index := range indices {
		if index >= len(sf) {
			p.log.Printf("ERROR: index %v out of range (%v)\n", index, len(sf))
			continue
		}
		if err := p.manager.DeleteObject(pk, sf[index].Path); err != nil {
			return modules.AddContext(err, "couldn't delete file")
		}
	}

	return nil
}

// getSiacoinAddress returns the SC payment address of an account.
func (p *Portal) getSiacoinAddress(email string) (address types.Address, err error) {
	tx, err := p.db.Begin()
	if err != nil {
		return
	}

	addr := make([]byte, 32)
	err = tx.QueryRow("SELECT sc_address FROM pt_accounts WHERE email = ?", email).Scan(&addr)
	if err != nil {
		tx.Rollback()
		return
	}

	// Generate a new address if there is none yet.
	b := make([]byte, 32)
	if bytes.Equal(addr, b) {
		var uc types.UnlockConditions
		uc, err = p.w.NextAddress()
		if err != nil {
			tx.Rollback()
			return
		}
		address = uc.UnlockHash()
		_, err = tx.Exec("UPDATE pt_accounts SET sc_address = ? WHERE email = ?", address[:], email)
		if err != nil {
			tx.Rollback()
			return
		}
	} else {
		copy(address[:], addr)
	}

	tx.Commit()
	return
}

// getSiacoinAddresses returns a map of SC addresses to the emails.
func (p *Portal) getSiacoinAddresses() (addrs map[types.Address]string, err error) {
	rows, err := p.db.Query("SELECT email, sc_address FROM pt_accounts")
	if err != nil {
		return
	}
	defer rows.Close()

	addrs = make(map[types.Address]string)
	for rows.Next() {
		a := make([]byte, 32)
		var email string
		var addr types.Address
		if err = rows.Scan(&email, &a); err != nil {
			return
		}
		copy(addr[:], a)
		if (addr != types.Address{}) {
			addrs[addr] = email
		}
	}

	return
}

// loadTransactions loads the watch list of the SC payment transactions.
func (p *Portal) loadTransactions() error {
	rows, err := p.db.Query(`
		SELECT txid, sc_address
		FROM pt_payments
		INNER JOIN pt_accounts
		ON pt_payments.email = pt_accounts.email
		WHERE conf_left > 0
	`)
	if err != nil {
		return modules.AddContext(err, "couldn't query transactions")
	}
	defer rows.Close()
	p.mu.Lock()
	defer p.mu.Unlock()

	for rows.Next() {
		t := make([]byte, 32)
		a := make([]byte, 32)
		var txid types.TransactionID
		var addr types.Address
		if err := rows.Scan(&t, &a); err != nil {
			return modules.AddContext(err, "couldn't scan transaction")
		}

		copy(txid[:], t)
		copy(addr[:], a)
		p.transactions[txid] = addr
	}

	return nil
}

// changePaymentPlan switches the user's payment plan between
// 'Pre-Payment' and 'Invoicing'.
// Invoicing allows the balance to become negative. Then, at the
// end of each month, the negative balance is settled using Stripe.
func (p *Portal) changePaymentPlan(email string) error {
	_, err := p.db.Exec(`
		UPDATE mg_balances
		SET subscribed = NOT subscribed
		WHERE email = ?
	`, email)
	return err
}
