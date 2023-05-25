package contractor

import (
	"errors"
	"fmt"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/proto"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// FormContract forms a contract with the specified host, puts it
// in the contract set, and returns it.
func (c *Contractor) FormContract(s *modules.RPCSession, pk, rpk, hpk types.SiaPublicKey, endHeight types.BlockHeight, funding types.Currency) (modules.RenterContract, error) {
	// No contract formation until the contractor is synced.
	if !c.managedSynced() {
		return modules.RenterContract{}, errors.New("contractor isn't synced yet")
	}

	// Find the renter.
	c.mu.RLock()
	renter, exists := c.renters[pk.String()]
	c.mu.RUnlock()
	if !exists {
		return modules.RenterContract{}, ErrRenterNotFound
	}

	// Determine the max and min initial contract funding.
	maxInitialContractFunds := funding.Mul64(MaxInitialContractFundingMulFactor).Div64(MaxInitialContractFundingDivFactor)
	minInitialContractFunds := funding.Div64(MinInitialContractFundingDivFactor)

	// Get the host.
	host, _, err := c.hdb.Host(hpk)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Calculate the anticipated transaction fee.
	_, maxFee := c.tpool.FeeEstimation()
	txnFee := maxFee.Mul64(smodules.EstimatedFileContractTransactionSetSize).Mul64(3)

	// Calculate the contract funding with the host.
	contractFunds := host.ContractPrice.Add(txnFee).Mul64(ContractFeeFundingMulFactor)

	// Check that the contract funding is reasonable compared to the max and
	// min initial funding.
	if contractFunds.Cmp(maxInitialContractFunds) > 0 {
		contractFunds = maxInitialContractFunds
	}
	if contractFunds.Cmp(minInitialContractFunds) < 0 {
		contractFunds = minInitialContractFunds
	}

	// Confirm that the wallet is unlocked.
	unlocked, err := c.wallet.Unlocked()
	if !unlocked || err != nil {
		return modules.RenterContract{}, errors.New("the wallet is locked")
	}

	// Attempt forming a contract with this host.
	start := time.Now()
	fundsSpent, newContract, err := c.managedFormNewContract(s, pk, rpk, host, endHeight, contractFunds)
	if err != nil {
		c.log.Printf("Attempted to form a contract with %v, time spent %v, but negotiation failed: %v\n", host.NetAddress, time.Since(start).Round(time.Millisecond), err)
		return modules.RenterContract{}, err
	}

	// Lock the funds in the database.
	funds, _ := fundsSpent.Float64()
	hastings, _ := types.SiacoinPrecision.Float64()
	amount := funds / hastings
	err = c.satellite.LockSiacoins(renter.Email, amount)
	if err != nil {
		c.log.Println("ERROR: couldn't lock funds:", err)
	}

	// Increment the number of formations in the database.
	err = c.satellite.IncrementStats(renter.Email, false)
	if err != nil {
		c.log.Println("ERROR: couldn't update stats")
	}

	// Add this contract to the contractor and save.
	err = c.managedAcquireAndUpdateContractUtility(newContract.ID, smodules.ContractUtility{
		GoodForUpload: true,
		GoodForRenew:  true,
	})
	if err != nil {
		c.log.Println("Failed to update the contract utilities", err)
		return modules.RenterContract{}, err
	}
	c.mu.Lock()
	err = c.save()
	c.mu.Unlock()
	if err != nil {
		c.log.Println("Unable to save the contractor:", err)
	}

	return newContract, nil
}

// managedFormNewContract negotiates an initial file contract with the specified
// host, saves it, and returns it.
func (c *Contractor) managedFormNewContract(s *modules.RPCSession, pk, rpk types.SiaPublicKey, host smodules.HostDBEntry, endHeight types.BlockHeight, funding types.Currency) (_ types.Currency, _ modules.RenterContract, err error) {
	// Get an address to use for negotiation.
	uc, err := c.wallet.NextAddress()
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}
	defer func() {
		if err != nil {
			wErr := c.wallet.MarkAddressUnused(uc)
			if wErr != nil {
				err = fmt.Errorf("%s; %s", err, wErr)
			}
		}
	}()

	// Create transaction builder and trigger contract formation.
	txnBuilder, err := c.wallet.StartTransaction()
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Form the contract.
	c.mu.RLock()
	startHeight := c.blockHeight
	c.mu.RUnlock()
	contract, formationTxnSet, sweepTxn, sweepParents, err := c.staticContracts.FormNewContract(s, pk, rpk, host, startHeight, endHeight, funding, uc.UnlockHash(), txnBuilder, c.tpool, c.hdb, c.tg.StopChan())
	if err != nil {
		txnBuilder.Drop() // Return unused outputs to wallet.
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	monitorContractArgs := monitorContractArgs{
		false,
		contract.ID,
		contract.Transaction,
		formationTxnSet,
		sweepTxn,
		sweepParents,
		startHeight,
	}
	err = c.staticWatchdog.callMonitorContract(monitorContractArgs)
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Add a mapping from the contract's id to the public keys of the host
	// and the renter.
	c.mu.Lock()
	_, exists := c.pubKeysToContractID[contract.RenterPublicKey.String() + contract.HostPublicKey.String()]
	if exists {
		c.mu.Unlock()
		txnBuilder.Drop()
		// We need to return a funding value because money was spent on this
		// host, even though the full process could not be completed.
		c.log.Println("WARN: Attempted to form a new contract with a host that this renter already has a contract with.")
		return funding, modules.RenterContract{}, fmt.Errorf("%v already has a contract with host %v", contract.RenterPublicKey.String(), contract.HostPublicKey.String())
	}
	c.pubKeysToContractID[contract.RenterPublicKey.String() + contract.HostPublicKey.String()] = contract.ID
	c.mu.Unlock()

	contractValue := contract.RenterFunds
	c.log.Printf("Formed contract %v with %v for %v\n", contract.ID, host.NetAddress, contractValue.HumanString())

	// Update the hostdb to include the new contract.
	err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
	if err != nil {
		c.log.Println("Unable to update hostdb contracts:", err)
	}
	return funding, contract, nil
}

// RenewContract tries to renew the given contract.
func (c *Contractor) RenewContract(s *modules.RPCSession, pk types.SiaPublicKey, contract modules.RenterContract, endHeight types.BlockHeight, funding types.Currency) (modules.RenterContract, error) {
	// No contract renewal until the contractor is synced.
	if !c.managedSynced() {
		return modules.RenterContract{}, errors.New("contractor isn't synced yet")
	}

	// Find the renter.
	c.mu.RLock()
	renter, exists := c.renters[pk.String()]
	c.mu.RUnlock()
	if !exists {
		return modules.RenterContract{}, ErrRenterNotFound
	}

	// Check if the contract belongs to the renter.
	r, err := c.managedFindRenter(contract.ID)
	if err != nil {
		c.log.Println("WARN: contract ID submitted that has no known renter associated with it:", contract.ID)
		return modules.RenterContract{}, err
	}
	if r.PublicKey.String() != pk.String() {
		c.log.Println("WARN: contract ID submitted that doesn't belong to this renter:", contract.ID, renter.PublicKey.String())
		return modules.RenterContract{}, errors.New("contract doesn't belong to this renter")
	}

	// Check if the wallet is unlocked.
	unlocked, err := c.wallet.Unlocked()
	if !unlocked || err != nil {
		c.log.Println("Contractor is attempting to renew a contract but the wallet is locked")
		return modules.RenterContract{}, err
	}

	// Renew the contract.
	fundsSpent, newContract, err := c.managedRenewOldContract(s, pk, contract, endHeight, funding)
	if err != nil {
		c.log.Printf("Attempted to renew a contract with %v, but renewal failed: %v\n", contract.HostPublicKey, err)
		return modules.RenterContract{}, err
	}

	// Lock the funds in the database.
	funds, _ := fundsSpent.Float64()
	hastings, _ := types.SiacoinPrecision.Float64()
	amount := funds / hastings
	err = c.satellite.LockSiacoins(renter.Email, amount)
	if err != nil {
		c.log.Println("ERROR: couldn't lock funds")
	}

	// Increment the number of renewals in the database.
	err = c.satellite.IncrementStats(renter.Email, true)
	if err != nil {
		c.log.Println("ERROR: couldn't update stats")
	}

	// Add this contract to the contractor and save.
	err = c.managedAcquireAndUpdateContractUtility(newContract.ID, smodules.ContractUtility{
		GoodForUpload: true,
		GoodForRenew:  true,
	})
	if err != nil {
		c.log.Println("Failed to update the contract utilities", err)
		return modules.RenterContract{}, err
	}
	c.mu.Lock()
	err = c.save()
	c.mu.Unlock()
	if err != nil {
		c.log.Println("Unable to save the contractor:", err)
	}

	return newContract, nil
}

// managedRenewOldContract renews a contract and returns the amount of
// money that was put for renewal.
func (c *Contractor) managedRenewOldContract(s *modules.RPCSession, pk types.SiaPublicKey, contract modules.RenterContract, endHeight types.BlockHeight, funding types.Currency) (fundsSpent types.Currency, newContract modules.RenterContract, err error) {
	// Pull out the variables.
	id := contract.ID
	renterKey := contract.RenterPublicKey
	hostKey := contract.HostPublicKey

	// Get the host.
	host, _, err := c.hdb.Host(hostKey)
	if err != nil {
		return
	}

	// Get the host settings.
	hostSettings, err := proto.HostSettings(string(host.NetAddress), hostKey)
	if err != nil {
		err = fmt.Errorf("%s: unable to fetch host settings", err)
		return
	}

	// Use the most recent hostSettings, along with the host db entry.
	host.HostExternalSettings = hostSettings

	// Cap host.MaxCollateral.
	if host.MaxCollateral.Cmp(maxCollateral) > 0 {
		host.MaxCollateral = maxCollateral
	}

	// Mark the contract as being renewed, and defer logic to unmark it
	// once renewing is complete.
	c.log.Println("Marking a contract for renew:", id)
	c.mu.Lock()
	startHeight := c.blockHeight
	c.renewing[id] = true
	c.mu.Unlock()
	defer func() {
		c.log.Println("Unmarking the contract for renew", id)
		c.mu.Lock()
		delete(c.renewing, id)
		c.mu.Unlock()
	}()

	// Get an address to use for negotiation.
	uc, err := c.wallet.NextAddress()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			wErr := c.wallet.MarkAddressUnused(uc)
			if wErr != nil {
				err = fmt.Errorf("%s; %s", err, wErr)
			}
		}
	}()

	// Create a transaction builder with the correct amount of funding for the renewal.
	txnBuilder, err := c.wallet.StartTransaction()
	if err != nil {
		return
	}
	err = txnBuilder.FundSiacoins(funding)
	if err != nil {
		txnBuilder.Drop() // Return unused outputs to wallet.
		return
	}
	// Add an output that sends all fund back to the refund address.
	// Note that in order to send this transaction, a miner fee will have to be subtracted.
	uh := uc.UnlockHash()
	output := types.SiacoinOutput{
		Value:      funding,
		UnlockHash: uh,
	}
	sweepTxn, sweepParents := txnBuilder.Sweep(output)

	var formationTxnSet []types.Transaction

	oldContract, ok := c.staticContracts.Acquire(id)
	if !ok {
		txnBuilder.Drop() // Return unused outputs to wallet.
		return types.ZeroCurrency, modules.RenterContract{}, errContractNotFound
	}

	// Perform the actual renewal.
	var errRenew error
	newContract, formationTxnSet, errRenew = c.staticContracts.RenewContract(s, oldContract, pk, renterKey, host, startHeight, endHeight, funding, uh, txnBuilder, c.tpool, c.hdb, c.tg.StopChan())
	c.staticContracts.Return(oldContract)
	if errRenew != nil {
		txnBuilder.Drop() // Return unused outputs to wallet.
	}

	if errRenew == nil {
		monitorContractArgs := monitorContractArgs{
			false,
			newContract.ID,
			newContract.Transaction,
			formationTxnSet,
			sweepTxn,
			sweepParents,
			startHeight,
		}
		err = c.staticWatchdog.callMonitorContract(monitorContractArgs)
		if err != nil {
			c.log.Println("Unable to monitor contract:", err)
		}

		// Add a mapping from the contract's id to the public keys of the renter
		// and the host. This will destroy the previous mapping from pubKey to
		// contract id but other modules are only interested in the most recent
		// contract anyway.
		c.mu.Lock()
		c.pubKeysToContractID[newContract.RenterPublicKey.String() + newContract.HostPublicKey.String()] = newContract.ID
		c.mu.Unlock()

		// Update the hostdb to include the new contract.
		err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
		if err != nil {
			c.log.Println("Unable to update hostdb contracts:", err)
		}
	}

	// Update the old contract.
	oldContract, exists := c.staticContracts.Acquire(id)
	if !exists {
		return types.ZeroCurrency, newContract, errors.New("failed to acquire oldContract after renewal")
	}
	oldUtility := oldContract.Utility()
	if errRenew != nil {
		// Increment the number of failed renewals for the contract if it
		// was the host's fault.
		if smodules.IsHostsFault(errRenew) {
			c.mu.Lock()
			c.numFailedRenews[oldContract.Metadata().ID]++
			totalFailures := c.numFailedRenews[oldContract.Metadata().ID]
			c.mu.Unlock()
			c.log.Println("remote host determined to be at fault, tallying up failed renews", totalFailures, id)
		}

		// Check if contract has to be replaced.
		md := oldContract.Metadata()
		c.mu.RLock()
		numRenews, failedBefore := c.numFailedRenews[md.ID]
		c.mu.RUnlock()
		secondHalfOfWindow := startHeight + host.WindowSize / 2 >= md.EndHeight
		replace := numRenews >= consecutiveRenewalsBeforeReplacement
		if failedBefore && secondHalfOfWindow && replace {
			oldUtility.GoodForRenew = false
			oldUtility.GoodForUpload = false
			oldUtility.Locked = true
			err := c.callUpdateUtility(oldContract, oldUtility, true)
			if err != nil {
				c.log.Println("WARN: failed to mark contract as !goodForRenew:", err)
			}
			c.log.Printf("WARN: consistently failed to renew %v, marked as bad and locked: %v\n",
				oldContract.Metadata().HostPublicKey, errRenew)
			c.staticContracts.Return(oldContract)
			return types.ZeroCurrency, newContract, fmt.Errorf("%s: contract marked as bad for too many consecutive failed renew attempts", errRenew)
		}

		// Seems like it doesn't have to be replaced yet. Log the
		// failure and number of renews that have failed so far.
		c.log.Printf("WARN: failed to renew contract %v [%v]: '%v', current height: %v, proposed end height: %v",
			oldContract.Metadata().HostPublicKey, numRenews, errRenew, startHeight, endHeight)
		c.staticContracts.Return(oldContract)
		return types.ZeroCurrency, newContract, fmt.Errorf("%s: contract renewal with host was unsuccessful", errRenew)
	}
	c.log.Printf("Renewed contract %v\n", id)

	// Update the utility values for the new contract, and for the old
	// contract.
	newUtility := smodules.ContractUtility{
		GoodForUpload: true,
		GoodForRenew:  true,
	}
	if err := c.managedAcquireAndUpdateContractUtility(newContract.ID, newUtility); err != nil {
		c.log.Println("Failed to update the contract utilities", err)
		c.staticContracts.Return(oldContract)
		return funding, newContract, nil
	}
	oldUtility.GoodForRenew = false
	oldUtility.GoodForUpload = false
	oldUtility.Locked = true
	if err := c.callUpdateUtility(oldContract, oldUtility, true); err != nil {
		c.log.Println("Failed to update the contract utilities", err)
		c.staticContracts.Return(oldContract)
		return funding, newContract, nil
	}

	// Lock the contractor as we update it to use the new contract
	// instead of the old contract.
	c.mu.Lock()
	// Link Contracts.
	c.renewedFrom[newContract.ID] = id
	c.renewedTo[id] = newContract.ID
	// Store the contract in the record of historic contracts.
	c.staticContracts.RetireContract(id)
	c.mu.Unlock()

	// Update the database.
	err = c.updateRenewedContract(id, newContract.ID)
	if err != nil {
		c.log.Println("Failed to update contracts in the database.")
	}

	// Delete the old contract.
	c.staticContracts.Delete(oldContract)

	// Signal to the watchdog that it should immediately post the last
	// revision for this contract.
	go c.staticWatchdog.threadedSendMostRecentRevision(oldContract.Metadata())
	return funding, newContract, nil
}
