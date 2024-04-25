package wallet

import (
	"fmt"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.uber.org/zap"
)

type chainUpdate interface {
	UpdateElementProof(*types.StateElement)
	ForEachTreeNode(func(row, col uint64, h types.Hash256))
	ForEachSiacoinElement(func(types.SiacoinElement, bool))
	ForEachSiafundElement(func(types.SiafundElement, bool))
}

func (w *Wallet) applyEvents(events []Event) {
	for _, event := range events {
		w.log.Info("found", zap.String("new", event.String()))
	}
}

func (w *Wallet) revertEvents(events []Event) {
	for _, event := range events {
		w.log.Info("found", zap.String("reverted", event.String()))
	}
}

func (w *Wallet) addSiacoinElements(sces []types.SiacoinElement) error {
	for _, sce := range sces {
		if err := w.insertSiacoinElement(sce); err != nil {
			return modules.AddContext(err, "failed to insert output")
		}
		w.log.Debug("added UTXO", zap.Stringer("address", sce.SiacoinOutput.Address), zap.Stringer("value", sce.SiacoinOutput.Value))
	}
	return nil
}

func (w *Wallet) removeSiacoinElements(sces []types.SiacoinElement) error {
	for _, sce := range sces {
		if err := w.deleteSiacoinElement(sce.SiacoinOutput.Address); err != nil {
			return modules.AddContext(err, "failed to delete output")
		}
		if err := w.removeSpentOutput(sce.ID); err != nil {
			return modules.AddContext(err, "failed to remove spent output")
		}
		w.log.Debug("removed UTXO", zap.Stringer("address", sce.SiacoinOutput.Address), zap.Stringer("value", sce.SiacoinOutput.Value))
	}
	return nil
}

func (w *Wallet) addSiafundElements(sfes []types.SiafundElement) error {
	for _, sfe := range sfes {
		if err := w.insertSiafundElement(sfe); err != nil {
			return modules.AddContext(err, "failed to insert output")
		}
		w.log.Debug("added UTXO", zap.Stringer("address", sfe.SiafundOutput.Address), zap.Uint64("value", sfe.SiafundOutput.Value))
	}
	return nil
}

func (w *Wallet) removeSiafundElements(sfes []types.SiafundElement) error {
	for _, sfe := range sfes {
		if err := w.deleteSiafundElement(sfe.SiafundOutput.Address); err != nil {
			return modules.AddContext(err, "failed to delete output")
		}
		if err := w.removeSpentOutput(sfe.ID); err != nil {
			return modules.AddContext(err, "failed to remove spent output")
		}
		w.log.Debug("removed UTXO", zap.Stringer("address", sfe.SiafundOutput.Address), zap.Uint64("value", sfe.SiafundOutput.Value))
	}
	return nil
}

// applyChainUpdate atomically applies the chain update to the database.
func (w *Wallet) applyChainUpdate(cau chain.ApplyUpdate) error {
	relevantAddress := func(addr types.Address) bool {
		return w.ownsAddress(addr)
	}

	// Determine which Siacoin and Siafund elements are ephemeral.
	created := make(map[types.Hash256]bool)
	ephemeral := make(map[types.Hash256]bool)
	for _, txn := range cau.Block.Transactions {
		for i := range txn.SiacoinOutputs {
			created[types.Hash256(txn.SiacoinOutputID(i))] = true
		}
		for _, input := range txn.SiacoinInputs {
			ephemeral[types.Hash256(input.ParentID)] = created[types.Hash256(input.ParentID)]
		}
		for i := range txn.SiafundOutputs {
			created[types.Hash256(txn.SiafundOutputID(i))] = true
		}
		for _, input := range txn.SiafundInputs {
			ephemeral[types.Hash256(input.ParentID)] = created[types.Hash256(input.ParentID)]
		}
	}

	// Add new Siacoin elements to the store.
	var newSiacoinElements, spentSiacoinElements []types.SiacoinElement
	cau.ForEachSiacoinElement(func(se types.SiacoinElement, spent bool) {
		if ephemeral[se.ID] {
			return
		}

		if !relevantAddress(se.SiacoinOutput.Address) {
			return
		}

		if spent {
			spentSiacoinElements = append(spentSiacoinElements, se)
		} else {
			newSiacoinElements = append(newSiacoinElements, se)
		}
	})

	if err := w.addSiacoinElements(newSiacoinElements); err != nil {
		return modules.AddContext(err, "failed to add Siacoin elements")
	} else if err := w.removeSiacoinElements(spentSiacoinElements); err != nil {
		return modules.AddContext(err, "failed to remove Siacoin elements")
	}

	// Add new Siafund elements to the store.
	var newSiafundElements, spentSiafundElements []types.SiafundElement
	cau.ForEachSiafundElement(func(se types.SiafundElement, spent bool) {
		if ephemeral[se.ID] {
			return
		}

		if !relevantAddress(se.SiafundOutput.Address) {
			return
		}

		if spent {
			spentSiafundElements = append(spentSiafundElements, se)
		} else {
			newSiafundElements = append(newSiafundElements, se)
		}
	})

	if err := w.addSiafundElements(newSiafundElements); err != nil {
		return modules.AddContext(err, "failed to add Siafund elements")
	} else if err := w.removeSiafundElements(spentSiafundElements); err != nil {
		return modules.AddContext(err, "failed to remove Siafund elements")
	}

	// Apply new events.
	w.applyEvents(AppliedEvents(cau.State, cau.Block, cau, relevantAddress))

	// Update proofs.
	if err := w.updateSiacoinElementProofs(cau); err != nil {
		return modules.AddContext(err, "failed to update Siacoin element proofs")
	} else if err := w.updateSiafundElementProofs(cau); err != nil {
		return modules.AddContext(err, "failed to update Siafund element proofs")
	}

	if err := w.updateTip(cau.State.Index); err != nil {
		return modules.AddContext(err, "failed to update last indexed tip")
	}
	return nil
}

// revertChainUpdate atomically reverts the chain update from the database.
func (w *Wallet) revertChainUpdate(cru chain.RevertUpdate) error {
	relevantAddress := func(addr types.Address) bool {
		return w.ownsAddress(addr)
	}

	// Determine which Siacoin and Siafund elements are ephemeral.
	created := make(map[types.Hash256]bool)
	ephemeral := make(map[types.Hash256]bool)
	for _, txn := range cru.Block.Transactions {
		for i := range txn.SiacoinOutputs {
			created[types.Hash256(txn.SiacoinOutputID(i))] = true
		}
		for _, input := range txn.SiacoinInputs {
			ephemeral[types.Hash256(input.ParentID)] = created[types.Hash256(input.ParentID)]
		}
		for i := range txn.SiafundOutputs {
			created[types.Hash256(txn.SiafundOutputID(i))] = true
		}
		for _, input := range txn.SiafundInputs {
			ephemeral[types.Hash256(input.ParentID)] = created[types.Hash256(input.ParentID)]
		}
	}

	var removedSiacoinElements, addedSiacoinElements []types.SiacoinElement
	cru.ForEachSiacoinElement(func(se types.SiacoinElement, spent bool) {
		if ephemeral[se.ID] {
			return
		}

		if !relevantAddress(se.SiacoinOutput.Address) {
			return
		}

		if spent {
			// Re-add any spent Siacoin elements.
			addedSiacoinElements = append(addedSiacoinElements, se)
		} else {
			// Delete any created Siacoin elements.
			removedSiacoinElements = append(removedSiacoinElements, se)
		}
	})

	// Revert Siacoin element changes.
	if err := w.addSiacoinElements(addedSiacoinElements); err != nil {
		return modules.AddContext(err, "failed to add Siacoin elements")
	} else if err := w.removeSiacoinElements(removedSiacoinElements); err != nil {
		return modules.AddContext(err, "failed to remove Siacoin elements")
	}

	var removedSiafundElements, addedSiafundElements []types.SiafundElement
	cru.ForEachSiafundElement(func(se types.SiafundElement, spent bool) {
		if ephemeral[se.ID] {
			return
		}

		if !relevantAddress(se.SiafundOutput.Address) {
			return
		}

		if spent {
			// Re-add any spent Siafund elements.
			addedSiafundElements = append(addedSiafundElements, se)
		} else {
			// Delete any created Siafund elements.
			removedSiafundElements = append(removedSiafundElements, se)
		}
	})

	// Revert Siafund element changes.
	if err := w.addSiafundElements(addedSiafundElements); err != nil {
		return modules.AddContext(err, "failed to add Siafund elements")
	} else if err := w.removeSiafundElements(removedSiafundElements); err != nil {
		return modules.AddContext(err, "failed to remove Siafund elements")
	}

	// Update proofs.
	if err := w.updateSiacoinElementProofs(cru); err != nil {
		return modules.AddContext(err, "failed to update Siacoin element proofs")
	} else if err := w.updateSiafundElementProofs(cru); err != nil {
		return modules.AddContext(err, "failed to update Siafund element proofs")
	}

	// Revert events.
	w.revertEvents(AppliedEvents(cru.State, cru.Block, cru, relevantAddress))

	if err := w.updateTip(cru.State.Index); err != nil {
		return modules.AddContext(err, "failed to update last indexed tip")
	}
	return nil
}

// UpdateChainState applies and reverts the ChainManager updates.
func (w *Wallet) UpdateChainState(reverted []chain.RevertUpdate, applied []chain.ApplyUpdate) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, cru := range reverted {
		revertedIndex := types.ChainIndex{
			ID:     cru.Block.ID(),
			Height: cru.State.Index.Height + 1,
		}
		if err := w.revertChainUpdate(cru); err != nil {
			return fmt.Errorf("failed to revert chain update %q: %w", revertedIndex, err)
		}
	}

	for _, cau := range applied {
		if err := w.applyChainUpdate(cau); err != nil {
			return fmt.Errorf("failed to apply chain update %q: %w", cau.State.Index, err)
		}
	}

	if err := w.save(); err != nil {
		w.log.Error("couldn't save wallet", zap.Error(err))
		return modules.AddContext(err, "couldn't commit changes")
	}

	if w.synced() {
		go w.threadedDefragWallet()
	}

	return nil
}
