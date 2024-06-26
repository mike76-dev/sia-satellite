package contractset

import (
	"database/sql"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
)

// contractAgeThreshold is the height after which an old contract is removed
// from the contract sets.
const contractAgeThreshold = modules.BlocksPerMonth // 1 month

// A ContractSet provides access to a set of contracts. Its purpose is to
// serialize modifications to individual contracts, as well as to provide
// operations on the set as a whole.
type ContractSet struct {
	contracts    map[types.FileContractID]*FileContract
	oldContracts map[types.FileContractID]*FileContract
	pubKeys      map[string]types.FileContractID
	mu           sync.Mutex
	db           *sql.DB
	log          *zap.Logger
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
		cs.log.Error("delete called on already deleted contract", zap.Stringer("id", c.header.ID()))
		return
	}
	id := c.header.ID()
	delete(cs.contracts, id)
	delete(cs.pubKeys, c.header.RenterPublicKey().String()+c.header.HostPublicKey().String())
	cs.mu.Unlock()
	c.revisionMu.Unlock()
}

// Erase removes a contract from the database.
func (cs *ContractSet) Erase(fcid types.FileContractID) {
	err := deleteContract(fcid, cs.db)
	if err != nil {
		cs.log.Error("unable to delete the contract", zap.Stringer("fcid", fcid))
	}
}

// DeleteOldContracts deletes old contracts from both the set and the database.
func (cs *ContractSet) DeleteOldContracts(height uint64) {
	var toDelete []types.FileContractID
	cs.mu.Lock()
	for _, oc := range cs.oldContracts {
		id := oc.header.ID()
		endHeight := oc.header.EndHeight()
		if height > endHeight+contractAgeThreshold {
			toDelete = append(toDelete, id)
			delete(cs.oldContracts, id)
		}
	}
	cs.mu.Unlock()

	for _, id := range toDelete {
		cs.Erase(id)
	}
}

// ReplaceOldContract replaces the duplicated old contract.
func (cs *ContractSet) ReplaceOldContract(fcid types.FileContractID, c *FileContract) {
	cs.mu.Lock()
	cs.oldContracts[fcid] = c
	cs.mu.Unlock()
}

// IDs returns the fcid of each contract with in the set. The contracts are not
// locked.
func (cs *ContractSet) IDs(rpk types.PublicKey) []types.FileContractID {
	return cs.managedFindIDs(rpk)
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
		cs.log.Error("no contract with that key", zap.Stringer("id", c.header.ID()))
		return
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

// ViewAll returns the metadata of each contract in the set. The contracts are
// not locked.
func (cs *ContractSet) ViewAll() []modules.RenterContract {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	contracts := make([]modules.RenterContract, 0, len(cs.contracts))
	for _, fc := range cs.contracts {
		contracts = append(contracts, fc.Metadata())
	}
	return contracts
}

// ByRenter works the same as ViewAll but filters the contracts by the renter.
func (cs *ContractSet) ByRenter(rpk types.PublicKey) []modules.RenterContract {
	ids := cs.managedFindIDs(rpk)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	contracts := make([]modules.RenterContract, 0, len(ids))
	for _, id := range ids {
		fc, exists := cs.contracts[id]
		if exists {
			contracts = append(contracts, fc.Metadata())
		}
	}
	return contracts
}

// OldContracts returns the metadata of each old contract.
func (cs *ContractSet) OldContracts() []modules.RenterContract {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	oldContracts := make([]modules.RenterContract, 0, len(cs.oldContracts))
	for _, oldContract := range cs.oldContracts {
		oldContracts = append(oldContracts, oldContract.Metadata())
	}
	return oldContracts
}

// OldByRenter works the same as OldContracts but filters the contracts by the renter.
func (cs *ContractSet) OldByRenter(rpk types.PublicKey) []modules.RenterContract {
	ids := cs.managedFindIDs(rpk)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	contracts := make([]modules.RenterContract, 0, len(ids))
	for _, id := range ids {
		fc, exists := cs.oldContracts[id]
		if exists {
			contracts = append(contracts, fc.Metadata())
		}
	}
	return contracts
}

// OldContract returns the metadata of the specified old contract.
func (cs *ContractSet) OldContract(id types.FileContractID) (modules.RenterContract, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	oldContract, ok := cs.oldContracts[id]
	if !ok {
		return modules.RenterContract{}, false
	}
	return oldContract.Metadata(), true
}

// RetireContract adds the contract to the old contracts map.
func (cs *ContractSet) RetireContract(id types.FileContractID) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c, exists := cs.contracts[id]
	if !exists {
		cs.log.Error("trying to retire a non-existing contract", zap.Stringer("id", id))
		return
	}
	cs.oldContracts[id] = c
}

// UnlockPayout sets the 'Unlocked' status of a contract to true.
func (cs *ContractSet) UnlockPayout(id types.FileContractID) {
	var c *FileContract
	c, exists := cs.contracts[id]
	if !exists {
		c, exists = cs.oldContracts[id]
		if !exists {
			cs.log.Error("contract not found", zap.Stringer("id", id))
			return
		}
	}
	err := c.unlockPayout()
	if err != nil {
		cs.log.Error("couldn't unlock contract payout", zap.Error(err))
	}
}

// NewContractSet returns a ContractSet storing its contracts in the specified
// database.
func NewContractSet(db *sql.DB, log *zap.Logger, height uint64) (*ContractSet, error) {
	cs := &ContractSet{
		contracts:    make(map[types.FileContractID]*FileContract),
		oldContracts: make(map[types.FileContractID]*FileContract),
		pubKeys:      make(map[string]types.FileContractID),
		db:           db,
		log:          log,
	}

	// Load the contracts from the database.
	err := cs.loadContracts(height)
	if err != nil {
		return nil, err
	}

	return cs, nil
}
