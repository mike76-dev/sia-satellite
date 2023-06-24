package consensus

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/siad/persist"
)

const (
	// logfile contains the filename of the consensus log.
	logFile = "consensus.log"
)

var (
	// errFoundationHardforkIncompatibility is returned if the consensus
	// database was not upgraded prior to the Foundation hardfork height.
	errFoundationHardforkIncompatibility = errors.New("cannot upgrade database for Foundation hardfork after activation height")
)

// loadDB pulls all the blocks that have been saved to disk into memory, using
// them to fill out the ConsensusSet.
func (cs *ConsensusSet) loadDB() error {
	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return err
	}

	// Check if the database has been initialized.
	err = cs.initDB(tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	// Check the initialization of the oak difficulty adjustment fields, and
	// create them if they do not exist.
	err = cs.initOak(tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	// Initialize the Foundation hardfork fields, if necessary.
	err = initFoundation(tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	// Check that the genesis block is correct - typically only incorrect
	// in the event of developer binaries vs. release binaires.
	genesisID, err := getBlockAtHeight(tx, 0)
	if err != nil {
		tx.Rollback()
		return err
	}
	if genesisID != cs.blockRoot.Block.ID() {
		tx.Rollback()
		return errors.New("blockchain has wrong genesis block")
	}

	return tx.Commit()
}

// initFoundation initializes the database fields relating to the Foundation
// subsidy hardfork. If these fields have already been set, it does nothing.
func initFoundation(tx *sql.Tx) error {
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM cs_fuh_current").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		// UnlockHashes have already been set; nothing to do.
		return nil
	}
	// If the current height is greater than the hardfork trigger date, return
	// an error and refuse to initialize.
	height := blockHeight(tx)
	if height >= modules.FoundationHardforkHeight {
		return errFoundationHardforkIncompatibility
	}
	// Set the initial Foundation addresses.
	err = setFoundationUnlockHashes(tx, modules.InitialFoundationUnlockHash, modules.InitialFoundationFailsafeUnlockHash)

	return err
}

// initPersist initializes the persistence structures of the consensus set, in
// particular loading the database and preparing to manage subscribers.
func (cs *ConsensusSet) initPersist(dir string) error {
	// Initialize the logger.
	var err error
	cs.log, err = persist.NewFileLogger(filepath.Join(dir, logFile))
	if err != nil {
		return err
	}
	// Set up closing the logger.
	cs.tg.AfterStop(func() {
		err := cs.log.Close()
		if err != nil {
			// State of the logger is unknown, a println will suffice.
			fmt.Println("Error shutting down consensus set logger:", err)
		}
	})

	// Load the database.
	err = cs.loadDB()
	if err != nil {
		return err
	}

	return nil
}
