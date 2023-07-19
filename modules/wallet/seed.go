package wallet

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"golang.org/x/crypto/ed25519"

	"lukechampine.com/frand"
)

var (
	errKnownSeed = errors.New("seed is already known")
)

type (
	// walletSalt is a randomly generated salt put at the front of every
	// persistence object. It is used to make sure that a different encryption
	// key can be used for every persistence object.
	walletSalt [32]byte

	// encryptedSeed stores an encrypted wallet seed on disk.
	encryptedSeed struct {
		UID                    walletSalt
		EncryptionVerification []byte
		Seed                   []byte
	}
)

// generateSpendableKey creates the keys and unlock conditions for seed at a
// given index.
func generateSpendableKey(seed modules.Seed, index uint64) spendableKey {
	h := types.NewHasher()
	h.E.Write(seed[:])
	h.E.WriteUint64(index)
	entropy := h.Sum()
	pk, sk, _ := ed25519.GenerateKey(bytes.NewReader(entropy[:]))
	return spendableKey{
		UnlockConditions: types.UnlockConditions{
			PublicKeys:         []types.UnlockKey{{
				Algorithm: types.SpecifierEd25519,
				Key:       pk,
			}},
			SignaturesRequired: 1,
		},
		SecretKeys: []types.PrivateKey{types.PrivateKey(sk)},
	}
}

// generateKeys generates n keys from seed, starting from index start.
func generateKeys(seed modules.Seed, start, n uint64) []spendableKey {
	// Generate in parallel, one goroutine per core.
	keys := make([]spendableKey, n)
	var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		go func(offset uint64) {
			defer wg.Done()
			for i := offset; i < n; i += uint64(runtime.NumCPU()) {
				// NOTE: don't bother trying to optimize generateSpendableKey;
				// profiling shows that ed25519 key generation consumes far
				// more CPU time than encoding or hashing.
				keys[i] = generateSpendableKey(seed, start + i)
			}
		}(uint64(cpu))
	}
	wg.Wait()
	return keys
}

// createSeed creates and encrypts a seed.
func createSeed(masterKey modules.WalletKey, seed modules.Seed) encryptedSeed {
	var es encryptedSeed
	frand.Read(es.UID[:])
	sek := saltedEncryptionKey(masterKey, es.UID)
	es.EncryptionVerification, _ = modules.Encrypt(sek, verificationPlaintext)
	buf := make([]byte, 32)
	copy(buf[:16], seed[:])
	binary.LittleEndian.PutUint64(buf[16:24], 0)
	binary.LittleEndian.PutUint64(buf[24:], 0)
	es.Seed, _ = modules.Encrypt(sek, buf)
	frand.Read(buf[:])
	return es
}

// decryptSeed decrypts a seed using the encryption key.
func decryptSeed(masterKey modules.WalletKey, es encryptedSeed) (seed modules.Seed, err error) {
	// Verify that the provided master key is the correct key.
	decryptionKey := saltedEncryptionKey(masterKey, es.UID)
	err = verifyEncryption(decryptionKey, es.EncryptionVerification)
	if err != nil {
		return modules.Seed{}, err
	}

	// Decrypt and return the seed.
	plainSeed, err := modules.Decrypt(decryptionKey, es.Seed)
	if err != nil {
		return modules.Seed{}, modules.AddContext(err, "failed to decrypt seed")
	}

	copy(seed[:], plainSeed[:16])
	frand.Read(plainSeed[:])

	return seed, nil
}

// regenerateLookahead creates future keys up to a maximum of maxKeys keys.
func (w *Wallet) regenerateLookahead(start uint64) {
	// Check how many keys need to be generated.
	maxKeys := maxLookahead(start)
	existingKeys := uint64(len(w.lookahead))

	for i, k := range generateKeys(w.primarySeed, start + existingKeys, maxKeys - existingKeys) {
		w.lookahead[k.UnlockConditions.UnlockHash()] = start + existingKeys + uint64(i)
	}
}

// integrateSeed generates n spendableKeys from the seed and loads them into
// the wallet.
func (w *Wallet) integrateSeed(seed modules.Seed, n uint64) {
	for _, sk := range generateKeys(seed, 0, n) {
		w.keys[sk.UnlockConditions.UnlockHash()] = sk
	}
}

// nextPrimarySeedAddress fetches the next n addresses from the primary seed.
func (w *Wallet) nextPrimarySeedAddresses(tx *sql.Tx, n uint64) ([]types.UnlockConditions, error) {
	// Check that the wallet has been unlocked.
	if !w.unlocked {
		return []types.UnlockConditions{}, modules.ErrLockedWallet
	}

	// Check how many unused addresses we have available.
	neededUnused := uint64(len(w.unusedKeys))
	if neededUnused > n {
		neededUnused = n
	}
	n -= neededUnused

	// Generate new keys if the unused ones are not enough. This happens first
	// since it's the only part of the code that might fail. So we don't want to
	// remove keys from the unused map until after we are sure this worked.
	var ucs []types.UnlockConditions
	if n > 0 {
		// Fetch and increment the seed progress.
		progress, err := dbGetPrimarySeedProgress(tx)
		if err != nil {
			return []types.UnlockConditions{}, err
		}
		if err = dbPutPrimarySeedProgress(tx, progress + n); err != nil {
			return []types.UnlockConditions{}, err
		}

		// Integrate the next keys into the wallet, and return the unlock
		// conditions. Also remove new keys from the future keys and update them
		// according to new progress
		spendableKeys := generateKeys(w.primarySeed, progress, n)
		ucs = make([]types.UnlockConditions, 0, len(spendableKeys))
		for _, spendableKey := range spendableKeys {
			w.keys[spendableKey.UnlockConditions.UnlockHash()] = spendableKey
			delete(w.lookahead, spendableKey.UnlockConditions.UnlockHash())
			ucs = append(ucs, spendableKey.UnlockConditions)
		}
		w.regenerateLookahead(progress + n)
	}

	// Add as many unused UCs as necessary.
	unusedUCs := make([]types.UnlockConditions, 0, int(neededUnused))
	for uh, uc := range w.unusedKeys {
		if neededUnused == 0 {
			break
		}
		unusedUCs = append(unusedUCs, uc)
		delete(w.unusedKeys, uh)
		neededUnused--
	}
	ucs = append(unusedUCs, ucs...)

	// Reset map if empty for GC to pick it up.
	if len(w.unusedKeys) == 0 {
		w.unusedKeys = make(map[types.Address]types.UnlockConditions)
	}

	return ucs, nil
}

// nextPrimarySeedAddress fetches the next address from the primary seed.
func (w *Wallet) nextPrimarySeedAddress(tx *sql.Tx) (types.UnlockConditions, error) {
	ucs, err := w.nextPrimarySeedAddresses(tx, 1)
	if err != nil {
		return types.UnlockConditions{}, err
	}
	return ucs[0], nil
}

// AllSeeds returns a list of all seeds known to and used by the wallet.
func (w *Wallet) AllSeeds() ([]modules.Seed, error) {
	if err := w.tg.Add(); err != nil {
		return nil, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return nil, modules.ErrLockedWallet
	}
	return append([]modules.Seed{w.primarySeed}, w.seeds...), nil
}

// PrimarySeed returns the decrypted primary seed of the wallet, as well as
// the number of addresses that the seed can be safely used to generate.
func (w *Wallet) PrimarySeed() (modules.Seed, uint64, error) {
	if err := w.tg.Add(); err != nil {
		return modules.Seed{}, 0, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return modules.Seed{}, 0, modules.ErrLockedWallet
	}
	progress, err := dbGetPrimarySeedProgress(w.dbTx)
	if err != nil {
		return modules.Seed{}, 0, err
	}

	// Addresses remaining is maxScanKeys-progress; generating more keys than
	// that risks not being able to recover them when using SweepSeed or
	// InitFromSeed.
	remaining := maxScanKeys - progress
	if progress > maxScanKeys {
		remaining = 0
	}
	return w.primarySeed, remaining, nil
}

// MarkAddressUnused marks the provided address as unused which causes it to be
// handed out by a subsequent call to `NextAddresses` again.
func (w *Wallet) MarkAddressUnused(addrs ...types.UnlockConditions) error {
	if err := w.tg.Add(); err != nil {
		return modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.managedMarkAddressUnused(addrs...)
	return nil
}

// managedMarkAddressUnused marks the provided address as unused which causes it
// to be handed out by a subsequent call to `NextAddresses` again.
func (w *Wallet) managedMarkAddressUnused(addrs ...types.UnlockConditions) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.markAddressUnused(addrs...)
}

// markAddressUnused marks the provided address as unused which causes it
// to be handed out by a subsequent call to `NextAddresses` again.
func (w *Wallet) markAddressUnused(addrs ...types.UnlockConditions) {
	for _, addr := range addrs {
		w.unusedKeys[addr.UnlockHash()] = addr
	}
}

// NextAddresses returns n unlock hashes that are ready to receive Siacoins or
// Siafunds. The addresses are generated using the primary address seed.
//
// Warning: If this function is used to generate large numbers of addresses,
// those addresses should be used. Otherwise the lookahead might not be able to
// keep up and multiple wallets with the same seed might desync.
func (w *Wallet) NextAddresses(n uint64) ([]types.UnlockConditions, error) {
	if err := w.tg.Add(); err != nil {
		return nil, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Generate some keys and sync the db.
	ucs, err := w.nextPrimarySeedAddresses(w.dbTx, n)
	err = modules.ComposeErrors(err, w.syncDB())
	if err != nil {
		return nil, err
	}

	return ucs, nil
}

// NextAddress returns an unlock hash that is ready to receive Siacoins or
// Siafunds. The address is generated using the primary address seed.
func (w *Wallet) NextAddress() (types.UnlockConditions, error) {
	if err := w.tg.Add(); err != nil {
		return types.UnlockConditions{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	ucs, err := w.NextAddresses(1)
	if err != nil {
		return types.UnlockConditions{}, err
	}
	return ucs[0], nil
}

// LoadSeed will track all of the addresses generated by the input seed,
// reclaiming any funds that were lost due to a deleted seed or lost encryption
// key. An error will be returned if the seed has already been integrated with
// the wallet.
func (w *Wallet) LoadSeed(masterKey modules.WalletKey, seed modules.Seed) error {
	if err := w.tg.Add(); err != nil {
		return modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	if !w.cs.Synced() {
		return errors.New("cannot load seed until blockchain is synced")
	}

	if !w.scanLock.TryLock() {
		return errScanInProgress
	}
	defer w.scanLock.Unlock()

	// Because the recovery seed does not have a UID, duplication must be
	// prevented by comparing with the list of decrypted seeds. This can only
	// occur while the wallet is unlocked.
	w.mu.RLock()
	if !w.unlocked {
		w.mu.RUnlock()
		return modules.ErrLockedWallet
	}
	for _, wSeed := range append([]modules.Seed{w.primarySeed}, w.seeds...) {
		if seed == wSeed {
			w.mu.RUnlock()
			return errKnownSeed
		}
	}
	w.mu.RUnlock()

	// Scan blockchain to determine how many keys to generate for the seed.
	s := newSeedScanner(seed, w.log)
	if err := s.scan(w.cs, w.tg.StopChan()); err != nil {
		return err
	}

	// Add 4% as a buffer because the seed may have addresses in the wild
	// that have not appeared in the blockchain yet.
	seedProgress := s.largestIndexSeen + 500
	seedProgress += seedProgress / 25
	w.log.Printf("INFO: found key index %v in blockchain. Setting auxiliary seed progress to %v", s.largestIndexSeen, seedProgress)

	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		err := checkMasterKey(w.dbTx, masterKey)
		if err != nil {
			return err
		}

		// Create an encrypted seed.
		es := createSeed(masterKey, seed)

		// Add the encrypted seed.
		current, err := dbGetAuxiliarySeeds(w.dbTx)
		if err != nil {
			return err
		}
		err = dbPutAuxiliarySeeds(w.dbTx, append(current, es))
		if err != nil {
			return err
		}

		// Load the seed's keys.
		w.integrateSeed(seed, seedProgress)
		w.seeds = append(w.seeds, seed)

		// Delete the set of processed transactions; they will be recreated
		// when we rescan.
		_, err = w.dbTx.Exec("DELETE FROM wt_addr")
		if err != nil {
			return err
		}
		_, err = w.dbTx.Exec("DELETE FROM wt_txn")
		if err != nil {
			return err
		} 
		w.unconfirmedProcessedTransactions = processedTransactionList{}

		// Reset the consensus change ID and height in preparation for rescan.
		err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
		if err != nil {
			return err
		}
		return dbPutConsensusHeight(w.dbTx, 0)
	}()
	if err != nil {
		return err
	}

	// Rescan the blockchain.
	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	done := make(chan struct{})
	go w.rescanMessage(done)
	defer close(done)

	err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
	if err != nil {
		return err
	}
	w.tpool.TransactionPoolSubscribe(w)
	return nil
}

// SweepSeed scans the blockchain for outputs generated from seed and creates
// a transaction that transfers them to the wallet. Note that this incurs a
// transaction fee. It returns the total value of the outputs, minus the fee.
// If only Siafunds were found, the fee is deducted from the wallet.
func (w *Wallet) SweepSeed(seed modules.Seed) (coins types.Currency, funds uint64, err error) {
	if err = w.tg.Add(); err != nil {
		return types.Currency{}, 0, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	if !w.scanLock.TryLock() {
		return types.Currency{}, 0, errScanInProgress
	}
	defer w.scanLock.Unlock()

	w.mu.RLock()
	match := seed == w.primarySeed
	w.mu.RUnlock()
	if match {
		return types.Currency{}, 0, errors.New("cannot sweep primary seed")
	}

	if !w.cs.Synced() {
		return types.Currency{}, 0, errors.New("cannot sweep until blockchain is synced")
	}

	// Get an address to spend into.
	w.mu.Lock()
	uc, err := w.nextPrimarySeedAddress(w.dbTx)
	height, err2 := dbGetConsensusHeight(w.dbTx)
	w.mu.Unlock()
	if err != nil {
		return types.Currency{}, 0, err
	}
	if err2 != nil {
		return types.Currency{}, 0, err2
	}
	defer func() {
		if err != nil {
			w.managedMarkAddressUnused(uc)
		}
	}()

	// Scan blockchain for outputs, filtering out 'dust' (outputs that cost
	// more in fees than they are worth).
	s := newSeedScanner(seed, w.log)
	_, maxFee := w.tpool.FeeEstimation()
	const outputSize = 350 // Approx. size in bytes of an output and accompanying signature.
	const maxOutputs = 50  // Approx. number of outputs that a transaction can handle.
	s.dustThreshold = maxFee.Mul64(outputSize)
	if err = s.scan(w.cs, w.tg.StopChan()); err != nil {
		return
	}

	if len(s.siacoinOutputs) == 0 && len(s.siafundOutputs) == 0 {
		// If we aren't sweeping any coins or funds, then just return an
		// error; no reason to proceed.
		return types.Currency{}, 0, errors.New("nothing to sweep")
	}

	// Flatten map to slice.
	var siacoinOutputs, siafundOutputs []scannedOutput
	for _, sco := range s.siacoinOutputs {
		siacoinOutputs = append(siacoinOutputs, sco)
	}
	for _, sfo := range s.siafundOutputs {
		siafundOutputs = append(siafundOutputs, sfo)
	}

	for len(siacoinOutputs) > 0 || len(siafundOutputs) > 0 {
		// Process up to maxOutputs siacoinOutputs.
		txnSiacoinOutputs := make([]scannedOutput, maxOutputs)
		n := copy(txnSiacoinOutputs, siacoinOutputs)
		txnSiacoinOutputs = txnSiacoinOutputs[:n]
		siacoinOutputs = siacoinOutputs[n:]

		// Process up to (maxOutputs-n) siafundOutputs.
		txnSiafundOutputs := make([]scannedOutput, maxOutputs - n)
		n = copy(txnSiafundOutputs, siafundOutputs)
		txnSiafundOutputs = txnSiafundOutputs[:n]
		siafundOutputs = siafundOutputs[n:]

		var txnCoins types.Currency
		var txnFunds uint64

		// Construct a transaction that spends the outputs.
		var txn types.Transaction
		var sweptCoins types.Currency // Total value of swept Siacoin outputs.
		var sweptFunds uint64         // Total value of swept Siafund outputs.
		for _, output := range txnSiacoinOutputs {
			// Construct a Siacoin input that spends the output.
			sk := generateSpendableKey(seed, output.seedIndex)
			txn.SiacoinInputs = append(txn.SiacoinInputs, types.SiacoinInput{
				ParentID:         types.SiacoinOutputID(output.id),
				UnlockConditions: sk.UnlockConditions,
			})
			sweptCoins = sweptCoins.Add(output.value)
		}
		for _, output := range txnSiafundOutputs {
			// Construct a Siafund input that spends the output.
			sk := generateSpendableKey(seed, output.seedIndex)
			txn.SiafundInputs = append(txn.SiafundInputs, types.SiafundInput{
				ParentID:         types.SiafundOutputID(output.id),
				UnlockConditions: sk.UnlockConditions,
			})
			sweptFunds = sweptFunds + output.value.Lo
		}

		// Estimate the transaction size and fee. NOTE: this equation doesn't
		// account for other fields in the transaction, but since we are
		// multiplying by maxFee, lowballing is ok.
		estTxnSize := (len(txnSiacoinOutputs) + len(txnSiafundOutputs)) * outputSize
		estFee := maxFee.Mul64(uint64(estTxnSize))
		txn.MinerFees = []types.Currency{estFee}

		// Calculate total Siacoin payout.
		if sweptCoins.Cmp(estFee) > 0 {
			txnCoins = sweptCoins.Sub(estFee)
		}
		txnFunds = sweptFunds

		var parents []types.Transaction
		switch {
		case txnCoins.IsZero() && txnFunds == 0:
			// If we aren't sweeping any coins or funds, then just return an
			// error; no reason to proceed.
			return types.Currency{}, 0, errors.New("transaction fee exceeds value of swept outputs")

		case !txnCoins.IsZero() && txnFunds == 0:
			// If we're sweeping coins but not funds, add a Siacoin output for
			// them.
			txn.SiacoinOutputs = append(txn.SiacoinOutputs, types.SiacoinOutput{
				Value:   txnCoins,
				Address: uc.UnlockHash(),
			})

		case txnCoins.IsZero() && txnFunds != 0:
			// If we're sweeping funds but not coins, add a Siafund output for
			// them. This is tricky because we still need to pay for the
			// transaction fee, but we can't simply subtract the fee from the
			// output value like we can with swept coins. Instead, we need to fund
			// the fee using the existing wallet balance.
			txn.SiafundOutputs = append(txn.SiafundOutputs, types.SiafundOutput{
				Value:   txnFunds,
				Address: uc.UnlockHash(),
			})
			parentTxn, _, err := w.FundTransaction(&txn, estFee)
			if err != nil {
				w.ReleaseInputs(txn)
				return types.Currency{}, 0, modules.AddContext(err, "couldn't pay transaction fee on swept funds")
			}
			parents = append(parents, parentTxn)

		case !txnCoins.IsZero() && txnFunds != 0:
			// If we're sweeping both coins and funds, add a Siacoin output and a
			// Siafund output.
			txn.SiacoinOutputs = append(txn.SiacoinOutputs, types.SiacoinOutput{
				Value:   txnCoins,
				Address: uc.UnlockHash(),
			})
			txn.SiafundOutputs = append(txn.SiafundOutputs, types.SiafundOutput{
				Value:   txnFunds,
				Address: uc.UnlockHash(),
			})
		}

		// Add signatures for all coins and funds.
		for _, output := range txnSiacoinOutputs {
			sk := generateSpendableKey(seed, output.seedIndex)
			addSignatures(&txn, modules.FullCoveredFields(), sk.UnlockConditions, types.Hash256(output.id), sk, height)
		}
		for _, sfo := range txnSiafundOutputs {
			sk := generateSpendableKey(seed, sfo.seedIndex)
			addSignatures(&txn, modules.FullCoveredFields(), sk.UnlockConditions, types.Hash256(sfo.id), sk, height)
		}
		// Usually, all the inputs will come from swept outputs. However, there is
		// an edge case in which inputs will be added from the wallet. To cover
		// this case, we iterate through the SiacoinInputs and add a signature for
		// any input that belongs to the wallet.
		w.mu.RLock()
		for _, input := range txn.SiacoinInputs {
			if key, ok := w.keys[input.UnlockConditions.UnlockHash()]; ok {
				addSignatures(&txn, modules.FullCoveredFields(), input.UnlockConditions, types.Hash256(input.ParentID), key, height)
			}
		}
		w.mu.RUnlock()

		// Append transaction to txnSet.
		txnSet := append(parents, txn)

		// Submit the transactions.
		err = w.tpool.AcceptTransactionSet(txnSet)
		if err != nil {
			w.ReleaseInputs(txn)
			return types.ZeroCurrency, 0, err
		}

		w.log.Println("INFO: creating a transaction set to sweep a seed, IDs:")
		for _, txn := range txnSet {
			w.log.Println("\t", txn.ID())
		}

		coins = coins.Add(txnCoins)
		funds = funds + txnFunds
	}
	return
}
