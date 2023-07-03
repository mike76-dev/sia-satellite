package transactionpool

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"
)

const tpoolSyncRate = time.Minute * 2

// threadedRegularSync will make sure that sync gets called on the database
// every once in a while.
func (tp *TransactionPool) threadedRegularSync() {
	if err := tp.tg.Add(); err != nil {
		return
	}
	defer tp.tg.Done()
	for {
		select {
		case <-tp.tg.StopChan():
			// A queued AfterStop will close out the db properly.
			return
		case <-time.After(tpoolSyncRate):
			tp.outerMu.Lock()
			tp.innerMu.Lock()
			tp.syncDB()
			tp.innerMu.Unlock()
			tp.outerMu.Unlock()
		}
	}
}

// syncDB commits the current global transaction and immediately begins a new
// one.
func (tp *TransactionPool) syncDB() {
	// Commit the existing tx.
	err := tp.dbTx.Commit()
	if err != nil {
		tp.log.Severe("ERROR: failed to apply database update:", err)
		tp.dbTx.Rollback()
	}
	// Begin a new tx.
	tp.dbTx, err = tp.db.Begin()
	if err != nil {
		tp.log.Severe("ERROR: failed to initialize a db transaction:", err)
	}
}

// resetDB deletes all consensus related persistence from the transaction pool.
func (tp *TransactionPool) resetDB() error {
	_, err := tp.dbTx.Exec("DELETE FROM tp_ctx")
	if err != nil {
		return err
	}
	err = tp.putRecentBlockID(types.BlockID{})
	if err != nil {
		return err
	}
	err = tp.putRecentConsensusChange(modules.ConsensusChangeBeginning)
	if err != nil {
		return err
	}
	return tp.putBlockHeight(0)
}

// initPersist initializes the database.
func (tp *TransactionPool) initPersist(dir string) error {
	// Create the tpool logger.
	var err error
	tp.log, err = persist.NewFileLogger(filepath.Join(dir, logFile))
	if err != nil {
		return modules.AddContext(err, "unable to initialize the transaction pool logger")
	}
	tp.tg.AfterStop(func() {
		err := tp.log.Close()
		if err != nil {
			fmt.Println("Unable to close the transaction pool logger:", err)
		}
	})

	// Create the global tpool tx that will be used for most persist actions.
	tp.dbTx, err = tp.db.Begin()
	if err != nil {
		return modules.AddContext(err, "unable to begin tpool dbTx")
	}
	tp.tg.AfterStop(func() {
		tp.outerMu.Lock()
		tp.innerMu.Lock()
		err := tp.dbTx.Commit()
		tp.innerMu.Unlock()
		tp.outerMu.Unlock()
		if err != nil {
			tp.log.Println("ERROR: unable to close transaction properly during shutdown:", err)
		}
	})

	// Spin up the thread that occasionally syncrhonizes the database.
	go tp.threadedRegularSync()

	// Get the recent consensus change.
	cc, err := tp.getRecentConsensusChange()
	if modules.ContainsError(err, errNilConsensusChange) {
		err = tp.putRecentConsensusChange(modules.ConsensusChangeBeginning)
	}
	if err != nil {
		return modules.AddContext(err, "unable to initialize the recent consensus change in the tpool")
	}

	// Get the most recent block height.
	bh, err := tp.getBlockHeight()
	if err != nil {
		tp.log.Println("INFO: block height is reporting as zero, setting up to subscribe from the beginning.")
		err = tp.putBlockHeight(0)
		if err != nil {
			return modules.AddContext(err, "unable to initialize the block height in the tpool")
		}
		err = tp.putRecentConsensusChange(modules.ConsensusChangeBeginning)
	} else {
		tp.blockHeight = bh
	}
	if err != nil {
		return modules.AddContext(err, "unable to initialize the block height in the tpool")
	}

	// Get the fee median data.
	mp, err := tp.getFeeMedian()
	if err != nil && !modules.ContainsError(err, errNilFeeMedian) {
		return modules.AddContext(err, "unable to load the fee median")
	}
	// Just leave the fields empty if no fee median was found. They will be
	// filled out.
	if !modules.ContainsError(err, errNilFeeMedian) {
		tp.recentMedians = mp.RecentMedians
		tp.recentMedianFee = mp.RecentMedianFee
	}

	// Subscribe to the consensus set using the most recent consensus change.
	go func() {
		err := tp.consensusSet.ConsensusSetSubscribe(tp, cc, tp.tg.StopChan())
		if modules.ContainsError(err, sync.ErrStopped) {
			return
		}
		if modules.ContainsError(err, modules.ErrInvalidConsensusChangeID) {
			tp.log.Println("WARN: invalid consensus change loaded; resetting. This can take a while.")
			// Reset and rescan because the consensus set does not recognize the
			// provided consensus change id.
			tp.outerMu.Lock()
			tp.innerMu.Lock()
			resetErr := tp.resetDB()
			tp.innerMu.Unlock()
			tp.outerMu.Unlock()
			if resetErr != nil {
				tp.log.Println("CRITICAL: failed to reset tpool", resetErr)
				return
			}
			freshScanErr := tp.consensusSet.ConsensusSetSubscribe(tp, modules.ConsensusChangeBeginning, tp.tg.StopChan())
			if modules.ContainsError(freshScanErr, sync.ErrStopped) {
				return
			}
			if freshScanErr != nil {
				tp.log.Println("CRITICAL: failed to subscribe tpool to consensusset", freshScanErr)
				return
			}
			tp.tg.OnStop(func() {
				tp.consensusSet.Unsubscribe(tp)
			})
			return
		}
		if err != nil {
			tp.log.Println("CRITICAL:", err)
			return
		}
	}()
	tp.tg.OnStop(func() {
		tp.consensusSet.Unsubscribe(tp)
	})
	return nil
}

// TransactionConfirmed returns true if the transaction has been seen on the
// blockchain. Note, however, that the block containing the transaction may
// later be invalidated by a reorg.
func (tp *TransactionPool) TransactionConfirmed(id types.TransactionID) (bool, error) {
	if err := tp.tg.Add(); err != nil {
		return false, modules.AddContext(err, "cannot check transaction status, the transaction pool has closed")
	}
	defer tp.tg.Done()
	tp.outerMu.Lock()
	tp.innerMu.Lock()
	defer func() {
		tp.innerMu.Unlock()
		tp.outerMu.Unlock()
	}()
	return tp.transactionConfirmed(id), nil
}

func (tp *TransactionPool) transactionConfirmed(id types.TransactionID) bool {
	var count int
	err := tp.dbTx.QueryRow("SELECT COUNT(*) from tp_ctx WHERE txid = ?", id[:]).Scan(&count)
	if err != nil {
		tp.log.Println("ERROR: unable to get transaction", err)
		return false
	}
	return count > 0
}
