package wallet

import (
	"bytes"
	"errors"
	"io"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

var (
	errDuplicateSpendableKey = errors.New("key has already been loaded into the wallet")

	// ErrInconsistentKeys is the error when keys provided are for different addresses.
	ErrInconsistentKeys = errors.New("keyfiles provided that are for different addresses")
	// ErrInsufficientKeys is the error when there's not enough keys provided to spend
	// the Siafunds.
	ErrInsufficientKeys = errors.New("not enough keys provided to spend the siafunds")
)

// decryptSpendableKey decrypts an encryptedSpendableKey, returning a
// spendableKey.
func decryptSpendableKey(masterKey modules.WalletKey, uk encryptedSpendableKey) (sk spendableKey, err error) {
	// Verify that the decryption key is correct.
	decryptionKey := saltedEncryptionKey(masterKey, uk.Salt)
	err = verifyEncryption(decryptionKey, uk.EncryptionVerification)
	if err != nil {
		return
	}

	// Decrypt the spendable key and add it to the wallet.
	encodedKey, err := modules.Decrypt(decryptionKey, uk.SpendableKey)
	if err != nil {
		return
	}
	buf := bytes.NewBuffer(encodedKey)
	d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(encodedKey))})
	sk.DecodeFrom(d)
	err = d.Err()

	return
}

// integrateSpendableKey loads a spendableKey into the wallet.
func (w *Wallet) integrateSpendableKey(masterKey modules.WalletKey, sk spendableKey) {
	w.keys[sk.UnlockConditions.UnlockHash()] = sk
}

// loadSpendableKey loads a spendable key into the wallet database.
func (w *Wallet) loadSpendableKey(masterKey modules.WalletKey, sk spendableKey) error {
	// Duplication is detected by looking at the set of unlock conditions. If
	// the wallet is locked, correct deduplication is uncertain.
	if !w.unlocked {
		return modules.ErrLockedWallet
	}

	// Check for duplicates.
	_, exists := w.keys[sk.UnlockConditions.UnlockHash()]
	if exists {
		return errDuplicateSpendableKey
	}

	// TODO: Check that the key is actually spendable.

	// Create a UID and encryption verification.
	var uk encryptedSpendableKey
	var err error
	frand.Read(uk.Salt[:])
	encryptionKey := saltedEncryptionKey(masterKey, uk.Salt)
	uk.EncryptionVerification, err = modules.Encrypt(encryptionKey, verificationPlaintext)
	if err != nil {
		return err
	}

	// Encrypt and save the key.
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	sk.EncodeTo(e)
	e.Flush()
	uk.SpendableKey, err = modules.Encrypt(encryptionKey, buf.Bytes())
	if err != nil {
		return err
	}

	err = checkMasterKey(w.dbTx, masterKey)
	if err != nil {
		return err
	}
	current, err := dbGetUnseededKeys(w.dbTx)
	if err != nil {
		return err
	}
	return dbPutUnseededKeys(w.dbTx, append(current, uk))
}
