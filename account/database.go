package account

import (
	"bytes"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
)

// load loads the account manager from the database.
func (am *AccountManager) load() error {
	rows, err := am.db.Query(`
		SELECT
			email,
			created_at,
			verified,
			invoicing,
			sc_total,
			sc_locked,
			negative,
			currency,
			stripe_id,
			sc_address
		FROM am_accounts
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't query accounts")
	}
	defer rows.Close()

	for rows.Next() {
		var email, currency, stripeID string
		var createdAt int64
		var verified, negative bool
		var invoicing byte
		var total, locked, addr []byte
		if err := rows.Scan(
			&email,
			&createdAt,
			&verified,
			&invoicing,
			&total,
			&locked,
			&negative,
			&currency,
			&stripeID,
			&addr,
		); err != nil {
			return utils.AddContext(err, "couldn't decode account")
		}

		acc := &Account{
			Email:       email,
			CreatedAt:   time.Unix(createdAt, 0),
			Verified:    verified,
			PaymentPlan: PredefinedPaymentPlans[invoicing],
			Currency:    currency,
			StripeID:    stripeID,
			Balance: Balance{
				Negative: negative,
			},
		}

		td := types.NewBufDecoder(total)
		(*types.V2Currency)(&acc.Balance.Total).DecodeFrom(td)
		if err := td.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode total balance")
		}

		ld := types.NewBufDecoder(locked)
		(*types.V2Currency)(&acc.Balance.Locked).DecodeFrom(ld)
		if err := ld.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode locked balance")
		}

		am.accounts[email] = acc
		if addr != nil {
			am.addresses[types.Address(addr)] = email
		}
	}

	sk := make([]byte, 64)
	if err := am.db.QueryRow("SELECT private_key FROM am_info WHERE id = 1").Scan(&sk); err != nil && errors.Is(err, sql.ErrNoRows) {
		am.key = utils.NewPrivateKey()
		_, err = am.db.Exec("INSERT INTO am_info (id, private_key) VALUES (1, ?)", am.key[:])
		if err != nil {
			return utils.AddContext(err, "couldn't save key")
		}
	} else if err != nil {
		return utils.AddContext(err, "couldn't read key")
	} else {
		am.key = sk
	}

	return nil
}

// readNonce reads the 16-byte nonce from the database.
func (am *AccountManager) readNonce(email string) ([]byte, error) {
	n := make([]byte, 16)
	err := am.db.QueryRow(`
		SELECT nonce
		FROM am_accounts
		WHERE email = ?
	`, email).Scan(&n)
	if err != nil {
		return nil, err
	}

	return n, nil
}

// saveNonce updates the user account record with the 16-byte nonce.
func (am *AccountManager) saveNonce(email string, nonce []byte) error {
	_, err := am.db.Exec(`
		UPDATE am_accounts
		SET nonce = ?
		WHERE email = ?
	`, nonce, email)
	return err
}

// verifyNonce verifies the nonce value against the user account.
func (am *AccountManager) verifyNonce(email string, nonce []byte) (bool, error) {
	n := make([]byte, 16)
	err := am.db.QueryRow(`
		SELECT nonce
		FROM am_accounts
		WHERE email = ?
	`, email).Scan(&n)
	if err != nil {
		return false, err
	}

	return bytes.Equal(n, nonce), nil
}

// VerifyPassword checks if the provided password is correct.
func (am *AccountManager) VerifyPassword(email, password string) error {
	var pwHash []byte
	if password != "" {
		pwHash = passwordHash(password)
	}

	ph := make([]byte, 32)
	if err := am.db.QueryRow(`
		SELECT password_hash
		FROM am_accounts
		WHERE email = ?
	`, email).Scan(&ph); err != nil && errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return utils.AddContext(err, "couldn't read password hash")
	}

	if bytes.Equal(ph, pwHash) {
		return nil
	} else {
		return ErrWrongPassword
	}
}

// ChangePassword changes the current password.
func (am *AccountManager) ChangePassword(email, password string) error {
	// Handle edge case.
	if password == "" {
		return errors.New("password cannot be empty")
	}

	pwh := passwordHash(password)
	_, err := am.db.Exec(`
		UPDATE am_accounts
		SET password_hash = ?
		WHERE email = ?
	`, pwh, email)

	return utils.AddContext(err, "couldn't change password")
}

// NewAccount creates a new account.
func (am *AccountManager) NewAccount(email, password string) (*Account, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	acc := &Account{
		Email:       email,
		CreatedAt:   time.Now(),
		Currency:    "USD",
		PaymentPlan: PredefinedPaymentPlans[PaymentPlanPrePayment],
	}

	am.accounts[email] = acc
	var pwh []byte
	if password != "" {
		pwh = passwordHash(password)
	}

	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	types.V2Currency(types.ZeroCurrency).EncodeTo(e)
	e.Flush()

	_, err := am.db.Exec(`
		INSERT INTO am_accounts (
			email,
			password_hash,
			created_at,
			verified,
			invoicing,
			sc_total,
			sc_locked,
			negative,
			currency,
			stripe_id,
			invoice,
			on_hold,
			nonce,
			sc_address
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		email,
		pwh,
		acc.CreatedAt.Unix(),
		false,
		false,
		buf.Bytes(),
		buf.Bytes(),
		false,
		acc.Currency,
		"",
		"",
		0,
		[]byte{},
		[]byte{},
	)
	if err != nil {
		return nil, utils.AddContext(err, "couldn't insert account")
	}

	return acc, nil
}

// SetVerified sets the Verified flag of the account to true.
func (am *AccountManager) SetVerified(acc *Account) error {
	acc.Verified = true
	_, err := am.db.Exec(`
		UPDATE am_accounts
		SET verified = TRUE
		WHERE email = ?
	`, acc.Email)
	if err != nil {
		return utils.AddContext(err, "couldn't mark account verified")
	}

	return nil
}

// GetAddress returns the Siacoin address of the account.
// If there is no address yet, it is generated.
func (am *AccountManager) GetAddress(acc *Account) (types.Address, error) {
	var addr []byte
	err := am.db.QueryRow(`
		SELECT sc_address
		FROM am_accounts
		WHERE email = ?
	`, acc.Email).Scan(&addr)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return types.Address{}, ErrUserNotFound
	} else if err != nil {
		return types.Address{}, utils.AddContext(err, "couldn't query address")
	}

	if addr != nil {
		return types.Address(addr), nil
	}

	// Generate a new address.
	address, err := am.wallet.NextAddress()
	if err != nil {
		return types.Address{}, utils.AddContext(err, "couldn't generate address")
	}

	// Update the database.
	_, err = am.db.Exec(`
		UPDATE am_accounts
		SET sc_address = ?
		WHERE email = ?
	`, address[:], acc.Email)
	if err != nil {
		return types.Address{}, utils.AddContext(err, "couldn't insert address")
	}

	am.mu.Lock()
	am.addresses[address] = acc.Email
	am.mu.Unlock()

	return address, nil
}

// GetPayments returns a list of payments made to the account.
func (am *AccountManager) GetPayments(acc *Account, offset, limit int) (payments []Payment, err error) {
	if limit < 0 {
		limit = math.MaxInt
	}

	rows, err := am.db.Query(`
		SELECT amount, currency, sc_rate, made_at, conf_left, txid
		FROM am_payments
		WHERE email = ?
		ORDER BY made_at DESC
		LIMIT ?, ?
	`, acc.Email, offset, limit)
	if err != nil {
		return nil, utils.AddContext(err, "couldn't query payments")
	}
	defer rows.Close()

	for rows.Next() {
		var amount, scRate float64
		var currency string
		var timestamp int64
		var confirmations int
		var txid []byte
		if err := rows.Scan(
			&amount,
			&currency,
			&scRate,
			&timestamp,
			&confirmations,
			&txid,
		); err != nil {
			return nil, utils.AddContext(err, "couldn't decode payment")
		}

		p := Payment{
			Amount:            amount,
			Currency:          currency,
			SCRate:            scRate,
			Timestamp:         time.Unix(timestamp, 0),
			ConfirmationsLeft: confirmations,
		}

		if txid != nil {
			p.TransactionID = types.TransactionID(txid)
		}

		payments = append(payments, p)
	}

	return
}
