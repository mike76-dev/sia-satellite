package contractor

import (
	"errors"
	"reflect"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
)

var (
	errAllowanceNotSynced = errors.New("you must be synced to set an allowance")

	// ErrAllowanceZeroFunds is returned if the allowance funds are being set to
	// zero when not cancelling the allowance.
	ErrAllowanceZeroFunds = errors.New("funds must be non-zero")
	// ErrAllowanceZeroPeriod is returned if the allowance period is being set
	// to zero when not cancelling the allowance.
	ErrAllowanceZeroPeriod = errors.New("period must be non-zero")
	// ErrAllowanceZeroWindow is returned if the allowance's renew window is being
	// set to zero when not cancelling the allowance.
	ErrAllowanceZeroWindow = errors.New("renew window must be non-zero")
	// ErrAllowanceNoHosts is returned if the allowance's hosts are being set to
	// zero when not cancelling the allowance.
	ErrAllowanceNoHosts = errors.New("hosts must be non-zero")
	// ErrAllowanceZeroExpectedStorage is returned if the allowance's expected
	// storage is being set to zero when not cancelling the allowance.
	ErrAllowanceZeroExpectedStorage = errors.New("expected storage must be non-zero")
	// ErrAllowanceZeroExpectedUpload is returned if the allowance's expected
	// upload is being set to zero when not cancelling the allowance.
	ErrAllowanceZeroExpectedUpload = errors.New("expected upload  must be non-zero")
	// ErrAllowanceZeroExpectedDownload is returned if the allowance's expected
	// download is being set to zero when not cancelling the allowance.
	ErrAllowanceZeroExpectedDownload = errors.New("expected download  must be non-zero")
	// ErrAllowanceWrongRedundancy is returned if the allowance's redundancy
	// parameters are being set to zero.
	ErrAllowanceWrongRedundancy = errors.New("wrong redundancy params")
	// ErrRenterNotFound is returned when no renter matches the provided public
	// key.
	ErrRenterNotFound = errors.New("no renter found with this public key")
)

// SetAllowance sets the amount of money the Contractor is allowed to spend on
// contracts over a given time period, divided among the number of hosts
// specified.
//
// If a is the empty allowance, SetAllowance will archive the current contract
// set. The contracts will not be renewed.
//
// NOTE: At this time, transaction fees are not counted towards the allowance.
// This means the contractor may spend more than allowance.Funds.
func (c *Contractor) SetAllowance(rpk types.PublicKey, a modules.Allowance) error {
	if reflect.DeepEqual(a, modules.Allowance{}) {
		return c.managedCancelAllowance(rpk)
	}

	// Sanity checks.
	if a.Funds.Cmp(types.ZeroCurrency) <= 0 {
		return ErrAllowanceZeroFunds
	} else if a.Hosts == 0 {
		return ErrAllowanceNoHosts
	} else if a.Period == 0 {
		return ErrAllowanceZeroPeriod
	} else if a.RenewWindow == 0 {
		return ErrAllowanceZeroWindow
	} else if a.ExpectedStorage == 0 {
		return ErrAllowanceZeroExpectedStorage
	} else if a.ExpectedUpload == 0 {
		return ErrAllowanceZeroExpectedUpload
	} else if a.ExpectedDownload == 0 {
		return ErrAllowanceZeroExpectedDownload
	} else if a.MinShards == 0 || a.TotalShards == 0 {
		return ErrAllowanceWrongRedundancy
	} else if !c.s.Synced() {
		return errAllowanceNotSynced
	}

	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return ErrRenterNotFound
	}

	if reflect.DeepEqual(a, renter.Allowance) {
		return nil
	}
	c.log.Info("setting allowance for", zap.Stringer("renter", rpk))

	// Set the current period if the existing allowance is empty.
	//
	// When setting the current period we want to ensure that it aligns with the
	// start and end heights of the contracts as we would expect. To do this we
	// have to consider the following. First, that the current period value is
	// incremented by the allowance period, and second, that the total length of
	// a contract is the period + renew window. This means the that contracts are
	// always overlapping periods, and we want that overlap to be the renew
	// window. In order to create this overlap we set the current period as such.
	//
	// If the renew window is less than the period the current period is set in
	// the past by the renew window.
	//
	// If the renew window is greater than or equal to the period we set the
	// current period to the current block height.
	//
	// Also remember that we might have to unlock our contracts if the allowance
	// was set to the empty allowance before.
	c.mu.Lock()
	unlockContracts := false
	if reflect.DeepEqual(renter.Allowance, modules.Allowance{}) {
		renter.CurrentPeriod = c.tip.Height
		if a.Period > a.RenewWindow {
			renter.CurrentPeriod -= a.RenewWindow
		}
		unlockContracts = true
	}
	renter.Allowance = a
	c.renters[rpk] = renter
	c.mu.Unlock()
	err := c.UpdateRenter(renter)
	if err != nil {
		c.log.Error("unable to update renter after setting allowance", zap.Error(err))
	}

	// Cycle through all contracts and unlock them again since they might have
	// been locked by managedCancelAllowance previously.
	if unlockContracts {
		ids := c.staticContracts.IDs(rpk)
		for _, id := range ids {
			contract, exists := c.staticContracts.Acquire(id)
			if !exists {
				continue
			}
			utility := contract.Utility()
			utility.Locked = false
			err := c.managedUpdateContractUtility(contract, utility)
			c.staticContracts.Return(contract)
			if err != nil {
				return err
			}
		}
	}

	// Inform the watchdog about the allowance change.
	c.staticWatchdog.callAllowanceUpdated(rpk, a)

	// We changed the allowance successfully. Update the hostdb.
	err = c.hdb.SetAllowance(a)
	if err != nil {
		return err
	}

	return nil
}

// managedCancelAllowance handles the special case where the allowance is empty.
func (c *Contractor) managedCancelAllowance(rpk types.PublicKey) error {
	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return ErrRenterNotFound
	}

	c.log.Info("canceling allowance", zap.Stringer("renter", rpk))

	// First need to mark all active contracts.
	ids := c.staticContracts.IDs(rpk)
	c.mu.Lock()
	for _, id := range ids {
		// We aren't renewing, but we don't want new sessions to be created.
		c.renewing[id] = true
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		for _, id := range ids {
			delete(c.renewing, id)
		}
		c.mu.Unlock()
	}()

	// Clear out the allowance and save.
	c.mu.Lock()
	renter.Allowance = modules.Allowance{}
	renter.CurrentPeriod = 0
	c.renters[rpk] = renter
	c.mu.Unlock()
	err := c.UpdateRenter(renter)
	if err != nil {
		return err
	}

	// Cycle through all contracts and mark them as !goodForRenew and !goodForUpload.
	ids = c.staticContracts.IDs(rpk)
	for _, id := range ids {
		contract, exists := c.staticContracts.Acquire(id)
		if !exists {
			continue
		}
		utility := contract.Utility()
		utility.GoodForRenew = false
		utility.GoodForUpload = false
		utility.Locked = true
		err := c.managedUpdateContractUtility(contract, utility)
		c.staticContracts.Return(contract)
		if err != nil {
			return err
		}
	}
	return nil
}
