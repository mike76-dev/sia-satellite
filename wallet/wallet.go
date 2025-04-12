package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/syncer"
	"go.sia.tech/coreutils/wallet"
	"go.uber.org/zap"
)

var (
	defragThreshold     = 300
	maxInputsForDefrag  = 300
	maxDefragUTXOs      = 10
	reservationDuration = 15 * time.Minute
)

// Wallet is a multi-address wallet. It doesn't support Siafunds
// and Foundation subsidies.
type Wallet struct {
	chain    *chain.Manager
	syncer   *syncer.Syncer
	store    *DBStore
	log      *zap.Logger
	mu       sync.Mutex
	ctx      context.Context
	cancel   func()
	locked   map[types.SiacoinOutputID]time.Time
	scanning bool
}

// New returns an initialized Wallet.
func New(cm *chain.Manager, s *syncer.Syncer, db *sql.DB, seedPhrase string, logger *zap.Logger) (*Wallet, error) {
	ctx, cancel := context.WithCancel(context.Background())
	store, tip, err := NewDBStore(db, seedPhrase, logger)
	if err != nil {
		cancel()
		return nil, utils.AddContext(err, "couldn't initialize store")
	}

	w := &Wallet{
		chain:  cm,
		syncer: s,
		store:  store,
		log:    logger,
		locked: make(map[types.SiacoinOutputID]time.Time),
		ctx:    ctx,
		cancel: cancel,
	}

	reorgCh := make(chan struct{}, 1)
	reorgCh <- struct{}{}
	stop := cm.OnReorg(func(index types.ChainIndex) {
		select {
		case reorgCh <- struct{}{}:
		default:
		}
	})

	syncWallet := func() {
		defer stop()

		for cm.Tip().Height <= tip.Height {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(5 * time.Second)
			}
		}

		for {
			if w.scanning {
				time.Sleep(time.Second)
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-reorgCh:
				index := store.lastSyncedIndex()
				if err := w.syncStore(index); err != nil {
					w.log.Error("failed to sync database", zap.Error(err))
				}
			}
		}
	}

	if len(store.addresses) == 0 { // rescan needed
		scanner := newScanner(store.seed)
		go func() {
			w.scanning = true

			// Wait until synced.
			for {
				if w.synced() {
					break
				}

				time.Sleep(time.Second)
			}

			addrs, err := scanner.scan(ctx, cm, defaultAddresses)
			if err != nil {
				logger.Error("failed to scan blockchain for addresses", zap.Error(err))
				w.scanning = false
				return
			}

			for _, index := range addrs {
				_, err = store.insertAddress(index)
				if err != nil {
					logger.Error("failed to insert address", zap.Error(err))
					w.scanning = false
					return
				}
			}

			if len(store.addresses) == 0 { // insert at least one address
				_, err = store.insertAddress(0)
				if err != nil {
					logger.Error("failed to insert root address", zap.Error(err))
					w.scanning = false
					return
				}
			}

			w.scanning = false
			go syncWallet()
		}()
	} else {
		go syncWallet()
	}

	return w, nil
}

// syncStore is called to apply the changed blockchain state to the wallet store.
func (w *Wallet) syncStore(index types.ChainIndex) error {
	for index != w.chain.Tip() {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		reverted, applied, err := w.chain.UpdatesSince(index, 100)
		if err != nil && strings.Contains(err.Error(), "missing block at index") {
			w.log.Warn("missing block at index, resetting chain state", zap.Uint64("height", index.Height))
			if err := w.store.resetChainState(); err != nil {
				return utils.AddContext(err, "failed to reset consensus state")
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to get updates since %v: %w", index, err)
		} else if len(reverted) == 0 && len(applied) == 0 {
			return nil
		}

		if err := w.updateChainState(reverted, applied); err != nil {
			return utils.AddContext(err, "failed to update wallet state")
		}

		if len(applied) > 0 {
			index = applied[len(applied)-1].State.Index
		} else {
			index = reverted[len(reverted)-1].State.Index
		}

		if err := w.store.updateChainState(index, true); err != nil {
			w.log.Error("couldn't update index", zap.Error(err))
			return err
		}
	}

	return nil
}

// Close shuts down the wallet.
func (w *Wallet) Close() {
	w.cancel()
	w.store.close()
}

// synced returns true if the wallet is synced to the blockchain.
func (w *Wallet) synced() bool {
	var count int
	for _, p := range w.syncer.Peers() {
		if p.Synced() {
			count++
		}
	}

	return count >= 5 && time.Since(w.chain.TipState().PrevTimestamps[0]) < 24*time.Hour
}

// isLocked returns true if the Siacoin output with given id is locked, this
// method must be called whilst holding the mutex lock.
func (w *Wallet) isLocked(id types.SiacoinOutputID) bool {
	return time.Now().Before(w.locked[id])
}

// Key returns the private key at the specified index.
func (w *Wallet) Key(index uint64) types.PrivateKey {
	return wallet.KeyFromSeed(w.store.seed, index)
}

// UnspentSiacoinElements returns the wallet's unspent siacoin outputs.
func (w *Wallet) UnspentSiacoinElements() []types.SiacoinElement {
	return w.store.unspentSiacoinElements()
}

// Address returns the address of the wallet at the specified index.
func (w *Wallet) Address(index uint64) types.Address {
	return types.StandardUnlockHash(wallet.KeyFromSeed(w.store.seed, index).PublicKey())
}

// UnlockConditions returns the unlock conditions of the wallet at the specified index.
func (w *Wallet) UnlockConditions(index uint64) types.UnlockConditions {
	return types.StandardUnlockConditions(wallet.KeyFromSeed(w.store.seed, index).PublicKey())
}

// Balance returns the balance of the wallet.
func (w *Wallet) Balance() (balance wallet.Balance) {
	outputs := w.store.unspentSiacoinElements()
	tpoolSpent := make(map[types.SiacoinOutputID]bool)
	tpoolUtxos := make(map[types.SiacoinOutputID]types.SiacoinElement)
	for _, txn := range w.chain.PoolTransactions() {
		for _, sci := range txn.SiacoinInputs {
			tpoolSpent[sci.ParentID] = true
			delete(tpoolUtxos, sci.ParentID)
		}
		for i, sco := range txn.SiacoinOutputs {
			if !w.store.addressFound(sco.Address) {
				continue
			}

			outputID := txn.SiacoinOutputID(i)
			tpoolUtxos[outputID] = types.SiacoinElement{
				ID:            types.SiacoinOutputID(outputID),
				StateElement:  types.StateElement{LeafIndex: types.UnassignedLeafIndex},
				SiacoinOutput: sco,
			}
		}
	}

	for _, txn := range w.chain.V2PoolTransactions() {
		for _, si := range txn.SiacoinInputs {
			tpoolSpent[si.Parent.ID] = true
			delete(tpoolUtxos, si.Parent.ID)
		}
		for i, sco := range txn.SiacoinOutputs {
			if !w.store.addressFound(sco.Address) {
				continue
			}
			sce := txn.EphemeralSiacoinOutput(i)
			tpoolUtxos[sce.ID] = sce.Move()
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	bh := w.chain.TipState().Index.Height
	for _, sco := range outputs {
		if sco.MaturityHeight > bh {
			balance.Immature = balance.Immature.Add(sco.SiacoinOutput.Value)
		} else {
			balance.Confirmed = balance.Confirmed.Add(sco.SiacoinOutput.Value)
			if !w.isLocked(sco.ID) && !tpoolSpent[sco.ID] {
				balance.Spendable = balance.Spendable.Add(sco.SiacoinOutput.Value)
			}
		}
	}

	for _, sco := range tpoolUtxos {
		balance.Unconfirmed = balance.Unconfirmed.Add(sco.SiacoinOutput.Value)
	}
	return
}

// NextAddress generates the next sequential wallet address.
func (w *Wallet) NextAddress() (addr types.Address, err error) {
	return w.store.insertAddress(w.store.greatestIndex + 1)
}

// UnconfirmedEvents returns all unconfirmed transactions relevant to the wallet.
func (w *Wallet) UnconfirmedEvents() (annotated []wallet.Event) {
	confirmed := w.store.unspentSiacoinElements()
	utxos := make(map[types.SiacoinOutputID]types.SiacoinElement)
	for _, se := range confirmed {
		utxos[se.ID] = se.Share()
	}

	index := types.ChainIndex{
		Height: w.chain.TipState().Index.Height + 1,
	}
	timestamp := time.Now().Truncate(time.Second)

	addEvent := func(id types.Hash256, eventType string, data wallet.EventData, relevantAddrs []types.Address) {
		ev := wallet.Event{
			ID:             id,
			Index:          index,
			MaturityHeight: index.Height,
			Timestamp:      timestamp,
			Type:           eventType,
			Data:           data,
			Relevant:       relevantAddrs,
		}

		if ev.SiacoinInflow().Equals(ev.SiacoinOutflow()) {
			// Ignore events that don't affect the wallet.
			return
		}
		annotated = append(annotated, ev)
	}

	for _, txn := range w.chain.PoolTransactions() {
		var relevant []types.Address
		event := wallet.EventV1Transaction{
			Transaction: txn,
		}

		var outflow types.Currency
		for _, sci := range txn.SiacoinInputs {
			sce, ok := utxos[sci.ParentID]
			if !ok {
				// Ignore inputs that don't belong to the wallet.
				continue
			}
			outflow = outflow.Add(sce.SiacoinOutput.Value)
			event.SpentSiacoinElements = append(event.SpentSiacoinElements, sce.Share())
			relevant = append(relevant, sce.SiacoinOutput.Address)
		}

		var inflow types.Currency
		for i, so := range txn.SiacoinOutputs {
			if w.store.addressFound(so.Address) {
				inflow = inflow.Add(so.Value)
				utxos[txn.SiacoinOutputID(i)] = types.SiacoinElement{
					ID:            txn.SiacoinOutputID(i),
					StateElement:  types.StateElement{LeafIndex: types.UnassignedLeafIndex},
					SiacoinOutput: so,
				}
				relevant = append(relevant, so.Address)
			}
		}

		// Skip transactions that don't affect the wallet.
		if inflow.IsZero() && outflow.IsZero() {
			continue
		}
		addEvent(types.Hash256(txn.ID()), wallet.EventTypeV1Transaction, event, relevant)
	}

	for _, txn := range w.chain.V2PoolTransactions() {
		var relevant []types.Address
		var inflow, outflow types.Currency
		for _, sci := range txn.SiacoinInputs {
			if !w.store.addressFound(sci.Parent.SiacoinOutput.Address) {
				continue
			}
			outflow = outflow.Add(sci.Parent.SiacoinOutput.Value)
			relevant = append(relevant, sci.Parent.SiacoinOutput.Address)
		}

		for _, sco := range txn.SiacoinOutputs {
			if !w.store.addressFound(sco.Address) {
				continue
			}
			inflow = inflow.Add(sco.Value)
			relevant = append(relevant, sco.Address)
		}

		// Skip transactions that don't affect the wallet.
		if inflow.IsZero() && outflow.IsZero() {
			continue
		}

		addEvent(types.Hash256(txn.ID()), wallet.EventTypeV2Transaction, wallet.EventV2Transaction(txn), relevant)
	}
	return annotated
}

// ReleaseInputs is a helper function that releases the inputs of txn for use in
// other transactions. It should only be called on transactions that are invalid
// or will never be broadcast.
func (w *Wallet) ReleaseInputs(txns []types.V2Transaction) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, txn := range txns {
		for _, in := range txn.SiacoinInputs {
			delete(w.locked, in.Parent.ID)
		}
	}
}

// selectUTXOs is used to select unspent Siacoin outputs for funding a transaction.
func (w *Wallet) selectUTXOs(amount types.Currency, inputs int, useUnconfirmed bool, elements []types.SiacoinElement) ([]types.SiacoinElement, types.Currency, error) {
	if amount.IsZero() {
		return nil, types.ZeroCurrency, nil
	}

	tpoolSpent := make(map[types.SiacoinOutputID]bool)
	tpoolUtxos := make(map[types.SiacoinOutputID]types.SiacoinElement)
	for _, txn := range w.chain.PoolTransactions() {
		for _, sci := range txn.SiacoinInputs {
			tpoolSpent[sci.ParentID] = true
			delete(tpoolUtxos, sci.ParentID)
		}
		for i, sco := range txn.SiacoinOutputs {
			tpoolUtxos[txn.SiacoinOutputID(i)] = types.SiacoinElement{
				ID:            txn.SiacoinOutputID(i),
				StateElement:  types.StateElement{LeafIndex: types.UnassignedLeafIndex},
				SiacoinOutput: sco,
			}
		}
	}
	for _, txn := range w.chain.V2PoolTransactions() {
		for _, sci := range txn.SiacoinInputs {
			tpoolSpent[sci.Parent.ID] = true
			delete(tpoolUtxos, sci.Parent.ID)
		}
		for i := range txn.SiacoinOutputs {
			sce := txn.EphemeralSiacoinOutput(i)
			tpoolUtxos[sce.ID] = sce.Move()
		}
	}

	// Remove immature, locked and spent outputs.
	cs := w.chain.TipState()
	utxos := make([]types.SiacoinElement, 0, len(elements))
	var usedSum types.Currency
	var immatureSum types.Currency
	for _, sce := range elements {
		if used := w.isLocked(sce.ID) || tpoolSpent[sce.ID]; used {
			usedSum = usedSum.Add(sce.SiacoinOutput.Value)
			continue
		} else if immature := cs.Index.Height < sce.MaturityHeight; immature {
			immatureSum = immatureSum.Add(sce.SiacoinOutput.Value)
			continue
		}
		utxos = append(utxos, sce.Share())
	}

	// Sort by value, descending.
	sort.Slice(utxos, func(i, j int) bool {
		return utxos[i].SiacoinOutput.Value.Cmp(utxos[j].SiacoinOutput.Value) > 0
	})

	var unconfirmedUTXOs []types.SiacoinElement
	var unconfirmedSum types.Currency
	if useUnconfirmed {
		for _, sce := range tpoolUtxos {
			if !w.store.addressFound(sce.SiacoinOutput.Address) || w.isLocked(sce.ID) {
				continue
			}
			unconfirmedUTXOs = append(unconfirmedUTXOs, sce.Share())
			unconfirmedSum = unconfirmedSum.Add(sce.SiacoinOutput.Value)
		}
	}

	// Sort by value, descending.
	sort.Slice(unconfirmedUTXOs, func(i, j int) bool {
		return unconfirmedUTXOs[i].SiacoinOutput.Value.Cmp(unconfirmedUTXOs[j].SiacoinOutput.Value) > 0
	})

	// Fund the transaction using the largest utxos first.
	var selected []types.SiacoinElement
	var inputSum types.Currency
	for i, sce := range utxos {
		if inputSum.Cmp(amount) >= 0 {
			utxos = utxos[i:]
			break
		}
		selected = append(selected, sce.Share())
		inputSum = inputSum.Add(sce.SiacoinOutput.Value)
	}

	if inputSum.Cmp(amount) < 0 && useUnconfirmed {
		// Try adding unconfirmed utxos.
		for _, sce := range unconfirmedUTXOs {
			selected = append(selected, sce.Share())
			inputSum = inputSum.Add(sce.SiacoinOutput.Value)
			if inputSum.Cmp(amount) >= 0 {
				break
			}
		}

		if inputSum.Cmp(amount) < 0 {
			// Still not enough funds.
			return nil, types.ZeroCurrency, fmt.Errorf("%w: inputs %v < needed %v (used: %v immature: %v unconfirmed: %v)", wallet.ErrNotEnoughFunds, inputSum.String(), amount.String(), usedSum.String(), immatureSum.String(), unconfirmedSum.String())
		}
	} else if inputSum.Cmp(amount) < 0 {
		return nil, types.ZeroCurrency, fmt.Errorf("%w: inputs %v < needed %v (used: %v immature: %v", wallet.ErrNotEnoughFunds, inputSum.String(), amount.String(), usedSum.String(), immatureSum.String())
	}

	// Check if remaining utxos should be defragged.
	txnInputs := inputs + len(selected)
	if len(utxos) > defragThreshold && txnInputs < maxInputsForDefrag {
		// Add the smallest utxos to the transaction.
		defraggable := utxos
		if len(defraggable) > maxDefragUTXOs {
			defraggable = defraggable[len(defraggable)-maxDefragUTXOs:]
		}
		for i := len(defraggable) - 1; i >= 0; i-- {
			if txnInputs >= maxInputsForDefrag {
				break
			}

			sce := &defraggable[i]
			selected = append(selected, sce.Share())
			inputSum = inputSum.Add(sce.SiacoinOutput.Value)
			txnInputs++
		}
	}
	return selected, inputSum, nil
}

// FundTransaction adds siacoin inputs worth at least amount to the provided
// V2 transaction. If necessary, a change output will also be added. The inputs
// will not be available to future calls to FundTransaction unless ReleaseInputs
// is called.
//
// The returned index should be used as the basis for AddV2PoolTransactions.
func (w *Wallet) FundTransaction(txn *types.V2Transaction, amount types.Currency, useUnconfirmed bool) (types.ChainIndex, []int, error) {
	if amount.IsZero() {
		return w.store.lastSyncedIndex(), nil, nil
	}

	// Fetch outputs from the store.
	elements := w.store.unspentSiacoinElements()

	w.mu.Lock()
	defer w.mu.Unlock()

	selected, inputSum, err := w.selectUTXOs(amount, len(txn.SiacoinInputs), useUnconfirmed, elements)
	if err != nil {
		return types.ChainIndex{}, nil, err
	}

	// Add a change output if necessary.
	if inputSum.Cmp(amount) > 0 {
		txn.SiacoinOutputs = append(txn.SiacoinOutputs, types.SiacoinOutput{
			Value:   inputSum.Sub(amount),
			Address: w.store.rootAddress(),
		})
	}

	toSign := make([]int, 0, len(selected))
	for _, sce := range selected {
		toSign = append(toSign, len(txn.SiacoinInputs))
		txn.SiacoinInputs = append(txn.SiacoinInputs, types.V2SiacoinInput{
			Parent: sce.Copy(),
		})
		w.locked[sce.ID] = time.Now().Add(reservationDuration)
	}

	return w.store.lastSyncedIndex(), toSign, nil
}

// SignInputs adds a signature to each of the specified siacoin inputs.
func (w *Wallet) SignInputs(txn *types.V2Transaction, toSign []int) {
	if len(toSign) == 0 {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	sigHash := w.chain.TipState().InputSigHash(*txn)
	for _, i := range toSign {
		w.store.mu.Lock()
		index, found := w.store.addresses[txn.SiacoinInputs[i].Parent.SiacoinOutput.Address]
		if !found {
			panic("missing address to sign SC input")
		}
		w.store.mu.Unlock()
		policy := w.SpendPolicy(index)
		txn.SiacoinInputs[i].SatisfiedPolicy = types.SatisfiedPolicy{
			Policy:     policy,
			Signatures: []types.Signature{w.SignHash(sigHash, index)},
		}
	}
}

// SpendPolicy returns the wallet's default spend policy at the specified index.
func (w *Wallet) SpendPolicy(index uint64) types.SpendPolicy {
	return types.SpendPolicy{Type: types.PolicyTypeUnlockConditions(w.UnlockConditions(index))}
}

// SignHash signs the hash with the wallet's private key at the specified index.
func (w *Wallet) SignHash(h types.Hash256, index uint64) types.Signature {
	return wallet.KeyFromSeed(w.store.seed, index).SignHash(h)
}

// Rescan rescans the wallet using up to num addresses.
func (w *Wallet) Rescan(num uint64) error {
	if !w.synced() {
		return errors.New("wallet not synced")
	}

	if w.scanning {
		return errors.New("another scan is already running")
	}

	w.mu.Lock()
	w.scanning = true
	w.mu.Unlock()

	if err := w.store.resetChainState(); err != nil {
		return utils.AddContext(err, "failed to reset store")
	}

	scanner := newScanner(w.store.seed)
	go func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		addrs, err := scanner.scan(w.ctx, w.chain, num)
		if err != nil {
			w.log.Error("failed to scan blockchain for addresses", zap.Error(err))
			w.scanning = false
			return
		}

		for _, index := range addrs {
			_, err = w.store.insertAddress(index)
			if err != nil {
				w.log.Error("failed to insert address", zap.Error(err))
				w.scanning = false
				return
			}
		}

		if len(w.store.addresses) == 0 { // insert at least one address
			_, err = w.store.insertAddress(0)
			if err != nil {
				w.log.Error("failed to insert root address", zap.Error(err))
				w.scanning = false
				return
			}
		}

		w.scanning = false
	}()

	return nil
}
