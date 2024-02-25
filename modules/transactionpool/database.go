package transactionpool

import (
	"bytes"
	"database/sql"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// Errors relating to the database.
var (
	// errNilConsensusChange is returned if there is no consensus change in the
	// database.
	errNilConsensusChange = errors.New("no consensus change found")

	// errNilFeeMedian is the message returned if a database does not find fee
	// median persistence.
	errNilFeeMedian = errors.New("no fee median found")

	// errNilRecentBlock is returned if there is no data stored in
	// fieldRecentBlockID.
	errNilRecentBlock = errors.New("no recent block found in the database")
)

// Complex objects that get stored in database fields.
type (
	// medianPersist is the object that gets stored in the database so that
	// the transaction pool can persist its block based fee estimations.
	medianPersist struct {
		RecentMedians   []types.Currency
		RecentMedianFee types.Currency
	}
)

// EncodeTo implements types.EncoderTo.
func (mp *medianPersist) EncodeTo(e *types.Encoder) {
	e.WritePrefix(len(mp.RecentMedians))
	for _, rm := range mp.RecentMedians {
		rm.EncodeTo(e)
	}
	mp.RecentMedianFee.EncodeTo(e)
}

// DecodeFrom implements types.DecoderFrom.
func (mp *medianPersist) DecodeFrom(d *types.Decoder) {
	mp.RecentMedians = make([]types.Currency, d.ReadPrefix())
	for i := 0; i < len(mp.RecentMedians); i++ {
		mp.RecentMedians[i].DecodeFrom(d)
	}
	mp.RecentMedianFee.DecodeFrom(d)
}

// deleteTransaction deletes a transaction from the list of confirmed
// transactions.
func (tp *TransactionPool) deleteTransaction(id types.TransactionID) error {
	_, err := tp.dbTx.Exec("DELETE FROM tp_ctx WHERE txid = ?", id[:])
	return err
}

// getBlockHeight returns the most recent block height from the database.
func (tp *TransactionPool) getBlockHeight() (bh uint64, err error) {
	err = tp.dbTx.QueryRow("SELECT height FROM tp_height WHERE id = 1").Scan(&bh)
	return
}

// getFeeMedian will get the fee median struct stored in the database.
func (tp *TransactionPool) getFeeMedian() (medianPersist, error) {
	var medianBytes []byte
	err := tp.dbTx.QueryRow("SELECT bytes FROM tp_median WHERE id = 1").Scan(&medianBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return medianPersist{}, errNilFeeMedian
	}
	if err != nil {
		return medianPersist{}, err
	}

	var mp medianPersist
	d := types.NewBufDecoder(medianBytes)
	mp.DecodeFrom(d)
	if err := d.Err(); err != nil {
		return medianPersist{}, modules.AddContext(err, "unable to unmarshal median data")
	}
	return mp, nil
}

// getRecentBlockID will fetch the most recent block id and most recent parent
// id from the database.
func (tp *TransactionPool) getRecentBlockID() (recentID types.BlockID, err error) {
	idBytes := make([]byte, 32)
	err = tp.dbTx.QueryRow("SELECT bid FROM tp_recent WHERE id = 1").Scan(&idBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return types.BlockID{}, errNilRecentBlock
	}
	if err != nil {
		return types.BlockID{}, err
	}
	copy(recentID[:], idBytes[:])
	if recentID == (types.BlockID{}) {
		return types.BlockID{}, errNilRecentBlock
	}
	return recentID, nil
}

// getRecentConsensusChange returns the most recent consensus change from the
// database.
func (tp *TransactionPool) getRecentConsensusChange() (cc modules.ConsensusChangeID, err error) {
	ccBytes := make([]byte, 32)
	err = tp.dbTx.QueryRow("SELECT ceid FROM tp_cc WHERE id = 1").Scan(&ccBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return modules.ConsensusChangeID{}, errNilConsensusChange
	}
	if err != nil {
		return modules.ConsensusChangeID{}, err
	}
	copy(cc[:], ccBytes)
	return cc, nil
}

// putBlockHeight updates the transaction pool's block height.
func (tp *TransactionPool) putBlockHeight(height uint64) error {
	tp.blockHeight = height
	_, err := tp.dbTx.Exec(`
		INSERT INTO tp_height (id, height) VALUES (1, ?) AS new
		ON DUPLICATE KEY UPDATE height = new.height
	`, height)
	return err
}

// putFeeMedian puts a median fees object into the database.
func (tp *TransactionPool) putFeeMedian(mp medianPersist) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	mp.EncodeTo(e)
	e.Flush()
	_, err := tp.dbTx.Exec(`
		INSERT INTO tp_median (id, bytes) VALUES (1, ?) AS new
		ON DUPLICATE KEY UPDATE bytes = new.bytes
	`, buf.Bytes())
	return err
}

// putRecentBlockID will store the most recent block id and the parent id of
// that block in the database.
func (tp *TransactionPool) putRecentBlockID(recentID types.BlockID) error {
	_, err := tp.dbTx.Exec(`
		INSERT INTO tp_recent (id, bid) VALUES (1, ?) AS new
		ON DUPLICATE KEY UPDATE bid = new.bid
	`, recentID[:])
	return err
}

// putRecentConsensusChange updates the most recent consensus change seen by
// the transaction pool.
func (tp *TransactionPool) putRecentConsensusChange(cc modules.ConsensusChangeID) error {
	_, err := tp.dbTx.Exec(`
		INSERT INTO tp_cc (id, ceid) VALUES (1, ?) AS new
		ON DUPLICATE KEY UPDATE ceid = new.ceid
	`, cc[:])
	return err
}

// putTransaction adds a transaction to the list of confirmed transactions.
func (tp *TransactionPool) putTransaction(id types.TransactionID) error {
	_, err := tp.dbTx.Exec("REPLACE INTO tp_ctx (txid) VALUES (?)", id[:])
	return err
}
