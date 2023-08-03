package wallet

import (
	"bytes"
	"database/sql"
	"errors"
	"sort"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

var (
	// errDustOutput indicates an output is not spendable because it is dust.
	errDustOutput = errors.New("output is too small")

	// errOutputTimelock indicates an output's timelock is still active.
	errOutputTimelock = errors.New("wallet consensus set height is lower than the output timelock")

	// errSpendHeightTooHigh indicates an output's spend height is greater than
	// the allowed height.
	errSpendHeightTooHigh = errors.New("output spend height exceeds the allowed height")
)

// addSignatures will sign a transaction using a spendable key, with support
// for multisig spendable keys. Because of the restricted input, the function
// is compatible with both Siacoin inputs and Siafund inputs.
func addSignatures(txn *types.Transaction, cf types.CoveredFields, uc types.UnlockConditions, parentID types.Hash256, spendKey spendableKey, height uint64) (newSigIndices []int) {
	// Try to find the matching secret key for each public key - some public
	// keys may not have a match. Some secret keys may be used multiple times,
	// which is why public keys are used as the outer loop.
	totalSignatures := uint64(0)
	for i, pk := range uc.PublicKeys {
		// Search for the matching secret key to the public key.
		for j := range spendKey.SecretKeys {
			pubKey := spendKey.SecretKeys[j].PublicKey()
			if !bytes.Equal(pk.Key, pubKey[:]) {
				continue
			}

			// Found the right secret key, add a signature.
			sig := types.TransactionSignature{
				ParentID:       parentID,
				CoveredFields:  cf,
				PublicKeyIndex: uint64(i),
			}
			newSigIndices = append(newSigIndices, len(txn.Signatures))
			txn.Signatures = append(txn.Signatures, sig)
			sigIndex := len(txn.Signatures) - 1
			sigHash := modules.SigHash(*txn, sigIndex, height)
			encodedSig := spendKey.SecretKeys[j].SignHash(sigHash)
			txn.Signatures[sigIndex].Signature = encodedSig[:]

			// Count that the signature has been added, and break out of the
			// secret key loop.
			totalSignatures++
			break
		}

		// If there are enough signatures to satisfy the unlock conditions,
		// break out of the outer loop.
		if totalSignatures == uc.SignaturesRequired {
			break
		}
	}
	return newSigIndices
}

// checkOutput is a helper function used to determine if an output is usable.
func (w *Wallet) checkOutput(tx *sql.Tx, currentHeight uint64, id types.SiacoinOutputID, output types.SiacoinOutput, dustThreshold types.Currency) error {
	// Check that an output is not dust.
	if output.Value.Cmp(dustThreshold) < 0 {
		return errDustOutput
	}
	// Check that this output has not recently been spent by the wallet.
	spendHeight, err := dbGetSpentOutput(tx, types.Hash256(id))
	if err == nil {
		if spendHeight+RespendTimeout > currentHeight {
			return errSpendHeightTooHigh
		}
	}
	outputUnlockConditions := w.keys[output.Address].UnlockConditions
	if currentHeight < outputUnlockConditions.Timelock {
		return errOutputTimelock
	}

	return nil
}

// FundTransaction adds Siacoin inputs worth at least the requested amount to
// the provided transaction. A change output is also added, if necessary. The
// inputs will not be available to future calls to FundTransaction unless
// ReleaseInputs is called.
func (w *Wallet) FundTransaction(txn *types.Transaction, amount types.Currency) (parentTxn types.Transaction, toSign []types.Hash256, err error) {
	if amount.IsZero() {
		return
	}
	// dustThreshold has to be obtained separate from the lock.
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	consensusHeight, err := dbGetConsensusHeight(w.dbTx)
	if err != nil {
		return
	}

	// Collect a value-sorted set of Siacoin outputs.
	var so sortedOutputs
	err = dbForEachSiacoinOutput(w.dbTx, func(scoid types.SiacoinOutputID, sco types.SiacoinOutput) {
		so.ids = append(so.ids, scoid)
		so.outputs = append(so.outputs, sco)
	})
	if err != nil {
		return
	}

	// Add all of the unconfirmed outputs as well.
	curr := w.unconfirmedProcessedTransactions.head
	for curr != nil {
		upt := curr.txn
		for i, sco := range upt.Transaction.SiacoinOutputs {
			// Determine if the output belongs to the wallet.
			_, exists := w.keys[sco.Address]
			if !exists {
				continue
			}
			so.ids = append(so.ids, upt.Transaction.SiacoinOutputID(i))
			so.outputs = append(so.outputs, sco)
		}
		curr = curr.next
	}
	sort.Sort(sort.Reverse(so))

	// Create and fund a parent transaction that will add the correct amount of
	// Siacoins to the transaction.
	var fund types.Currency
	// potentialFund tracks the balance of the wallet including outputs that
	// have been spent in other unconfirmed transactions recently. This is to
	// provide the user with a more useful error message in the event that they
	// are overspending.
	var potentialFund types.Currency
	var spentScoids []types.SiacoinOutputID
	for i := range so.ids {
		scoid := so.ids[i]
		sco := so.outputs[i]
		// Check that the output can be spent.
		if err := w.checkOutput(w.dbTx, consensusHeight, scoid, sco, dustThreshold); err != nil {
			if modules.ContainsError(err, errSpendHeightTooHigh) {
				potentialFund = potentialFund.Add(sco.Value)
			}
			continue
		}

		// Add a Siacoin input for this output.
		sci := types.SiacoinInput{
			ParentID:         scoid,
			UnlockConditions: w.keys[sco.Address].UnlockConditions,
		}
		parentTxn.SiacoinInputs = append(parentTxn.SiacoinInputs, sci)
		spentScoids = append(spentScoids, scoid)

		// Add the output to the total fund.
		fund = fund.Add(sco.Value)
		potentialFund = potentialFund.Add(sco.Value)
		if fund.Cmp(amount) >= 0 {
			break
		}
	}
	if potentialFund.Cmp(amount) >= 0 && fund.Cmp(amount) < 0 {
		return types.Transaction{}, nil, modules.ErrIncompleteTransactions
	}
	if fund.Cmp(amount) < 0 {
		return types.Transaction{}, nil, modules.ErrLowBalance
	}

	// Create and add the output that will be used to fund the standard
	// transaction.
	parentUnlockConditions, err := w.nextPrimarySeedAddress(w.dbTx)
	if err != nil {
		return types.Transaction{}, nil, err
	}
	defer func() {
		if err != nil {
			w.managedMarkAddressUnused(parentUnlockConditions)
		}
	}()

	exactOutput := types.SiacoinOutput{
		Value:   amount,
		Address: parentUnlockConditions.UnlockHash(),
	}
	parentTxn.SiacoinOutputs = append(parentTxn.SiacoinOutputs, exactOutput)

	// Create a refund output if needed.
	if !amount.Equals(fund) {
		refundUnlockConditions, err := w.nextPrimarySeedAddress(w.dbTx)
		if err != nil {
			return types.Transaction{}, nil, err
		}
		defer func() {
			if err != nil {
				w.managedMarkAddressUnused(refundUnlockConditions)
			}
		}()
		refundOutput := types.SiacoinOutput{
			Value:   fund.Sub(amount),
			Address: refundUnlockConditions.UnlockHash(),
		}
		parentTxn.SiacoinOutputs = append(parentTxn.SiacoinOutputs, refundOutput)
	}

	// Sign all of the inputs to the transaction.
	for _, sci := range parentTxn.SiacoinInputs {
		addSignatures(&parentTxn, modules.FullCoveredFields(), sci.UnlockConditions, types.Hash256(sci.ParentID), w.keys[sci.UnlockConditions.UnlockHash()], consensusHeight)
	}

	// Mark the parent output as spent. Must be done after the transaction is
	// finished because otherwise the txid and output id will change.
	err = dbPutSpentOutput(w.dbTx, types.Hash256(parentTxn.SiacoinOutputID(0)), consensusHeight)
	if err != nil {
		return types.Transaction{}, nil, err
	}

	// Add the exact output.
	newInput := types.SiacoinInput{
		ParentID:         parentTxn.SiacoinOutputID(0),
		UnlockConditions: parentUnlockConditions,
	}
	txn.SiacoinInputs = append(txn.SiacoinInputs, newInput)
	toSign = append(toSign, types.Hash256(newInput.ParentID))

	// Mark all outputs that were spent as spent.
	for _, scoid := range spentScoids {
		err = dbPutSpentOutput(w.dbTx, types.Hash256(scoid), consensusHeight)
		if err != nil {
			return types.Transaction{}, nil, err
		}
	}

	return
}

// SweepTransaction creates a funded txn that sends the inputs of the
// transaction to the specified output if submitted to the blockchain.
func (w *Wallet) SweepTransaction(txn types.Transaction, output types.SiacoinOutput) (types.Transaction, []types.Transaction) {
	newTxn := modules.CopyTransaction(txn)
	newTxn.SiacoinOutputs = append(newTxn.SiacoinOutputs, output)
	_, parents, exists := w.tpool.Transaction(txn.ID())
	if !exists {
		w.log.Println("WARN: couldn't find transaction parents")
	}
	return newTxn, parents
}

// ReleaseInputs is a helper function that releases the inputs of txn for use in
// other transactions. It should only be called on transactions that are invalid
// or will never be broadcast.
func (w *Wallet) ReleaseInputs(txnSet []types.Transaction) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Iterate through all transactions and restore all outputs to the list of
	// available outputs.
	for _, txn := range txnSet {
		for _, sci := range txn.SiacoinInputs {
			dbDeleteSpentOutput(w.dbTx, types.Hash256(sci.ParentID))
		}
	}
}

// MarkWalletInputs scans a transaction and infers which inputs belong to this
// wallet. This allows those inputs to be signed.
func (w *Wallet) MarkWalletInputs(txn types.Transaction) (toSign []types.Hash256) {
	for _, sci := range txn.SiacoinInputs {
		unlockHash := sci.UnlockConditions.UnlockHash()
		if w.managedCanSpendUnlockHash(unlockHash) {
			toSign = append(toSign, types.Hash256(sci.ParentID))
		}
	}

	for _, sfi := range txn.SiafundInputs {
		unlockHash := sfi.UnlockConditions.UnlockHash()
		if w.managedCanSpendUnlockHash(unlockHash) {
			toSign = append(toSign, types.Hash256(sfi.ParentID))
		}
	}

	return
}

// Sign will sign any inputs added by FundTransaction.
func (w *Wallet) Sign(txn *types.Transaction, toSign []types.Hash256, cf types.CoveredFields) error {
	w.mu.Lock()
	consensusHeight, err := dbGetConsensusHeight(w.dbTx)
	w.mu.Unlock()
	if err != nil {
		return err
	}

	// For each Siacoin input covered by toSign, provide a signature.
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, id := range toSign {
		index := -1
		for i, input := range txn.SiacoinInputs {
			if id == types.Hash256(input.ParentID) {
				index = i
				break
			}
		}
		if index == -1 {
			return errors.New("toSign references an input not present in the transaction")
		}

		input := txn.SiacoinInputs[index]
		key, ok := w.keys[input.UnlockConditions.UnlockHash()]
		if !ok {
			return errors.New("cannot sign input")
		}

		addSignatures(txn, cf, input.UnlockConditions, types.Hash256(input.ParentID), key, consensusHeight)
	}

	return nil
}

// DropTransactions is a helper function that releases the inputs of
// a transaction set. It should only be called on transactions that
// are invalid or will never be broadcast.
func (w *Wallet) DropTransactions(txnSet []types.Transaction) {
}
