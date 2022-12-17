package contractor

import (
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// managedUpdatePubKeyToContractIDMap updates the pubkeysToContractID map
func (c *Contractor) managedUpdatePubKeyToContractIDMap() {
	// Grab the current contracts and the blockheight
	contracts := c.staticContracts.ViewAll()
	c.mu.Lock()
	c.updatePubKeyToContractIDMap(contracts)
	c.mu.Unlock()
}

// updatePubKeyToContractIDMap updates the pubkeysToContractID map.
func (c *Contractor) updatePubKeyToContractIDMap(contracts []modules.RenterContract) {
	// Sanity check - there should be an equal number of GFU contracts in each
	// the ViewAll set of contracts, and also in the pubKeyToContractID map.
	uniqueGFU := make(map[string]types.FileContractID)

	// Reset the pubkey to contract id map, also create a map from each
	// contract's fcid to the contract itself, then try adding each contract to
	// the map. The most recent contract for each host will be favored as the
	// contract in the map.
	c.pubKeysToContractID = make(map[string]types.FileContractID)
	for i := 0; i < len(contracts); i++ {
		c.tryAddContractToPubKeyMap(contracts[i])

		// Fill out the uniqueGFU map, tracking every contract that is marked as
		// GoodForUpload.
		if contracts[i].Utility.GoodForUpload {
			uniqueGFU[contracts[i].HostPublicKey.String()] = contracts[i].ID
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

// tryAddContractToPubKeyMap will try and add the contract to the
// pubKeysToContractID map. The most recent contract with the best utility for
// each pubKey will be added.
func (c *Contractor) tryAddContractToPubKeyMap(newContract modules.RenterContract) {
	// Ignore any contracts that have been renewed.
	_, exists := c.renewedTo[newContract.ID]
	if exists {
		gfu, gfr := newContract.Utility.GoodForUpload, newContract.Utility.GoodForRenew
		if gfu || gfr {
			c.log.Critical("renewed contract is marked as good for upload or good for renew", gfu, gfr)
		}
		return
	}
	pk := newContract.HostPublicKey.String()

	// If there is not existing contract in the map for this pubkey, add it.
	_, exists = c.pubKeysToContractID[pk]
	if exists {
		// Sanity check - the contractor should not have multiple contract tips for the
		// same contract.
		c.log.Critical("Contractor has multiple contracts that don't form a renewedTo line for the same host")
	}
	c.pubKeysToContractID[pk] = newContract.ID
}
