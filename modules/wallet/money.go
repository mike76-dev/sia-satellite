package wallet

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.sia.tech/core/consensus"
	"go.sia.tech/core/types"
	"go.uber.org/zap"
)

// sortedOutputs is a struct containing a slice of siacoin outputs and their
// corresponding ids. sortedOutputs can be sorted using the sort package.
type sortedOutputs struct {
	ids     []types.SiacoinOutputID
	outputs []types.SiacoinOutput
}

// Len returns the number of elements in the sortedOutputs struct.
func (so sortedOutputs) Len() int {
	return len(so.ids)
}

// Less returns whether element 'i' is less than element 'j'. The currency
// value of each output is used for comparison.
func (so sortedOutputs) Less(i, j int) bool {
	return so.outputs[i].Value.Cmp(so.outputs[j].Value) < 0
}

// Swap swaps two elements in the sortedOutputs set.
func (so sortedOutputs) Swap(i, j int) {
	so.ids[i], so.ids[j] = so.ids[j], so.ids[i]
	so.outputs[i], so.outputs[j] = so.outputs[j], so.outputs[i]
}

// DustThreshold returns the quantity per byte below which a Currency is
// considered to be Dust.
func (w *Wallet) DustThreshold() types.Currency {
	return w.cm.RecommendedFee().Mul64(3)
}

// ConfirmedBalance returns the total balance of the wallet.
func (w *Wallet) ConfirmedBalance() (siacoins, immatureSiacoins types.Currency, siafunds uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	dustThreshold := w.DustThreshold()
	height := w.cm.Tip().Height
	for _, sce := range w.sces {
		if sce.SiacoinOutput.Value.Cmp(dustThreshold) > 0 {
			if height >= sce.MaturityHeight {
				siacoins = siacoins.Add(sce.SiacoinOutput.Value)
			} else {
				immatureSiacoins = immatureSiacoins.Add(sce.SiacoinOutput.Value)
			}
		}
	}

	for _, sfe := range w.sfes {
		siafunds += sfe.SiafundOutput.Value
	}

	return
}

// UnconfirmedBalance returns the balance of the wallet contained in
// the unconfirmed transactions.
func (w *Wallet) UnconfirmedBalance() (outgoing, incoming types.Currency) {
	w.mu.Lock()
	defer w.mu.Unlock()

	dustThreshold := w.DustThreshold()
	ids := make(map[types.SiacoinOutputID]types.Currency)
	for _, sce := range w.sces {
		ids[types.SiacoinOutputID(sce.ID)] = sce.SiacoinOutput.Value
	}

	txns := w.cm.PoolTransactions()
	for _, txn := range txns {
		for _, sci := range txn.SiacoinInputs {
			if value, exists := ids[sci.ParentID]; exists {
				outgoing = outgoing.Add(value)
			}
		}
		for _, sco := range txn.SiacoinOutputs {
			if _, exists := w.addrs[sco.Address]; exists && sco.Value.Cmp(dustThreshold) > 0 {
				incoming = incoming.Add(sco.Value)
			}
		}
	}

	return
}

// AddressBalance returns the balance of the given address.
func (w *Wallet) AddressBalance(addr types.Address) (siacoins types.Currency, siafunds uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	sce, exists := w.sces[addr]
	if exists {
		siacoins = sce.SiacoinOutput.Value
	}

	sfe, exists := w.sfes[addr]
	if exists {
		siafunds = sfe.SiafundOutput.Value
	}

	return
}

// Fund adds Siacoin inputs with the required amount to the transaction.
func (w *Wallet) Fund(txn *types.Transaction, amount types.Currency) (parents []types.Transaction, toSign []types.Hash256, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if amount.IsZero() {
		return nil, nil, nil
	}

	var utxos []types.SiacoinElement
	for _, sce := range w.sces {
		utxos = append(utxos, sce)
	}
	sort.Slice(utxos, func(i, j int) bool {
		return utxos[i].SiacoinOutput.Value.Cmp(utxos[j].SiacoinOutput.Value) > 0
	})

	inPool := make(map[types.SiacoinOutputID]bool)
	for _, ptxn := range w.cm.PoolTransactions() {
		for _, in := range ptxn.SiacoinInputs {
			inPool[in.ParentID] = true
		}
	}

	var outputSum types.Currency
	var fundingElements []types.SiacoinElement
	for _, sce := range utxos {
		if w.used[types.Hash256(sce.ID)] || inPool[types.SiacoinOutputID(sce.ID)] {
			continue
		}
		fundingElements = append(fundingElements, sce)
		outputSum = outputSum.Add(sce.SiacoinOutput.Value)
		if outputSum.Cmp(amount) >= 0 {
			break
		}
	}

	if outputSum.Cmp(amount) < 0 {
		return nil, nil, modules.ErrInsufficientBalance
	} else if outputSum.Cmp(amount) > 0 {
		refundUC, err := w.nextAddress()
		defer func() {
			if err != nil {
				w.markAddressUnused(refundUC)
			}
		}()
		if err != nil {
			return nil, nil, err
		}
		txn.SiacoinOutputs = append(txn.SiacoinOutputs, types.SiacoinOutput{
			Value:   outputSum.Sub(amount),
			Address: refundUC.UnlockHash(),
		})
	}

	toSign = make([]types.Hash256, len(fundingElements))
	for i, sce := range fundingElements {
		if key, ok := w.keys[sce.SiacoinOutput.Address]; ok {
			txn.SiacoinInputs = append(txn.SiacoinInputs, types.SiacoinInput{
				ParentID:         types.SiacoinOutputID(sce.ID),
				UnlockConditions: types.StandardUnlockConditions(key.PublicKey()),
			})
			toSign[i] = types.Hash256(sce.ID)
			w.used[sce.ID] = true
		}
	}

	return w.cm.UnconfirmedParents(*txn), toSign, nil
}

// Release marks the outputs as unused.
func (w *Wallet) Release(txnSet []types.Transaction) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, txn := range txnSet {
		for i := range txn.SiacoinOutputs {
			delete(w.used, types.Hash256(txn.SiacoinOutputID((i))))
		}
	}
}

// Reserve reserves the given ids for the given duration.
func (w *Wallet) Reserve(ids []types.Hash256, duration time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if any of the ids are already reserved.
	for _, id := range ids {
		if w.used[id] {
			return fmt.Errorf("output %q already reserved", id)
		}
	}

	// Reserve the ids.
	for _, id := range ids {
		w.used[id] = true
	}

	// Sleep for the duration and then unreserve the ids.
	time.AfterFunc(duration, func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		for _, id := range ids {
			delete(w.used, id)
		}
	})
	return nil
}

// Sign signs the specified transaction using keys derived from the wallet seed.
// If toSign is nil, SignTransaction will automatically add Signatures for each
// input owned by the seed. If toSign is not nil, it is a list of IDs of Signatures
// already present in txn; SignTransaction will fill in the Signature field of each.
func (w *Wallet) Sign(cs consensus.State, txn *types.Transaction, toSign []types.Hash256) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(toSign) == 0 {
		// Lazy mode: add standard sigs for every input we own.
		for _, sci := range txn.SiacoinInputs {
			if key, ok := w.keys[sci.UnlockConditions.UnlockHash()]; ok {
				txn.Signatures = append(txn.Signatures, StandardTransactionSignature(types.Hash256(sci.ParentID)))
				SignTransaction(cs, txn, len(txn.Signatures)-1, key)
			}
		}
		for _, sfi := range txn.SiafundInputs {
			if key, ok := w.keys[sfi.UnlockConditions.UnlockHash()]; ok {
				txn.Signatures = append(txn.Signatures, StandardTransactionSignature(types.Hash256(sfi.ParentID)))
				SignTransaction(cs, txn, len(txn.Signatures)-1, key)
			}
		}
		return nil
	}

	sigAddr := func(id types.Hash256) (types.Address, bool) {
		for _, sci := range txn.SiacoinInputs {
			if types.Hash256(sci.ParentID) == id {
				return sci.UnlockConditions.UnlockHash(), true
			}
		}
		for _, sfi := range txn.SiafundInputs {
			if types.Hash256(sfi.ParentID) == id {
				return sfi.UnlockConditions.UnlockHash(), true
			}
		}
		for _, fcr := range txn.FileContractRevisions {
			if types.Hash256(fcr.ParentID) == id {
				return fcr.UnlockConditions.UnlockHash(), true
			}
		}
		return types.Address{}, false
	}

outer:
	for _, parent := range toSign {
		for sigIndex, sig := range txn.Signatures {
			if sig.ParentID == parent {
				if addr, ok := sigAddr(parent); !ok {
					return fmt.Errorf("ID %v not present in transaction", parent)
				} else if key, ok := w.keys[addr]; !ok {
					return fmt.Errorf("missing key for ID %v", parent)
				} else {
					SignTransaction(cs, txn, sigIndex, key)
					continue outer
				}
			}
		}
		return fmt.Errorf("signature %v not present in transaction", parent)
	}
	return nil
}

// SendSiacoins creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned. Fees
// are added to the amount sent.
func (w *Wallet) SendSiacoins(amount types.Currency, dest types.Address) ([]types.Transaction, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()

	if !w.synced() {
		return nil, errors.New("cannot send Siacoins until fully synced")
	}

	fee := w.cm.RecommendedFee().Mul64(750)
	output := types.SiacoinOutput{
		Value:   amount,
		Address: dest,
	}
	txn := types.Transaction{
		SiacoinOutputs: []types.SiacoinOutput{output},
		MinerFees:      []types.Currency{fee},
	}

	parents, toSign, err := w.Fund(&txn, amount.Add(fee))
	if err != nil {
		w.log.Error("failed to fund transaction", zap.Error(err))
		return nil, modules.AddContext(err, "unable to fund transaction")
	}

	for _, id := range toSign {
		txn.Signatures = append(txn.Signatures, types.TransactionSignature{
			ParentID:      id,
			CoveredFields: types.CoveredFields{WholeTransaction: true},
		})
	}

	err = w.Sign(w.cm.TipState(), &txn, toSign)
	if err != nil {
		w.log.Error("failed to sign transaction", zap.Error(err))
		w.Release(append(parents, txn))
		return nil, modules.AddContext(err, "unable to sign transaction")
	}

	txnSet := append(parents, txn)
	_, err = w.cm.AddPoolTransactions(txnSet)
	if err != nil {
		w.Release(txnSet)
		w.log.Error("transaction set rejected", zap.Error(err))
		return nil, modules.AddContext(err, "invalid transaction set")
	}

	w.s.BroadcastTransactionSet(txnSet)
	w.log.Info("successfully sent amount", zap.Stringer("amount", amount), zap.Stringer("fee", fee), zap.Stringer("destination", dest))

	return txnSet, nil
}
