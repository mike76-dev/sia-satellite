package wallet

import (
	"database/sql"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.sia.tech/core/types"
)

// insertAddress inserts a new wallet address.
func (w *Wallet) insertAddress(addr types.Address) error {
	res, err := w.tx.Exec("INSERT INTO wt_addresses (addr) VALUES (?)", addr[:])
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't insert address")
	}

	index, err := res.LastInsertId()
	if err != nil {
		return err
	}
	w.addrs[addr] = uint64(index)

	return nil
}

// AddWatch adds the given watched address.
func (w *Wallet) AddWatch(addr types.Address) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	index, ok := w.addrs[addr]
	if !ok {
		return errors.New("address not found")
	}

	w.watchedAddrs[addr] = index
	_, err := w.tx.Exec("INSERT INTO wt_watched (address_id) VALUES (?)", index)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't insert watched address")
	}

	return nil
}

// RemoveWatch removes the given watched address.
func (w *Wallet) RemoveWatch(addr types.Address) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.watchedAddrs, addr)
	_, err := w.tx.Exec(`
		DELETE FROM wt_watched
		WHERE address_id IN (
			SELECT id FROM wt_addresses
			WHERE addr = ?
		)
	`, addr[:])
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't delete address")
	}

	return nil
}

// WatchedAddresses returns a list of the addresses watched by the wallet.
func (w *Wallet) WatchedAddresses() (addrs []types.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for addr := range w.watchedAddrs {
		addrs = append(addrs, addr)
	}

	return addrs
}

// updateTip updates the current tip of the wallet.
func (w *Wallet) updateTip(ci types.ChainIndex) error {
	w.tip = ci
	_, err := w.tx.Exec(`
		REPLACE INTO wt_tip (id, height, bid)
		VALUES (1, ?, ?)
	`, ci.Height, ci.ID[:])
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't update tip")
	}

	return nil
}

// insertSiacoinElement inserts the given Siacoin element.
func (w *Wallet) insertSiacoinElement(sce types.SiacoinElement) error {
	sce.MerkleProof = append([]types.Hash256(nil), sce.MerkleProof...)
	w.sces[sce.SiacoinOutput.Address] = sce
	_, err := w.tx.Exec(`
		INSERT INTO wt_sces (
			scoid,
			sc_value,
			merkle_proof,
			leaf_index,
			maturity_height,
			address_id
		)
		VALUES (?, ?, ?, ?, ?, (
			SELECT id FROM wt_addresses
			WHERE addr = ?
		))
	`,
		sce.ID[:],
		encodeCurrency(sce.SiacoinOutput.Value),
		encodeProof(sce.MerkleProof),
		sce.LeafIndex,
		sce.MaturityHeight,
		sce.SiacoinOutput.Address[:],
	)
	if err != nil {
		w.dbError = true
	}

	return err
}

// deleteSiacoinElement deletes the Siacoin element with the given ID.
func (w *Wallet) deleteSiacoinElement(addr types.Address) error {
	delete(w.sces, addr)
	_, err := w.tx.Exec(`
		DELETE FROM wt_sces
		WHERE address_id IN (
			SELECT id FROM wt_addresses
			WHERE addr = ?
		)
	`, addr[:])
	if err != nil {
		w.dbError = true
	}

	return err
}

// insertSiafundElement inserts the given Siafund element.
func (w *Wallet) insertSiafundElement(sfe types.SiafundElement) error {
	sfe.MerkleProof = append([]types.Hash256(nil), sfe.MerkleProof...)
	w.sfes[sfe.SiafundOutput.Address] = sfe
	_, err := w.tx.Exec(`
		INSERT INTO wt_sfes (
			sfoid,
			claim_start,
			merkle_proof,
			leaf_index,
			sf_value,
			address_id
		)
		VALUES (?, ?, ?, ?, ?, (
			SELECT id FROM wt_addresses
			WHERE addr = ?
		))
	`,
		sfe.ID[:],
		encodeCurrency(sfe.ClaimStart),
		encodeProof(sfe.MerkleProof),
		sfe.LeafIndex,
		sfe.SiafundOutput.Value,
		sfe.SiafundOutput.Address[:],
	)
	if err != nil {
		w.dbError = true
	}

	return err
}

// deleteSiafundElement deletes the Siafund element with the given ID.
func (w *Wallet) deleteSiafundElement(addr types.Address) error {
	delete(w.sfes, addr)
	_, err := w.tx.Exec(`
		DELETE FROM wt_sfes
		WHERE address_id IN (
			SELECT id FROM wt_addresses
			WHERE addr = ?
		)
	`, addr[:])
	if err != nil {
		w.dbError = true
	}

	return err
}

// updateSiacoinElementProofs updates the Merkle proofs on each SC element.
func (w *Wallet) updateSiacoinElementProofs(cu chainUpdate) error {
	updateStmt, err := w.tx.Prepare(`
		UPDATE wt_sces
		SET merkle_proof = ?, leaf_index = ?
		WHERE scoid = ?
	`)
	if err != nil {
		return modules.AddContext(err, "failed to prepare update statement")
	}
	defer updateStmt.Close()

	for _, sce := range w.sces {
		cu.UpdateElementProof(&sce.StateElement)
		w.sces[sce.SiacoinOutput.Address] = sce
		_, err := updateStmt.Exec(encodeProof(sce.MerkleProof), sce.LeafIndex, sce.ID[:])
		if err != nil {
			w.dbError = true
			return modules.AddContext(err, "failed to update Siacoin element")
		}
	}

	return nil
}

// updateSiafundElementProofs updates the Merkle proofs on each SF element.
func (w *Wallet) updateSiafundElementProofs(cu chainUpdate) error {
	updateStmt, err := w.tx.Prepare(`
		UPDATE wt_sfes
		SET merkle_proof = ?, leaf_index = ?
		WHERE sfoid = ?
	`)
	if err != nil {
		return modules.AddContext(err, "failed to prepare update statement")
	}
	defer updateStmt.Close()

	for _, sfe := range w.sfes {
		cu.UpdateElementProof(&sfe.StateElement)
		w.sfes[sfe.SiafundOutput.Address] = sfe
		_, err := updateStmt.Exec(encodeProof(sfe.MerkleProof), sfe.LeafIndex, sfe.ID[:])
		if err != nil {
			w.dbError = true
			return modules.AddContext(err, "failed to update Siafund element")
		}
	}

	return nil
}

// load loads the wallet data from the database.
func (w *Wallet) load() (err error) {
	s := make([]byte, 32)
	var progress uint64
	if err := w.db.QueryRow(`
		SELECT seed, progress
		FROM wt_info
		WHERE id = 1
	`).Scan(&s, &progress); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modules.AddContext(err, "couldn't load seed")
	}
	copy(w.seed[:], s)
	for _, key := range generateKeys(w.seed, 0, progress) {
		w.keys[types.StandardUnlockHash(key.PublicKey())] = key
	}
	w.regenerateLookahead(progress)

	b := make([]byte, 32)
	if err := w.db.QueryRow(`
		SELECT height, bid
		FROM wt_tip
		WHERE id = 1
	`).Scan(&w.tip.Height, &b); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modules.AddContext(err, "couldn't load last chain index")
	}
	copy(w.tip.ID[:], b)

	rows, err := w.db.Query(`
		SELECT id, addr
		FROM wt_addresses
	`)
	if err != nil {
		return modules.AddContext(err, "couldn't query addresses")
	}

	for rows.Next() {
		var index uint64
		var addr types.Address
		if err := rows.Scan(&index, &b); err != nil {
			return modules.AddContext(err, "couldn't scan address")
		}
		copy(addr[:], b)
		w.addrs[addr] = index
	}

	rows.Close()

	rows, err = w.db.Query(`
		SELECT id, addr
		FROM wt_addresses
		WHERE id IN (
			SELECT address_id
			FROM wt_watched
		)
	`)
	if err != nil {
		return modules.AddContext(err, "couldn't query watched addresses")
	}

	for rows.Next() {
		var index uint64
		var addr types.Address
		if err := rows.Scan(&index, &b); err != nil {
			return modules.AddContext(err, "couldn't scan watched address")
		}
		copy(addr[:], b)
		w.watchedAddrs[addr] = index
	}

	rows.Close()

	rows, err = w.db.Query(`
		SELECT
			wt_sces.scoid,
			wt_sces.sc_value,
			wt_sces.merkle_proof,
			wt_sces.leaf_index,
			wt_sces.maturity_height,
			wt_addresses.addr
		FROM wt_sces
		INNER JOIN wt_addresses
		ON wt_sces.address_id = wt_addresses.id
	`)
	if err != nil {
		return modules.AddContext(err, "couldn't query SC elements")
	}

	for rows.Next() {
		id := make([]byte, 32)
		addr := make([]byte, 32)
		var v, proof []byte
		var li, mh uint64
		if err = rows.Scan(&id, &v, &proof, &li, &mh, &addr); err != nil {
			return modules.AddContext(err, "couldn't scan SC element")
		}
		sce := types.SiacoinElement{
			StateElement: types.StateElement{
				MerkleProof: decodeProof(proof),
				LeafIndex:   li,
			},
			MaturityHeight: mh,
			SiacoinOutput: types.SiacoinOutput{
				Value: decodeCurrency(v),
			},
		}
		copy(sce.ID[:], id)
		copy(sce.SiacoinOutput.Address[:], addr)
		w.sces[sce.SiacoinOutput.Address] = sce
	}

	rows.Close()

	rows, err = w.db.Query(`
		SELECT
			wt_sfes.sfoid,
			wt_sfes.claim_start,
			wt_sfes.merkle_proof,
			wt_sfes.leaf_index,
			wt_sfes.sf_value,
			wt_addresses.addr
		FROM wt_sfes
		INNER JOIN wt_addresses
		ON wt_sfes.address_id = wt_addresses.id
	`)
	if err != nil {
		return modules.AddContext(err, "couldn't query SF elements")
	}

	for rows.Next() {
		id := make([]byte, 32)
		addr := make([]byte, 32)
		var cs, proof []byte
		var value, li uint64
		if err = rows.Scan(&id, &cs, &proof, &li, &value, &addr); err != nil {
			return modules.AddContext(err, "couldn't scan SF element")
		}
		sfe := types.SiafundElement{
			StateElement: types.StateElement{
				MerkleProof: decodeProof(proof),
				LeafIndex:   li,
			},
			SiafundOutput: types.SiafundOutput{Value: value},
			ClaimStart:    decodeCurrency(cs),
		}
		copy(sfe.ID[:], id)
		copy(sfe.SiafundOutput.Address[:], addr)
		w.sfes[sfe.SiafundOutput.Address] = sfe
	}

	rows.Close()

	rows, err = w.db.Query("SELECT id FROM wt_spent")
	if err != nil {
		return modules.AddContext(err, "couldn't query spent outputs")
	}

	for rows.Next() {
		var id types.Hash256
		if err := rows.Scan(&b); err != nil {
			return modules.AddContext(err, "couldn't scan spent output")
		}
		copy(id[:], b)
		w.used[id] = true
	}

	rows.Close()

	w.tx, err = w.db.Begin()
	if err != nil {
		return modules.AddContext(err, "couldn't start wallet transaction")
	}

	return nil
}

// save saves the wallet state.
// A lock must be acquired before calling this function.
func (w *Wallet) save() (err error) {
	if w.dbError {
		err = w.tx.Rollback()
		if err != nil {
			return err
		}
		w.dbError = false
	} else {
		err = w.tx.Commit()
		if err != nil {
			return err
		}
	}
	w.tx, err = w.db.Begin()
	return err
}

// reset resets the database before rescanning.
func (w *Wallet) reset() (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err = w.tx.Exec("DROP TABLE wt_sces")
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't drop SC elements")
	}
	_, err = w.tx.Exec("DROP TABLE wt_sfes")
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't drop SF elements")
	}
	_, err = w.tx.Exec("DROP TABLE wt_watched")
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't drop watched addresses")
	}
	_, err = w.tx.Exec("DROP TABLE wt_addresses")
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't drop addresses")
	}
	_, err = w.tx.Exec("DROP TABLE wt_spent")
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't drop spent outputs")
	}

	_, err = w.tx.Exec(`
		CREATE TABLE wt_addresses (
			id   BIGINT NOT NULL AUTO_INCREMENT,
			addr BINARY(32) NOT NULL UNIQUE,
			PRIMARY KEY (id)
		)
	`)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't create addresses")
	}

	_, err = w.tx.Exec(`
		CREATE TABLE wt_sces (
			id              BIGINT NOT NULL AUTO_INCREMENT,
			scoid           BINARY(32) NOT NULL UNIQUE,
			sc_value        BLOB NOT NULL,
			merkle_proof    BLOB NOT NULL,
			leaf_index      BIGINT UNSIGNED NOT NULL,
			maturity_height BIGINT UNSIGNED NOT NULL,
			address_id      BIGINT NOT NULL,
			PRIMARY KEY (id),
			FOREIGN KEY (address_id) REFERENCES wt_addresses(id)
		)
	`)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't create SC elements")
	}

	_, err = w.tx.Exec(`
		CREATE TABLE wt_sfes (
			id              BIGINT NOT NULL AUTO_INCREMENT,
			sfoid           BINARY(32) NOT NULL UNIQUE,
			claim_start     BLOB NOT NULL,
			merkle_proof    BLOB NOT NULL,
			leaf_index      BIGINT UNSIGNED NOT NULL,
			sf_value        BIGINT UNSIGNED NOT NULL,
			address_id      BIGINT NOT NULL,
			PRIMARY KEY (id),
			FOREIGN KEY (address_id) REFERENCES wt_addresses(id)
		)
	`)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't create SF elements")
	}

	_, err = w.tx.Exec(`
		CREATE TABLE wt_watched (
			address_id BIGINT NOT NULL UNIQUE,
			FOREIGN KEY (address_id) REFERENCES wt_addresses(id)
		)
	`)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't create watched addresses")
	}

	_, err = w.tx.Exec(`
		CREATE TABLE wt_spent (
			id BINARY(32) NOT NULL,
			PRIMARY KEY (id)
		)
	`)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't create spent outputs")
	}

	if err := w.updateTip(w.tip); err != nil {
		return err
	}

	return w.save()
}

// saveSeed saves the new seed.
func (w *Wallet) saveSeed(progress uint64) error {
	_, err := w.tx.Exec(`
		REPLACE INTO wt_info (id, seed, progress)
		VALUES (1, ?, ?)
	`, w.seed[:], progress)
	if err != nil {
		w.dbError = true
		return modules.AddContext(err, "couldn't save wallet seed")
	}
	return w.save()
}

// getSeedProgress returns the current seed progress.
func (w *Wallet) getSeedProgress() (progress uint64, err error) {
	err = w.tx.QueryRow("SELECT progress FROM wt_info WHERE id = 1").Scan(&progress)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return
}

// putSeedProgress updates the current seed progress.
func (w *Wallet) putSeedProgress(progress uint64) error {
	_, err := w.tx.Exec("UPDATE wt_info SET progress = ? WHERE id = 1", progress)
	if err != nil {
		w.dbError = true
	}
	return err
}

// insertSpentOutput adds a new spent output.
func (w *Wallet) insertSpentOutput(id types.Hash256) error {
	w.used[id] = true
	_, err := w.tx.Exec("INSERT INTO wt_spent (id) VALUES (?)", id[:])
	if err != nil {
		w.dbError = true
	}
	return err
}

// removeSpentOutput removes a spent output.
func (w *Wallet) removeSpentOutput(id types.Hash256) error {
	delete(w.used, id)
	_, err := w.tx.Exec("DELETE FROM wt_spent WHERE id = ?", id[:])
	if err != nil {
		w.dbError = true
	}
	return err
}
