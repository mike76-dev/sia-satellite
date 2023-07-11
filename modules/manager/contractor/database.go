package contractor

import (
	"bytes"
	"database/sql"
	"errors"
	"io"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// initDB initializes the sync state.
func (c *Contractor) initDB() error {
	var count int
	err := c.db.QueryRow("SELECT COUNT(*) FROM ctr_info").Scan(&count)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = c.db.Exec(`
		INSERT INTO ctr_info (height, last_change, synced)
		VALUES (?, ?, ?)
	`, 0,modules.ConsensusChangeBeginning[:], false)
	return err
}

// loadState loads the sync state of the contractor.
func (c *Contractor) loadState() error {
	cc := make([]byte, 32)
	var height uint64
	var synced bool
	err := c.db.QueryRow(`
		SELECT height, last_change, synced
		FROM ctr_info
		WHERE id = 1
	`).Scan(&height, &cc, &synced)
	if err != nil {
		return err
	}

	c.blockHeight = height
	copy(c.lastChange[:], cc)
	c.synced = make(chan struct{})
	if synced {
		close(c.synced)
	}

	return nil
}

// updateState saves the sync state of the contractor.
func (c *Contractor) updateState() error {
	synced := false
	select {
	case <-c.synced:
		synced = true
	default:
	}
	_, err := c.db.Exec(`
		UPDATE ctr_info
		SET height = ?, last_change = ?, synced = ?
		WHERE id = 1
	`, c.blockHeight, c.lastChange[:], synced)
	return err
}

// UpdateRenter updates the renter record in the database.
// The record must have already been created.
func (c *Contractor) UpdateRenter(renter modules.Renter) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	renter.Allowance.EncodeTo(e)
	e.Flush()
	_, err := c.db.Exec(`
		UPDATE ctr_renters
		SET current_period = ?, allowance = ?, private_key = ?, auto_renew_contracts = ?
		WHERE email = ?
	`, renter.CurrentPeriod, buf.Bytes(), renter.PrivateKey, renter.Settings.AutoRenewContracts, renter.Email)
	return err
}

// updateRenewedContract updates renewed_from and renewed_to
// fields in the contracts table.
func (c *Contractor) updateRenewedContract(oldID, newID types.FileContractID) error {
	_, err := c.db.Exec("UPDATE ctr_contracts SET renewed_from = ? WHERE id = ?", oldID[:], newID[:])
	if err != nil {
		return err
	}
	_, err = c.db.Exec("UPDATE ctr_contracts SET renewed_to = ? WHERE id = ?", newID[:], oldID[:])
	return err
}

// managedFindRenter tries to find a renter by the contract ID.
func (c *Contractor) managedFindRenter(fcid types.FileContractID) (modules.Renter, error) {
	key := make([]byte, 32)
	err := c.db.QueryRow("SELECT renter_pk FROM ctr_contracts WHERE id = ?", fcid[:]).Scan(&key)
	if err != nil {
		return modules.Renter{}, ErrRenterNotFound
	}

	var rpk types.PublicKey
	copy(rpk[:], key)
	renter, exists := c.renters[rpk]
	if exists {
		return renter, nil
	}

	return modules.Renter{}, ErrRenterNotFound
}

// loadDoubleSpent loads the double-spent contracts map from the database.
func (c *Contractor) loadDoubleSpent() error {
	rows, err := c.db.Query("SELECT id, height FROM ctr_dspent")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		id := make([]byte, 32)
		var height uint64
		var fcid types.FileContractID
		if err := rows.Scan(&id, &height); err != nil {
			return err
		}
		copy(fcid[:], id)
		c.doubleSpentContracts[fcid] = height
	}

	return nil
}

// updateDoubleSpent updates the double-spent contracts map in the database.
func (c *Contractor) updateDoubleSpent() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_dspent")
	if err != nil {
		c.log.Println("ERROR: couldn't clear double-spent contracts:", err)
		tx.Rollback()
		return err
	}

	for id, height := range c.doubleSpentContracts {
		_, err := tx.Exec("INSERT INTO ctr_dspent (id, height) VALUES (?, ?)", id[:], height)
		if err != nil {
			c.log.Println("ERROR: couldn't update double-spent contracts:", err)
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// loadRenewHistory loads the renewal history from the database.
func (c *Contractor) loadRenewHistory() error {
	rows, err := c.db.Query("SELECT id, renewed_from, renewed_to FROM ctr_contracts")
	if err != nil {
		return err
	}
	defer rows.Close()

	id := make([]byte, 32)
	from := make([]byte, 32)
	to := make([]byte, 32)
	var fcid, fcidNew, fcidOld types.FileContractID
	for rows.Next() {
		if err := rows.Scan(&id, &from, &to); err != nil {
			c.log.Println("Error scanning database row:", err)
			continue
		}
		copy(fcid[:], id)
		copy(fcidOld[:], from)
		copy(fcidNew[:], to)

		if fcidOld != (types.FileContractID{}) {
			c.renewedFrom[fcid] = fcidOld
		}
		if fcidNew != (types.FileContractID{}) {
			c.renewedTo[fcid] = fcidNew
		}
	}

	return nil
}

// loadRenters load the renters from the database.
func (c *Contractor) loadRenters() error {
	rows, err := c.db.Query(`
		SELECT email, public_key, current_period,
		allowance, private_key, auto_renew_contracts
		FROM ctr_renters
	`)
	if err != nil {
		c.log.Println("ERROR: could not load the renters:", err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var email string
		pk := make([]byte, 32)
		var aBytes []byte
		var period uint64
		sk := make([]byte, 64)
		var autoRenew bool
		if err := rows.Scan(&email, &pk, &period, &aBytes, &sk, &autoRenew); err != nil {
			c.log.Println("ERROR: could not load the renter:", err)
			continue
		}

		var a modules.Allowance
		buf := bytes.NewBuffer(aBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(aBytes))})
		a.DecodeFrom(d)
		if err := d.Err(); err != nil {
			return err
		}

		 renter := modules.Renter{
			Allowance:     a,
			CurrentPeriod: period,
			Email:         email,
			PrivateKey:    types.PrivateKey(sk),
			Settings:      modules.RenterSettings{
				AutoRenewContracts: autoRenew,
			},
		}
		copy(renter.PublicKey[:], pk)
		c.renters[renter.PublicKey] = renter
	}

	return nil
}

// save saves the watchdog to the database.
func (w *watchdog) save() error {
	tx, err := w.contractor.db.Begin()
	if err != nil {
		w.contractor.log.Println("ERROR: couldn't save watchdog:", err)
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_watchdog")
	if err != nil {
		w.contractor.log.Println("ERROR: couldn't clear watchdog data:", err)
		tx.Rollback()
		return err
	}

	for id, fcs := range w.contracts {
		var buf bytes.Buffer
		e := types.NewEncoder(&buf)
		fcs.EncodeTo(e)
		e.Flush()
		_, err := tx.Exec("INSERT INTO ctr_watchdog (id, bytes) VALUES (?, ?)", id[:], buf.Bytes())
		if err != nil {
			w.contractor.log.Println("ERROR: couldn't save watchdog:", err)
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// load loads the watchdog from the database.
func (w *watchdog) load() error {
	tx, err := w.contractor.db.Begin()
	if err != nil {
		w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
		return err
	}

	rows, err := tx.Query("SELECT id, bytes FROM ctr_watchdog")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
		tx.Rollback()
		return err
	}

	for rows.Next() {
		fcs := new(fileContractStatus)
		id := make([]byte, 32)
		var fcsBytes []byte
		if err := rows.Scan(&id, &fcsBytes); err != nil {
			w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
			rows.Close()
			tx.Rollback()
			return err
		}
		var fcid types.FileContractID
		copy(fcid[:], id)
		buf := bytes.NewBuffer(fcsBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(fcsBytes))})
		fcs.DecodeFrom(d)
		if err := d.Err(); err != nil {
			w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
			rows.Close()
			tx.Rollback()
			return err
		}
		w.contracts[fcid] = fcs
	}

	rows.Close()
	return tx.Commit()
}
