package wallet

import (
	"fmt"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/wallet"
)

// relevantV1Txn returns true if the transaction mentions at least one of the addresses provided.
func relevantV1Txn(txn types.Transaction, addrs map[types.Address]uint64) (relevant bool, relevantAddrs []types.Address) {
	for _, sci := range txn.SiacoinInputs {
		addr := sci.UnlockConditions.UnlockHash()
		_, found := addrs[addr]
		if found {
			relevant = true
			relevantAddrs = append(relevantAddrs, addr)
		}
	}

	for _, sco := range txn.SiacoinOutputs {
		_, found := addrs[sco.Address]
		if found {
			relevant = true
			relevantAddrs = append(relevantAddrs, sco.Address)
		}
	}

	return
}

// relevantV2Txn returns true if the transaction mentions at least one of the addresses provided.
func relevantV2Txn(txn types.V2Transaction, addrs map[types.Address]uint64) (relevant bool, relevantAddrs []types.Address) {
	for _, sci := range txn.SiacoinInputs {
		_, found := addrs[sci.Parent.SiacoinOutput.Address]
		if found {
			relevant = true
			relevantAddrs = append(relevantAddrs, sci.Parent.SiacoinOutput.Address)
		}
	}

	for _, sco := range txn.SiacoinOutputs {
		_, found := addrs[sco.Address]
		if found {
			relevant = true
			relevantAddrs = append(relevantAddrs, sco.Address)
		}
	}

	return
}

// appliedEvents returns a slice of events that are relevant to the wallet in the chain update.
func appliedEvents(cau chain.ApplyUpdate, addrs map[types.Address]uint64) (events []wallet.Event) {
	cs := cau.State
	block := cau.Block
	index := cs.Index
	siacoinElements := make(map[types.SiacoinOutputID]types.SiacoinElement)

	// Cache the value of siacoin elements to use when calculating v1 outflow.
	for _, sced := range cau.SiacoinElementDiffs() {
		sced.SiacoinElement.StateElement.MerkleProof = nil // clear the proof to save space
		siacoinElements[sced.SiacoinElement.ID] = sced.SiacoinElement.Move()
	}

	addEvent := func(id types.Hash256, eventType string, data wallet.EventData, maturityHeight uint64, relevantAddrs []types.Address) {
		ev := wallet.Event{
			ID:             id,
			Index:          index,
			Data:           data,
			Type:           eventType,
			Timestamp:      block.Timestamp,
			MaturityHeight: maturityHeight,
			Relevant:       relevantAddrs,
		}

		if ev.SiacoinInflow().Equals(ev.SiacoinOutflow()) {
			// Skip events that don't affect the wallet.
			return
		}
		events = append(events, ev)
	}

	for _, txn := range block.Transactions {
		relevant, relevantAddrs := relevantV1Txn(txn, addrs)
		if !relevant {
			continue
		}

		event := wallet.EventV1Transaction{
			Transaction: txn,
		}

		for _, si := range txn.SiacoinInputs {
			se, ok := siacoinElements[types.SiacoinOutputID(si.ParentID)]
			if !ok {
				panic("missing transaction siacoin element")
			} else {
				_, found := addrs[se.SiacoinOutput.Address]
				if !found {
					continue
				}
			}
			event.SpentSiacoinElements = append(event.SpentSiacoinElements, se.Copy())
		}
		addEvent(types.Hash256(txn.ID()), wallet.EventTypeV1Transaction, event, index.Height, relevantAddrs)
	}

	for _, txn := range block.V2Transactions() {
		relevant, relevantAddrs := relevantV2Txn(txn, addrs)
		if !relevant {
			continue
		}

		addEvent(types.Hash256(txn.ID()), wallet.EventTypeV2Transaction, wallet.EventV2Transaction(txn), index.Height, relevantAddrs)
	}

	// Add the file contract outputs.
	for _, fced := range cau.FileContractElementDiffs() {
		if !fced.Resolved {
			continue
		}
		fced.FileContractElement.StateElement.MerkleProof = nil // clear the proof to save space
		fce := fced.FileContractElement.Move()

		if fced.Valid {
			for i, so := range fce.FileContract.ValidProofOutputs {
				_, found := addrs[so.Address]
				if !found {
					continue
				}

				outputID := fce.ID.ValidOutputID(i)
				sce, ok := siacoinElements[outputID]
				if !ok {
					panic("missing siacoin element")
				}

				addEvent(types.Hash256(outputID), wallet.EventTypeV1ContractResolution, wallet.EventV1ContractResolution{
					Parent:         fce.Copy(),
					SiacoinElement: sce.Copy(),
					Missed:         false,
				}, sce.MaturityHeight, []types.Address{so.Address})
			}
		} else {
			for i, so := range fce.FileContract.MissedProofOutputs {
				_, found := addrs[so.Address]
				if !found {
					continue
				}

				outputID := fce.ID.MissedOutputID(i)
				sce, ok := siacoinElements[outputID]
				if !ok {
					panic("missing siacoin element")
				}

				addEvent(types.Hash256(outputID), wallet.EventTypeV1ContractResolution, wallet.EventV1ContractResolution{
					Parent:         fce.Copy(),
					SiacoinElement: sce.Copy(),
					Missed:         true,
				}, sce.MaturityHeight, []types.Address{so.Address})
			}
		}
	}

	for _, fced := range cau.V2FileContractElementDiffs() {
		if fced.Resolution == nil {
			continue
		}
		fced.V2FileContractElement.StateElement.MerkleProof = nil // clear the proof to save space
		fce := fced.V2FileContractElement.Move()

		_, missed := fced.Resolution.(*types.V2FileContractExpiration)
		_, found := addrs[fce.V2FileContract.HostOutput.Address]
		if !found {
			outputID := fce.ID.V2HostOutputID()
			sce, ok := siacoinElements[outputID]
			if !ok {
				panic("missing siacoin element")
			}

			addEvent(types.Hash256(outputID), wallet.EventTypeV2ContractResolution, wallet.EventV2ContractResolution{
				Resolution: types.V2FileContractResolution{
					Parent:     fce.Copy(),
					Resolution: fced.Resolution,
				},
				SiacoinElement: sce.Copy(),
				Missed:         missed,
			}, sce.MaturityHeight, []types.Address{fce.V2FileContract.HostOutput.Address})
		}

		_, found = addrs[fce.V2FileContract.RenterOutput.Address]
		if !found {
			outputID := fce.ID.V2RenterOutputID()
			sce, ok := siacoinElements[outputID]
			if !ok {
				panic("missing siacoin element")
			}

			addEvent(types.Hash256(outputID), wallet.EventTypeV2ContractResolution, wallet.EventV2ContractResolution{
				Resolution: types.V2FileContractResolution{
					Parent:     fce.Copy(),
					Resolution: fced.Resolution,
				},
				SiacoinElement: sce.Copy(),
				Missed:         missed,
			}, sce.MaturityHeight, []types.Address{fce.V2FileContract.RenterOutput.Address})
		}
	}

	// Add miner payouts.
	blockID := block.ID()
	for i, so := range block.MinerPayouts {
		_, found := addrs[so.Address]
		if !found {
			continue
		}

		outputID := blockID.MinerOutputID(i)
		sce, ok := siacoinElements[outputID]
		if !ok {
			panic("missing siacoin element")
		}
		addEvent(types.Hash256(outputID), wallet.EventTypeMinerPayout, wallet.EventPayout{
			SiacoinElement: sce.Copy(),
		}, sce.MaturityHeight, []types.Address{so.Address})
	}

	return
}

// applyChainUpdate atomically applies a chain update.
func (w *Wallet) applyChainUpdate(cau chain.ApplyUpdate) error {
	// Update current state elements.
	if err := w.store.updateWalletSiacoinElementProofs(cau); err != nil {
		return utils.AddContext(err, "failed to update state elements")
	}

	var createdUTXOs, spentUTXOs []types.SiacoinElement
	for _, sced := range cau.SiacoinElementDiffs() {
		switch {
		case sced.Created && sced.Spent:
			continue // ignore ephemeral elements
		case !w.store.addressFound(sced.SiacoinElement.SiacoinOutput.Address):
			continue // ignore elements that are not related to the wallet
		case sced.Created:
			createdUTXOs = append(createdUTXOs, sced.SiacoinElement.Share())
		case sced.Spent:
			spentUTXOs = append(spentUTXOs, sced.SiacoinElement.Share())
		default:
			panic("unexpected siacoin element") // developer error
		}
	}

	if err := w.store.walletApplyIndex(createdUTXOs, spentUTXOs, appliedEvents(cau, w.store.addresses)); err != nil {
		return utils.AddContext(err, "failed to apply index")
	}

	return nil
}

// revertChainUpdate atomically reverts a chain update from a wallet.
func (w *Wallet) revertChainUpdate(cru chain.RevertUpdate) error {
	var removedUTXOs, unspentUTXOs []types.SiacoinElement
	for _, sced := range cru.SiacoinElementDiffs() {
		switch {
		case sced.Created && sced.Spent:
			continue // ignore ephemeral elements
		case !w.store.addressFound(sced.SiacoinElement.SiacoinOutput.Address):
			continue // ignore elements that are not related to the wallet
		case sced.Spent:
			unspentUTXOs = append(unspentUTXOs, sced.SiacoinElement.Share())
		case sced.Created:
			removedUTXOs = append(removedUTXOs, sced.SiacoinElement.Share())
		default:
			panic("unexpected siacoin element") // developer error
		}
	}

	// Remove any existing events that were added in the reverted block.
	if err := w.store.walletRevertIndex(removedUTXOs, unspentUTXOs); err != nil {
		return utils.AddContext(err, "failed to revert block")
	}

	// Update the remaining state elements.
	if err := w.store.updateWalletSiacoinElementProofs(cru); err != nil {
		return utils.AddContext(err, "failed to update state elements")
	}

	return nil
}

// updateChainState atomically applies and reverts chain updates to the wallet.
func (w *Wallet) updateChainState(reverted []chain.RevertUpdate, applied []chain.ApplyUpdate) error {
	for _, cru := range reverted {
		err := w.revertChainUpdate(cru)
		if err != nil {
			return fmt.Errorf("failed to revert chain update %q: %w", cru.State.Index, err)
		}
	}

	for _, cau := range applied {
		err := w.applyChainUpdate(cau)
		if err != nil {
			return fmt.Errorf("failed to apply chain update %q: %w", cau.State.Index, err)
		}
	}
	return nil
}
