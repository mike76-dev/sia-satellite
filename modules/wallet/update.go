package wallet

import (
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

func (w *Wallet) applyEvents(events []Event) error {
	for _, event := range events {
		w.log.Info("found", zap.String("new", event.String()))
	}
	return nil
}

func (w *Wallet) applySiacoinElements(index types.ChainIndex, cu chainUpdate, relevant func(types.Address) bool) (err error) {
	cu.ForEachSiacoinElement(func(sce types.SiacoinElement, spent bool) {
		if err != nil {
			return
		} else if !relevant(sce.SiacoinOutput.Address) {
			return
		}

		if spent {
			err = w.deleteSiacoinElement(sce.SiacoinOutput.Address)
			if err != nil {
				err = modules.AddContext(err, "failed to delete output")
				return
			}
			if err = w.removeSpentOutput(sce.ID); err != nil {
				err = modules.AddContext(err, "failed to remove spent output")
				return
			}

			w.log.Debug("removed UTXO", zap.Stringer("address", sce.SiacoinOutput.Address), zap.Stringer("value", sce.SiacoinOutput.Value))
		} else {
			err = w.insertSiacoinElement(sce)
			if err != nil {
				err = modules.AddContext(err, "failed to insert output")
				return
			}

			w.log.Debug("added UTXO", zap.Stringer("address", sce.SiacoinOutput.Address), zap.Stringer("value", sce.SiacoinOutput.Value))
		}
	})

	return err
}

func (w *Wallet) applySiafundElements(cu chainUpdate, relevant func(types.Address) bool) (err error) {
	cu.ForEachSiafundElement(func(sfe types.SiafundElement, spent bool) {
		if err != nil {
			return
		} else if !relevant(sfe.SiafundOutput.Address) {
			return
		}

		if spent {
			err = w.deleteSiafundElement(sfe.SiafundOutput.Address)
			if err != nil {
				err = modules.AddContext(err, "failed to delete output")
				return
			}
			if err = w.removeSpentOutput(sfe.ID); err != nil {
				err = modules.AddContext(err, "failed to remove spent output")
				return
			}

			w.log.Debug("removed UTXO", zap.Stringer("address", sfe.SiafundOutput.Address), zap.Uint64("value", sfe.SiafundOutput.Value))
		} else {
			err = w.insertSiafundElement(sfe)
			if err != nil {
				err = modules.AddContext(err, "failed to insert output")
				return
			}

			w.log.Debug("added UTXO", zap.Stringer("address", sfe.SiafundOutput.Address), zap.Uint64("value", sfe.SiafundOutput.Value))
		}
	})

	return err
}

// applyChainUpdates applies the given chain updates to the database.
func (w *Wallet) applyChainUpdates(updates []*chain.ApplyUpdate) error {
	relevantAddress := func(addr types.Address) bool {
		return w.ownsAddress(addr)
	}

	for _, update := range updates {
		// Apply new events.
		events := AppliedEvents(update.State, update.Block, update, relevantAddress)
		if err := w.applyEvents(events); err != nil {
			return modules.AddContext(err, "failed to apply events")
		}

		// Apply new elements.
		if err := w.applySiacoinElements(update.State.Index, update, relevantAddress); err != nil {
			return modules.AddContext(err, "failed to apply Siacoin elements")
		} else if err := w.applySiafundElements(update, relevantAddress); err != nil {
			return modules.AddContext(err, "failed to apply Siafund elements")
		}

		// Update proofs.
		if err := w.updateSiacoinElementProofs(update); err != nil {
			return modules.AddContext(err, "failed to update Siacoin element proofs")
		} else if err := w.updateSiafundElementProofs(update); err != nil {
			return modules.AddContext(err, "failed to update Siafund element proofs")
		}
	}

	lastTip := updates[len(updates)-1].State.Index
	if err := w.updateTip(lastTip); err != nil {
		return modules.AddContext(err, "failed to update last indexed tip")
	}
	return nil
}

// ProcessChainApplyUpdate implements chain.Subscriber
func (w *Wallet) ProcessChainApplyUpdate(cau *chain.ApplyUpdate, mayCommit bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.updates = append(w.updates, cau)
	if mayCommit || len(w.updates) >= 100 {
		if err := w.applyChainUpdates(w.updates); err != nil {
			w.log.Error("couldn't apply update", zap.Error(err))
			return err
		}

		w.updates = nil
		if err := w.save(); err != nil {
			w.log.Error("couldn't save wallet", zap.Error(err))
			return modules.AddContext(err, "couldn't commit changes")
		}
		return nil
	}

	if w.synced() {
		go w.threadedDefragWallet()
	}

	return nil
}

// ProcessChainRevertUpdate implements chain.Subscriber
func (w *Wallet) ProcessChainRevertUpdate(cru *chain.RevertUpdate) error {
	// Update hasn't been committed yet.
	if len(w.updates) > 0 && w.updates[len(w.updates)-1].Block.ID() == cru.Block.ID() {
		w.updates = w.updates[:len(w.updates)-1]
		return nil
	}

	// Update has been committed, revert it.
	relevantAddress := func(addr types.Address) bool {
		return w.ownsAddress(addr)
	}

	if err := w.applySiacoinElements(cru.State.Index, cru, relevantAddress); err != nil {
		return modules.AddContext(err, "failed to apply Siacoin elements")
	} else if err := w.applySiafundElements(cru, relevantAddress); err != nil {
		return modules.AddContext(err, "failed to apply Siafund elements")
	}

	// Update proofs.
	if err := w.updateSiacoinElementProofs(cru); err != nil {
		return modules.AddContext(err, "failed to update Siacoin element proofs")
	} else if err := w.updateSiafundElementProofs(cru); err != nil {
		return modules.AddContext(err, "failed to update Siafund element proofs")
	}

	return nil
}
