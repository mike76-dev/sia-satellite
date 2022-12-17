package contractor

import (
	"go.sia.tech/siad/modules"
)

// ProcessConsensusChange will be called by the consensus set every time there
// is a change in the blockchain. Updates will always be called in order.
func (c *Contractor) ProcessConsensusChange(cc modules.ConsensusChange) {
	c.mu.Lock()
	// If the allowance is set and we have entered the next period, update
	// currentPeriod.
	if c.allowance.Active() && c.blockHeight >= c.currentPeriod + c.allowance.Period {
		c.currentPeriod += c.allowance.Period
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

	c.lastChange = cc.ID
	err := c.save()
	if err != nil {
		c.log.Println("Unable to save while processing a consensus change:", err)
	}
	c.mu.Unlock()

	// Perform contract maintenance if our blockchain is synced. Use a separate
	// goroutine so that the rest of the contractor is not blocked during
	// maintenance.
	/*if cc.Synced {
		go c.threadedContractMaintenance()
	}*/ //TODO
}
