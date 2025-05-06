package account

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
)

// requestTemplate contains the text send by email when an invoice
// payment fails.
const requestTemplate = `
	<!-- template.html -->
	<!DOCTYPE html>
	<html>
	<body>
		<h2>Invoice Not Paid</h2>
		<p>There was an issue paying the monthly invoice of <strong>{{.Amount}}</strong>
			on behalf of <strong>{{.Name}}</strong>.</p>
		<p>Please visit your dashboard and make a payment.</p>
		<p>If no payment is received within 24 hours, your account will be put on hold.</p>
	</body>
	</html>
`

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
			sc_address,
			invoice,
			on_hold
		FROM am_accounts
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't query accounts")
	}
	defer rows.Close()

	for rows.Next() {
		var email, currency, stripeID, invoice string
		var createdAt, onHoldSince int64
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
			&invoice,
			&onHoldSince,
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
			invoice: invoice,
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
			acc.address = types.Address(addr)
			am.addresses[types.Address(addr)] = email
		}

		if onHoldSince != 0 {
			acc.onHoldSince = time.Unix(onHoldSince, 0)
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

	var height uint64
	id := make([]byte, 32)
	err = am.db.QueryRow(`
		SELECT height, bid
		FROM am_tip
		WHERE id = 1
	`).Scan(&height, &id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return utils.AddContext(err, "couldn't load tip")
	}
	am.tip.Height = height
	copy(am.tip.ID[:], id)

	return nil
}

// saveTip updates the AccountManager's scanned index.
func (am *AccountManager) saveTip(index types.ChainIndex) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.tip = index

	_, err := am.db.Exec(`
		REPLACE INTO am_tip (id, height, bid)
		VALUES (1, ?, ?)
	`, index.Height, index.ID[:])
	if err != nil {
		return utils.AddContext(err, "couldn't save tip")
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

	// Create a new settings record.
	_, err = am.db.Exec(`
		INSERT INTO am_settings (
			email,
			max_storage_price,
			max_ingress_price,
			max_egress_price,
			max_contract_price,
			max_latency,
			min_upload_speed,
			min_download_speed,
			basis,
			countries,
			contract_count,
			contract_period,
			renew_window,
			ingress,
			egress,
			min_shards,
			total_shards,
			manage_contracts,
			backup_metadata,
			auto_repair
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		email,
		buf.Bytes(),
		buf.Bytes(),
		buf.Bytes(),
		buf.Bytes(),
		0,
		0,
		0,
		"global",
		"",
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		false,
		false,
		false,
	)
	if err != nil {
		return nil, utils.AddContext(err, "couldn't insert settings record")
	}

	return acc, nil
}

// SaveAccount updates the account in the database.
func (am *AccountManager) SaveAccount(acc *Account) error {
	var total, locked bytes.Buffer
	e := types.NewEncoder(&total)
	types.V2Currency(acc.Balance.Total).EncodeTo(e)
	e.Flush()
	e = types.NewEncoder(&locked)
	types.V2Currency(acc.Balance.Locked).EncodeTo(e)
	e.Flush()

	_, err := am.db.Exec(`
		UPDATE am_accounts
		SET
			invoicing = ?,
			sc_total = ?,
			sc_locked = ?,
			negative = ?,
			currency = ?,
			stripe_id = ?,
			invoice = ?
		WHERE email = ?
	`,
		acc.PaymentPlan == PredefinedPaymentPlans[PaymentPlanInvoicing],
		total.Bytes(),
		locked.Bytes(),
		acc.Balance.Negative,
		acc.Currency,
		acc.Email,
		acc.StripeID,
		acc.invoice,
	)
	return err
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
	if (acc.address != types.Address{}) {
		return acc.address, nil
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

	acc.address = address
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

// NewFiatPayment adds a new fiat payment to the account.
func (am *AccountManager) NewFiatPayment(id string, amount float64, currency string) error {
	// Find the account.
	acc := am.findByID(id)
	if acc == nil {
		return ErrUserNotFound
	}

	currency = strings.ToUpper(currency)
	rate := am.GetSiacoinRate(currency)
	if rate == 0 {
		return fmt.Errorf("couldn't calculate SC/%s exchange rate", currency)
	}

	amountSC, err := types.ParseCurrency(fmt.Sprintf("%fSC", amount/rate))
	if err != nil {
		return fmt.Errorf("couldn't parse amount %f: %v", amount/rate, err)
	}

	// Insert payment record.
	_, err = am.db.Exec(`
		INSERT INTO am_payments (email, amount, currency, sc_rate, made_at, conf_left)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		acc.Email,
		amount,
		currency,
		rate,
		time.Now().Unix(),
		0,
	)
	if err != nil {
		return utils.AddContext(err, "couldn't insert payment record")
	}

	// Update account balance.
	if acc.Balance.Negative {
		if acc.Balance.Total.Cmp(amountSC) > 0 {
			acc.Balance.Total = acc.Balance.Total.Sub(amountSC)
		} else {
			acc.Balance.Total = amountSC.Sub(acc.Balance.Total)
			acc.Balance.Negative = false
		}
	} else {
		acc.Balance.Total = acc.Balance.Total.Add(amountSC)
	}

	// Set the new default currency.
	acc.Currency = currency

	// Update the account.
	if err := am.SaveAccount(acc); err != nil {
		return utils.AddContext(err, "couldn't save account")
	}

	return nil
}

// newSiacoinPayment adds a new siacoin payment to the account.
// Note that the balance is not changed at this point.
func (am *AccountManager) newSiacoinPayment(acc *Account, txn types.V2Transaction, timestamp time.Time) error {
	if acc == nil {
		return nil
	}

	var amount types.Currency
	for _, sco := range txn.SiacoinOutputs {
		if sco.Address == acc.address {
			amount = amount.Add(sco.Value)
		}
	}

	// Insert payment record.
	txid := txn.ID()
	_, err := am.db.Exec(`
		INSERT INTO am_payments (email, amount, currency, sc_rate, made_at, conf_left, txid)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		acc.Email,
		amount.Siacoins(),
		"SC",
		1,
		timestamp.Unix(),
		6,
		txid[:],
	)
	if err != nil {
		return utils.AddContext(err, "couldn't insert payment record")
	}

	return nil
}

// confirmSiacoinPayment decrements the required number of confirmations.
func (am *AccountManager) confirmSiacoinPayment(acc *Account, txid types.TransactionID) error {
	if acc == nil {
		return nil
	}

	var count int
	var amount float64
	if err := am.db.QueryRow(`
		SELECT amount, conf_left
		FROM am_payments
		WHERE email = ?
		AND txid = ?
	`, acc.Email, txid[:]).Scan(&amount, &count); err != nil && errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return utils.AddContext(err, "couldn't decode payment record")
	}

	// Decrement the counter. If it becomes zero, update the balance.
	count--
	if count == 0 {
		amountSC, err := types.ParseCurrency(fmt.Sprintf("%fSC", amount))
		if err != nil {
			return utils.AddContext(err, "couldn't parse amount")
		}

		if acc.Balance.Negative {
			if acc.Balance.Total.Cmp(amountSC) > 0 {
				acc.Balance.Total = acc.Balance.Total.Sub(amountSC)
			} else {
				acc.Balance.Total = amountSC.Sub(acc.Balance.Total)
				acc.Balance.Negative = false
			}
		} else {
			acc.Balance.Total = acc.Balance.Total.Add(amountSC)
		}

		// Update the account.
		if err := am.SaveAccount(acc); err != nil {
			return utils.AddContext(err, "couldn't save account")
		}

		// Remove the txn from the watch list.
		delete(am.transactions, txid)
	}

	// Update the payment record.
	_, err := am.db.Exec(`
		UPDATE am_payments
		SET conf_left = ?
		WHERE email = ?
		AND txid = ?
	`, count, acc.Email, txid[:])
	if err != nil {
		return utils.AddContext(err, "couldn't update payment record")
	}

	return nil
}

// unconfirmSiacoinPayment increments the required number of confirmations.
func (am *AccountManager) unconfirmSiacoinPayment(acc *Account, txid types.TransactionID) error {
	if acc == nil {
		return nil
	}

	var count int
	var amount float64
	if err := am.db.QueryRow(`
		SELECT amount, conf_left
		FROM am_payments
		WHERE email = ?
		AND txid = ?
	`, acc.Email, txid[:]).Scan(&amount, &count); err != nil && errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return utils.AddContext(err, "couldn't decode payment record")
	}

	// If count is zero, we have to deduct the balance as well.
	if count == 0 {
		amountSC, err := types.ParseCurrency(fmt.Sprintf("%fSC", amount))
		if err != nil {
			return utils.AddContext(err, "couldn't parse amount")
		}

		if acc.Balance.Negative {
			acc.Balance.Total = acc.Balance.Total.Add(amountSC)
		} else {
			if acc.Balance.Total.Cmp(amountSC) > 0 {
				acc.Balance.Total = acc.Balance.Total.Sub(amountSC)
			} else {
				acc.Balance.Total = amountSC.Sub(acc.Balance.Total)
				acc.Balance.Negative = true
			}
		}

		// Update the account.
		if err := am.SaveAccount(acc); err != nil {
			return utils.AddContext(err, "couldn't save account")
		}

		// Recreate the txn in the watch list.
		am.transactions[txid] = make(map[types.Address]string)
		am.transactions[txid][acc.address] = acc.Email
	}

	// Increment the counter.
	count++
	_, err := am.db.Exec(`
		UPDATE am_payments
		SET conf_left = ?
		WHERE email = ?
		AND txid = ?
	`, count, acc.Email, txid[:])
	if err != nil {
		return utils.AddContext(err, "couldn't update payment record")
	}

	return nil
}

// revertSiacoinPayment removes the payment record from the database.
func (am *AccountManager) revertSiacoinPayment(acc *Account, txid types.TransactionID) error {
	if acc == nil {
		return nil
	}

	_, err := am.db.Exec(`
		DELETE FROM am_payments
		WHERE email = ?
		AND txid = ?
	`, acc.Email, txid[:])
	if err != nil {
		return utils.AddContext(err, "couldn't delete payment record")
	}

	return nil
}

// RequestPayment notifies the user about a failed invoice payment and
// puts a hold on the account.
func (am *AccountManager) RequestPayment(id string, invoice string, amount float64, currency string) (err error) {
	// Get the balance record.
	acc := am.findByID(id)
	if acc == nil {
		return ErrUserNotFound
	}

	// Only send a request if the failed payment comes from a tracked invoice.
	if acc.invoice != invoice {
		return nil
	}

	// Send a payment request.
	type request struct {
		Name   string
		Amount string
	}
	t := template.New("request")
	t, err = t.Parse(requestTemplate)
	if err != nil {
		return utils.AddContext(err, "unable to parse HTML template")
	}

	var b bytes.Buffer
	t.Execute(&b, request{
		Name:   am.serverName,
		Amount: fmt.Sprintf("%.2f %s", amount, currency),
	})
	err = am.mail.SendMail("Sia Satellite", acc.Email, "Action Required", &b)
	if err != nil {
		return fmt.Errorf("unable to send request to %s", acc.Email)
	}

	// Place a temporary hold on the account.
	_, err = am.db.Exec(`
		UPDATE am_accounts
		SET on_hold = ?
		WHERE stripe_id = ?
	`, uint64(time.Now().Unix()), id)
	if err != nil {
		return fmt.Errorf("unable to put a hold on the account: %s, %v", acc.Email, err)
	}

	return nil
}
