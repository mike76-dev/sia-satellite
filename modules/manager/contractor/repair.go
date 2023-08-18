package contractor

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// healthCutoff represents the health of a slab, beyond which
// it shall be repaired.
const healthCutoff = 0.75

type (
	// slabInfo combines a slab with some other information.
	slabInfo struct {
		modules.Slab
		renterKey types.PublicKey
		health    float64
	}
)

// managedCheckFileHealth checks the health of the slabs and
// initiates their repairs if required.
func (c *Contractor) managedCheckFileHealth() {
	// Load slabs.
	slabs, err := c.getSlabs()
	if err != nil {
		c.log.Println("ERROR: couldn't load slabs:", err)
		return
	}

	// Iterate through the slabs and check if any needs a repair.
	var toRepair []slabInfo
	for _, slab := range slabs {
		// Sanity check.
		if slab.MinShards > uint8(len(slab.Shards)) {
			c.log.Printf("ERROR: retrieved less shards than MinShards (%v/%v)\n", len(slab.Shards), slab.MinShards)
			continue
		}

		// Check if the renter has opted in for repairs.
		renter, err := c.GetRenter(slab.renterKey)
		if err != nil {
			c.log.Println("ERROR: couldn't fetch the renter:", err)
			continue
		}
		if !renter.Settings.AutoRepairFiles {
			continue
		}

		contracts := c.staticContracts.ByRenter(renter.PublicKey)
		var numShards uint8
		for _, shard := range slab.Shards {
			// Fetch the host.
			host, exists, err := c.hdb.Host(shard.Host)
			if err != nil {
				c.log.Println("ERROR: couldn't fetch host:", err)
				continue
			}
			if !exists {
				c.log.Printf("WARN: host %v not found in the database\n", shard.Host)
				continue
			}
			if !host.ScanHistory[len(host.ScanHistory)-1].Success {
				// Host is offline.
				continue
			}
			// Check if the renter has a contract with this host.
			var found bool
			for _, contract := range contracts {
				if contract.HostPublicKey == shard.Host {
					found = true
					break
				}
			}
			if found {
				numShards++
			}
		}

		// Catch the edge cases.
		if slab.MinShards == uint8(len(slab.Shards)) {
			if numShards == slab.MinShards {
				slab.health = 1
			}
		} else if numShards > slab.MinShards {
			slab.health = float64(numShards-slab.MinShards) / float64(uint8(len(slab.Shards))-slab.MinShards)
		}

		if slab.health < healthCutoff {
			toRepair = append(toRepair, slab)
		}
	}
}
