package wallet

import (
	"database/sql"
	"math"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

type (
	spentSiacoinOutputSet map[types.SiacoinOutputID]types.SiacoinOutput
	spentSiafundOutputSet map[types.SiafundOutputID]types.SiafundOutput
)

// threadedResetSubscriptions unsubscribes the wallet from the consensus set and transaction pool
// and subscribes again.
func (w *Wallet) threadedResetSubscriptions() {
	if !w.scanLock.TryLock() {
		w.log.Println("ERROR: failed to lock wallet", errScanInProgress)
		return
	}
	defer w.scanLock.Unlock()

	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	err := w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
	if err != nil {
		w.log.Println("ERROR: failed to subscribe wallet to consensus", err)
		return
	}
	w.tpool.TransactionPoolSubscribe(w)
}

// advanceSeedLookahead generates all keys from the current primary seed progress up to index
// and adds them to the set of spendable keys.  Therefore the new primary seed progress will
// be index+1 and new lookahead keys will be generated starting from index+1.
// Returns true if a blockchain rescan is required.
func (w *Wallet) advanceSeedLookahead(index uint64) (bool, error) {
	progress, err := dbGetPrimarySeedProgress(w.dbTx)
	if err != nil {
		return false, err
	}
	newProgress := index + 1

	// Add spendable keys and remove them from lookahead.
	spendableKeys := generateKeys(w.primarySeed, progress, newProgress - progress)
	for _, key := range spendableKeys {
		w.keys[key.UnlockConditions.UnlockHash()] = key
		delete(w.lookahead, key.UnlockConditions.UnlockHash())
	}

	// Update the primarySeedProgress.
	dbPutPrimarySeedProgress(w.dbTx, newProgress)

	// Regenerate lookahead.
	w.regenerateLookahead(newProgress)

	// If more than lookaheadRescanThreshold keys were generated
	// also initialize a rescan just to be safe.
	if uint64(len(spendableKeys)) > lookaheadRescanThreshold {
		return true, nil
	}

	return false, nil
}

// isWalletAddress is a helper function that checks if an Address is
// derived from one of the wallet's spendable keys or is being explicitly watched.
func (w *Wallet) isWalletAddress(uh types.Address) bool {
	_, spendable := w.keys[uh]
	_, watchonly := w.watchedAddrs[uh]
	return spendable || watchonly
}

// updateLookahead uses a consensus change to update the seed progress if one of the outputs
// contains an unlock hash of the lookahead set. Returns true if a blockchain rescan is required.
func (w *Wallet) updateLookahead(tx *sql.Tx, cc modules.ConsensusChange) (bool, error) {
	var largestIndex uint64
	for _, diff := range cc.SiacoinOutputDiffs {
		if index, ok := w.lookahead[diff.SiacoinOutput.Address]; ok {
			if index > largestIndex {
				largestIndex = index
			}
		}
	}
	for _, diff := range cc.SiafundOutputDiffs {
		if index, ok := w.lookahead[diff.SiafundOutput.Address]; ok {
			if index > largestIndex {
				largestIndex = index
			}
		}
	}
	if largestIndex > 0 {
		return w.advanceSeedLookahead(largestIndex)
	}

	return false, nil
}

// updateConfirmedSet uses a consensus change to update the confirmed set of
// outputs as understood by the wallet.
func (w *Wallet) updateConfirmedSet(tx *sql.Tx, cc modules.ConsensusChange) error {
	for _, diff := range cc.SiacoinOutputDiffs {
		// Verify that the diff is relevant to the wallet.
		if !w.isWalletAddress(diff.SiacoinOutput.Address) {
			continue
		}

		var err error
		if diff.Direction == modules.DiffApply {
			w.log.Println("INFO: wallet has gained a spendable Siacoin output:", diff.ID, "::", diff.SiacoinOutput.Value)
			err = dbPutSiacoinOutput(tx, diff.ID, diff.SiacoinOutput)
		} else {
			w.log.Println("INFO: wallet has lost a spendable Siacoin output:", diff.ID, "::", diff.SiacoinOutput.Value)
			err = dbDeleteSiacoinOutput(tx, diff.ID)
		}
		if err != nil {
			w.log.Severe("ERROR: could not update Siacoin output:", err)
			return err
		}
	}
	for _, diff := range cc.SiafundOutputDiffs {
		// Verify that the diff is relevant to the wallet.
		if !w.isWalletAddress(diff.SiafundOutput.Address) {
			continue
		}

		var err error
		if diff.Direction == modules.DiffApply {
			w.log.Println("INFO: wallet has gained a spendable Siafund output:", diff.ID, "::", diff.SiafundOutput.Value)
			err = dbPutSiafundOutput(tx, diff.ID, diff.SiafundOutput)
		} else {
			w.log.Println("INFO: wallet has lost a spendable Siafund output:", diff.ID, "::", diff.SiafundOutput.Value)
			err = dbDeleteSiafundOutput(tx, diff.ID)
		}
		if err != nil {
			w.log.Severe("ERROR: could not update Siafund output:", err)
			return err
		}
	}
	for _, diff := range cc.SiafundPoolDiffs {
		var err error
		if diff.Direction == modules.DiffApply {
			err = dbPutSiafundPool(tx, diff.Adjusted)
		} else {
			err = dbPutSiafundPool(tx, diff.Previous)
		}
		if err != nil {
			w.log.Severe("ERROR: could not update Siafund pool:", err)
			return err
		}
	}
	return nil
}

// revertHistory reverts any transaction history that was destroyed by reverted
// blocks in the consensus change.
func (w *Wallet) revertHistory(tx *sql.Tx, reverted []types.Block) error {
	for _, block := range reverted {
		// Remove any transactions that have been reverted.
		for i := len(block.Transactions) - 1; i >= 0; i-- {
			// If the transaction is relevant to the wallet, it will be the
			// most recent transaction in bucketProcessedTransactions.
			txid := block.Transactions[i].ID()
			pt, err := dbGetLastProcessedTransaction(tx)
			if err != nil {
				break
			}
			if txid == pt.TransactionID {
				w.log.Println("INFO: a wallet transaction has been reverted due to a reorg:", txid)
				if err := dbDeleteLastProcessedTransaction(tx); err != nil {
					w.log.Severe("ERROR: could not revert transaction:", err)
					return err
				}
			}
		}

		// Remove the miner payout transaction if applicable.
		for i, mp := range block.MinerPayouts {
			// If the transaction is relevant to the wallet, it will be the
			// most recent transaction in bucketProcessedTransactions.
			pt, err := dbGetLastProcessedTransaction(tx)
			if err != nil {
				break
			}
			if types.TransactionID(block.ID()) == pt.TransactionID {
				w.log.Println("INFO: miner payout has been reverted due to a reorg:", block.ID().MinerOutputID(i), "::", mp.Value)
				if err := dbDeleteLastProcessedTransaction(tx); err != nil {
					w.log.Severe("ERROR: could not revert transaction:", err)
					return err
				}
				break // There will only ever be one miner transaction.
			}
		}
	}
	return nil
}

// computeSpentSiacoinOutputSet scans a slice of Siacoin output diffs for spent
// outputs and collects them in a map of SiacoinOutputID -> SiacoinOutput.
func computeSpentSiacoinOutputSet(diffs []modules.SiacoinOutputDiff) spentSiacoinOutputSet {
	outputs := make(spentSiacoinOutputSet)
	for _, diff := range diffs {
		if diff.Direction == modules.DiffRevert {
			// DiffRevert means spent.
			outputs[diff.ID] = diff.SiacoinOutput
		}
	}
	return outputs
}

// computeSpentSiafundOutputSet scans a slice of Siafund output diffs for spent
// outputs and collects them in a map of SiafundOutputID -> SiafundOutput.
func computeSpentSiafundOutputSet(diffs []modules.SiafundOutputDiff) spentSiafundOutputSet {
	outputs := make(spentSiafundOutputSet)
	for _, diff := range diffs {
		if diff.Direction == modules.DiffRevert {
			// DiffRevert means spent.
			outputs[diff.ID] = diff.SiafundOutput
		}
	}
	return outputs
}

// computeProcessedTransactionsFromBlock searches all the miner payouts and
// transactions in a block and computes a ProcessedTransaction slice containing
// all of the transactions processed for the given block.
func (w *Wallet) computeProcessedTransactionsFromBlock(tx *sql.Tx, block types.Block, spentSiacoinOutputs spentSiacoinOutputSet, spentSiafundOutputs spentSiafundOutputSet, consensusHeight uint64) []modules.ProcessedTransaction {
	var pts []modules.ProcessedTransaction

	// Find ProcessedTransactions from miner payouts.
	relevant := false
	for _, mp := range block.MinerPayouts {
		relevant = relevant || w.isWalletAddress(mp.Address)
	}
	if relevant {
		w.log.Println("INFO: wallet has received new miner payouts:", block.ID())
		// Apply the miner payout transaction if applicable.
		minerPT := modules.ProcessedTransaction{
			Transaction:           types.Transaction{},
			TransactionID:         types.TransactionID(block.ID()),
			ConfirmationHeight:    consensusHeight,
			ConfirmationTimestamp: block.Timestamp,
		}
		for i, mp := range block.MinerPayouts {
			w.log.Println("\tminer payout:", block.ID().MinerOutputID(i), "::", mp.Value)
			minerPT.Outputs = append(minerPT.Outputs, modules.ProcessedOutput{
				ID:             types.Hash256(block.ID().MinerOutputID(i)),
				FundType:       specifierMinerPayout,
				MaturityHeight: consensusHeight + modules.MaturityDelay,
				WalletAddress:  w.isWalletAddress(mp.Address),
				RelatedAddress: mp.Address,
				Value:          mp.Value,
			})
		}
		pts = append(pts, minerPT)
	}

	// Find ProcessedTransactions from transactions.
	for _, txn := range block.Transactions {
		// Determine if transaction is relevant.
		relevant := false
		for _, sci := range txn.SiacoinInputs {
			relevant = relevant || w.isWalletAddress(sci.UnlockConditions.UnlockHash())
		}
		for _, sco := range txn.SiacoinOutputs {
			relevant = relevant || w.isWalletAddress(sco.Address)
		}
		for _, sfi := range txn.SiafundInputs {
			relevant = relevant || w.isWalletAddress(sfi.UnlockConditions.UnlockHash())
		}
		for _, sfo := range txn.SiafundOutputs {
			relevant = relevant || w.isWalletAddress(sfo.Address)
		}
		for _, fc := range txn.FileContracts {
			for _, o := range fc.ValidProofOutputs {
				relevant = relevant || w.isWalletAddress(o.Address)
			}
			for _, o := range fc.MissedProofOutputs {
				relevant = relevant || w.isWalletAddress(o.Address)
			}
		}
		for _, fcr := range txn.FileContractRevisions {
			for _, o := range fcr.ValidProofOutputs {
				relevant = relevant || w.isWalletAddress(o.Address)
			}
			for _, o := range fcr.MissedProofOutputs {
				relevant = relevant || w.isWalletAddress(o.Address)
			}
		}

		// Only create a ProcessedTransaction if transaction is relevant.
		if !relevant {
			continue
		}
		w.log.Println("INFO: a transaction has been confirmed on the blockchain:", txn.ID())

		pt := modules.ProcessedTransaction{
			Transaction:           txn,
			TransactionID:         txn.ID(),
			ConfirmationHeight:    consensusHeight,
			ConfirmationTimestamp: block.Timestamp,
		}

		for _, sci := range txn.SiacoinInputs {
			pi := modules.ProcessedInput{
				ParentID:       types.Hash256(sci.ParentID),
				FundType:       specifierSiacoinInput,
				WalletAddress:  w.isWalletAddress(sci.UnlockConditions.UnlockHash()),
				RelatedAddress: sci.UnlockConditions.UnlockHash(),
				Value:          spentSiacoinOutputs[sci.ParentID].Value,
			}
			pt.Inputs = append(pt.Inputs, pi)

			// Log any wallet-relevant inputs.
			if pi.WalletAddress {
				w.log.Println("\tSiacoin Input:", pi.ParentID, "::", pi.Value)
			}
		}

		for i, sco := range txn.SiacoinOutputs {
			po := modules.ProcessedOutput{
				ID:             types.Hash256(txn.SiacoinOutputID(i)),
				FundType:       types.SpecifierSiacoinOutput,
				MaturityHeight: consensusHeight,
				WalletAddress:  w.isWalletAddress(sco.Address),
				RelatedAddress: sco.Address,
				Value:          sco.Value,
			}
			pt.Outputs = append(pt.Outputs, po)

			// Log any wallet-relevant outputs.
			if po.WalletAddress {
				w.log.Println("\tSiacoin Output:", po.ID, "::", po.Value)
			}
		}

		for _, sfi := range txn.SiafundInputs {
			pi := modules.ProcessedInput{
				ParentID:       types.Hash256(sfi.ParentID),
				FundType:       specifierSiafundInput,
				WalletAddress:  w.isWalletAddress(sfi.UnlockConditions.UnlockHash()),
				RelatedAddress: sfi.UnlockConditions.UnlockHash(),
				Value:          types.NewCurrency64(spentSiafundOutputs[sfi.ParentID].Value),
			}
			pt.Inputs = append(pt.Inputs, pi)
			// Log any wallet-relevant inputs.
			if pi.WalletAddress {
				w.log.Println("\tSiafund Input:", pi.ParentID, "::", pi.Value)
			}

			siafundPool, err := dbGetSiafundPool(w.dbTx)
			if err != nil {
				w.log.Severe("ERROR: could not get Siafund pool: ", err)
				continue
			}

			sfo := spentSiafundOutputs[sfi.ParentID]
			po := modules.ProcessedOutput{
				ID:             types.Hash256(sfi.ParentID),
				FundType:       specifierClaimOutput,
				MaturityHeight: consensusHeight + modules.MaturityDelay,
				WalletAddress:  w.isWalletAddress(sfi.UnlockConditions.UnlockHash()),
				RelatedAddress: sfi.ClaimAddress,
				// TODO This code is incorrect, because it doesn't take sfo.ClaimStart
				// into account. For now, we have no way to fetch sfo.ClaimStart, because
				// it is absent in `core`.
				Value:          siafundPool.Mul64(sfo.Value),
			}
			pt.Outputs = append(pt.Outputs, po)
			// Log any wallet-relevant outputs.
			if po.WalletAddress {
				w.log.Println("\tClaim Output:", po.ID, "::", po.Value)
			}
		}

		for i, sfo := range txn.SiafundOutputs {
			po := modules.ProcessedOutput{
				ID:             types.Hash256(txn.SiafundOutputID(i)),
				FundType:       types.SpecifierSiafundOutput,
				MaturityHeight: consensusHeight,
				WalletAddress:  w.isWalletAddress(sfo.Address),
				RelatedAddress: sfo.Address,
				Value:          types.NewCurrency64(sfo.Value),
			}
			pt.Outputs = append(pt.Outputs, po)
			// Log any wallet-relevant outputs.
			if po.WalletAddress {
				w.log.Println("\tSiafund Output:", po.ID, "::", po.Value)
			}
		}

		for _, fee := range txn.MinerFees {
			pt.Outputs = append(pt.Outputs, modules.ProcessedOutput{
				FundType:       specifierMinerFee,
				MaturityHeight: consensusHeight + modules.MaturityDelay,
				Value:          fee,
			})
		}

		for i, fc := range txn.FileContracts {
			for j, o := range fc.ValidProofOutputs {
				po := modules.ProcessedOutput{
					ID:             types.Hash256(modules.StorageProofOutputID(txn.FileContractID(i), true, j)),
					FundType:       types.SpecifierSiacoinOutput,
					MaturityHeight: fc.WindowEnd + modules.MaturityDelay,
					WalletAddress:  w.isWalletAddress(o.Address),
					RelatedAddress: o.Address,
					Value:          o.Value,
				}
				pt.Outputs = append(pt.Outputs, po)
				// Log any wallet-relevant outputs.
				if po.WalletAddress {
					w.log.Println("\tFile Contract Valid Output:", po.ID, "::", po.Value)
				}
			}
			for j, o := range fc.MissedProofOutputs {
				po := modules.ProcessedOutput{
					ID:             types.Hash256(modules.StorageProofOutputID(txn.FileContractID(i), false, j)),
					FundType:       types.SpecifierSiacoinOutput,
					MaturityHeight: fc.WindowEnd + modules.MaturityDelay,
					WalletAddress:  w.isWalletAddress(o.Address),
					RelatedAddress: o.Address,
					Value:          o.Value,
				}
				pt.Outputs = append(pt.Outputs, po)
				// Log any wallet-relevant outputs.
				if po.WalletAddress {
					w.log.Println("\tFile Contract Missed Output:", po.ID, "::", po.Value)
				}
			}
		}

		for _, fcr := range txn.FileContractRevisions {
			for j, o := range fcr.ValidProofOutputs {
				po := modules.ProcessedOutput{
					ID:             types.Hash256(modules.StorageProofOutputID(fcr.ParentID, true, j)),
					FundType:       types.SpecifierSiacoinOutput,
					MaturityHeight: fcr.WindowEnd + modules.MaturityDelay,
					WalletAddress:  w.isWalletAddress(o.Address),
					RelatedAddress: o.Address,
					Value:          o.Value,
				}
				pt.Outputs = append(pt.Outputs, po)
				// Log any wallet-relevant outputs.
				if po.WalletAddress {
					w.log.Println("\tFile Contract Revision Valid Output:", po.ID, "::", po.Value)
				}
			}
			for j, o := range fcr.MissedProofOutputs {
				po := modules.ProcessedOutput{
					ID:             types.Hash256(modules.StorageProofOutputID(fcr.ParentID, false, j)),
					FundType:       types.SpecifierSiacoinOutput,
					MaturityHeight: fcr.WindowEnd + modules.MaturityDelay,
					WalletAddress:  w.isWalletAddress(o.Address),
					RelatedAddress: o.Address,
					Value:          o.Value,
				}
				pt.Outputs = append(pt.Outputs, po)
				// Log any wallet-relevant outputs.
				if po.WalletAddress {
					w.log.Println("\tFile Contract Revision Missed Output:", po.ID, "::", po.Value)
				}
			}
		}

		pts = append(pts, pt)
	}
	return pts
}

// applyHistory applies any transaction history that the applied blocks
// introduced.
func (w *Wallet) applyHistory(tx *sql.Tx, cc modules.ConsensusChange) error {
	spentSiacoinOutputs := computeSpentSiacoinOutputSet(cc.SiacoinOutputDiffs)
	spentSiafundOutputs := computeSpentSiafundOutputSet(cc.SiafundOutputDiffs)
	consensusHeight := cc.InitialHeight()

	for _, block := range cc.AppliedBlocks {
		// Increment the consensus height.
		if block.ID() != modules.GenesisID {
			consensusHeight++
		}

		pts := w.computeProcessedTransactionsFromBlock(tx, block, spentSiacoinOutputs, spentSiafundOutputs, consensusHeight)
		for _, pt := range pts {
			err := dbAppendProcessedTransaction(tx, pt)
			if err != nil {
				return modules.AddContext(err, "could not put processed transaction")
			}
		}
	}

	return nil
}

// ProcessConsensusChange parses a consensus change to update the set of
// confirmed outputs known to the wallet.
func (w *Wallet) ProcessConsensusChange(cc modules.ConsensusChange) {
	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	if needRescan, err := w.updateLookahead(w.dbTx, cc); err != nil {
		w.log.Severe("ERROR: failed to update lookahead:", err)
		w.dbRollback = true
	} else if needRescan {
		go w.threadedResetSubscriptions()
	}
	if err := w.updateConfirmedSet(w.dbTx, cc); err != nil {
		w.log.Severe("ERROR: failed to update confirmed set:", err)
		w.dbRollback = true
	}
	if err := w.revertHistory(w.dbTx, cc.RevertedBlocks); err != nil {
		w.log.Severe("ERROR: failed to revert consensus change:", err)
		w.dbRollback = true
	}
	if err := w.applyHistory(w.dbTx, cc); err != nil {
		w.log.Severe("ERROR: failed to apply consensus change:", err)
		w.dbRollback = true
	}
	if err := dbPutConsensusChangeID(w.dbTx, cc.ID); err != nil {
		w.log.Severe("ERROR: failed to update consensus change ID:", err)
		w.dbRollback = true
	}
	if err := dbPutConsensusHeight(w.dbTx, cc.BlockHeight); err != nil {
		w.log.Severe("ERROR: failed to update consensus block height:", err)
		w.dbRollback = true
	}

	if cc.Synced {
		go w.threadedDefragWallet()
	}
}

// ReceiveUpdatedUnconfirmedTransactions updates the wallet's unconfirmed
// transaction set.
func (w *Wallet) ReceiveUpdatedUnconfirmedTransactions(diff *modules.TransactionPoolDiff) {
	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Do the pruning first. If there are any pruned transactions, we will need
	// to re-allocate the whole processed transactions array.
	droppedTransactions := make(map[types.TransactionID]struct{})
	for i := range diff.RevertedTransactions {
		txids := w.unconfirmedSets[diff.RevertedTransactions[i]]
		for i := range txids {
			droppedTransactions[txids[i]] = struct{}{}
		}
		delete(w.unconfirmedSets, diff.RevertedTransactions[i])
	}

	// Skip the reallocation if we can, otherwise reallocate the
	// unconfirmedProcessedTransactions to no longer have the dropped
	// transactions.
	if len(droppedTransactions) != 0 {
		// Capacity can't be reduced, because we have no way of knowing if the
		// dropped transactions are relevant to the wallet or not, and some will
		// not be relevant to the wallet, meaning they don't have a counterpart
		// in w.unconfirmedProcessedTransactions.
		var newUPT processedTransactionList
		curr := w.unconfirmedProcessedTransactions.head
		for curr.next != nil {
			_, exists := droppedTransactions[curr.txn.TransactionID]
			if !exists {
				// Transaction was not dropped, add it to the new unconfirmed
				// transactions.
				newUPT.add(curr.txn)
			}
			curr = curr.next
		}

		// Set the unconfirmed processed transactions to the pruned set.
		w.unconfirmedProcessedTransactions = newUPT
	}

	// Scroll through all of the diffs and add any new transactions.
	for _, unconfirmedTxnSet := range diff.AppliedTransactions {
		// Mark all of the transactions that appeared in this set.
		//
		// TODO: Technically only necessary to mark the ones that are relevant
		// to the wallet, but overhead should be low.
		w.unconfirmedSets[unconfirmedTxnSet.ID] = unconfirmedTxnSet.IDs

		// Get the values for the spent outputs.
		spentSiacoinOutputs := make(map[types.SiacoinOutputID]types.SiacoinOutput)
		for _, scod := range unconfirmedTxnSet.Change.SiacoinOutputDiffs {
			// Only need to grab the reverted ones, because only reverted ones
			// have the possibility of having been spent.
			if scod.Direction == modules.DiffRevert {
				spentSiacoinOutputs[scod.ID] = scod.SiacoinOutput
			}
		}

		// Add each transaction to our set of unconfirmed transactions.
		for i, txn := range unconfirmedTxnSet.Transactions {
			// Determine whether transaction is relevant to the wallet.
			relevant := false
			for _, sci := range txn.SiacoinInputs {
				relevant = relevant || w.isWalletAddress(sci.UnlockConditions.UnlockHash())
			}
			for _, sco := range txn.SiacoinOutputs {
				relevant = relevant || w.isWalletAddress(sco.Address)
			}

			// Only create a ProcessedTransaction if txn is relevant.
			if !relevant {
				continue
			}

			pt := modules.ProcessedTransaction{
				Transaction:           txn,
				TransactionID:         unconfirmedTxnSet.IDs[i],
				ConfirmationHeight:    math.MaxUint64,
				ConfirmationTimestamp: time.Unix(math.MaxInt64, math.MaxInt64),
			}
			for _, sci := range txn.SiacoinInputs {
				pt.Inputs = append(pt.Inputs, modules.ProcessedInput{
					ParentID:       types.Hash256(sci.ParentID),
					FundType:       specifierSiacoinInput,
					WalletAddress:  w.isWalletAddress(sci.UnlockConditions.UnlockHash()),
					RelatedAddress: sci.UnlockConditions.UnlockHash(),
					Value:          spentSiacoinOutputs[sci.ParentID].Value,
				})
			}
			for i, sco := range txn.SiacoinOutputs {
				pt.Outputs = append(pt.Outputs, modules.ProcessedOutput{
					ID:             types.Hash256(txn.SiacoinOutputID(i)),
					FundType:       types.SpecifierSiacoinOutput,
					MaturityHeight: math.MaxUint64,
					WalletAddress:  w.isWalletAddress(sco.Address),
					RelatedAddress: sco.Address,
					Value:          sco.Value,
				})
			}
			for _, fee := range txn.MinerFees {
				pt.Outputs = append(pt.Outputs, modules.ProcessedOutput{
					FundType: specifierMinerFee,
					Value:    fee,
				})
			}
			w.unconfirmedProcessedTransactions.add(pt)
		}
	}
}
