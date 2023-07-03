package wallet

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

// threadedDBUpdate commits the active database transaction and starts a new
// transaction.
func (w *Wallet) threadedDBUpdate() {
	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	for {
		select {
		case <-time.After(2 * time.Minute):
		case <-w.tg.StopChan():
			return
		}
		w.mu.Lock()
		err := w.syncDB()
		w.mu.Unlock()
		if err != nil {
			// If the database is having problems, we need to close it to
			// protect it. This will likely cause a panic somewhere when another
			// caller tries to access dbTx but it is nil.
			w.log.Severe("ERROR: syncDB encountered an error. Closing database to protect wallet. Wallet may crash:", err)
			w.db.Close()
			return
		}
	}
}

// syncDB commits the current global transaction and immediately begins a
// new one. It must be called with a write-lock.
func (w *Wallet) syncDB() error {
	// If the rollback flag is set, it means that somewhere in the middle of an
	// atomic update there  was a failure, and that failure needs to be rolled
	// back. An error will be returned.
	if w.dbRollback {
		err := errors.New("database unable to sync - rollback requested")
		return modules.ComposeErrors(err, w.dbTx.Rollback())
	}

	// Commit the current tx.
	err := w.dbTx.Commit()
	if err != nil {
		w.log.Severe("ERROR: failed to apply database update:", err)
		err = modules.ComposeErrors(err, w.dbTx.Rollback())
		return modules.AddContext(err, "unable to commit dbTx in syncDB")
	}
	// Begin a new tx.
	w.dbTx, err = w.db.Begin()
	if err != nil {
		w.log.Severe("ERROR: failed to start database update:", err)
		return modules.AddContext(err, "unable to begin new dbTx in syncDB")
	}
	return nil
}

// dbReset wipes and reinitializes a wallet database.
func dbReset(tx *sql.Tx) error {
	_, err := tx.Exec("DELETE FROM wt_addr")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM wt_txn")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM wt_sco")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM wt_sfo")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM wt_spo")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM wt_uc")
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		REPLACE INTO wt_info (id, cc, height, encrypted, sfpool, salt, progress, seed, pwd)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
	`, modules.ConsensusChangeBeginning[:], 0, []byte{}, []byte{}, frand.Bytes(len(walletSalt{})), 0, []byte{}, []byte{})
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM wt_aux")
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM wt_keys")
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM wt_watch")
	return err
}

// dbPutSiacoinOutput inserts a Siacoin output into the database.
func dbPutSiacoinOutput(tx *sql.Tx, id types.SiacoinOutputID, output types.SiacoinOutput) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	output.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("INSERT INTO wt_sco (scoid, bytes) VALUES (?, ?)", id[:], buf.Bytes())
	return err
}

// dbDeleteSiacoinOutput removes a Siacoin output from the database.
func dbDeleteSiacoinOutput(tx *sql.Tx, id types.SiacoinOutputID) error {
	_, err := tx.Exec("DELETE FROM wt_sco WHERE scoid = ?", id[:])
	return err
}

// dbForEachSiacoinOutput performs an action on each Siacoin output.
func dbForEachSiacoinOutput(tx *sql.Tx, fn func(types.SiacoinOutputID, types.SiacoinOutput)) error {
	rows, err := tx.Query("SELECT scoid, bytes FROM wt_sco")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var scoid types.SiacoinOutputID
		var sco types.SiacoinOutput
		id := make([]byte, 32)
		scoBytes := make([]byte, 0, 56)
		if err := rows.Scan(&id, &scoBytes); err != nil {
			return err
		}
		copy(scoid[:], id)
		buf := bytes.NewBuffer(scoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(scoBytes))})
		sco.DecodeFrom(d)
		if err := d.Err(); err != nil {
			return err
		}
		fn(scoid, sco)
	}

	return nil
}

// dbPutSiafundOutput inserts a Siafund output into the database.
func dbPutSiafundOutput(tx *sql.Tx, id types.SiafundOutputID, output types.SiafundOutput) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	output.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("INSERT INTO wt_sfo (sfoid, bytes) VALUES (?, ?)", id[:], buf.Bytes())
	return err
}

// dbDeleteSiafundOutput removes a Siafund output from the database.
func dbDeleteSiafundOutput(tx *sql.Tx, id types.SiafundOutputID) error {
	_, err := tx.Exec("DELETE FROM wt_sfo WHERE sfoid = ?", id[:])
	return err
}

// dbForEachSiafundOutput performs an action on each Siafund output.
func dbForEachSiafundOutput(tx *sql.Tx, fn func(types.SiafundOutputID, types.SiafundOutput, types.Currency)) error {
	rows, err := tx.Query("SELECT sfoid, bytes FROM wt_sfo")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sfoid types.SiafundOutputID
		var sfo types.SiafundOutput
		var claimStart types.Currency
		id := make([]byte, 32)
		sfoBytes := make([]byte, 0, 80)
		if err := rows.Scan(&id, &sfoBytes); err != nil {
			return err
		}
		copy(sfoid[:], id)
		buf := bytes.NewBuffer(sfoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(sfoBytes))})
		var val types.Currency
		val.DecodeFrom(d)
		sfo.Value = val.Lo
		sfo.Address.DecodeFrom(d)
		claimStart.DecodeFrom(d)
		if err := d.Err(); err != nil {
			return err
		}
		fn(sfoid, sfo, claimStart)
	}

	return nil
}

// dbPutSpentOutput inserts a new spent output into the database.
func dbPutSpentOutput(tx *sql.Tx, id types.Hash256, height uint64) error {
	_, err := tx.Exec("INSERT INTO wt_spo (oid, height) VALUES (?, ?)", id[:], height)
	return err
}

// dbGetSpentOutput retrieves a spent output from the database.
func dbGetSpentOutput(tx *sql.Tx, id types.Hash256) (height uint64, err error) {
	err = tx.QueryRow("SELECT height FROM wt_spo WHERE oid = ?", id[:]).Scan(&height)
	return
}

// dbDeleteSpentOutput removes a spent output from the database.
func dbDeleteSpentOutput(tx *sql.Tx, id types.Hash256) error {
	_, err := tx.Exec("DELETE FROM wt_spo WHERE oid = ?", id[:])
	return err
}

// dbPutAddrTransactions inserts a new address-txn mapping.
func dbPutAddrTransactions(tx *sql.Tx, addr types.Address, txns []types.TransactionID) error {
	for _, txid := range txns {
		_, err := tx.Exec("INSERT INTO wt_addr (addr, txid) VALUES (?, ?)", addr[:], txid[:])
		if err != nil {
			return err
		}
	}
	return nil
}

// dbGetAddrTransactions retrieves an address-txn mapping from the database.
func dbGetAddrTransactions(tx *sql.Tx, addr types.Address) (txns []types.TransactionID, err error) {
	rows, err := tx.Query("SELECT txid FROM wt_addr WHERE addr = ?", addr[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var txid types.TransactionID
		id := make([]byte, 32)
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		copy(txid[:], id)
		txns = append(txns, txid)
	}
	return txns, nil
}

// dbPutUnlockConditions adds new UnlockConditions to the database.
func dbPutUnlockConditions(tx *sql.Tx, uc types.UnlockConditions) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	uc.EncodeTo(e)
	e.Flush()
	uh := uc.UnlockHash()
	_, err := tx.Exec("INSERT INTO wt_uc (addr, bytes) VALUES (?, ?)", uh[:], buf.Bytes())
	return err
}

// dbGetUnlockConditions retrieves UnlockConditions from the database.
func dbGetUnlockConditions(tx *sql.Tx, addr types.Address) (uc types.UnlockConditions, err error) {
	var ucBytes []byte
	err = tx.QueryRow("SELECT bytes FROM wt_uc WHERE addr = ?", addr[:]).Scan(&ucBytes)
	if err != nil {
		return
	}
	buf := bytes.NewBuffer(ucBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(ucBytes))})
	uc.DecodeFrom(d)
	return uc, d.Err()
}

// dbAddAddrTransaction appends a single transaction ID to the set of
// transactions associated with addr. If the ID is already in the set, it is
// not added again.
func dbAddAddrTransaction(tx *sql.Tx, addr types.Address, txid types.TransactionID) error {
	var c int
	err := tx.QueryRow("SELECT COUNT(*) FROM wt_addr WHERE addr = ? AND txid = ?", addr[:], txid[:]).Scan(&c)
	if err != nil {
		return err
	}
	if c > 0 {
		return nil
	}
	_, err = tx.Exec("INSERT INTO wt_addr (addr, txid) VALUES (?, ?)", addr[:], txid[:])
	return err
}

// dbAddProcessedTransactionAddrs updates the address-txn mappings to associate
// every address in pt with txid.
func dbAddProcessedTransactionAddrs(tx *sql.Tx, pt modules.ProcessedTransaction) error {
	addrs := make(map[types.Address]struct{})
	for _, input := range pt.Inputs {
		addrs[input.RelatedAddress] = struct{}{}
	}
	for _, output := range pt.Outputs {
		// Miner fees don't have an address, so skip them.
		if output.FundType == specifierMinerFee {
			continue
		}
		addrs[output.RelatedAddress] = struct{}{}
	}
	for addr := range addrs {
		if err := dbAddAddrTransaction(tx, addr, pt.TransactionID); err != nil {
			return modules.AddContext(err, fmt.Sprintf("failed to add txn %v to address %v",
				pt.TransactionID, addr))
		}
	}
	return nil
}

// decodeProcessedTransaction decodes a marshalled ProcessedTransaction.
func decodeProcessedTransaction(ptBytes []byte, pt *modules.ProcessedTransaction) error {
	buf := bytes.NewBuffer(ptBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(ptBytes))})
	pt.DecodeFrom(d)
	return d.Err()
}

// dbAppendProcessedTransaction adds a new ProcessedTransaction.
func dbAppendProcessedTransaction(tx *sql.Tx, pt modules.ProcessedTransaction) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	pt.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("INSERT INTO wt_txn (txid, bytes) VALUES (?, ?)", pt.TransactionID[:], buf.Bytes())
	if err != nil {
		return modules.AddContext(err, "failed to store processed txn in database")
	}

	// Also add this txid to wt_addr.
	if err = dbAddProcessedTransactionAddrs(tx, pt); err != nil {
		return modules.AddContext(err, "failed to add processed transaction to addresses in database")
	}

	return nil
}

// dbGetLastProcessedTransaction retrieves the last ProcessedTransaction.
func dbGetLastProcessedTransaction(tx *sql.Tx) (pt modules.ProcessedTransaction, err error) {
	var ptBytes []byte
	err = tx.QueryRow("SELECT bytes FROM wt_txn ORDER BY id DESC LIMIT 1").Scan(&ptBytes)
	if err != nil {
		return
	}
	err = decodeProcessedTransaction(ptBytes, &pt)
	return
}

// dbDeleteLastProcessedTransaction deletes the last ProcessedTransaction.
func dbDeleteLastProcessedTransaction(tx *sql.Tx) error {
	// Get the last processed txn.
	var txid types.TransactionID
	id := make([]byte, 32)
	err := tx.QueryRow("SELECT txid FROM wt_txn ORDER BY id DESC LIMIT 1").Scan(&id)
	if err != nil {
		return err
	}

	// Delete the associated mappings.
	copy(txid[:], id)
	_, err = tx.Exec("DELETE FROM wt_addr WHERE txid = ?", txid[:])
	if err != nil {
		return err
	}

	// Delete the last processed txn.
	_, err = tx.Exec("DELETE FROM wt_txn WHERE txid = ?", txid[:])
	return err
}

// dbGetProcessedTransaction retrieves a txn from the database.
func dbGetProcessedTransaction(tx *sql.Tx, txid types.TransactionID) (pt modules.ProcessedTransaction, err error) {
	var ptBytes []byte
	err = tx.QueryRow("SELECT bytes FROM wt_txn WHERE txid = ?", txid[:]).Scan(&ptBytes)
	if err != nil {
		return
	}
	err = decodeProcessedTransaction(ptBytes, &pt)
	return
}

// dbGetWalletSalt returns the salt used by the wallet to derive encryption keys.
func dbGetWalletSalt(tx *sql.Tx) (uid walletSalt) {
	salt := make([]byte, 32)
	err := tx.QueryRow("SELECT salt FROM wt_info WHERE id = 1").Scan(&salt)
	if err != nil {
		return
	}
	copy(uid[:], salt)
	return
}

// dbPutWalletSalt saves the salt to disk.
func dbPutWalletSalt(tx *sql.Tx, uid walletSalt) error {
	_, err := tx.Exec("UPDATE wt_info SET salt = ? WHERE id = 1", uid[:])
	return err
}

// dbGetPrimarySeedProgress returns the number of keys generated from the
// primary seed.
func dbGetPrimarySeedProgress(tx *sql.Tx) (progress uint64, err error) {
	err = tx.QueryRow("SELECT progress FROM wt_info WHERE id = 1").Scan(&progress)
	return
}

// dbPutPrimarySeedProgress sets the primary seed progress counter.
func dbPutPrimarySeedProgress(tx *sql.Tx, progress uint64) error {
	_, err := tx.Exec("UPDATE wt_info SET progress = ? WHERE id = 1", progress)
	return err
}

// dbGetConsensusChangeID returns the ID of the last ConsensusChange processed by the wallet.
func dbGetConsensusChangeID(tx *sql.Tx) (cc modules.ConsensusChangeID) {
	ccBytes := make([]byte, 32)
	err := tx.QueryRow("SELECT cc FROM wt_info WHERE id = 1").Scan(&ccBytes)
	if err != nil {
		return modules.ConsensusChangeID{}
	}
	copy(cc[:], ccBytes)
	return
}

// dbPutConsensusChangeID stores the ID of the last ConsensusChange processed by the wallet.
func dbPutConsensusChangeID(tx *sql.Tx, cc modules.ConsensusChangeID) error {
	_, err := tx.Exec("UPDATE wt_info SET cc = ? WHERE id = 1", cc[:])
	return err
}

// dbGetConsensusHeight returns the height that the wallet has scanned to.
func dbGetConsensusHeight(tx *sql.Tx) (height uint64, err error) {
	err = tx.QueryRow("SELECT height FROM wt_info WHERE id = 1").Scan(&height)
	return
}

// dbPutConsensusHeight stores the height that the wallet has scanned to.
func dbPutConsensusHeight(tx *sql.Tx, height uint64) error {
	_, err := tx.Exec("UPDATE wt_info SET height = ? WHERE id = 1", height)
	return err
}

// dbGetSiafundPool returns the value of the Siafund pool.
func dbGetSiafundPool(tx *sql.Tx) (pool types.Currency, err error) {
	poolBytes := make([]byte, 0, 24)
	err = tx.QueryRow("SELECT sfpool FROM wt_info WHERE id = 1").Scan(&poolBytes)
	if err != nil {
		return types.ZeroCurrency, err
	}
	if len(poolBytes) == 0 {
		return types.ZeroCurrency, nil
	}
	d := types.NewDecoder(io.LimitedReader{R: bytes.NewBuffer(poolBytes), N: int64(len(poolBytes))})
	pool.DecodeFrom(d)
	return pool, d.Err()
}

// dbPutSiafundPool stores the value of the Siafund pool.
func dbPutSiafundPool(tx *sql.Tx, pool types.Currency) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	pool.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("UPDATE wt_info SET sfpool = ? WHERE id = 1", buf.Bytes())
	return err
}

// dbGetWatchedAddresses retrieves the set of watched addresses.
func dbGetWatchedAddresses(tx *sql.Tx) (addrs []types.Address, err error) {
	rows, err := tx.Query("SELECT addr FROM wt_watch")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var addr types.Address
		a := make([]byte, 32)
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		copy(addr[:], a)
		addrs = append(addrs, addr)
	}

	return
}

// dbPutWatchedAddresses stores the set of watched addresses.
func dbPutWatchedAddresses(tx *sql.Tx, addrs []types.Address) error {
	for _, addr := range addrs {
		_, err := tx.Exec("REPLACE INTO wt_watch (addr) VALUES (?)", addr[:])
		if err != nil {
			return err
		}
	}
	return nil
}

// dbGetEncryptedVerification returns the encrypted ciphertext.
func dbGetEncryptedVerification(tx *sql.Tx) ([]byte, error) {
	var encrypted []byte
	err := tx.QueryRow("SELECT encrypted FROM wt_info WHERE id = 1").Scan(&encrypted)
	if err != nil {
		return nil, err
	}
	empty := make([]byte, len(encrypted))
	if bytes.Equal(encrypted, empty) {
		return nil, nil
	}
	return encrypted, nil
}

// dbPutEncryptedVerification sets a new encrypted ciphertext.
func dbPutEncryptedVerification(tx *sql.Tx, encrypted []byte) error {
	_, err := tx.Exec("UPDATE wt_info SET encrypted = ? WHERE id = 1", encrypted)
	return err
}

// dbGetPrimarySeed returns the wallet primary seed.
func dbGetPrimarySeed(tx *sql.Tx) (seed encryptedSeed, err error) {
	u := make([]byte, 32)
	var e, s []byte
	err = tx.QueryRow("SELECT salt, encrypted, seed FROM wt_info WHERE id = 1").Scan(&u, &e, &s)
	if err != nil {
		return
	}
	copy(seed.UID[:], u)
	seed.EncryptionVerification = make([]byte, len(e))
	copy(seed.EncryptionVerification, e)
	seed.Seed = make([]byte, len(s))
	copy(seed.Seed, s)
	return
}

// dbPutPrimarySeed saves the wallet primary seed.
func dbPutPrimarySeed(tx *sql.Tx, seed encryptedSeed) error {
	_, err := tx.Exec("UPDATE wt_info SET salt = ?, encrypted = ?, seed = ? WHERE id = 1", seed.UID[:], seed.EncryptionVerification, seed.Seed)
	return err
}

// dbGetWalletPassword returns the wallet password.
func dbGetWalletPassword(tx *sql.Tx) (pwd []byte, err error) {
	err = tx.QueryRow("SELECT pwd FROM wt_info WHERE id = 1").Scan(&pwd)
	return
}

// dbPutWalletPassword saves the wallet password.
func dbPutWalletPassword(tx *sql.Tx, pwd []byte) error {
	_, err := tx.Exec("UPDATE wt_info SET pwd = ? WHERE id = 1", pwd)
	return err
}

// dbGetAuxiliarySeeds retrieves the auxiliary seeds.
func dbGetAuxiliarySeeds(tx *sql.Tx) (seeds []encryptedSeed, err error) {
	rows, err := tx.Query("SELECT salt, encrypted, seed FROM wt_aux")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var seed encryptedSeed
		u := make([]byte, 32)
		var e, s []byte
		if err := rows.Scan(&u, &e, &s); err != nil {
			return nil, err
		}
		copy(seed.UID[:], u)
		seed.EncryptionVerification = make([]byte, len(e))
		copy(seed.EncryptionVerification, e)
		seed.Seed = make([]byte, len(s))
		copy(seed.Seed, s)
		seeds = append(seeds, seed)
	}

	return
}

// dbPutAuxiliarySeeds saves the auxiliary seeds.
func dbPutAuxiliarySeeds(tx *sql.Tx, seeds []encryptedSeed) error {
	_, err := tx.Exec("DELETE FROM wt_aux")
	if err != nil {
		return err
	}
	for _, seed := range seeds {
		_, err := tx.Exec("INSERT INTO wt_aux (salt, encrypted, seed) VALUES (?, ?, ?)", seed.UID[:], seed.EncryptionVerification, seed.Seed)
		if err != nil {
			return err
		}
	}
	return nil
}

// dbGetUnseededKeys retrieves the spendable keys.
func dbGetUnseededKeys(tx *sql.Tx) (keys []encryptedSpendableKey, err error) {
	rows, err := tx.Query("SELECT salt, encrypted, skey FROM wt_keys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sk encryptedSpendableKey
		s := make([]byte, 32)
		var e, k []byte
		if err := rows.Scan(&s, &e, &k); err != nil {
			return nil, err
		}
		copy(sk.Salt[:], s)
		sk.EncryptionVerification = make([]byte, len(e))
		copy(sk.EncryptionVerification, e)
		sk.SpendableKey = make([]byte, len(k))
		copy(sk.SpendableKey, k)
		keys = append(keys, sk)
	}

	return
}

// dbPutUnseededKeys saves the spendable keys.
func dbPutUnseededKeys(tx *sql.Tx, keys []encryptedSpendableKey) error {
	_, err := tx.Exec("DELETE FROM wt_keys")
	if err != nil {
		return err
	}
	for _, key := range keys {
		_, err := tx.Exec("INSERT INTO wt_keys (salt, encrypted, skey) VALUES (?, ?, ?)", key.Salt[:], key.EncryptionVerification, key.SpendableKey)
		if err != nil {
			return err
		}
	}
	return nil
}
