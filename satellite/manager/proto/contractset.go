package proto

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	"go.sia.tech/siad/types"
)

// A ContractSet provides access to a set of contracts. Its purpose is to
// serialize modifications to individual contracts, as well as to provide
// operations on the set as a whole.
type ContractSet struct {
	contracts map[types.FileContractID]*FileContract
	pubKeys   map[string]types.FileContractID
	mu        sync.Mutex
	db        *sql.DB
	log       *persist.Logger
}

// Acquire looks up the contract for the specified host key and locks it before
// returning it. If the contract is not present in the set, Acquire returns
// false and a zero-valued RenterContract.
func (cs *ContractSet) Acquire(id types.FileContractID) (*FileContract, bool) {
	cs.mu.Lock()
	fileContract, ok := cs.contracts[id]
	cs.mu.Unlock()
	if !ok {
		return nil, false
	}
	fileContract.revisionMu.Lock()
	// We need to check if the contract is still in the map or if it has been
	// deleted in the meantime.
	cs.mu.Lock()
	_, ok = cs.contracts[id]
	cs.mu.Unlock()
	if !ok {
		fileContract.revisionMu.Unlock()
		return nil, false
	}
	return fileContract, true
}

// Delete removes a contract from the set. The contract must have been
// previously acquired by Acquire. If the contract is not present in the set,
// Delete is a no-op.
func (cs *ContractSet) Delete(c *FileContract) {
	cs.mu.Lock()
	_, ok := cs.contracts[c.header.ID()]
	if !ok {
		cs.mu.Unlock()
		fmt.Println("CRITICAL: Delete called on already deleted contract")
		return
	}
	id := c.header.ID()
	delete(cs.contracts, id)
	delete(cs.pubKeys, c.header.RenterPublicKey().String() + c.header.HostPublicKey().String())
	cs.mu.Unlock()
	c.revisionMu.Unlock()

	// Delete from the database.
	if err := deleteContract(id, cs.db); err != nil {
		cs.log.Printf("Unable to delete contract %v from database: %v\n", id, err)
	}
}

// IDs returns the fcid of each contract with in the set. The contracts are not
// locked.
func (cs *ContractSet) IDs() []types.FileContractID {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	pks := make([]types.FileContractID, 0, len(cs.contracts))
	for fcid := range cs.contracts {
		pks = append(pks, fcid)
	}
	return pks
}

// InsertContract inserts an existing contract into the set.
func (cs *ContractSet) InsertContract(rc modules.RecoverableContract, revTxn types.Transaction, roots []crypto.Hash, sk crypto.SecretKey) (modules.RenterContract, error) {
	// Estimate the totalCost.
	// NOTE: The actual totalCost is the funding amount. Which means
	// renterPayout + txnFee + basePrice + contractPrice.
	// Since we don't know the basePrice and contractPrice, we don't add them.
	var totalCost types.Currency
	totalCost = totalCost.Add(rc.FileContract.ValidRenterPayout())
	totalCost = totalCost.Add(rc.TxnFee)
	return cs.managedInsertContract(contractHeader{
		Transaction: revTxn,
		SecretKey:   sk,
		StartHeight: rc.StartHeight,
		TotalCost:   totalCost,
		TxnFee:      rc.TxnFee,
		SiafundFee:  types.Tax(rc.StartHeight, rc.Payout),
	},)
}

// Len returns the number of contracts in the set.
func (cs *ContractSet) Len() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.contracts)
}

// Return returns a locked contract to the set and unlocks it. The contract
// must have been previously acquired by Acquire. If the contract is not
// present in the set, Return panics.
func (cs *ContractSet) Return(c *FileContract) {
	cs.mu.Lock()
	_, ok := cs.contracts[c.header.ID()]
	if !ok {
		cs.mu.Unlock()
		fmt.Println("CRITICAL: No contract with that key")
	}
	cs.mu.Unlock()
	c.revisionMu.Unlock()
}

// View returns a copy of the contract with the specified host key. The contract
// is not locked. If the contract is not present in the set, View returns false
// and a zero-valued RenterContract.
func (cs *ContractSet) View(id types.FileContractID) (modules.RenterContract, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	fileContract, ok := cs.contracts[id]
	if !ok {
		return modules.RenterContract{}, false
	}
	return fileContract.Metadata(), true
}

// PublicKey returns the public key capable of verifying the renter's signature
// on a contract.
func (cs *ContractSet) PublicKey(id types.FileContractID) (crypto.PublicKey, bool) {
	cs.mu.Lock()
	fileContract, ok := cs.contracts[id]
	cs.mu.Unlock()
	if !ok {
		return crypto.PublicKey{}, false
	}
	return fileContract.PublicKey(), true
}

// ViewAll returns the metadata of each contract in the set. The contracts are
// not locked.
func (cs *ContractSet) ViewAll() []modules.RenterContract {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	contracts := make([]modules.RenterContract, 0, len(cs.contracts))
	for _, fileContract := range cs.contracts {
		contracts = append(contracts, fileContract.Metadata())
	}
	return contracts
}

// NewContractSet returns a ContractSet storing its contracts in the specified
// database.
func NewContractSet(db *sql.DB, log *persist.Logger) (*ContractSet, error) {
	// Load the contract IDs.
	keys, err := loadContracts(db)
	if err != nil {
		return nil, err
	}
	
	cs := &ContractSet{
		contracts: make(map[types.FileContractID]*FileContract),
		pubKeys:   keys,
		db:        db,
		log:       log,
	}

	// Load the contracts from the database.
	var err error
	for _, fcid := range keys {
		err = cs.loadFileContract(fcid)
		if err != nil {
			cs.log.Printf("Error inserting contract %v: %v\n", fcid, err)
		}
	}

	return cs, nil
}
