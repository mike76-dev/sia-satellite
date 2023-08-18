package wallet

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

var (
	errAlreadyUnlocked   = errors.New("wallet has already been unlocked")
	errReencrypt         = errors.New("wallet is already encrypted, cannot encrypt again")
	errScanInProgress    = errors.New("another wallet rescan is already underway")
	errUnencryptedWallet = errors.New("wallet has not been encrypted yet")

	// verificationPlaintext is the plaintext used to verify encryption keys.
	// By storing the corresponding ciphertext for a given key, we can later
	// verify that a key is correct by using it to decrypt the ciphertext and
	// comparing the result to verificationPlaintext.
	verificationPlaintext = []byte("Sia Satellite is built upon Sia!")
)

// saltedEncryptionKey creates an encryption key that is used to decrypt a
// specific key.
func saltedEncryptionKey(masterKey modules.WalletKey, salt walletSalt) modules.WalletKey {
	h := types.NewHasher()
	h.E.Write(masterKey[:])
	h.E.Write(salt[:])
	entropy := h.Sum()
	return modules.WalletKey(entropy[:])
}

// walletPasswordEncryptionKey creates an encryption key that is used to
// encrypt/decrypt the master encryption key.
func walletPasswordEncryptionKey(seed modules.Seed, salt walletSalt) modules.WalletKey {
	h := types.NewHasher()
	h.E.Write(seed[:])
	h.E.Write(salt[:])
	entropy := h.Sum()
	return modules.WalletKey(entropy[:])
}

// verifyEncryption verifies that key properly decrypts the ciphertext to a
// preset plaintext.
func verifyEncryption(key modules.WalletKey, encrypted []byte) error {
	verification, err := modules.Decrypt(key, encrypted)
	if err != nil {
		contextErr := modules.AddContext(modules.ErrBadEncryptionKey, "failed to decrypt key")
		return modules.ComposeErrors(err, contextErr)
	}
	if !bytes.Equal(verificationPlaintext, verification) {
		return modules.ErrBadEncryptionKey
	}
	return nil
}

// checkMasterKey verifies that the masterKey is the key used to encrypt the
// wallet.
func checkMasterKey(tx *sql.Tx, masterKey modules.WalletKey) error {
	if masterKey == nil {
		return modules.ErrBadEncryptionKey
	}
	uk := saltedEncryptionKey(masterKey, dbGetWalletSalt(tx))
	encryptedVerification, err := dbGetEncryptedVerification(tx)
	if err != nil {
		return err
	}
	return verifyEncryption(uk, encryptedVerification)
}

// initEncryption initializes and encrypts the primary seed.
func (w *Wallet) initEncryption(masterKey modules.WalletKey, seed modules.Seed, progress uint64) (modules.Seed, error) {
	// Check if the wallet encryption key has already been set.
	encryptedVerification, err := dbGetEncryptedVerification(w.dbTx)
	if err != nil {
		return modules.Seed{}, err
	}
	if encryptedVerification != nil {
		return modules.Seed{}, errReencrypt
	}

	// Save the primary seed.
	s := createSeed(masterKey, seed)
	err = dbPutPrimarySeed(w.dbTx, s)
	if err != nil {
		return modules.Seed{}, err
	}

	// Record the progress.
	err = dbPutPrimarySeedProgress(w.dbTx, progress)
	if err != nil {
		return modules.Seed{}, err
	}

	// Establish the encryption verification using the masterKey. After this
	// point, the wallet is encrypted.
	uk := saltedEncryptionKey(masterKey, dbGetWalletSalt(w.dbTx))
	encryptedVerification, err = modules.Encrypt(uk, verificationPlaintext)
	if err != nil {
		return modules.Seed{}, modules.AddContext(err, "failed to encrypt verification")
	}
	err = dbPutEncryptedVerification(w.dbTx, encryptedVerification)
	if err != nil {
		return modules.Seed{}, err
	}

	// Encrypt the masterkey using the seed to allow for a masterkey recovery using
	// the seed.
	wpk := walletPasswordEncryptionKey(seed, dbGetWalletSalt(w.dbTx))
	encrypted, err := modules.Encrypt(wpk, masterKey[:])
	if err != nil {
		return modules.Seed{}, modules.AddContext(err, "failed to encrypt masterkey")
	}
	err = dbPutWalletPassword(w.dbTx, encrypted)
	if err != nil {
		return modules.Seed{}, err
	}

	// On future startups, this field will be set by w.initPersist.
	w.encrypted = true

	return seed, nil
}

// managedMasterKey retrieves the masterkey that was stored encrypted in the
// wallet's database.
func (w *Wallet) managedMasterKey(seed modules.Seed) (modules.WalletKey, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if wallet is encrypted.
	if !w.encrypted {
		return nil, errUnencryptedWallet
	}

	// Compute password from seed.
	wpk := walletPasswordEncryptionKey(seed, dbGetWalletSalt(w.dbTx))

	// Grab the encrypted masterkey.
	encryptedMK, err := dbGetWalletPassword(w.dbTx)
	if err != nil {
		return nil, err
	}
	if len(encryptedMK) == 0 {
		return nil, errors.New("wallet is encrypted but masterkey is missing")
	}

	// Decrypt the masterkey.
	masterKey, err := modules.Decrypt(wpk, encryptedMK)
	if err != nil {
		return nil, modules.AddContext(err, "failed to decrypt masterkey")
	}

	return modules.WalletKey(masterKey), nil
}

// managedUnlock loads all of the encrypted structures into wallet memory. Even
// after loading, the structures are kept encrypted, but some data such as
// addresses are decrypted so that the wallet knows what to track.
func (w *Wallet) managedUnlock(masterKey modules.WalletKey) <-chan error {
	errChan := make(chan error, 1)

	// Blocking unlock.
	lastChange, err := w.managedBlockingUnlock(masterKey)
	if err != nil {
		errChan <- err
		return errChan
	}

	// Non-blocking unlock.
	go func() {
		defer close(errChan)
		if err := w.tg.Add(); err != nil {
			errChan <- err
			return
		}
		defer w.tg.Done()
		err := w.managedAsyncUnlock(lastChange)
		if err != nil {
			errChan <- err
		}
	}()
	return errChan
}

// managedBlockingUnlock handles the blocking part of hte managedUnlock method.
func (w *Wallet) managedBlockingUnlock(masterKey modules.WalletKey) (modules.ConsensusChangeID, error) {
	w.mu.RLock()
	unlocked := w.unlocked
	encrypted := w.encrypted
	w.mu.RUnlock()
	if unlocked {
		return modules.ConsensusChangeID{}, errAlreadyUnlocked
	} else if !encrypted {
		return modules.ConsensusChangeID{}, errUnencryptedWallet
	}

	// Load db objects into memory.
	var lastChange modules.ConsensusChangeID
	var encryptedPrimarySeed encryptedSeed
	var primarySeedProgress uint64
	var auxiliarySeeds []encryptedSeed
	var unseededKeys []encryptedSpendableKey
	var watchedAddrs []types.Address
	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		// Verify masterKey.
		err := checkMasterKey(w.dbTx, masterKey)
		if err != nil {
			return err
		}

		// lastChange.
		lastChange = dbGetConsensusChangeID(w.dbTx)

		// encryptedPrimarySeed + primarySeedProgress.
		encryptedPrimarySeed, err = dbGetPrimarySeed(w.dbTx)
		if err != nil {
			return err
		}
		primarySeedProgress, err = dbGetPrimarySeedProgress(w.dbTx)
		if err != nil {
			return err
		}

		// auxiliarySeeds.
		auxiliarySeeds, err = dbGetAuxiliarySeeds(w.dbTx)
		if err != nil {
			return err
		}

		// unseededKeys.
		unseededKeys, err = dbGetUnseededKeys(w.dbTx)
		if err != nil {
			return err
		}

		// watchedAddrs.
		watchedAddrs, err = dbGetWatchedAddresses(w.dbTx)
		if err != nil {
			return err
		}

		return nil
	}()
	if err != nil {
		return modules.ConsensusChangeID{}, err
	}

	// Decrypt + load keys.
	err = func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		// primarySeed.
		primarySeed, err := decryptSeed(masterKey, encryptedPrimarySeed)
		if err != nil {
			return err
		}
		w.integrateSeed(primarySeed, primarySeedProgress)
		w.primarySeed = primarySeed
		w.regenerateLookahead(primarySeedProgress)

		// auxiliarySeeds.
		for _, as := range auxiliarySeeds {
			auxSeed, err := decryptSeed(masterKey, as)
			if err != nil {
				return err
			}
			w.integrateSeed(auxSeed, modules.PublicKeysPerSeed)
			w.seeds = append(w.seeds, auxSeed)
		}

		// unseededKeys.
		for _, uk := range unseededKeys {
			sk, err := decryptSpendableKey(masterKey, uk)
			if err != nil {
				return err
			}
			w.integrateSpendableKey(masterKey, sk)
		}

		// watchedAddrs.
		for _, addr := range watchedAddrs {
			w.watchedAddrs[addr] = struct{}{}
		}

		// If the wallet password hasn't been encrypted yet using the seed, do it.
		wpk, err := dbGetWalletPassword(w.dbTx)
		if err != nil {
			return err
		}
		if len(wpk) == 0 {
			wpk = walletPasswordEncryptionKey(primarySeed, dbGetWalletSalt(w.dbTx))
			encrypted, err := modules.Encrypt(wpk, masterKey)
			if err != nil {
				return modules.AddContext(err, "failed to encrypt masterkey")
			}
			return dbPutWalletPassword(w.dbTx, encrypted)
		}
		return nil
	}()
	if err != nil {
		return modules.ConsensusChangeID{}, err
	}

	w.mu.Lock()
	w.unlocked = true
	w.mu.Unlock()
	return lastChange, nil
}

// managedAsyncUnlock handles the async part of hte managedUnlock method.
func (w *Wallet) managedAsyncUnlock(lastChange modules.ConsensusChangeID) error {
	// Subscribe to the consensus set if this is the first unlock for the
	// wallet object.
	w.subscribedMu.Lock()
	defer w.subscribedMu.Unlock()
	if !w.subscribed {
		// Subscription can take a while, so spawn a goroutine to print the
		// wallet height every few seconds. (If subscription completes
		// quickly, nothing will be printed.)
		done := make(chan struct{})
		go w.rescanMessage(done)
		defer close(done)

		err := w.cs.ConsensusSetSubscribe(w, lastChange, w.tg.StopChan())
		if modules.ContainsError(err, modules.ErrInvalidConsensusChangeID) {
			// Something went wrong; resubscribe from the beginning.
			err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
			if err != nil {
				return fmt.Errorf("failed to reset db during rescan: %v", err)
			}
			err = dbPutConsensusHeight(w.dbTx, 0)
			if err != nil {
				return fmt.Errorf("failed to reset db during rescan: %v", err)
			}
			// Delete the wallet history before resubscribing. Otherwise we
			// will end up with duplicate entries in the database.
			err = dbResetBeforeRescan(w.dbTx)
			if err != nil {
				return fmt.Errorf("failed to reset wallet history during rescan: %v", err)
			}
			err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
		}
		if err != nil {
			return fmt.Errorf("wallet subscription failed: %v", err)
		}
		w.tpool.TransactionPoolSubscribe(w)
	}
	w.subscribed = true
	return nil
}

// rescanMessage prints the blockheight every 3 seconds until done is closed.
func (w *Wallet) rescanMessage(done chan struct{}) {
	// Sleep first because we may not need to print a message at all if
	// done is closed quickly.
	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
	}

	for {
		w.mu.Lock()
		height, _ := dbGetConsensusHeight(w.dbTx)
		w.mu.Unlock()
		fmt.Printf("\rWallet: scanned to height %d...", height)

		select {
		case <-done:
			fmt.Println("\nDone!")
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// wipeSecrets erases all of the seeds and secret keys in the wallet.
func (w *Wallet) wipeSecrets() {
	// 'for i := range' must be used to prevent copies of secret data from
	// being made.
	for i := range w.keys {
		for j := range w.keys[i].SecretKeys {
			for k := range w.keys[i].SecretKeys[j] {
				w.keys[i].SecretKeys[j][k] = 0
			}
		}
	}
	for i := range w.seeds {
		for j := range w.seeds[i] {
			w.seeds[i][j] = 0
		}
	}
	for i := range w.primarySeed {
		w.primarySeed[i] = 0
	}
	w.seeds = w.seeds[:0]
}

// Encrypted returns whether or not the wallet has been encrypted.
func (w *Wallet) Encrypted() (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encrypted, nil
}

// Encrypt will create a primary seed for the wallet and encrypt it using
// masterKey. If masterKey is blank, then the hash of the primary seed will be
// used instead. The wallet will still be locked after Encrypt is called.
//
// Encrypt can only be called once throughout the life of the wallet, and will
// return an error on subsequent calls (even after restarting the wallet). To
// reset the wallet, the wallet must be deleted.
func (w *Wallet) Encrypt(masterKey modules.WalletKey) (modules.Seed, error) {
	if err := w.tg.Add(); err != nil {
		return modules.Seed{}, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()

	// Create a random seed.
	var seed modules.Seed
	frand.Read(seed[:])

	// If masterKey is blank, use the hash of the seed.
	if masterKey == nil {
		h := types.NewHasher()
		h.E.Write(seed[:])
		hash := h.Sum()
		key := make([]byte, len(hash))
		copy(key[:], hash[:])
		masterKey = modules.WalletKey(key)
		frand.Read(hash[:])
	}

	// Initial seed progress is 0.
	return w.initEncryption(masterKey, seed, 0)
}

// Reset will reset the wallet, clearing the database and returning it to
// the unencrypted state.
func (w *Wallet) Reset() error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	// Unsubscribe if we are currently subscribed.
	w.subscribedMu.Lock()
	if w.subscribed {
		w.cs.Unsubscribe(w)
		w.tpool.Unsubscribe(w)
		w.subscribed = false
	}
	w.subscribedMu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	err := dbReset(w.dbTx)
	if err != nil {
		return err
	}
	w.wipeSecrets()
	w.keys = make(map[types.Address]spendableKey)
	w.lookahead = make(map[types.Address]uint64)
	w.seeds = []modules.Seed{}
	w.unconfirmedProcessedTransactions = processedTransactionList{}
	w.unlocked = false
	w.encrypted = false

	return nil
}

// InitFromSeed functions like Init, but using a specified seed. Unlike Init,
// the blockchain will be scanned to determine the seed's progress. For this
// reason, InitFromSeed should not be called until the blockchain is fully
// synced.
func (w *Wallet) InitFromSeed(masterKey modules.WalletKey, seed modules.Seed) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	if !w.cs.Synced() {
		return errors.New("cannot init from seed until blockchain is synced")
	}

	// If masterKey is blank, use the hash of the seed.
	var err error
	if masterKey == nil {
		h := types.NewHasher()
		h.E.Write(seed[:])
		hash := h.Sum()
		key := make([]byte, len(hash))
		copy(key[:], hash[:])
		masterKey = modules.WalletKey(key)
	}

	if !w.scanLock.TryLock() {
		return errScanInProgress
	}
	defer w.scanLock.Unlock()

	// Estimate the primarySeedProgress by scanning the blockchain.
	s := newSeedScanner(seed, w.log)
	if err := s.scan(w.cs, w.tg.StopChan()); err != nil {
		return err
	}
	// NOTE: each time the wallet generates a key for index n, it sets its
	// progress to n+1, so the progress should be the largest index seen + 1.
	// We also add 10% as a buffer because the seed may have addresses in the
	// wild that have not appeared in the blockchain yet.
	progress := s.largestIndexSeen + 1
	progress += progress / 10
	w.log.Printf("INFO: found key index %v in blockchain. Setting primary seed progress to %v", s.largestIndexSeen, progress)

	// Initialize the wallet with the appropriate seed progress.
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.initEncryption(masterKey, seed, progress)
	return err
}

// Unlocked indicates whether the wallet is locked or unlocked.
func (w *Wallet) Unlocked() (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, err
	}
	defer w.tg.Done()
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.unlocked, nil
}

// Lock will erase all keys from memory and prevent the wallet from spending
// coins until it is unlocked.
func (w *Wallet) Lock() error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()
	return w.managedLock()
}

// ChangeKey changes the wallet's encryption key from masterKey to newKey.
func (w *Wallet) ChangeKey(masterKey modules.WalletKey, newKey modules.WalletKey) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	return w.managedChangeKey(masterKey, newKey)
}

// ChangeKeyWithSeed is the same as ChangeKey but uses the primary seed
// instead of the current masterKey.
func (w *Wallet) ChangeKeyWithSeed(seed modules.Seed, newKey modules.WalletKey) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()
	mk, err := w.managedMasterKey(seed)
	if err != nil {
		return modules.AddContext(err, "failed to retrieve masterkey by seed")
	}
	return w.managedChangeKey(mk, newKey)
}

// IsMasterKey verifies that the masterKey is the key used to encrypt the
// wallet.
func (w *Wallet) IsMasterKey(masterKey modules.WalletKey) (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check provided key.
	err := checkMasterKey(w.dbTx, masterKey)
	if modules.ContainsError(err, modules.ErrBadEncryptionKey) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// UnlockAsync will decrypt the wallet seed and load all of the addresses into
// memory.
func (w *Wallet) UnlockAsync(masterKey modules.WalletKey) <-chan error {
	errChan := make(chan error, 1)
	defer close(errChan)
	// By having the wallet's ThreadGroup track the Unlock method, we ensure
	// that Unlock will never unlock the wallet once the ThreadGroup has been
	// stopped. Without this precaution, the wallet's Close method would be
	// unsafe because it would theoretically be possible for another function
	// to Unlock the wallet in the short interval after Close calls w.Lock
	// and before Close calls w.mu.Lock.
	if err := w.tg.Add(); err != nil {
		errChan <- err
		return errChan
	}
	defer w.tg.Done()

	if !w.scanLock.TryLock() {
		errChan <- errScanInProgress
		return errChan
	}
	defer w.scanLock.Unlock()

	w.log.Println("INFO: unlocking wallet.")

	// Initialize all of the keys in the wallet under a lock. While holding the
	// lock, also grab the subscriber status.
	return w.managedUnlock(masterKey)
}

// Unlock will decrypt the wallet seed and load all of the addresses into
// memory.
func (w *Wallet) Unlock(masterKey modules.WalletKey) error {
	return <-w.UnlockAsync(masterKey)
}

// managedChangeKey safely performs the database operations required to change
// the wallet's encryption key.
func (w *Wallet) managedChangeKey(masterKey modules.WalletKey, newKey modules.WalletKey) error {
	w.mu.Lock()
	encrypted := w.encrypted
	w.mu.Unlock()
	if !encrypted {
		return errUnencryptedWallet
	}

	// Grab the current seeds.
	var encryptedPrimarySeed encryptedSeed
	var encryptedAuxiliarySeeds []encryptedSeed
	var unseededKeys []encryptedSpendableKey

	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		// Verify masterKey.
		err := checkMasterKey(w.dbTx, masterKey)
		if err != nil {
			return modules.AddContext(err, "unable to verify master key")
		}

		// encryptedPrimarySeed.
		encryptedPrimarySeed, err = dbGetPrimarySeed(w.dbTx)
		if err != nil {
			return modules.AddContext(err, "unable to decode primary seed file")
		}

		// encryptedAuxiliarySeeds.
		encryptedAuxiliarySeeds, err = dbGetAuxiliarySeeds(w.dbTx)
		if err != nil {
			return modules.AddContext(err, "unable to decode auxiliary seed file")
		}

		// unseededKeys.
		unseededKeys, err = dbGetUnseededKeys(w.dbTx)
		if err != nil {
			return modules.AddContext(err, "unable to decode unseeded key file")
		}

		return nil
	}()
	if err != nil {
		return err
	}

	// Decrypt key files.
	var primarySeed modules.Seed
	var auxiliarySeeds []modules.Seed
	var spendableKeys []spendableKey

	primarySeed, err = decryptSeed(masterKey, encryptedPrimarySeed)
	if err != nil {
		return modules.AddContext(err, "unable to decrypt primary seed file")
	}
	for _, as := range encryptedAuxiliarySeeds {
		auxSeed, err := decryptSeed(masterKey, as)
		if err != nil {
			return modules.AddContext(err, "unable to decrypt auxiliary seed file")
		}
		auxiliarySeeds = append(auxiliarySeeds, auxSeed)
	}
	for _, uk := range unseededKeys {
		sk, err := decryptSpendableKey(masterKey, uk)
		if err != nil {
			return modules.AddContext(err, "unable to decrypt unseed key file")
		}
		spendableKeys = append(spendableKeys, sk)
	}

	// Encrypt new keys using newKey.
	var newPrimarySeed encryptedSeed
	var newAuxiliarySeeds []encryptedSeed
	var newUnseededKeys []encryptedSpendableKey

	newPrimarySeed = createSeed(newKey, primarySeed)
	for _, seed := range auxiliarySeeds {
		as := createSeed(newKey, seed)
		newAuxiliarySeeds = append(newAuxiliarySeeds, as)
	}
	for _, sk := range spendableKeys {
		var uk encryptedSpendableKey
		frand.Read(uk.Salt[:])
		encryptionKey := saltedEncryptionKey(newKey, uk.Salt)
		uk.EncryptionVerification, err = modules.Encrypt(encryptionKey, verificationPlaintext)
		if err != nil {
			return modules.AddContext(err, "failed to encrypt verification")
		}

		// Encrypt and save the key.
		var buf bytes.Buffer
		e := types.NewEncoder(&buf)
		sk.EncodeTo(e)
		e.Flush()
		uk.SpendableKey, err = modules.Encrypt(encryptionKey, buf.Bytes())
		if err != nil {
			return modules.AddContext(err, "failed to encrypt unseeded key")
		}
		newUnseededKeys = append(newUnseededKeys, uk)
	}

	// Put the newly encrypted keys in the database.
	err = func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		err = dbPutPrimarySeed(w.dbTx, newPrimarySeed)
		if err != nil {
			return modules.AddContext(err, "unable to put primary key into db")
		}
		err = dbPutAuxiliarySeeds(w.dbTx, newAuxiliarySeeds)
		if err != nil {
			return modules.AddContext(err, "unable to put auxiliary key into db")
		}
		err = dbPutUnseededKeys(w.dbTx, newUnseededKeys)
		if err != nil {
			return modules.AddContext(err, "unable to put unseeded key into db")
		}

		// TODO remove.
		uk := saltedEncryptionKey(newKey, dbGetWalletSalt(w.dbTx))
		encrypted, err := modules.Encrypt(uk, verificationPlaintext)
		if err != nil {
			return modules.AddContext(err, "failed to encrypt verification")
		}
		err = dbPutEncryptedVerification(w.dbTx, encrypted)
		if err != nil {
			return modules.AddContext(err, "unable to put key encryption verification into db")
		}

		wpk := walletPasswordEncryptionKey(primarySeed, dbGetWalletSalt(w.dbTx))
		encrypted, err = modules.Encrypt(wpk, newKey)
		err = dbPutWalletPassword(w.dbTx, encrypted)
		if err != nil {
			return modules.AddContext(err, "unable to put wallet password into db")
		}

		return nil
	}()
	if err != nil {
		return err
	}

	return nil
}

// managedLock will erase all keys from memory and prevent the wallet from
// spending coins until it is unlocked.
func (w *Wallet) managedLock() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return modules.ErrLockedWallet
	}
	w.log.Println("INFO: locking wallet.")

	// Wipe all of the seeds and secret keys. They will be replaced upon
	// calling 'Unlock' again. Note that since the public keys are not wiped,
	// we can continue processing blocks.
	w.wipeSecrets()
	w.unlocked = false
	return nil
}

// managedUnlocked indicates whether the wallet is locked or unlocked.
func (w *Wallet) managedUnlocked() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.unlocked
}
