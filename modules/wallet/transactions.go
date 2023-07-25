package wallet

import (
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

var (
	errOutOfBounds = errors.New("requesting transactions at unknown confirmation heights")
)

// AddressTransactions returns all of the wallet transactions associated with a
// single unlock hash.
func (w *Wallet) AddressTransactions(uh types.Address) (pts []modules.ProcessedTransaction, err error) {
	if err := w.tg.Add(); err != nil {
		return []modules.ProcessedTransaction{}, err
	}
	defer w.tg.Done()

	// Ensure durability of reported transactions.
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	txnIndices, _ := dbGetAddrTransactions(w.dbTx, uh)
	for _, i := range txnIndices {
		pt, err := dbGetProcessedTransaction(w.dbTx, i)
		if err != nil {
			continue
		}
		pts = append(pts, pt)
	}
	return pts, nil
}

// AddressUnconfirmedTransactions returns all of the unconfirmed wallet transactions
// related to a specific address.
func (w *Wallet) AddressUnconfirmedTransactions(uh types.Address) (pts []modules.ProcessedTransaction, err error) {
	if err := w.tg.Add(); err != nil {
		return []modules.ProcessedTransaction{}, err
	}
	defer w.tg.Done()

	// Ensure durability of reported transactions.
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	// Scan the full list of unconfirmed transactions to see if there are any
	// related transactions.
	curr := w.unconfirmedProcessedTransactions.head
	for curr != nil {
		pt := curr.txn
		relevant := false
		for _, input := range pt.Inputs {
			if input.RelatedAddress == uh {
				relevant = true
				break
			}
		}
		for _, output := range pt.Outputs {
			if output.RelatedAddress == uh {
				relevant = true
				break
			}
		}
		if relevant {
			pts = append(pts, pt)
		}
		curr = curr.next
	}
	return pts, err
}

// Transaction returns the transaction with the given id. 'False' is returned
// if the transaction does not exist.
func (w *Wallet) Transaction(txid types.TransactionID) (pt modules.ProcessedTransaction, found bool, err error) {
	if err := w.tg.Add(); err != nil {
		return modules.ProcessedTransaction{}, false, err
	}
	defer w.tg.Done()

	// Ensure durability of reported transaction.
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	// Get the txn for the given txid.
	pt, err = dbGetProcessedTransaction(w.dbTx, txid)
	if err != nil {
		curr := w.unconfirmedProcessedTransactions.head
		for curr != nil {
			txn := curr.txn
			if txn.TransactionID == txid {
				return txn, true, nil
			}
			curr = curr.next
		}
		return modules.ProcessedTransaction{}, false, nil
	}

	return pt, true, nil
}

// Transactions returns all transactions relevant to the wallet that were
// confirmed in the range [startHeight, endHeight].
func (w *Wallet) Transactions(startHeight, endHeight uint64) (pts []modules.ProcessedTransaction, err error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()

	// There may be transactions which haven't been saved / committed yet. Sync
	// the database to ensure that any information which gets reported to the
	// user will be persisted through a restart.
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return nil, err
	}

	height, err := dbGetConsensusHeight(w.dbTx)
	if err != nil {
		return
	} else if startHeight > height || startHeight > endHeight {
		return nil, errOutOfBounds
	}

	rows, err := w.dbTx.Query("SELECT bytes FROM wt_txn")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pt modules.ProcessedTransaction
		var ptBytes []byte
		if err = rows.Scan(&ptBytes); err != nil {
			return nil, err
		}

		if err = decodeProcessedTransaction(ptBytes, &pt); err != nil {
			return nil, err
		}

		if pt.ConfirmationHeight > endHeight {
			break
		}

		if pt.ConfirmationHeight >= startHeight {
			pts = append(pts, pt)
		}
	}

	return
}

// ComputeValuedTransactions creates ValuedTransaction from a set of
// ProcessedTransactions.
func ComputeValuedTransactions(pts []modules.ProcessedTransaction, blockHeight uint64) ([]modules.ValuedTransaction, error) {
	// Loop over all transactions and map the id of each contract to the most
	// recent revision within the set.
	revisionMap := make(map[types.FileContractID]types.FileContractRevision)
	for _, pt := range pts {
		for _, rev := range pt.Transaction.FileContractRevisions {
			revisionMap[rev.ParentID] = rev
		}
	}

	sts := make([]modules.ValuedTransaction, 0, len(pts))
	for _, pt := range pts {
		// Determine the value of the transaction assuming that it's a regular
		// transaction.
		var outgoingSiacoins types.Currency
		for _, input := range pt.Inputs {
			if input.FundType == specifierSiacoinInput && input.WalletAddress {
				outgoingSiacoins = outgoingSiacoins.Add(input.Value)
			}
		}
		var incomingSiacoins types.Currency
		for _, output := range pt.Outputs {
			if output.FundType == specifierMinerPayout && output.WalletAddress {
				incomingSiacoins = incomingSiacoins.Add(output.Value)
			}
			if output.FundType == types.SpecifierSiacoinOutput && output.WalletAddress {
				incomingSiacoins = incomingSiacoins.Add(output.Value)
			}
		}

		// Create the txn assuming that it's a regular txn without contracts or
		// revisions.
		st := modules.ValuedTransaction{
			ProcessedTransaction:   pt,
			ConfirmedIncomingValue: incomingSiacoins,
			ConfirmedOutgoingValue: outgoingSiacoins,
		}

		// If the transaction doesn't contain contracts or revisions we are done.
		if len(pt.Transaction.FileContracts) == 0 && len(pt.Transaction.FileContractRevisions) == 0 {
			sts = append(sts, st)
			continue
		}

		// If there are contracts, then there can't be revisions in the
		// transaction.
		if len(pt.Transaction.FileContracts) > 0 {
			// A contract doesn't generate incoming value for the wallet.
			st.ConfirmedIncomingValue = types.ZeroCurrency

			// A contract with a revision doesn't have outgoing value since the
			// outgoing value is determined by the latest revision.
			_, hasRevision := revisionMap[pt.Transaction.FileContractID(0)]
			if hasRevision {
				st.ConfirmedOutgoingValue = types.ZeroCurrency
			}
			sts = append(sts, st)
			continue
		}

		// Else the contract contains a revision.
		rev := pt.Transaction.FileContractRevisions[0]
		latestRev, ok := revisionMap[rev.ParentID]
		if !ok {
			err := errors.New("no revision exists for the provided id which should never happen")
			return nil, err
		}

		// If the revision isn't the latest one, it has neither incoming nor
		// outgoing value.
		if rev.RevisionNumber != latestRev.RevisionNumber {
			st.ConfirmedIncomingValue = types.ZeroCurrency
			st.ConfirmedOutgoingValue = types.ZeroCurrency
			sts = append(sts, st)
			continue
		}

		// It is the latest but if it hasn't reached maturiy height yet we
		// don't count the incoming value.
		if blockHeight <= rev.WindowEnd + modules.MaturityDelay {
			st.ConfirmedIncomingValue = types.ZeroCurrency
			sts = append(sts, st)
			continue
		}

		// Otherwise we leave the incoming and outgoing value fields the way
		// they are.
		sts = append(sts, st)
		continue
	}

	return sts, nil
}

// UnconfirmedTransactions returns the set of unconfirmed transactions that are
// relevant to the wallet.
func (w *Wallet) UnconfirmedTransactions() ([]modules.ProcessedTransaction, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	w.mu.RLock()
	defer w.mu.RUnlock()
	pts := []modules.ProcessedTransaction{}
	curr := w.unconfirmedProcessedTransactions.head
	for curr != nil {
		pts = append(pts, curr.txn)
		curr = curr.next
	}
	return pts, nil
}
