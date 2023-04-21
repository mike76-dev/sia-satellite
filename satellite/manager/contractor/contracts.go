package contractor

import (
	"github.com/mike76-dev/sia-satellite/satellite/manager/proto"
	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// managedCancelContract cancels a contract by setting its utility fields to
// false and locking the utilities. The contract can still be used for
// downloads after this but it won't be used for uploads or renewals.
func (c *Contractor) managedCancelContract(cid types.FileContractID) error {
	return c.managedAcquireAndUpdateContractUtility(cid, smodules.ContractUtility{
		GoodForRenew:  false,
		GoodForUpload: false,
		Locked:        true,
	})
}

// managedContractUtility returns the ContractUtility for a contract with a given id.
func (c *Contractor) managedContractUtility(id types.FileContractID) (smodules.ContractUtility, bool) {
	rc, exists := c.staticContracts.View(id)
	if !exists {
		return smodules.ContractUtility{}, false
	}
	return rc.Utility, true
}

// managedUpdatePubKeysToContractIDMap updates the pubkeysToContractID map.
func (c *Contractor) managedUpdatePubKeysToContractIDMap() {
	// Grab the current contracts.
	contracts := c.staticContracts.ViewAll()
	c.mu.Lock()
	c.updatePubKeysToContractIDMap(contracts)
	c.mu.Unlock()
}

// updatePubKeysToContractIDMap updates the pubkeysToContractID map.
func (c *Contractor) updatePubKeysToContractIDMap(contracts []modules.RenterContract) {
	// Sanity check - there should be an equal number of GFU contracts in each
	// the ViewAll set of contracts, and also in the pubKeysToContractID map.
	uniqueGFU := make(map[string]types.FileContractID)

	// Reset the pubkeys to contract id map, also create a map from each
	// contract's fcid to the contract itself, then try adding each contract to
	// the map. The most recent contract for each host will be favored as the
	// contract in the map.
	c.pubKeysToContractID = make(map[string]types.FileContractID)
	for i := 0; i < len(contracts); i++ {
		c.tryAddContractToPubKeysMap(contracts[i])

		// Fill out the uniqueGFU map, tracking every contract that is marked as
		// GoodForUpload.
		if contracts[i].Utility.GoodForUpload {
			uniqueGFU[contracts[i].RenterPublicKey.String() + contracts[i].HostPublicKey.String()] = contracts[i].ID
		}
	}

	// Every contract that appears in the uniqueGFU map should also appear in
	// the pubKeysToContractID map.
	for pk, fcid := range uniqueGFU {
		if c.pubKeysToContractID[pk] != fcid {
			c.log.Critical("Contractor is not correctly mapping from pubkey to contract id, missing GFU contracts")
		}
	}
}

// tryAddContractToPubKeysMap will try and add the contract to the
// pubKeysToContractID map. The most recent contract with the best utility for
// each pubKey will be added.
func (c *Contractor) tryAddContractToPubKeysMap(newContract modules.RenterContract) {
	// Ignore any contracts that have been renewed.
	_, exists := c.renewedTo[newContract.ID]
	if exists {
		gfu, gfr := newContract.Utility.GoodForUpload, newContract.Utility.GoodForRenew
		if gfu || gfr {
			c.log.Critical("renewed contract is marked as good for upload or good for renew", gfu, gfr)
		}
		return
	}
	pk := newContract.RenterPublicKey.String() + newContract.HostPublicKey.String()

	// If there is not existing contract in the map for this pubkey, add it.
	_, exists = c.pubKeysToContractID[pk]
	if exists {
		// Sanity check - the contractor should not have multiple contract tips for the
		// same contract.
		c.log.Critical("Contractor has multiple contracts that don't form a renewedTo line for the same host and the same renter")
	}
	c.pubKeysToContractID[pk] = newContract.ID
}

// CancelContract cancels the Contractor's contract by marking it !GoodForRenew
// and !GoodForUpload
func (c *Contractor) CancelContract(id types.FileContractID) error {
	if err := c.tg.Add(); err != nil {
		return err
	}
	defer c.tg.Done()
	defer c.threadedContractMaintenance()
	return c.managedCancelContract(id)
}

// Contracts returns the contracts formed by the contractor in the current
// allowance period.
func (c *Contractor) Contracts() []modules.RenterContract {
	return c.staticContracts.ViewAll()
}

// ContractsByRenter returns the contracts belonging to a specific renter.
func (c *Contractor) ContractsByRenter(rpk types.SiaPublicKey) []modules.RenterContract {
	return c.staticContracts.ByRenter(rpk)
}

// OldContractsByRenter returns the old contracts of a specific renter.
func (c *Contractor) OldContractsByRenter(rpk types.SiaPublicKey) []modules.RenterContract {
	return c.staticContracts.OldByRenter(rpk)
}

// ContractUtility returns the utility fields for the given contract.
func (c *Contractor) ContractUtility(rpk, hpk types.SiaPublicKey) (smodules.ContractUtility, bool) {
	c.mu.RLock()
	id, ok := c.pubKeysToContractID[rpk.String() + hpk.String()]
	c.mu.RUnlock()
	if !ok {
		return smodules.ContractUtility{}, false
	}
	return c.managedContractUtility(id)
}

// MarkContractBad will mark a specific contract as bad.
func (c *Contractor) MarkContractBad(id types.FileContractID) error {
	if err := c.tg.Add(); err != nil {
		return err
	}
	defer c.tg.Done()

	sc, exists := c.staticContracts.Acquire(id)
	if !exists {
		return errors.New("contract not found")
	}
	defer c.staticContracts.Return(sc)
	return c.managedMarkContractBad(sc)
}

// OldContracts returns the contracts formed by the contractor that have
// expired.
func (c *Contractor) OldContracts() []modules.RenterContract {
	return c.staticContracts.OldContracts()
}

// managedMarkContractBad marks an already acquired SafeContract as bad.
func (c *Contractor) managedMarkContractBad(fc *proto.FileContract) error {
	u := fc.Utility()
	u.GoodForUpload = false
	u.GoodForRenew = false
	u.BadContract = true
	err := c.callUpdateUtility(fc, u, false)
	return errors.AddContext(err, "unable to mark contract as bad")
}
