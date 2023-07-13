package contractset

import (
	"bytes"
	"database/sql"
	"errors"
	"io"

	"go.sia.tech/core/types"
)

// saveContract saves the FileContract in the database. A lock must be acquired
// on the contract.
func (fc *FileContract) saveContract(rpk types.PublicKey) error {
	id := fc.header.ID()
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	fc.EncodeTo(e)
	e.Flush()

	// Check if the contract is already in the database.
	key := make([]byte, 32)
	var renterKey types.PublicKey
	err := fc.db.QueryRow(`
		SELECT renter_pk
		FROM ctr_contracts
		WHERE id = ?
	`, id[:]).Scan(&key)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	copy(renterKey[:], key)
	if renterKey == (types.PublicKey{}) {
		renterKey = rpk
	}

	// Insert the contract. If it already exists, it will be updated.
	_, err = fc.db.Exec(`
		INSERT INTO ctr_contracts (id, renter_pk, renewed_from, renewed_to, bytes)
		VALUES (?, ?, ?, ?, ?) AS new
		ON DUPLICATE KEY UPDATE
		SET renter_pk = new.renter_pk, bytes = new.bytes
	`, id[:], renterKey[:], []byte{}, []byte{}, buf.Bytes())

	return err
}

// deleteContract deletes the contract from the database.
func deleteContract(fcid types.FileContractID, db *sql.DB) error {
	_, err := db.Exec("DELETE FROM ctr_contracts WHERE id = ?", fcid[:])
	return err
}

// loadContracts loads the entire ctr_contracts table into the contract set.
func (cs *ContractSet) loadContracts(height uint64) error {
	// Load the contracts.
	rows, err := cs.db.Query("SELECT id, renewed_to, bytes FROM ctr_contracts")
	if err != nil {
		return err
	}
	defer rows.Close()

	// Iterate through each contract.
	var fcid, renewedTo types.FileContractID
	id := make([]byte, 32)
	renewed := make([]byte, 32)
	var fcBytes []byte
	for rows.Next() {
		if err := rows.Scan(&id, &renewed, &fcBytes); err != nil {
			cs.log.Println("ERROR: unable to load file contract:", err)
			continue
		}

		buf := bytes.NewBuffer(fcBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(fcBytes))})
		fc := &FileContract{db: cs.db}
		fc.DecodeFrom(d)
		if err := d.Err(); err != nil {
			cs.log.Println("ERROR: unable to decode file contract:", err)
			continue
		}
		copy(fcid[:], id)
		copy(renewedTo[:], renewed)

		// Check if the contract has expired.
		if renewedTo == (types.FileContractID{}) && fc.header.EndHeight() >= height {
			cs.contracts[fcid] = fc
			rpk := fc.header.RenterPublicKey().String()
			hpk := fc.header.HostPublicKey().String()
			cs.pubKeys[rpk + hpk] = fcid
		} else {
			cs.oldContracts[fcid] = fc
		}
	}

	return nil
}

// managedFindIDs returns a list of contract IDs belonging to the given renter.
// NOTE: this function also returns the old contracts.
func (cs *ContractSet) managedFindIDs(rpk types.PublicKey) []types.FileContractID {
	rows, err := cs.db.Query(`
		SELECT id
		FROM ctr_contracts
		WHERE renter_pk = ?
	`, rpk.String())
	if err != nil {
		cs.log.Println("ERROR: couldn't query database:", err)
		return nil
	}
	defer rows.Close()

	var ids []types.FileContractID
	for rows.Next() {
		id := make([]byte, 32)
		if err := rows.Scan(&id); err != nil {
			cs.log.Println("ERROR: unable to get contract ID:", err)
			continue
		}
		var fcid types.FileContractID
		copy(fcid[:], id)
		ids = append(ids, fcid)
	}

	return ids
}