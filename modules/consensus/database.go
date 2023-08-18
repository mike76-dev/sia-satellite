package consensus

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// The address of the devs.
var (
	oldDevAddr = types.Address{125, 12, 68, 247, 102, 78, 45, 52, 229, 62, 253, 224, 102, 26, 111, 98, 142, 201, 38, 71, 133, 174, 142, 60, 215, 201, 115, 232, 209, 144, 195, 201}
	newDevAddr = types.Address{243, 113, 199, 11, 206, 158, 184, 151, 156, 213, 9, 159, 89, 158, 196, 228, 252, 177, 78, 10, 252, 243, 31, 151, 145, 224, 62, 100, 150, 164, 192, 179}
)

// errRepeatInsert is used when a duplicate field is found in the database.
var errRepeatInsert = errors.New("attempting to add an already existing item to the consensus set")

// markInconsistency flags the database to indicate that inconsistency has been
// detected.
func markInconsistency(tx *sql.Tx) error {
	// Place a 'true' in the consistency bucket to indicate that
	// inconsistencies have been found.
	_, err := tx.Exec("REPLACE INTO cs_consistency (id, inconsistency) VALUES (1, TRUE)")
	if err != nil {
		return err
	}

	return nil
}

// initDB creates a database with sane initial values.
func (cs *ConsensusSet) initDB(tx *sql.Tx) error {
	// If the database has already been initialized, there is nothing to do.
	// Initialization can be detected by looking for the presence of the Siafund
	// pool bucket. (legacy design choice - ultimately probably not the best way
	// to tell).
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM cs_sfpool").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Create the components of the database.
	err = cs.createConsensusDB(tx)
	if err != nil {
		return err
	}
	err = cs.createChangeLog(tx)
	if err != nil {
		return err
	}

	// Place a 'false' in the consistency bucket to indicate that no
	// inconsistencies have been found.
	_, err = tx.Exec("REPLACE INTO cs_consistency (id, inconsistency) VALUES (1, FALSE)")
	if err != nil {
		return err
	}

	// Init Oak field.
	_, err = tx.Exec("REPLACE INTO cs_oak_init (id, init) VALUES (1, FALSE)")
	if err != nil {
		return err
	}

	return nil
}

// createConsensusDB initializes the consensus portions of the database.
func (cs *ConsensusSet) createConsensusDB(tx *sql.Tx) error {
	// Update the Siacoin output diffs map for the genesis block on disk. This
	// needs to happen between the database being opened/initialized and the
	// consensus set hash being calculated.
	for _, scod := range cs.blockRoot.SiacoinOutputDiffs {
		err := commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	// Set the Siafund pool to 0.
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	types.NewCurrency64(0).EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("REPLACE INTO cs_sfpool (id, bytes) VALUES (1, ?)", buf.Bytes())
	if err != nil {
		return err
	}

	// Update the Siafund output diffs map for the genesis block on disk. This
	// needs to happen between the database being opened/initialized and the
	// consensus set hash being calculated.
	for _, sfod := range cs.blockRoot.SiafundOutputDiffs {
		err := commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	// Add the miner payout from the genesis block to the delayed siacoin
	// outputs - unspendable, as the unlock hash is blank.
	err = addDSCO(tx, modules.MaturityDelay, cs.blockRoot.Block.ID().MinerOutputID(0), types.SiacoinOutput{
		Value:   modules.CalculateCoinbase(0),
		Address: types.Address{},
	})
	if err != nil {
		return err
	}

	// Add the genesis block to the block structures.
	err = pushPath(tx, cs.blockRoot.Block.ID())
	if err != nil {
		return err
	}

	return addBlock(tx, &cs.blockRoot)
}

// blockHeight returns the height of the blockchain.
func blockHeight(tx *sql.Tx) uint64 {
	var height uint64
	err := tx.QueryRow("SELECT height FROM cs_height WHERE id = 1").Scan(&height)
	if err != nil {
		return 0
	}
	return height
}

// currentBlockID returns the id of the most recent block in the consensus set.
func currentBlockID(tx *sql.Tx) types.BlockID {
	id, err := getBlockAtHeight(tx, blockHeight(tx))
	if err != nil {
		return types.BlockID{}
	}
	return id
}

// currentProcessedBlock retrieves the most recent processed block.
func currentProcessedBlock(tx *sql.Tx) *processedBlock {
	pb, exists, err := findBlockByID(tx, currentBlockID(tx))
	if err != nil || !exists {
		return nil
	}
	return pb
}

// findBlockByID tries to find a block with the given ID in the consensus set.
func findBlockByID(tx *sql.Tx, id types.BlockID) (*processedBlock, bool, error) {
	var pbBytes []byte
	err := tx.QueryRow("SELECT bytes FROM cs_map WHERE bid = ?", id[:]).Scan(&pbBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	pb := new(processedBlock)
	buf := bytes.NewBuffer(pbBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(pbBytes))})
	pb.DecodeFrom(d)
	return pb, true, d.Err()
}

// getParentID is a convenience method for retrieving only some fields
// from the block.
func getParentID(tx *sql.Tx, id types.BlockID) (parentID types.BlockID, timestamp time.Time, err error) {
	var pbBytes []byte
	err = tx.QueryRow("SELECT bytes FROM cs_map WHERE bid = ?", id[:]).Scan(&pbBytes)
	if err != nil {
		return
	}
	copy(parentID[:], pbBytes[:32])
	ts := binary.LittleEndian.Uint64(pbBytes[40:48])
	return parentID, time.Unix(int64(ts), 0), nil
}

// addBlock adds the processed block to the block map.
func addBlock(tx *sql.Tx, pb *processedBlock) error {
	id := pb.Block.ID()
	return saveBlock(tx, id, pb)
}

// saveBlock adds the processed block under the given ID.
func saveBlock(tx *sql.Tx, id types.BlockID, pb *processedBlock) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	pb.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec(`
		INSERT INTO cs_map (bid, bytes) VALUES (?, ?) AS new
		ON DUPLICATE KEY UPDATE bytes = new.bytes
	`, id[:], buf.Bytes())
	return err
}

// getBlockAtHeight retrieves the ID of the block at the given height.
func getBlockAtHeight(tx *sql.Tx, height uint64) (bid types.BlockID, err error) {
	id := make([]byte, 32)
	err = tx.QueryRow("SELECT bid FROM cs_path WHERE height = ?", height).Scan(&id)
	if err != nil {
		return types.BlockID{}, err
	}

	copy(bid[:], id[:])
	return
}

// pushPath adds a block to the block path at current height + 1.
func pushPath(tx *sql.Tx, bid types.BlockID) error {
	// Update the block height.
	_, err := tx.Exec(`
		INSERT INTO cs_height (id, height) VALUES (1, 0)
		ON DUPLICATE KEY UPDATE height = height + 1
	`)
	if err != nil {
		return err
	}

	// Add the block to the block path.
	_, err = tx.Exec(`
		INSERT INTO cs_path (height, bid) VALUES (
		(SELECT height FROM cs_height WHERE id = 1), ?)
	`, bid[:])

	return err
}

// popPath removes a block from the "end" of the chain, i.e. the block
// with the largest height.
func popPath(tx *sql.Tx) error {
	// Fetch and update the block height.
	_, err := tx.Exec("UPDATE cs_height SET height = height - 1 WHERE id = 1")
	if err != nil {
		return err
	}

	// Remove the block from the path.
	_, err = tx.Exec(`
		DELETE FROM cs_path WHERE height = (
		SELECT height FROM cs_height WHERE id = 1) + 1
	`)
	return err
}

// isSiacoinOutput returns true if there is a siacoin output of that id in the
// database.
func isSiacoinOutput(tx *sql.Tx, id types.SiacoinOutputID) bool {
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM cs_sco WHERE scoid = ?", id[:]).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// saveConsensusChange stores the consensus change node in the database.
func saveConsensusChange(tx *sql.Tx, ceid modules.ConsensusChangeID, cn changeNode) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	cn.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec(`
		INSERT INTO cs_cl (ceid, bytes) VALUES (?, ?) AS new
		ON DUPLICATE KEY UPDATE bytes = new.bytes
	`, ceid[:], buf.Bytes())
	return err
}

// loadConsensusChange retrieves the consensus change node from the database.
func loadConsensusChange(tx *sql.Tx, ceid modules.ConsensusChangeID) (cn changeNode, err error) {
	cnBytes := make([]byte, 0, 1024)
	err = tx.QueryRow("SELECT bytes FROM cs_cl WHERE ceid = ?", ceid[:]).Scan(&cnBytes)
	if err != nil {
		return
	}
	buf := bytes.NewBuffer(cnBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(cnBytes))})
	cn.DecodeFrom(d)
	return cn, d.Err()
}

// changeLogTailID retrieves the ID of the most recent consensus change.
func changeLogTailID(tx *sql.Tx) (tailID modules.ConsensusChangeID) {
	id := make([]byte, 32)
	err := tx.QueryRow("SELECT bytes FROM cs_changelog WHERE id = 1").Scan(&id)
	if err != nil {
	}
	copy(tailID[:], id[:])
	return
}

// setChangeLogTailID updates the ID of the most recent consensus change.
func setChangeLogTailID(tx *sql.Tx, ceid modules.ConsensusChangeID) (err error) {
	_, err = tx.Exec("REPLACE INTO cs_changelog (id, bytes) VALUES (1, ?)", ceid[:])
	return
}

// countDelayedSiacoins returns the sum of all delayed Siacoin outputs.
func countDelayedSiacoins(tx *sql.Tx) (total types.Currency, err error) {
	rows, err := tx.Query("SELECT bytes FROM cs_dsco")
	if err != nil {
		return
	}

	for rows.Next() {
		var dsco types.SiacoinOutput
		dscoBytes := make([]byte, 0, 56)
		if err = rows.Scan(&dscoBytes); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		buf := bytes.NewBuffer(dscoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(dscoBytes))})
		dsco.DecodeFrom(d)
		if err = d.Err(); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		total = total.Add(dsco.Value)
	}
	rows.Close()

	return
}

// countSiacoins returns the sum of all Siacoin outputs.
func countSiacoins(tx *sql.Tx) (total types.Currency, err error) {
	rows, err := tx.Query("SELECT bytes FROM cs_sco")
	if err != nil {
		return
	}

	for rows.Next() {
		var sco types.SiacoinOutput
		scoBytes := make([]byte, 0, 56)
		if err = rows.Scan(&scoBytes); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		buf := bytes.NewBuffer(scoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(scoBytes))})
		sco.DecodeFrom(d)
		if err = d.Err(); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		total = total.Add(sco.Value)
	}
	rows.Close()

	return
}

// countFileContractPayouts returns the sum of all file contract payouts.
func countFileContractPayouts(tx *sql.Tx) (total types.Currency, err error) {
	rows, err := tx.Query("SELECT bytes FROM cs_fc")
	if err != nil {
		return
	}

	for rows.Next() {
		var fc types.FileContract
		var fcBytes []byte
		if err = rows.Scan(&fcBytes); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		buf := bytes.NewBuffer(fcBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(fcBytes))})
		fc.DecodeFrom(d)
		if err = d.Err(); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		var fcCoins types.Currency
		for _, output := range fc.ValidProofOutputs {
			fcCoins = fcCoins.Add(output.Value)
		}
		total = total.Add(fcCoins)
	}
	rows.Close()

	return
}

// countSiafundClaims returns the sum of all Siacoins from Siafund claims.
func countSiafundClaims(tx *sql.Tx) (total types.Currency, err error) {
	coinsPerFund := getSiafundPool(tx)
	rows, err := tx.Query("SELECT bytes FROM cs_sfo")
	if err != nil {
		return
	}

	for rows.Next() {
		sfoBytes := make([]byte, 0, 80)
		if err = rows.Scan(&sfoBytes); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		buf := bytes.NewBuffer(sfoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(sfoBytes))})
		var value, claimStart types.Currency
		value.DecodeFrom(d)
		(&types.Address{}).DecodeFrom(d)
		claimStart.DecodeFrom(d)
		if err = d.Err(); err != nil {
			rows.Close()
			return types.ZeroCurrency, err
		}
		claimCoins := coinsPerFund.Sub(claimStart).Mul(value).Div64(modules.SiafundCount)
		total = total.Add(claimCoins)
	}
	rows.Close()

	return
}

// countSiafunds returns the sum of all Siafund outputs.
func countSiafunds(tx *sql.Tx) (total uint64, err error) {
	rows, err := tx.Query("SELECT bytes FROM cs_sfo")
	if err != nil {
		return
	}

	for rows.Next() {
		var sfo types.SiafundOutput
		sfoBytes := make([]byte, 0, 80)
		if err = rows.Scan(&sfoBytes); err != nil {
			rows.Close()
			return 0, err
		}
		buf := bytes.NewBuffer(sfoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(sfoBytes))})
		sfo.DecodeFrom(d)
		if err = d.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		total = total + sfo.Value
	}
	rows.Close()

	return
}

// findSiacoinOutput tries to find a Siacoin output with the given ID in the
// consensus set.
func findSiacoinOutput(tx *sql.Tx, scoid types.SiacoinOutputID) (sco types.SiacoinOutput, exists bool, err error) {
	scoBytes := make([]byte, 0, 56)
	err = tx.QueryRow("SELECT bytes FROM cs_sco WHERE scoid = ?", scoid[:]).Scan(&scoBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return types.SiacoinOutput{}, false, nil
	}
	if err != nil {
		return types.SiacoinOutput{}, false, err
	}
	buf := bytes.NewBuffer(scoBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(scoBytes))})
	sco.DecodeFrom(d)
	return sco, true, d.Err()
}

// addSiacoinOutput adds a Siacoin output to the database. An error is returned
// if the Siacoin output is already in the database.
func addSiacoinOutput(tx *sql.Tx, id types.SiacoinOutputID, sco types.SiacoinOutput) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	sco.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("REPLACE INTO cs_sco (scoid, bytes) VALUES (?, ?)", id[:], buf.Bytes())
	return err
}

// removeSiacoinOutput removes a Siacoin output from the database. An error is
// returned if the Siacoin output is not in the database prior to removal.
func removeSiacoinOutput(tx *sql.Tx, id types.SiacoinOutputID) error {
	_, err := tx.Exec("DELETE FROM cs_sco WHERE scoid = ?", id[:])
	return err
}

// findSiafundOutput tries to find a Siafund output with the given ID in the
// consensus set.
func findSiafundOutput(tx *sql.Tx, sfoid types.SiafundOutputID) (sfo types.SiafundOutput, claimStart types.Currency, exists bool, err error) {
	sfoBytes := make([]byte, 0, 80)
	err = tx.QueryRow("SELECT bytes FROM cs_sfo WHERE sfoid = ?", sfoid[:]).Scan(&sfoBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return types.SiafundOutput{}, types.Currency{}, false, nil
	}
	if err != nil {
		return types.SiafundOutput{}, types.Currency{}, false, err
	}
	buf := bytes.NewBuffer(sfoBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(sfoBytes))})
	var val types.Currency
	val.DecodeFrom(d)
	sfo.Value = val.Lo
	sfo.Address.DecodeFrom(d)
	claimStart.DecodeFrom(d)
	err = d.Err()
	if err == nil && blockHeight(tx) > modules.DevAddrHardforkHeight && sfo.Address == oldDevAddr {
		sfo.Address = newDevAddr
	}
	return sfo, claimStart, true, err
}

// addSiafundOutput adds a Siafund output to the database. An error is returned
// if the Siafund output is already in the database.
func addSiafundOutput(tx *sql.Tx, id types.SiafundOutputID, sfo types.SiafundOutput, claimStart types.Currency) error {
	if sfo.Value == 0 {
		return errors.New("zero value Siafund being added")
	}
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	types.NewCurrency64(sfo.Value).EncodeTo(e)
	sfo.Address.EncodeTo(e)
	claimStart.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("REPLACE INTO cs_sfo (sfoid, bytes) VALUES (?, ?)", id[:], buf.Bytes())
	return err
}

// removeSiafundOutput removes a Siafund output from the database. An error is
// returned if the Siafund output is not in the database prior to removal.
func removeSiafundOutput(tx *sql.Tx, id types.SiafundOutputID) error {
	_, err := tx.Exec("DELETE FROM cs_sfo WHERE sfoid = ?", id[:])
	return err
}

// findFileContract tries to find a file contract with the given ID in the
// consensus set.
func findFileContract(tx *sql.Tx, fcid types.FileContractID) (fc types.FileContract, exists bool, err error) {
	var fcBytes []byte
	err = tx.QueryRow("SELECT bytes FROM cs_fc WHERE fcid = ?", fcid[:]).Scan(&fcBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return types.FileContract{}, false, nil
	}
	if err != nil {
		return types.FileContract{}, false, err
	}
	buf := bytes.NewBuffer(fcBytes)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(fcBytes))})
	fc.DecodeFrom(d)
	return fc, true, d.Err()
}

// addFileContract adds a file contract to the database. An error is returned
// if the file contract is already in the database.
func addFileContract(tx *sql.Tx, id types.FileContractID, fc types.FileContract) error {
	// Sanity check - should not be adding a zero-payout file contract.
	if fc.Payout.IsZero() {
		return errors.New("adding zero-payout file contract")
	}

	// Add the file contract to the database.
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	fc.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("REPLACE INTO cs_fc (fcid, bytes) VALUES (?, ?)", id[:], buf.Bytes())
	if err != nil {
		return err
	}

	// Add an entry for when the file contract expires.
	_, err = tx.Exec("REPLACE INTO cs_fcex (height, fcid, bytes) VALUES (?, ?, ?)", fc.WindowEnd, id[:], []byte{})
	return err
}

// removeFileContract removes a file contract from the database.
func removeFileContract(tx *sql.Tx, id types.FileContractID) error {
	// Delete the file contract entry.
	_, err := tx.Exec("DELETE FROM cs_fc WHERE fcid = ?", id[:])
	if err != nil {
		return err
	}

	// Delete the entry for the file contract's expiration.
	_, err = tx.Exec("DELETE FROM cs_fcex WHERE fcid = ?", id[:])
	return err
}

// getSiafundPool returns the current value of the Siafund pool. No error is
// returned as the Siafund pool should always be available.
func getSiafundPool(tx *sql.Tx) (pool types.Currency) {
	poolBytes := make([]byte, 0, 24)
	err := tx.QueryRow("SELECT bytes FROM cs_sfpool WHERE id = 1").Scan(&poolBytes)
	if err != nil {
		return types.ZeroCurrency
	}

	d := types.NewDecoder(io.LimitedReader{R: bytes.NewBuffer(poolBytes), N: int64(len(poolBytes))})
	pool.DecodeFrom(d)
	if err := d.Err(); err != nil {
		return types.ZeroCurrency
	}

	return pool
}

// setSiafundPool updates the saved Siafund pool in the database.
func setSiafundPool(tx *sql.Tx, c types.Currency) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	c.EncodeTo(e)
	e.Flush()
	_, err := tx.Exec("REPLACE INTO cs_sfpool (id, bytes) VALUES (1, ?)", buf.Bytes())
	return err
}

// getFoundationUnlockHashes returns the current primary and failsafe Foundation
// addresses.
func getFoundationUnlockHashes(tx *sql.Tx) (primary, failsafe types.Address, err error) {
	fuhBytes := make([]byte, 64)
	err = tx.QueryRow("SELECT bytes FROM cs_fuh_current WHERE id = 1").Scan(&fuhBytes)
	if err != nil {
		return types.Address{}, types.Address{}, err
	}
	copy(primary[:], fuhBytes[:32])
	copy(failsafe[:], fuhBytes[32:])
	return primary, failsafe, nil
}

// setFoundationUnlockHashes updates the primary and failsafe Foundation
// addresses.
func setFoundationUnlockHashes(tx *sql.Tx, primary, failsafe types.Address) error {
	fuhBytes := make([]byte, 64)
	copy(fuhBytes[:32], primary[:])
	copy(fuhBytes[32:], failsafe[:])
	_, err := tx.Exec("REPLACE INTO cs_fuh_current (id, bytes) VALUES (1, ?)", fuhBytes)
	return err
}

// getPriorFoundationUnlockHashes returns the primary and failsafe Foundation
// addresses immediately prior to the application of the specified block.
func getPriorFoundationUnlockHashes(tx *sql.Tx, height uint64) (primary, failsafe types.Address, exists bool, err error) {
	fuhBytes := make([]byte, 64)
	err = tx.QueryRow("SELECT bytes FROM cs_fuh WHERE height = ?", height).Scan(&fuhBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Address{}, types.Address{}, false, nil
	}
	if err != nil {
		return types.Address{}, types.Address{}, false, err
	}
	copy(primary[:], fuhBytes[:32])
	copy(failsafe[:], fuhBytes[32:])
	return primary, failsafe, true, nil
}

// setPriorFoundationUnlockHashes sets the primary and failsafe Foundation
// addresses immediately prior to the application of the specified block.
func setPriorFoundationUnlockHashes(tx *sql.Tx, height uint64) error {
	fuhBytes := make([]byte, 64)
	err := tx.QueryRow("SELECT bytes FROM cs_fuh_current WHERE id = 1").Scan(&fuhBytes)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		REPLACE INTO cs_fuh (height, bytes) VALUES (?, ?)
	`, height, fuhBytes)
	return err
}

// deletePriorFoundationUnlockHashes deletes the primary and failsafe Foundation
// addresses for the specified height.
func deletePriorFoundationUnlockHashes(tx *sql.Tx, height uint64) error {
	_, err := tx.Exec("DELETE FROM cs_fuh WHERE height = ?", height)
	return err
}

// addDSCO adds a delayed Siacoin output to the consnesus set.
func addDSCO(tx *sql.Tx, bh uint64, id types.SiacoinOutputID, sco types.SiacoinOutput) error {
	// Sanity check - output should not already be in the full set of outputs.
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM cs_sco WHERE scoid = ?", id[:]).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("dsco already in output set")
	}

	// Sanity check - should not be adding an item already in the db.
	err = tx.QueryRow("SELECT COUNT(*) FROM cs_dsco WHERE scoid = ?", id[:]).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return errRepeatInsert
	}

	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	sco.EncodeTo(e)
	e.Flush()
	_, err = tx.Exec("REPLACE INTO cs_dsco (height, scoid, bytes) VALUES (?, ?, ?)", bh, id[:], buf.Bytes())

	return err
}

// removeDSCO removes a delayed siacoin output from the consensus set.
func removeDSCO(tx *sql.Tx, bh uint64, id types.SiacoinOutputID) error {
	_, err := tx.Exec("DELETE FROM cs_dsco WHERE height = ? AND scoid = ?", bh, id[:])
	return err
}

// checkDoSBlock checks if the block is a known DoS block.
func checkDoSBlock(tx *sql.Tx, id types.BlockID) (known bool, err error) {
	var count int
	err = tx.QueryRow("SELECT COUNT(*) FROM cs_dos WHERE bid = ?", id[:]).Scan(&count)
	if err != nil {
	}
	return count > 0, err
}

// addDoSBlock adds the block to known blocks list.
func addDoSBlock(tx *sql.Tx, id types.BlockID) error {
	_, err := tx.Exec(`
		REPLACE INTO cs_dos (bid) VALUES (?)
	`, id[:])
	return err
}
