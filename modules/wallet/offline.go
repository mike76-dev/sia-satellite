package wallet

import (
	"bytes"
	"errors"
	"math"

	"github.com/mike76-dev/sia-satellite/modules"
	
	"go.sia.tech/core/types"
)

// UnspentOutputs returns the unspent outputs tracked by the wallet.
func (w *Wallet) UnspentOutputs() ([]modules.UnspentOutput, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure durability of reported outputs.
	if err := w.syncDB(); err != nil {
		return nil, err
	}

	// Build initial list of confirmed outputs.
	var outputs []modules.UnspentOutput
	dbForEachSiacoinOutput(w.dbTx, func(scoid types.SiacoinOutputID, sco types.SiacoinOutput) {
		outputs = append(outputs, modules.UnspentOutput{
			FundType:   types.SpecifierSiacoinOutput,
			ID:         types.Hash256(scoid),
			UnlockHash: sco.Address,
			Value:      sco.Value,
		})
	})
	dbForEachSiafundOutput(w.dbTx, func(sfoid types.SiafundOutputID, sfo types.SiafundOutput, _ types.Currency) {
		outputs = append(outputs, modules.UnspentOutput{
			FundType:   types.SpecifierSiafundOutput,
			ID:         types.Hash256(sfoid),
			UnlockHash: sfo.Address,
			Value:      types.NewCurrency(sfo.Value, 0),
		})
	})

	// Don't include outputs marked as spent in pending transactions.
	pending := make(map[types.Hash256]struct{})
	curr := w.unconfirmedProcessedTransactions.head
	for curr != nil {
		pt := curr.txn
		for _, input := range pt.Inputs {
			if input.WalletAddress {
				pending[input.ParentID] = struct{}{}
			}
		}
		curr = curr.next
	}
	filtered := outputs[:0]
	for _, o := range outputs {
		if _, ok := pending[o.ID]; !ok {
			filtered = append(filtered, o)
		}
	}
	outputs = filtered

	// Set the confirmation height for each output.
outer:
	for i, o := range outputs {
		txnIndices, err := dbGetAddrTransactions(w.dbTx, o.UnlockHash)
		if err != nil {
			return nil, err
		}
		for _, j := range txnIndices {
			pt, err := dbGetProcessedTransaction(w.dbTx, j)
			if err != nil {
				return nil, err
			}
			for _, sco := range pt.Outputs {
				if sco.ID == o.ID {
					outputs[i].ConfirmationHeight = pt.ConfirmationHeight
					continue outer
				}
			}
		}
	}

	// Add unconfirmed outputs, except those that are spent in pending
	// transactions.
	curr = w.unconfirmedProcessedTransactions.head
	for curr != nil {
		pt := curr.txn
		for _, o := range pt.Outputs {
			if _, ok := pending[o.ID]; !ok && o.WalletAddress {
				outputs = append(outputs, modules.UnspentOutput{
					FundType:           types.SpecifierSiacoinOutput,
					ID:                 o.ID,
					UnlockHash:         o.RelatedAddress,
					Value:              o.Value,
					ConfirmationHeight: math.MaxUint64, // Unconfirmed.
				})
			}
		}
		curr = curr.next
	}

	// Mark the watch-only outputs.
	for i, o := range outputs {
		_, ok := w.watchedAddrs[o.UnlockHash]
		outputs[i].IsWatchOnly = ok
	}

	return outputs, nil
}

// UnlockConditions returns the UnlockConditions for the specified address, if
// they are known to the wallet.
func (w *Wallet) UnlockConditions(addr types.Address) (uc types.UnlockConditions, err error) {
	if err := w.tg.Add(); err != nil {
		return types.UnlockConditions{}, err
	}
	defer w.tg.Done()
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.unlocked {
		return types.UnlockConditions{}, modules.ErrLockedWallet
	}
	if sk, ok := w.keys[addr]; ok {
		uc = sk.UnlockConditions
	} else {
		// Not in memory; try database.
		uc, err = dbGetUnlockConditions(w.dbTx, addr)
		if err != nil {
			return types.UnlockConditions{}, errors.New("no record of UnlockConditions for that UnlockHash")
		}
	}
	// Make a copy of the public key slice; otherwise the caller can modify it.
	uc.PublicKeys = append([]types.UnlockKey(nil), uc.PublicKeys...)
	return uc, nil
}

// AddUnlockConditions adds a set of UnlockConditions to the wallet database.
func (w *Wallet) AddUnlockConditions(uc types.UnlockConditions) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.unlocked {
		return modules.ErrLockedWallet
	}
	return dbPutUnlockConditions(w.dbTx, uc)
}

// SignTransaction signs txn using secret keys known to the wallet. The
// transaction should be complete with the exception of the Signature fields
// of each TransactionSignature referenced by toSign. For convenience, if
// toSign is empty, SignTransaction signs everything that it can.
func (w *Wallet) SignTransaction(txn *types.Transaction, toSign []types.Hash256, cf types.CoveredFields) error {
	// Helper function to add the required number of signatures for the SC input.
	addSiacoinInputSignature := func(input types.SiacoinInput) {
		pubKeys := make(map[uint64]struct{})
		for _, sig := range txn.Signatures {
			if sig.ParentID == types.Hash256(input.ParentID) {
				pubKeys[sig.PublicKeyIndex] = struct{}{}
			}
		}
		for i := range input.UnlockConditions.PublicKeys {
			if _, ok := pubKeys[uint64(i)]; !ok {
				txn.Signatures = append(txn.Signatures, types.TransactionSignature{
					ParentID:       types.Hash256(input.ParentID),
					CoveredFields:  cf,
					PublicKeyIndex: uint64(i),
				})
			}
		}
	}

	// Helper function to add the required number of signatures for the SF input.
	addSiafundInputSignature := func(input types.SiafundInput) {
		pubKeys := make(map[uint64]struct{})
		for _, sig := range txn.Signatures {
			if sig.ParentID == types.Hash256(input.ParentID) {
				pubKeys[sig.PublicKeyIndex] = struct{}{}
			}
		}
		for i := range input.UnlockConditions.PublicKeys {
			if _, ok := pubKeys[uint64(i)]; !ok {
				txn.Signatures = append(txn.Signatures, types.TransactionSignature{
					ParentID:       types.Hash256(input.ParentID),
					CoveredFields:  cf,
					PublicKeyIndex: uint64(i),
				})
			}
		}
	}

	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return modules.ErrLockedWallet
	}
	consensusHeight, err := dbGetConsensusHeight(w.dbTx)
	if err != nil {
		return err
	}

	// Add a signature for each input.
	for _, input := range txn.SiacoinInputs {
		addSiacoinInputSignature(input)
	}
	for _, input := range txn.SiafundInputs {
		addSiafundInputSignature(input)
	}

	// If toSign is empty, sign all inputs that we have keys for.
	if len(toSign) == 0 {
		for _, sci := range txn.SiacoinInputs {
			if _, ok := w.keys[sci.UnlockConditions.UnlockHash()]; ok {
				toSign = append(toSign, types.Hash256(sci.ParentID))
			}
		}
		for _, sfi := range txn.SiafundInputs {
			if _, ok := w.keys[sfi.UnlockConditions.UnlockHash()]; ok {
				toSign = append(toSign, types.Hash256(sfi.ParentID))
			}
		}
	}
	return signTransaction(txn, w.keys, toSign, cf, consensusHeight)
}

// SignTransaction signs txn using secret keys derived from seed. The
// transaction should be complete with the exception of the Signature fields
// of each TransactionSignature referenced by toSign, which must not be empty.
//
// SignTransaction must derive all of the keys from scratch, so it is
// appreciably slower than calling the Wallet.SignTransaction method. Only the
// first 1 million keys are derived.
func SignTransaction(txn *types.Transaction, seed modules.Seed, toSign []types.Hash256, height uint64) error {
	if len(toSign) == 0 {
		// Unlike the wallet method, we can't simply "sign all inputs we have
		// keys for," because without generating all of the keys upfront, we
		// don't know how many inputs we actually have keys for.
		return errors.New("toSign cannot be empty")
	}
	// Generate keys in batches up to 1e6 before giving up.
	keys := make(map[types.Address]spendableKey, 1e6)
	var keyIndex uint64
	const keysPerBatch = 1000
	for len(keys) < 1e6 {
		for _, sk := range generateKeys(seed, keyIndex, keyIndex + keysPerBatch) {
			keys[sk.UnlockConditions.UnlockHash()] = sk
		}
		keyIndex += keysPerBatch
		if err := signTransaction(txn, keys, toSign, modules.FullCoveredFields(), height); err == nil {
			return nil
		}
	}
	return signTransaction(txn, keys, toSign, modules.FullCoveredFields(), height)
}

// signTransaction signs the specified inputs of txn using the specified keys.
// It returns an error if any of the specified inputs cannot be signed.
func signTransaction(txn *types.Transaction, keys map[types.Address]spendableKey, toSign []types.Hash256, cf types.CoveredFields, height uint64) error {
	// Helper function to lookup unlock conditions in the txn associated with
	// a transaction signature's ParentID.
	findUnlockConditions := func(id types.Hash256) (types.UnlockConditions, bool) {
		for _, sci := range txn.SiacoinInputs {
			if types.Hash256(sci.ParentID) == id {
				return sci.UnlockConditions, true
			}
		}
		for _, sfi := range txn.SiafundInputs {
			if types.Hash256(sfi.ParentID) == id {
				return sfi.UnlockConditions, true
			}
		}
		return types.UnlockConditions{}, false
	}
	// Helper function to lookup the secret key that can sign.
	findSigningKey := func(uc types.UnlockConditions, pubkeyIndex uint64) (types.PrivateKey, bool) {
		if pubkeyIndex >= uint64(len(uc.PublicKeys)) {
			return types.PrivateKey{}, false
		}
		pk := uc.PublicKeys[pubkeyIndex]
		sk, ok := keys[uc.UnlockHash()]
		if !ok {
			return types.PrivateKey{}, false
		}
		for _, key := range sk.SecretKeys {
			pubKey := key.PublicKey()
			if bytes.Equal(pk.Key, pubKey[:]) {
				return key, true
			}
		}
		return types.PrivateKey{}, false
	}

	for _, id := range toSign {
		// Find associated txn signature.
		sigIndex := -1
		for i, sig := range txn.Signatures {
			if sig.ParentID == id {
				sigIndex = i
				break
			}
		}
		if sigIndex == -1 {
			return errors.New("toSign references signatures not present in transaction")
		}

		// Find associated input.
		uc, ok := findUnlockConditions(id)
		if !ok {
			return errors.New("toSign references IDs not present in transaction")
		}

		// Lookup the signing key.
		sk, ok := findSigningKey(uc, txn.Signatures[sigIndex].PublicKeyIndex)
		if !ok {
			return errors.New("could not locate signing key for " + id.String())
		}

		// Add signature.
		//
		// NOTE: it's possible that the Signature field will already be filled
		// out. Although we could save a bit of work by not signing it, in
		// practice it's probably best to overwrite any existing signatures,
		// since we know that ours will be valid.
		txn.Signatures[sigIndex].CoveredFields = cf
		sigHash := modules.SigHash(*txn, sigIndex, height)
		encodedSig := sk.SignHash(sigHash)
		txn.Signatures[sigIndex].Signature = encodedSig[:]
	}

	return nil
}

// AddWatchAddresses instructs the wallet to begin tracking a set of
// addresses, in addition to the addresses it was previously tracking. If none
// of the addresses have appeared in the blockchain, the unused flag may be
// set to true. Otherwise, the wallet must rescan the blockchain to search for
// transactions containing the addresses.
func (w *Wallet) AddWatchAddresses(addrs []types.Address, unused bool) error {
	if err := w.tg.Add(); err != nil {
		return modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.unlocked {
			return modules.ErrLockedWallet
		}

		// Update in-memory map.
		for _, addr := range addrs {
			w.watchedAddrs[addr] = struct{}{}
		}

		// Update database.
		alladdrs := make([]types.Address, 0, len(w.watchedAddrs))
		for addr := range w.watchedAddrs {
			alladdrs = append(alladdrs, addr)
		}
		if err := dbPutWatchedAddresses(w.dbTx, alladdrs); err != nil {
			return err
		}

		if !unused {
			// Prepare to rescan.
			_, err := w.dbTx.Exec("DELETE FROM wt_addr")
			if err != nil {
				return err
			}
			_, err = w.dbTx.Exec("DELETE FROM wt_txn")
			if err != nil {
				return err
			}
			w.unconfirmedProcessedTransactions = processedTransactionList{}
			if err := dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning); err != nil {
				return err
			}
			if err := dbPutConsensusHeight(w.dbTx, 0); err != nil {
				return err
			}
		}
		return w.syncDB()
	}()
	if err != nil {
		return err
	}

	if !unused {
		// Rescan the blockchain.
		w.cs.Unsubscribe(w)
		w.tpool.Unsubscribe(w)

		done := make(chan struct{})
		go w.rescanMessage(done)
		defer close(done)
		if err := w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan()); err != nil {
			return err
		}
		w.tpool.TransactionPoolSubscribe(w)
	}

	return nil
}

// RemoveWatchAddresses instructs the wallet to stop tracking a set of
// addresses and delete their associated transactions. If none of the
// addresses have appeared in the blockchain, the unused flag may be set to
// true. Otherwise, the wallet must rescan the blockchain to rebuild its
// transaction history.
func (w *Wallet) RemoveWatchAddresses(addrs []types.Address, unused bool) error {
	if err := w.tg.Add(); err != nil {
		return modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.unlocked {
			return modules.ErrLockedWallet
		}

		// Update in-memory map.
		for _, addr := range addrs {
			delete(w.watchedAddrs, addr)
		}

		// Update database.
		alladdrs := make([]types.Address, 0, len(w.watchedAddrs))
		for addr := range w.watchedAddrs {
			alladdrs = append(alladdrs, addr)
		}
		if err := dbPutWatchedAddresses(w.dbTx, alladdrs); err != nil {
			return err
		}

		if !unused {
			// Outputs associated with the addresses may be present in the
			// SiacoinOutputs bucket. Iterate through the bucket and remove
			// any outputs that we are no longer watching.
			var outputIDs []types.SiacoinOutputID
			dbForEachSiacoinOutput(w.dbTx, func(scoid types.SiacoinOutputID, sco types.SiacoinOutput) {
				if !w.isWalletAddress(sco.Address) {
					outputIDs = append(outputIDs, scoid)
				}
			})
			for _, scoid := range outputIDs {
				if err := dbDeleteSiacoinOutput(w.dbTx, scoid); err != nil {
					return err
				}
			}

			// Prepare to rescan.
			_, err := w.dbTx.Exec("DELETE FROM wt_addr")
			if err != nil {
				return err
			}
			_, err = w.dbTx.Exec("DELETE FROM wt_txn")
			if err != nil {
				return err
			}
			w.unconfirmedProcessedTransactions = processedTransactionList{}
			if err := dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning); err != nil {
				return err
			}
			if err := dbPutConsensusHeight(w.dbTx, 0); err != nil {
				return err
			}
		}
		return w.syncDB()
	}()
	if err != nil {
		return err
	}

	if !unused {
		// Rescan the blockchain.
		w.cs.Unsubscribe(w)
		w.tpool.Unsubscribe(w)

		done := make(chan struct{})
		go w.rescanMessage(done)
		defer close(done)
		if err := w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan()); err != nil {
			return err
		}
		w.tpool.TransactionPoolSubscribe(w)
	}

	return nil
}

// WatchAddresses returns the set of addresses that the wallet is currently
// watching.
func (w *Wallet) WatchAddresses() ([]types.Address, error) {
	if err := w.tg.Add(); err != nil {
		return nil, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.RLock()
	defer w.mu.RUnlock()

	addrs := make([]types.Address, 0, len(w.watchedAddrs))
	for addr := range w.watchedAddrs {
		addrs = append(addrs, addr)
	}
	return addrs, nil
}
