package contractor

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// managedArchiveContracts will figure out which contracts are no longer needed
// and move them to the historic set of contracts.
func (c *Contractor) managedArchiveContracts() {
	// Determine the current block height.
	c.mu.RLock()
	currentHeight := c.blockHeight
	c.mu.RUnlock()

	// Loop through the current set of contracts and migrate any expired ones to
	// the set of old contracts.
	var expired []types.FileContractID
	for _, contract := range c.staticContracts.ViewAll() {
		// Check map of renewedTo in case renew code was interrupted before
		// archiving old contract.
		c.mu.RLock()
		_, renewed := c.renewedTo[contract.ID]
		c.mu.RUnlock()
		if currentHeight > contract.EndHeight-modules.BlocksPerDay || renewed {
			id := contract.ID
			c.staticContracts.RetireContract(id)
			expired = append(expired, id)
			c.log.Println("INFO: archived expired contract", id)
		}
	}

	// Delete all the expired contracts from the contract set.
	for _, id := range expired {
		if fc, ok := c.staticContracts.Acquire(id); ok {
			c.staticContracts.Delete(fc)
		}
	}

	// Go through all historic contracts and unlock the balance if required.
	totalContracts := append(c.staticContracts.ViewAll(), c.staticContracts.OldContracts()...)
	for _, contract := range totalContracts {
		c.mu.RLock()
		_, renewed := c.renewedTo[contract.ID]
		c.mu.RUnlock()
		if !contract.Unlocked && !contract.Imported && (currentHeight > contract.EndHeight-modules.BlocksPerDay || renewed) {
			c.UnlockBalance(contract.ID)
		}
	}
}

// ProcessConsensusChange will be called by the consensus set every time there
// is a change in the blockchain. Updates will always be called in order.
func (c *Contractor) ProcessConsensusChange(cc modules.ConsensusChange) {
	c.mu.Lock()

	c.blockHeight = cc.InitialHeight()
	for _, block := range cc.AppliedBlocks {
		if block.ID() != modules.GenesisID {
			c.blockHeight++
		}
	}
	c.staticWatchdog.callScanConsensusChange(cc)

	// If the allowance is set and we have entered the next period, update
	// CurrentPeriod.
	renters := c.renters
	for key, renter := range renters {
		if renter.Allowance.Active() && c.blockHeight >= renter.CurrentPeriod+renter.Allowance.Period {
			renter.CurrentPeriod += renter.Allowance.Period
			c.renters[key] = renter
			err := c.UpdateRenter(renter)
			if err != nil {
				c.log.Println("ERROR: unable to update renter:", err)
			}
		}
	}

	// Check if c.synced already signals that the contractor is synced.
	synced := false
	select {
	case <-c.synced:
		synced = true
	default:
	}
	// If we weren't synced but are now, we close the channel. If we were
	// synced but aren't anymore, we need a new channel.
	if !synced && cc.Synced {
		close(c.synced)
	} else if synced && !cc.Synced {
		c.synced = make(chan struct{})
	}

	// Let the watchdog take any necessary actions and update its state. We do
	// this before persisting the contractor so that the watchdog is up-to-date on
	// reboot. Otherwise it is possible that e.g. that the watchdog thinks a
	// storage proof was missed and marks down a host for that. Other watchdog
	// actions are innocuous.
	if cc.Synced {
		c.staticWatchdog.callCheckContracts()
	}

	c.lastChange = cc.ID
	err := c.updateState()
	if err != nil {
		c.log.Println("ERROR: unable to save while processing a consensus change:", err)
	}
	c.mu.Unlock()

	// Perform contract maintenance if our blockchain is synced. Use a separate
	// goroutine so that the rest of the contractor is not blocked during
	// maintenance.
	if cc.Synced {
		go c.threadedContractMaintenance()
	}
}
