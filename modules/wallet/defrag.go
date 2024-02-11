package wallet

import (
	"errors"
	"sort"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
)

var (
	errDefragNotNeeded = errors.New("defragging not needed, wallet is already sufficiently defragged")
	errDustOutput      = errors.New("output is too small")
	errSpentOutput     = errors.New("output is already spent")
)

const (
	defragStartIndex = 10
	defragBatchSize  = 35
)

// managedCreateDefragTransaction creates a transaction that spends multiple existing
// wallet outputs into a single new address.
func (w *Wallet) managedCreateDefragTransaction() (_ []types.Transaction, err error) {
	dustThreshold := w.DustThreshold()
	fee := w.cm.RecommendedFee()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Collect a value-sorted set of Siacoin outputs.
	var so sortedOutputs
	for _, sce := range w.sces {
		if w.checkOutput(sce, dustThreshold) == nil {
			so.ids = append(so.ids, types.SiacoinOutputID(sce.ID))
			so.outputs = append(so.outputs, sce.SiacoinOutput)
		}
	}
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(so))

	// Only defrag if there are enough outputs to merit defragging.
	if len(so.ids) <= 50 {
		return nil, errDefragNotNeeded
	}

	// Skip over the 'defragStartIndex' largest outputs, so that the user can
	// still reasonably use their wallet while the defrag is happening.
	var amount types.Currency
	var parentTxn types.Transaction
	var spentScoids []types.SiacoinOutputID
	for i := defragStartIndex; i < defragStartIndex+defragBatchSize; i++ {
		scoid := so.ids[i]
		sco := so.outputs[i]

		// Add a Siacoin input for this output.
		outputUnlockConditions := types.StandardUnlockConditions(w.keys[sco.Address].PublicKey())
		sci := types.SiacoinInput{
			ParentID:         scoid,
			UnlockConditions: outputUnlockConditions,
		}
		parentTxn.SiacoinInputs = append(parentTxn.SiacoinInputs, sci)
		spentScoids = append(spentScoids, scoid)

		// Add the output to the total fund.
		amount = amount.Add(sco.Value)
	}

	// Create and add the output that will be used to fund the defrag
	// transaction.
	parentUnlockConditions, err := w.nextAddress()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			w.markAddressUnused(parentUnlockConditions)
		}
	}()
	exactOutput := types.SiacoinOutput{
		Value:   amount,
		Address: parentUnlockConditions.UnlockHash(),
	}
	parentTxn.SiacoinOutputs = append(parentTxn.SiacoinOutputs, exactOutput)

	// Sign all of the inputs to the parent transaction.
	for i, sci := range parentTxn.SiacoinInputs {
		parentTxn.Signatures = append(parentTxn.Signatures, types.TransactionSignature{
			ParentID:       types.Hash256(sci.ParentID),
			CoveredFields:  types.CoveredFields{WholeTransaction: true},
			PublicKeyIndex: uint64(i),
		})
		SignTransaction(w.cm.TipState(), &parentTxn, i, w.keys[sci.UnlockConditions.UnlockHash()])
	}

	// Create the defrag transaction.
	refundAddr, err := w.nextAddress()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			w.markAddressUnused(refundAddr)
		}
	}()

	// Compute the transaction fee.
	sizeAvgOutput := uint64(250)
	fee = fee.Mul64(sizeAvgOutput * defragBatchSize)

	txn := types.Transaction{
		SiacoinInputs: []types.SiacoinInput{{
			ParentID:         parentTxn.SiacoinOutputID(0),
			UnlockConditions: parentUnlockConditions,
		}},
		SiacoinOutputs: []types.SiacoinOutput{{
			Value:   amount.Sub(fee),
			Address: refundAddr.UnlockHash(),
		}},
		MinerFees: []types.Currency{fee},
	}
	txn.Signatures = append(txn.Signatures, types.TransactionSignature{
		ParentID:      types.Hash256(parentTxn.SiacoinOutputID(0)),
		CoveredFields: types.CoveredFields{WholeTransaction: true},
	})
	SignTransaction(w.cm.TipState(), &txn, 0, w.keys[parentUnlockConditions.UnlockHash()])

	// Mark all outputs that were spent as spent.
	for _, scoid := range spentScoids {
		if err := w.insertSpentOutput(types.Hash256(scoid)); err != nil {
			return nil, err
		}

	}

	// Mark the parent output as spent. Must be done after the transaction is
	// finished because otherwise the txid and output id will change.
	if err := w.insertSpentOutput(types.Hash256(parentTxn.SiacoinOutputID(0))); err != nil {
		return nil, err
	}

	// Construct the final transaction set.
	return []types.Transaction{parentTxn, txn}, nil
}

// threadedDefragWallet computes the sum of the 15 largest outputs in the wallet and
// sends that sum to itself, effectively defragmenting the wallet. This defrag
// operation is only performed if the wallet has greater than defragThreshold
// outputs.
func (w *Wallet) threadedDefragWallet() {
	err := w.tg.Add()
	if err != nil {
		return
	}
	defer w.tg.Done()

	// Create the defrag transaction.
	txnSet, err := w.managedCreateDefragTransaction()
	defer func() {
		if err == nil {
			return
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, txn := range txnSet {
			for _, sci := range txn.SiacoinInputs {
				if err := w.removeSpentOutput(types.Hash256(sci.ParentID)); err != nil {
					w.log.Error("couldn't remove spent output", zap.Error(err))
				}
			}
		}
	}()
	if modules.ContainsError(err, errDefragNotNeeded) {
		// Begin.
		return
	} else if err != nil {
		w.log.Warn("couldn't create defrag transaction", zap.Error(err))
		return
	}

	// Submit the defrag to the transaction pool.
	_, err = w.cm.AddPoolTransactions(txnSet)
	if err != nil {
		w.Release(txnSet)
		w.log.Error("invalid transaction set", zap.Error(err))
		return
	}
	w.s.BroadcastTransactionSet(txnSet)
	w.log.Info("submitting a transaction set to defragment the wallet's outputs")
}
