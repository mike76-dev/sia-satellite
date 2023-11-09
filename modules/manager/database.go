package manager

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/rs/xid"

	"go.sia.tech/core/types"
)

// dbGetBlockTimestamps retrieves the block timestamps from the database.
func dbGetBlockTimestamps(tx *sql.Tx) (curr blockTimestamp, prev blockTimestamp, err error) {
	var height, timestamp uint64
	err = tx.QueryRow("SELECT height, time FROM mg_timestamp WHERE id = 1").Scan(&height, &timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return blockTimestamp{}, blockTimestamp{}, nil
	}
	if err != nil {
		return blockTimestamp{}, blockTimestamp{}, err
	}
	curr.BlockHeight = height
	curr.Timestamp = time.Unix(int64(timestamp), 0)

	err = tx.QueryRow("SELECT height, time FROM mg_timestamp WHERE id = 2").Scan(&height, &timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return curr, blockTimestamp{}, nil
	}
	if err != nil {
		return curr, blockTimestamp{}, err
	}
	prev.BlockHeight = height
	prev.Timestamp = time.Unix(int64(timestamp), 0)

	return
}

// dbPutBlockTimestamps saves the block timestamps in the database.
func dbPutBlockTimestamps(tx *sql.Tx, curr blockTimestamp, prev blockTimestamp) error {
	var ct, pt uint64
	if curr.BlockHeight > 0 {
		ct = uint64(curr.Timestamp.Unix())
	}
	_, err := tx.Exec(`
		REPLACE INTO mg_timestamp (id, height, time)
		VALUES (1, ?, ?)
	`, curr.BlockHeight, ct)
	if err != nil {
		return err
	}

	if prev.BlockHeight > 0 {
		pt = uint64(prev.Timestamp.Unix())
	}
	_, err = tx.Exec(`
		REPLACE INTO mg_timestamp (id, height, time)
		VALUES (2, ?, ?)
	`, prev.BlockHeight, pt)

	return err
}

// dbGetAverages retrieves the host network averages from the database.
func dbGetAverages(tx *sql.Tx) (avg modules.HostAverages, err error) {
	var avgBytes []byte
	err = tx.QueryRow("SELECT bytes FROM mg_averages WHERE id = 1").Scan(&avgBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return
	}

	d := types.NewDecoder(io.LimitedReader{R: bytes.NewBuffer(avgBytes), N: int64(len(avgBytes))})
	avg.DecodeFrom(d)
	err = d.Err()

	return
}

// dbPutAverages retrieves the host network averages from the database.
func dbPutAverages(tx *sql.Tx, avg modules.HostAverages) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	avg.EncodeTo(e)
	e.Flush()

	_, err := tx.Exec("REPLACE INTO mg_averages (id, bytes) VALUES (1, ?)", buf.Bytes())

	return err
}

// GetBalance retrieves the balance information on the account.
// An empty struct is returned when there is no data.
func (m *Manager) GetBalance(email string) (modules.UserBalance, error) {
	var sub bool
	var b, l float64
	var c, id, in string
	var oh uint64
	err := m.db.QueryRow(`
		SELECT subscribed, sc_balance, sc_locked, currency, stripe_id, invoice, on_hold
		FROM mg_balances WHERE email = ?
	`, email).Scan(&sub, &b, &l, &c, &id, &in, &oh)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modules.UserBalance{}, err
	}

	// New user: set USD as the default currency.
	if errors.Is(err, sql.ErrNoRows) {
		c = "USD"
	}

	scRate, scErr := m.GetSiacoinRate(c)
	if scErr != nil {
		return modules.UserBalance{}, scErr
	}

	renters := m.Renters()
	var found bool
	for _, renter := range renters {
		if renter.Email == email {
			found = true
			break
		}
	}

	ub := modules.UserBalance{
		IsUser:     !errors.Is(err, sql.ErrNoRows),
		IsRenter:   found,
		Subscribed: sub,
		Balance:    b,
		Locked:     l,
		Currency:   c,
		StripeID:   id,
		SCRate:     scRate,
		Invoice:    in,
		OnHold:     oh,
	}

	return ub, nil
}

// UpdateBalance updates the balance information on the account.
func (m *Manager) UpdateBalance(email string, ub modules.UserBalance) error {
	_, err := m.db.Exec(`
		REPLACE INTO mg_balances
		(email, subscribed, sc_balance, sc_locked, currency, stripe_id, invoice, on_hold)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, email, ub.Subscribed, ub.Balance, ub.Locked, ub.Currency, ub.StripeID, ub.Invoice, ub.OnHold)

	return err
}

// GetSpendings retrieves the user's spendings.
func (m *Manager) GetSpendings(email string) (modules.UserSpendings, error) {
	var currLocked, currUsed, currOverhead float64
	var prevLocked, prevUsed, prevOverhead float64
	var currFormed, currRenewed, prevFormed, prevRenewed uint64
	var currSlabsSaved, currSlabsRetrieved, currSlabsMigrated uint64
	var prevSlabsSaved, prevSlabsRetrieved, prevSlabsMigrated uint64

	err := m.db.QueryRow(`
		SELECT current_locked, current_used, current_overhead,
			prev_locked, prev_used, prev_overhead,
			current_formed, current_renewed,
			current_slabs_saved, current_slabs_retrieved, current_slabs_migrated,
			prev_formed, prev_renewed,
			prev_slabs_saved, prev_slabs_retrieved, prev_slabs_migrated
		FROM mg_spendings
		WHERE email = ?`, email).Scan(&currLocked, &currUsed, &currOverhead, &prevLocked, &prevUsed, &prevOverhead, &currFormed, &currRenewed, &currSlabsSaved, &currSlabsRetrieved, &currSlabsMigrated, &prevFormed, &prevRenewed, &prevSlabsSaved, &prevSlabsRetrieved, &prevSlabsMigrated)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modules.UserSpendings{}, err
	}

	us := modules.UserSpendings{
		CurrentLocked:         currLocked,
		CurrentUsed:           currUsed,
		CurrentOverhead:       currOverhead,
		PrevLocked:            prevLocked,
		PrevUsed:              prevUsed,
		PrevOverhead:          prevOverhead,
		CurrentFormed:         currFormed,
		CurrentRenewed:        currRenewed,
		CurrentSlabsSaved:     currSlabsSaved,
		CurrentSlabsRetrieved: currSlabsRetrieved,
		CurrentSlabsMigrated:  currSlabsMigrated,
		PrevFormed:            prevFormed,
		PrevRenewed:           prevRenewed,
		PrevSlabsSaved:        prevSlabsSaved,
		PrevSlabsRetrieved:    prevSlabsRetrieved,
		PrevSlabsMigrated:     prevSlabsMigrated,
	}

	return us, nil
}

// UpdateSpendings updates the user's spendings.
func (m *Manager) UpdateSpendings(email string, us modules.UserSpendings) error {
	_, err := m.db.Exec(`
		REPLACE INTO mg_spendings
		(email, current_locked, current_used, current_overhead,
		prev_locked, prev_used, prev_overhead,
		current_formed, current_renewed,
		current_slabs_saved, current_slabs_retrieved, current_slabs_migrated,
		prev_formed, prev_renewed,
		prev_slabs_saved, prev_slabs_retrieved, prev_slabs_migrated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, email, us.CurrentLocked, us.CurrentUsed, us.CurrentOverhead, us.PrevLocked, us.PrevUsed, us.PrevOverhead, us.CurrentFormed, us.CurrentRenewed, us.CurrentSlabsSaved, us.CurrentSlabsRetrieved, us.CurrentSlabsMigrated, us.PrevFormed, us.PrevRenewed, us.PrevSlabsSaved, us.PrevSlabsRetrieved, us.PrevSlabsMigrated)

	return err
}

// IncrementStats increments the number of formed or renewed contracts.
func (m *Manager) IncrementStats(email string, renewed bool) (err error) {
	if renewed {
		_, err = m.db.Exec(`
			UPDATE mg_spendings
			SET current_renewed = current_renewed + 1
			WHERE email = ?
		`, email)
	} else {
		_, err = m.db.Exec(`
			UPDATE mg_spendings
			SET current_formed = current_formed + 1
			WHERE email = ?
		`, email)
	}
	return
}

// numSlabs returns the count of file slab metadata objects stored for
// the specified renter.
func (m *Manager) numSlabs(pk types.PublicKey) (count int, partial uint64, err error) {
	err = m.db.QueryRow(`
		SELECT COUNT(*)
		FROM ctr_slabs
		WHERE renter_pk = ?
	`, pk[:]).Scan(&count)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, modules.AddContext(err, "couldn't fetch slab count")
	}

	err = m.db.QueryRow(`
		SELECT SUM(len)
		FROM ctr_buffers
		WHERE renterpk = ?
	`, pk[:]).Scan(&partial)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, modules.AddContext(err, "couldn't fetch data length")
	}

	return
}

// getEmailPreferences retrieves the email preferences.
func (m *Manager) getEmailPreferences() error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	var email string
	b := make([]byte, 0, 24)
	err = tx.QueryRow(`
		SELECT email, threshold
		FROM mg_email
		WHERE id = 1
	`).Scan(&email, &b)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec(`
			INSERT INTO mg_email (email, threshold, time_sent)
			VALUES (?, ?, ?)
		`, "", "", 0)
		if err != nil {
			tx.Rollback()
			return err
		} else {
			return tx.Commit()
		}
	}
	if err != nil {
		tx.Rollback()
		return err
	}

	buf := bytes.NewBuffer(b)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: 24})
	var threshold types.Currency
	threshold.DecodeFrom(d)
	m.email = email
	m.warnThreshold = threshold
	tx.Commit()
	return nil
}

// setEmailPreferences changes the email preferences.
func (m *Manager) setEmailPreferences(email string, threshold types.Currency) error {
	m.email = email
	m.warnThreshold = threshold

	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	threshold.EncodeTo(e)
	e.Flush()
	_, err := m.db.Exec(`
		UPDATE mg_email
		SET email = ?, threshold = ?, time_sent = ?
		WHERE id = 1
	`, email, buf.Bytes(), 0)

	return err
}

// warningTemplate contains the text send by email when the wallet
// balance falls below the set threshold.
const warningTemplate = `
	<!-- template.html -->
	<!DOCTYPE html>
	<html>
	<body>
   	<h2>Please Check Your Wallet Balance</h2>
    <p>The wallet balance of <strong>{{.Name}}</strong> is <strong>{{.Balance}}</strong>,
	which is below the threshold of <strong>{{.Threshold}}</strong>.</p>
	</body>
	</html>
`

// sendWarning sends a warning email to the satellite operator.
func (m *Manager) sendWarning() {
	// Check if the email is set.
	if m.email == "" {
		return
	}

	// Check the wallet balance.
	balance, _, _, err := m.wallet.ConfirmedBalance()
	if err != nil {
		m.log.Println("ERROR: couldn't retrieve wallet balance:", err)
		return
	}
	if balance.Cmp(m.warnThreshold) >= 0 {
		return
	}

	// Balance is low; check if a warning has been sent today.
	tx, err := m.db.Begin()
	if err != nil {
		m.log.Println("ERROR: couldn't start transaction:", err)
		return
	}
	var timestamp uint64
	err = tx.QueryRow("SELECT time_sent FROM mg_email WHERE id = 1").Scan(&timestamp)
	if err != nil {
		m.log.Println("ERROR: couldn't retrieve timestamp:", err)
		tx.Rollback()
		return
	}
	daySent := time.Unix(int64(timestamp), 0).Day()
	dayNow := time.Now().Day()
	if timestamp > 0 && daySent == dayNow {
		tx.Rollback()
		return
	}

	// Send a warning.
	type warning struct {
		Name      string
		Balance   types.Currency
		Threshold types.Currency
	}
	t := template.New("warning")
	t, err = t.Parse(warningTemplate)
	if err != nil {
		m.log.Printf("ERROR: unable to parse HTML template: %v\n", err)
		tx.Rollback()
		return
	}
	var b bytes.Buffer
	t.Execute(&b, warning{
		Name:      m.name,
		Balance:   balance,
		Threshold: m.warnThreshold,
	})
	err = m.ms.SendMail("Sia Satellite", m.email, "Warning: Balance Low", &b)
	if err != nil {
		m.log.Println("ERROR: unable to send a warning:", err)
		tx.Rollback()
		return
	}

	// Update the database.
	_, err = tx.Exec(`
		UPDATE mg_email
		SET time_sent = ?
		WHERE id = 1
	`, time.Now().Unix())
	if err != nil {
		m.log.Println("ERROR: couldn't update database:", err)
		tx.Rollback()
		return
	}

	if err := tx.Commit(); err != nil {
		m.log.Println("ERROR: couldn't commit the changes:", err)
	}
}

// putInvoice puts the specified invoice ID in the balance record.
func (m *Manager) putInvoice(id string, invoice string) error {
	_, err := m.db.Exec("UPDATE mg_balances SET invoice = ? WHERE stripe_id = ?", invoice, id)
	return err
}

// UpdatePrices updates the pricing table in the database.
func (m *Manager) UpdatePrices(prices modules.Pricing) error {
	_, err := m.db.Exec(`
		REPLACE INTO mg_prices (
			id,
			form_contract_prepayment,
			form_contract_invoicing,
			save_metadata_prepayment,
			save_metadata_invoicing,
			store_metadata_prepayment,
			store_metadata_invoicing,
			store_partial_prepayment,
			store_partial_invoicing,
			retrieve_metadata_prepayment,
			retrieve_metadata_invoicing,
			migrate_slab_prepayment,
			migrate_slab_invoicing
		)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		prices.FormContract.PrePayment,
		prices.FormContract.Invoicing,
		prices.SaveMetadata.PrePayment,
		prices.SaveMetadata.Invoicing,
		prices.StoreMetadata.PrePayment,
		prices.StoreMetadata.Invoicing,
		prices.StorePartialData.PrePayment,
		prices.StorePartialData.Invoicing,
		prices.RetrieveMetadata.PrePayment,
		prices.RetrieveMetadata.Invoicing,
		prices.MigrateSlab.PrePayment,
		prices.MigrateSlab.Invoicing,
	)

	if err == nil {
		modules.StaticPricing = prices
	}

	return err
}

// loadPrices loads the prices from the database. If no prices are
// stored yet, default prices are taken.
func (m *Manager) loadPrices() error {
	err := m.db.QueryRow(`
		SELECT
			form_contract_prepayment,
			form_contract_invoicing,
			save_metadata_prepayment,
			save_metadata_invoicing,
			store_metadata_prepayment,
			store_metadata_invoicing,
			store_partial_prepayment,
			store_partial_invoicing,
			retrieve_metadata_prepayment,
			retrieve_metadata_invoicing,
			migrate_slab_prepayment,
			migrate_slab_invoicing
		FROM mg_prices
		WHERE id = 1
	`).Scan(
		&modules.StaticPricing.FormContract.PrePayment,
		&modules.StaticPricing.FormContract.Invoicing,
		&modules.StaticPricing.SaveMetadata.PrePayment,
		&modules.StaticPricing.SaveMetadata.Invoicing,
		&modules.StaticPricing.StoreMetadata.PrePayment,
		&modules.StaticPricing.StoreMetadata.Invoicing,
		&modules.StaticPricing.StorePartialData.PrePayment,
		&modules.StaticPricing.StorePartialData.Invoicing,
		&modules.StaticPricing.RetrieveMetadata.PrePayment,
		&modules.StaticPricing.RetrieveMetadata.Invoicing,
		&modules.StaticPricing.MigrateSlab.PrePayment,
		&modules.StaticPricing.MigrateSlab.Invoicing,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if err != nil {
		modules.StaticPricing = modules.DefaultPricing
		return m.UpdatePrices(modules.DefaultPricing)
	}

	return nil
}

// StartMaintenance switches the maintenance mode on and off.
func (m *Manager) StartMaintenance(start bool) error {
	_, err := m.db.Exec(`
		REPLACE INTO mg_maintenance (id, maintenance)
		VALUES (1, ?)
	`, start)
	if err != nil {
		return modules.AddContext(err, "couldn't set maintenance flag")
	}
	m.mu.Lock()
	m.maintenance = start
	m.mu.Unlock()
	return nil
}

// loadMaintenance loads the maintenance flag from the database.
func (m *Manager) loadMaintenance() error {
	var maintenance bool
	err := m.db.QueryRow(`
		SELECT maintenance
		FROM mg_maintenance
		WHERE id = 1
	`).Scan(&maintenance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.maintenance = maintenance
	m.mu.Unlock()
	return nil
}

// BytesUploaded returns the size of the file already uploaded.
func (m *Manager) BytesUploaded(pk types.PublicKey, bucket, path string) (string, uint64, error) {
	var name string
	err := m.db.QueryRow(`
		SELECT filename
		FROM ctr_uploads
		WHERE renter_pk = ?
		AND bucket = ?
		AND filepath = ?
	`, pk[:], bucket, path).Scan(&name)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		name = xid.New().String()
		return filepath.Join(filepath.Join(m.dir, bufferedFilesDir), name), 0, nil
	}
	if err != nil {
		return "", 0, err
	}

	p := filepath.Join(filepath.Join(m.dir, bufferedFilesDir), name)
	fi, err := os.Stat(p)
	if err != nil {
		return "", 0, err
	}

	return p, uint64(fi.Size()), nil
}

// RegisterUpload associates the uploaded file with the object.
func (m *Manager) RegisterUpload(pk types.PublicKey, bucket, path, filename string, complete bool) error {
	_, err := m.db.Exec(`
		REPLACE INTO ctr_uploads
			(filename, bucket, filepath, renter_pk, ready)
		VALUES (?, ?, ?, ?, ?)
	`, filepath.Base(filename), bucket, path, pk[:], complete)
	return err
}

// DeleteBufferedFiles deletes the files waiting to be uploaded.
func (m *Manager) DeleteBufferedFiles(pk types.PublicKey) error {
	// Make a list of file names.
	rows, err := m.db.Query("SELECT filename FROM ctr_uploads WHERE renter_pk = ?", pk[:])
	if err != nil {
		m.log.Println("ERROR: unable to query files:", err)
		return modules.AddContext(err, "unable to query files")
	}

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			m.log.Println("ERROR: unable to retrieve filename:", err)
			return modules.AddContext(err, "unable to retrieve filename")
		}
		names = append(names, name)
	}
	rows.Close()

	// Delete the files one by one.
	for _, name := range names {
		if err := os.Remove(filepath.Join(m.BufferedFilesDir(), name)); err != nil {
			m.log.Println("ERROR: unable to delete file:", err)
			return modules.AddContext(err, "unable to delete file")
		}
		_, err = m.db.Exec("DELETE FROM ctr_uploads WHERE filename = ?", name)
		if err != nil {
			m.log.Println("ERROR: unable to delete file record:", err)
			return modules.AddContext(err, "unable to delete file record")
		}
	}

	return nil
}

// DeleteBufferedFile deletes the specified file and the associated
// database record.
func (m *Manager) DeleteBufferedFile(pk types.PublicKey, bucket, path string) error {
	var name string
	err := m.db.QueryRow(`
		SELECT filename
		FROM ctr_uploads
		WHERE bucket = ?
		AND filepath = ?
		AND renter_pk = ?
	`, bucket, path, pk[:]).Scan(&name)
	if err != nil {
		return modules.AddContext(err, "couldn't query buffered files")
	}

	if err := os.Remove(filepath.Join(m.BufferedFilesDir(), name)); err != nil {
		return modules.AddContext(err, "unable to delete file")
	}

	_, err = m.db.Exec("DELETE FROM ctr_uploads WHERE filename = ?", name)
	if err != nil {
		return modules.AddContext(err, "couldn't delete record")
	}

	return nil
}
