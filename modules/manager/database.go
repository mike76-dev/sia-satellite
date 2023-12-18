package manager

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/rs/xid"
	"lukechampine.com/frand"

	"go.sia.tech/core/types"
)

const (
	// multipartUploadPruneInterval determines how often incomplete
	// multipart uploads are pruned.
	multipartUploadPruneInterval = 30 * time.Minute

	// multipartUploadPruneThreshold determines how old an incomplete
	// multipart upload has to be to get pruned.
	multipartUploadPruneThreshold = 24 * time.Hour
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
func (m *Manager) GetSpendings(email string, month, year int) (us modules.UserSpendings, err error) {
	period := fmt.Sprintf("%02d%04d", month, year)
	err = m.db.QueryRow(`
		SELECT locked, used, overhead,
			formed, renewed, slabs_saved,
			slabs_retrieved, slabs_migrated
		FROM mg_spendings
		WHERE email = ?
		AND period = ?`, email, period,
	).Scan(
		&us.Locked,
		&us.Used,
		&us.Overhead,
		&us.Formed,
		&us.Renewed,
		&us.SlabsSaved,
		&us.SlabsRetrieved,
		&us.SlabsMigrated,
	)

	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}

	return
}

// UpdateSpendings updates the user's spendings.
func (m *Manager) UpdateSpendings(email string, us modules.UserSpendings, month, year int) error {
	period := fmt.Sprintf("%02d%04d", month, year)
	_, err := m.db.Exec(`
		INSERT INTO mg_spendings
		(email, period, locked, used, overhead,
		formed, renewed, slabs_saved,
		slabs_retrieved, slabs_migrated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) AS new
		ON DUPLICATE KEY UPDATE
			locked = new.locked,
			used = new.used,
			overhead = new.overhead,
			formed = new.formed,
			renewed = new.renewed,
			slabs_saved = new.slabs_saved,
			slabs_retrieved = new.slabs_retrieved,
			slabs_migrated = new.slabs_migrated
	`,
		email,
		period,
		us.Locked,
		us.Used,
		us.Overhead,
		us.Formed,
		us.Renewed,
		us.SlabsSaved,
		us.SlabsRetrieved,
		us.SlabsMigrated,
	)

	return err
}

// IncrementStats increments the number of formed or renewed contracts.
func (m *Manager) IncrementStats(email string, renewed bool) (err error) {
	year, month, _ := time.Now().Date()
	period := fmt.Sprintf("%02d%04d", int(month), year)
	if renewed {
		_, err = m.db.Exec(`
			UPDATE mg_spendings
			SET renewed = renewed + 1
			WHERE email = ?
			AND period = ?
		`, email, period)
	} else {
		_, err = m.db.Exec(`
			UPDATE mg_spendings
			SET formed = formed + 1
			WHERE email = ?
			AND period = ?
		`, email, period)
	}
	return
}

// deleteOldSpendings deletes all spendings records older than two months.
func (m *Manager) deleteOldSpendings() error {
	var curr, prev string
	year, month, _ := time.Now().Date()
	curr = fmt.Sprintf("%02d%04d", int(month), year)
	if int(month) > 1 {
		prev = fmt.Sprintf("%02d%04d", int(month)-1, year)
	} else {
		prev = fmt.Sprintf("%02d%04d", 12, year-1)
	}
	_, err := m.db.Exec(`
		DELETE FROM mg_spendings
		WHERE period <> ?
		AND period <> ?
	`, curr, prev)
	return err
}

// numSlabs returns the count of file slab metadata objects stored for
// the specified renter.
func (m *Manager) numSlabs(pk types.PublicKey) (count int, partial uint64, err error) {
	err = m.db.QueryRow(`
		SELECT COUNT(*)
		FROM ctr_slabs
		WHERE renter_pk = ?
		AND orphan = FALSE
	`, pk[:]).Scan(&count)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, modules.AddContext(err, "couldn't fetch slab count")
	}

	rows, err := m.db.Query(`
		SELECT len
		FROM ctr_slabs
		WHERE renter_pk = ?
		AND partial = TRUE
		AND orphan = FALSE
	`, pk[:])
	if err != nil {
		return 0, 0, modules.AddContext(err, "couldn't query data length")
	}
	defer rows.Close()

	for rows.Next() {
		var data uint64
		if err := rows.Scan(&data); err != nil {
			return 0, 0, modules.AddContext(err, "couldn't fetch data length")
		}
		partial += data
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
func (m *Manager) BytesUploaded(pk types.PublicKey, bucket, path []byte) (string, uint64, error) {
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
func (m *Manager) RegisterUpload(pk types.PublicKey, bucket, path, mimeType []byte, encrypted bool, filename string, complete bool) error {
	fi, err := os.Stat(filename)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.bufferSize += uint64(fi.Size())
	m.mu.Unlock()

	var encText string
	if encrypted {
		encText = fmt.Sprintf("%d", fi.Size())
	}
	_, err = m.db.Exec(`
		REPLACE INTO ctr_uploads
			(filename, bucket, filepath, mime, renter_pk, ready, encrypted)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		filepath.Base(filename),
		bucket,
		path,
		mimeType,
		pk[:],
		complete,
		encText,
	)
	if err != nil {
		return err
	}

	// Initiate the upload.
	go m.hostContractor.StartUploading()

	return nil
}

// GetBufferSize returns the total size of the temporary files.
func (m *Manager) GetBufferSize() (total uint64, err error) {
	rows, err := m.db.Query("SELECT filename FROM ctr_uploads UNION SELECT filename FROM ctr_parts")
	if err != nil {
		return 0, modules.AddContext(err, "unable to query files")
	}

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, modules.AddContext(err, "unable to retrieve filename")
		}
		names = append(names, name)
	}
	rows.Close()

	for _, name := range names {
		fi, err := os.Stat(filepath.Join(m.BufferedFilesDir(), name))
		if err != nil {
			return 0, modules.AddContext(err, "unable to get file size")
		}
		total += uint64(fi.Size())
	}

	return
}

// DeleteBufferedFiles deletes the files waiting to be uploaded.
func (m *Manager) DeleteBufferedFiles(pk types.PublicKey) error {
	// Make a list of file names.
	rows, err := m.db.Query(`
		SELECT filename, bucket, filepath
		FROM ctr_uploads
		WHERE renter_pk = ?
	`, pk[:])
	if err != nil {
		m.log.Println("ERROR: unable to query files:", err)
		return modules.AddContext(err, "unable to query files")
	}

	// Make a list of filenames and also cancel any running uploads.
	var names []string
	for rows.Next() {
		var name string
		var bucket, path []byte
		if err := rows.Scan(&name, &bucket, &path); err != nil {
			rows.Close()
			m.log.Println("ERROR: unable to retrieve filename:", err)
			return modules.AddContext(err, "unable to retrieve filename")
		}
		m.hostContractor.CancelUpload(pk, bucket, path)
		names = append(names, name)
	}
	rows.Close()

	// Delete the files one by one.
	for _, name := range names {
		p := filepath.Join(m.BufferedFilesDir(), name)
		fi, err := os.Stat(p)
		if err != nil {
			m.log.Println("ERROR: unable to get file size:", err)
			return modules.AddContext(err, "unable to get file size")
		}
		if err := os.Remove(p); err != nil {
			m.log.Println("ERROR: unable to delete file:", err)
			return modules.AddContext(err, "unable to delete file")
		}
		m.mu.Lock()
		m.bufferSize -= uint64(fi.Size())
		m.mu.Unlock()
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
func (m *Manager) DeleteBufferedFile(pk types.PublicKey, bucket, path []byte) error {
	// Cancel the upload.
	m.hostContractor.CancelUpload(pk, bucket, path)

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

	p := filepath.Join(m.BufferedFilesDir(), name)
	fi, err := os.Stat(p)
	if err != nil {
		return modules.AddContext(err, "unable to get file size")
	}
	if err := os.Remove(filepath.Join(m.BufferedFilesDir(), name)); err != nil {
		return modules.AddContext(err, "unable to delete file")
	}
	m.mu.Lock()
	m.bufferSize -= uint64(fi.Size())
	m.mu.Unlock()

	_, err = m.db.Exec("DELETE FROM ctr_uploads WHERE filename = ?", name)
	if err != nil {
		return modules.AddContext(err, "couldn't delete record")
	}

	return nil
}

// DeleteMultipartUploads deletes the unfinished multipart uploads.
func (m *Manager) DeleteMultipartUploads(pk types.PublicKey) error {
	// Make a list of file names.
	rows, err := m.db.Query("SELECT filename FROM ctr_parts WHERE renter_pk = ?", pk[:])
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
		p := filepath.Join(m.BufferedFilesDir(), name)
		fi, err := os.Stat(p)
		if err != nil {
			m.log.Println("ERROR: unable to get file size:", err)
			return modules.AddContext(err, "unable to get file size")
		}
		if err := os.Remove(p); err != nil {
			m.log.Println("ERROR: unable to delete file:", err)
			return modules.AddContext(err, "unable to delete file")
		}
		m.mu.Lock()
		m.bufferSize -= uint64(fi.Size())
		m.mu.Unlock()
		_, err = m.db.Exec("DELETE FROM ctr_parts WHERE filename = ?", name)
		if err != nil {
			m.log.Println("ERROR: unable to delete file record:", err)
			return modules.AddContext(err, "unable to delete file record")
		}
	}

	// Delete multipart uploads.
	_, err = m.db.Exec("DELETE FROM ctr_multipart WHERE renter_pk = ?", pk[:])
	if err != nil {
		m.log.Println("ERROR: unable to delete multipart uploads:", err)
		return modules.AddContext(err, "unable to delete multipart uploads")
	}

	return nil
}

// RegisterMultipart registers a new multipart upload.
func (m *Manager) RegisterMultipart(rpk types.PublicKey, key types.Hash256, bucket, path, mimeType []byte, encrypted bool) (id types.Hash256, err error) {
	frand.Read(id[:])
	_, err = m.db.Exec(`
		INSERT INTO ctr_multipart (id, enc_key, bucket, filepath, mime, renter_pk, created, encrypted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		id[:],
		key[:],
		bucket,
		path,
		mimeType,
		rpk[:],
		time.Now().Unix(),
		encrypted,
	)
	return
}

// DeleteMultipart deletes an aborted multipart upload.
func (m *Manager) DeleteMultipart(rpk types.PublicKey, id types.Hash256) error {
	tx, err := m.db.Begin()
	if err != nil {
		return modules.AddContext(err, "unable to start transaction")
	}

	rows, err := tx.Query("SELECT filename FROM ctr_parts WHERE upload_id = ?", id[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to query files")
	}

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			tx.Rollback()
			return modules.AddContext(err, "unable to retrieve filename")
		}
		names = append(names, name)
	}
	rows.Close()

	for _, name := range names {
		p := filepath.Join(m.BufferedFilesDir(), name)
		fi, err := os.Stat(p)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "unable to get file size")
		}
		if err := os.Remove(p); err != nil {
			tx.Rollback()
			return modules.AddContext(err, "unable to delete file")
		}
		m.mu.Lock()
		m.bufferSize -= uint64(fi.Size())
		m.mu.Unlock()
	}

	_, err = tx.Exec("DELETE FROM ctr_parts WHERE upload_id = ?", id[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to delete file records")
	}
	_, err = tx.Exec("DELETE FROM ctr_multipart WHERE id = ?", id[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to delete multipart upload")
	}

	return tx.Commit()
}

// managedPruneMultipartUploads deletes any multipart uploads that are older
// than the threshold together with the associated parts.
func (m *Manager) managedPruneMultipartUploads() {
	tx, err := m.db.Begin()
	if err != nil {
		m.log.Println("ERROR: unable to start transaction:", err)
		return
	}

	// Make a list of multipart uploads that are old enough to be pruned.
	rows, err := tx.Query(`
		SELECT id
		FROM ctr_multipart
		WHERE created < ?
	`, uint64(time.Now().Add(-multipartUploadPruneThreshold).Unix()))
	if err != nil {
		m.log.Println("ERROR: unable to query multipart uploads:", err)
		tx.Rollback()
		return
	}

	var ids []types.Hash256
	for rows.Next() {
		key := make([]byte, 32)
		if err := rows.Scan(&key); err != nil {
			m.log.Println("ERROR: unable to get upload ID:", err)
			rows.Close()
			tx.Rollback()
			return
		}
		var id types.Hash256
		copy(id[:], key)
		ids = append(ids, id)
	}
	rows.Close()

	// Delete the files associated with the parts and the parts themselves.
	var numParts int64
	for _, id := range ids {
		rows, err = tx.Query("SELECT filename FROM ctr_parts WHERE upload_id = ?", id[:])
		if err != nil {
			m.log.Println("ERROR: unable to query files:", err)
			tx.Rollback()
			return
		}

		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				m.log.Println("ERROR: unable to retrieve filename:", err)
				tx.Rollback()
				return
			}
			names = append(names, name)
		}
		rows.Close()

		for _, name := range names {
			p := filepath.Join(m.BufferedFilesDir(), name)
			fi, err := os.Stat(p)
			if err != nil {
				m.log.Println("ERROR: unable to get file size:", err)
				tx.Rollback()
				return
			}
			if err := os.Remove(p); err != nil {
				m.log.Println("ERROR: unable to delete file:", err)
				tx.Rollback()
				return
			}
			m.mu.Lock()
			m.bufferSize -= uint64(fi.Size())
			m.mu.Unlock()
			res, err := tx.Exec("DELETE FROM ctr_parts WHERE filename = ?", name)
			if err != nil {
				m.log.Println("ERROR: unable to delete file record:", err)
				tx.Rollback()
				return
			}
			np, _ := res.RowsAffected()
			numParts += np
		}
	}

	// Delete the multipart uploads.
	res, err := tx.Exec("DELETE FROM ctr_multipart WHERE created < ?", uint64(time.Now().Add(-multipartUploadPruneThreshold).Unix()))
	if err != nil {
		m.log.Println("ERROR: unable to prune multipart uploads:", err)
		tx.Rollback()
		return
	}

	if err := tx.Commit(); err != nil {
		m.log.Println("ERROR: unable to commit transaction:", err)
		return
	}

	numUploads, _ := res.RowsAffected()
	if numUploads > 0 {
		m.log.Printf("INFO: pruned %d multipart uploads with %d parts\n", numUploads, numParts)
	}
}

// threadedPruneMultipartUploads prunes the incomplete multipart
// uploads with the certain interval.
func (m *Manager) threadedPruneMultipartUploads() {
	if err := m.tg.Add(); err != nil {
		return
	}
	defer m.tg.Done()

	for {
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(multipartUploadPruneInterval):
		}
		m.managedPruneMultipartUploads()
	}
}

// PutMultipartPart associates the uploaded file with the part of
// a multipart upload.
func (m *Manager) PutMultipartPart(pk types.PublicKey, id types.Hash256, part int, filename string) error {
	// Lock the upload.
	m.mu.Lock()
	m.multipartUploads[id] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.multipartUploads, id)
		m.mu.Unlock()
	}()

	// Delete the old part if it exists.
	var name string
	err := m.db.QueryRow(`
		SELECT filename
		FROM ctr_parts
		WHERE upload_id = ?
		AND num = ?
	`, id[:], part).Scan(&name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modules.AddContext(err, "couldn't get filename")
	}
	if err == nil {
		path := filepath.Join(m.BufferedFilesDir(), name)
		fi, err := os.Stat(path)
		if err != nil {
			return modules.AddContext(err, "couldn't read file size")
		}
		if err := os.Remove(path); err != nil {
			return modules.AddContext(err, "couldn't delete file")
		}
		m.mu.Lock()
		m.bufferSize -= uint64(fi.Size())
		m.mu.Unlock()
		_, err = m.db.Exec("DELETE FROM ctr_parts WHERE filename = ?", name)
		if err != nil {
			return modules.AddContext(err, "couldn't delete part")
		}
	}

	path := filepath.Join(m.BufferedFilesDir(), filename)
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.bufferSize += uint64(fi.Size())
	m.mu.Unlock()

	_, err = m.db.Exec(`
		INSERT INTO ctr_parts
			(filename, num, upload_id, renter_pk)
		VALUES (?, ?, ?, ?)
	`, filename, part, id[:], pk[:])
	return err
}

// AssembleParts puts together the parts of a multipart upload and
// marks the resulting file as pending upload.
func (m *Manager) AssembleParts(pk types.PublicKey, id types.Hash256) (err error) {
	// Sleep until the upload is unlocked.
	for {
		var locked bool
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(time.Second):
			m.mu.Lock()
			_, locked = m.multipartUploads[id]
			m.mu.Unlock()
		}
		if !locked {
			break
		}
	}

	// Retrieve necessary parameters.
	var bucket, path, mimeType []byte
	var encrypted bool
	err = m.db.QueryRow(`
		SELECT bucket, filepath, mime, encrypted
		FROM ctr_multipart
		WHERE id = ?
	`, id[:]).Scan(&bucket, &path, &mimeType, &encrypted)
	if err != nil {
		return modules.AddContext(err, "couldn't find multipart upload")
	}

	// Get the names of the parts.
	rows, err := m.db.Query(`
		SELECT filename
		FROM ctr_parts
		WHERE upload_id = ?
		ORDER BY num ASC
	`, id[:])
	if err != nil {
		return modules.AddContext(err, "couldn't query parts")
	}

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return modules.AddContext(err, "couldn't get filename")
		}
		names = append(names, filepath.Join(m.BufferedFilesDir(), name))
	}
	rows.Close()

	// Return early if there are no parts to assemble.
	if len(names) == 0 {
		return nil
	}

	// Assemble the parts.
	dst, err := os.OpenFile(names[0], os.O_APPEND|os.O_WRONLY, 0660)
	if err != nil {
		return modules.AddContext(err, "couldn't open file")
	}

	var encText string
	if encrypted {
		fi, err := dst.Stat()
		if err != nil {
			return modules.AddContext(err, "couldn't read file size")
		}
		encText = fmt.Sprintf("%d", fi.Size())
	}

	defer func() {
		if fErr := dst.Sync(); fErr != nil {
			err = modules.ComposeErrors(err, modules.AddContext(fErr, "couldn't sync file"))
		} else if fErr := dst.Close(); fErr != nil {
			err = modules.ComposeErrors(err, modules.AddContext(fErr, "couldn't close file"))
		}
	}()

	for i := 1; i < len(names); i++ {
		src, err := os.Open(names[i])
		if err != nil {
			return modules.AddContext(err, "couldn't open file")
		}
		if encrypted {
			fi, err := src.Stat()
			if err != nil {
				return modules.AddContext(err, "couldn't read file size")
			}
			if len(encText) > 0 {
				encText += ","
			}
			encText += fmt.Sprintf("%d", fi.Size())
		}
		_, err = io.Copy(dst, src)
		if err != nil {
			src.Close()
			return modules.AddContext(err, "couldn't copy file contents")
		}
		if err = src.Close(); err != nil {
			return modules.AddContext(err, "couldn't close file")
		}
		if err = os.Remove(names[i]); err != nil {
			return modules.AddContext(err, "couldn't delete file")
		}
	}

	// Delete the parts.
	_, err = m.db.Exec("DELETE FROM ctr_parts WHERE upload_id = ?", id[:])
	if err != nil {
		return modules.AddContext(err, "couldn't delete parts")
	}

	// Delete the multipart upload.
	_, err = m.db.Exec("DELETE FROM ctr_multipart WHERE id = ?", id[:])
	if err != nil {
		return modules.AddContext(err, "couldn't delete multipart upload")
	}

	// Register a new file to upload.
	_, err = m.db.Exec(`
		INSERT INTO ctr_uploads (filename, bucket, filepath, mime, renter_pk, ready, encrypted)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		filepath.Base(names[0]),
		bucket,
		path,
		mimeType,
		pk[:],
		true,
		encText,
	)
	if err != nil {
		return modules.AddContext(err, "couldn't register new upload")
	}

	// Initiate the upload.
	go m.hostContractor.StartUploading()

	return
}
