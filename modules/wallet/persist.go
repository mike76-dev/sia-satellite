package wallet

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

const (
	logFile = "wallet.log"
)

// encryptedSpendableKey stores an encrypted spendable key on disk.
type encryptedSpendableKey struct {
	Salt                   walletSalt
	EncryptionVerification modules.WalletKey
	SpendableKey           []byte
}

// EncodeTo implements types.EncoderTo.
func (uk *encryptedSpendableKey) EncodeTo(e *types.Encoder) {
	e.Write(uk.Salt[:])
	e.WriteBytes(uk.EncryptionVerification)
	e.WriteBytes(uk.SpendableKey)
}

// DecodeFrom implements types.DecoderFrom.
func (uk *encryptedSpendableKey) DecodeFrom(d *types.Decoder) {
	d.Read(uk.Salt[:])
	uk.EncryptionVerification = d.ReadBytes()
	uk.SpendableKey = d.ReadBytes()
}

// openDB loads the set database and populates it with the necessary data.
func (w *Wallet) openDB() (err error) {
	// Initialize the database.
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("unable to start transaction: %v", err)
	}

	// If the wallet does not have a UID, create one.
	if (dbGetWalletSalt(tx) == walletSalt{}) {
		if err = dbReset(tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("couldn't reset wallet: %v", err)
		}
		var uid walletSalt
		frand.Read(uid[:])
		if err = dbPutWalletSalt(tx, uid); err != nil {
			tx.Rollback()
			return fmt.Errorf("couldn't save wallet UID: %v", err)
		}
	}

	// Check whether wallet is encrypted.
	encrypted, err := dbGetEncryptedVerification(tx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("couldn't check wallet encryption: %v", err)
	}
	w.encrypted = encrypted != nil

	return tx.Commit()
}

// initPersist loads all of the wallet's persistence into memory.
func (w *Wallet) initPersist(dir string) error {
	// Start logging.
	var err error
	w.log, err = persist.NewFileLogger(filepath.Join(dir, logFile))
	if err != nil {
		return err
	}
	w.tg.AfterStop(func() {
		w.log.Close()
	})

	// Open the database.
	err = w.openDB()
	if err != nil {
		return err
	}

	// Begin the initial transaction.
	w.dbTx, err = w.db.Begin()
	if err != nil {
		w.log.Critical("ERROR: failed to start database update:", err)
	}

	// Ensure that the final db transaction is committed when the wallet closes.
	w.tg.AfterStop(func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		// Wait if we are syncing.
		for w.syncing {}

		var err error
		if w.dbRollback {
			// Rollback txn if necessry.
			err = errors.New("database unable to sync - rollback requested")
			err = modules.ComposeErrors(err, w.dbTx.Rollback())
		} else {
			// Else commit the transaction.
			err = w.dbTx.Commit()
		}
		if err != nil {
			w.log.Severe("ERROR: failed to apply database update:", err)
		}
	})

	// Spawn a goroutine to commit the db transaction at regular intervals.
	go w.threadedDBUpdate()
	return nil
}
