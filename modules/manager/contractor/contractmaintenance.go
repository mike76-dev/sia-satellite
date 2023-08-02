package contractor

import (
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/contractor/contractset"

	"go.sia.tech/core/types"
)

// MaxCriticalRenewFailThreshold is the maximum number of contracts failing to renew as
// fraction of the total hosts in the allowance before renew alerts are made
// critical.
const MaxCriticalRenewFailThreshold = 0.2

var (
	// ErrInsufficientAllowance indicates that the renter's allowance is less
	// than the amount necessary to store at least one sector
	ErrInsufficientAllowance = errors.New("allowance is not large enough to cover fees of contract creation")
	errTooExpensive          = errors.New("host price was too high")

	// errContractEnded is the error returned when the contract has already ended
	errContractEnded = errors.New("contract has already ended")

	// errContractNotGFR is used to indicate that a contract renewal failed
	// because the contract was marked !GFR.
	errContractNotGFR = errors.New("contract is not GoodForRenew")

	// errHostBlocked is the error returned when the host is blocked
	errHostBlocked = errors.New("host is blocked")
)

// callNotifyDoubleSpend is used by the watchdog to alert the contractor
// whenever a monitored file contract input is double-spent. This function
// marks down the host score, and marks the contract as !GoodForRenew and
// !GoodForUpload.
func (c *Contractor) callNotifyDoubleSpend(fcID types.FileContractID, blockHeight uint64) {
	c.log.Println("WARN: watchdog found a double-spend: ", fcID, blockHeight)

	// Mark the contract as double-spent. This will cause the contract to be
	// excluded in period spending.
	c.mu.Lock()
	c.doubleSpentContracts[fcID] = blockHeight
	c.mu.Unlock()

	err := c.MarkContractBad(fcID)
	if err != nil {
		c.log.Println("callNotifyDoubleSpend error in MarkContractBad", err)
	}
}

// managedCheckForDuplicates checks for static contracts that have the same host
// key and the same renter key and moves the older one to old contracts.
func (c *Contractor) managedCheckForDuplicates() {
	// Build map for comparison.
	pubkeys := make(map[string]types.FileContractID)
	var newContract, oldContract modules.RenterContract
	for _, contract := range c.staticContracts.ViewAll() {
		key := contract.RenterPublicKey.String() + contract.HostPublicKey.String()
		id, exists := pubkeys[key]
		if !exists {
			pubkeys[key] = contract.ID
			continue
		}

		// Duplicate contract found, determine older contract to delete.
		if rc, ok := c.staticContracts.View(id); ok {
			if rc.StartHeight >= contract.StartHeight {
				newContract, oldContract = rc, contract
			} else {
				newContract, oldContract = contract, rc
			}
			c.log.Printf("WARN: duplicate contract found. New contract is %x and old contract is %v\n", newContract.ID, oldContract.ID)

			// Get FileContract.
			oldSC, ok := c.staticContracts.Acquire(oldContract.ID)
			if !ok {
				// Update map.
				pubkeys[key] = newContract.ID
				continue
			}

			// Link the contracts to each other and then store the old contract
			// in the record of historic contracts.
			//
			// Note: This means that if there are multiple duplicates, say 3
			// contracts that all share the same host, then the ordering may not
			// be perfect. If in reality the renewal order was A<->B<->C, it's
			// possible for the contractor to end up with A->C and B<->C in the
			// mapping.
			c.mu.Lock()
			c.renewedFrom[newContract.ID] = oldContract.ID
			c.renewedTo[oldContract.ID] = newContract.ID
			c.staticContracts.ReplaceOldContract(oldContract.ID, oldSC)
			c.mu.Unlock()

			// Delete the contract.
			c.staticContracts.Delete(oldSC)
			c.staticContracts.Erase(oldSC.Metadata().ID)
			err := c.updateRenewedContract(oldContract.ID, newContract.ID)
			if err != nil {
				c.log.Println("ERROR: failed to update renewal history.")
			}

			// Update the pubkeys map to contain the newest contract id.
			pubkeys[key] = newContract.ID
		}
	}
}

// managedEstimateRenewFundingRequirements estimates the amount of money that a
// contract is going to need in the next billing cycle by looking at how much
// storage is in the contract and what the historic usage pattern of the
// contract has been.
func (c *Contractor) managedEstimateRenewFundingRequirements(contract modules.RenterContract, blockHeight uint64, allowance modules.Allowance) (types.Currency, error) {
	// Fetch the host pricing to use in the estimate.
	host, exists, err := c.hdb.Host(contract.HostPublicKey)
	if err != nil {
		return types.ZeroCurrency, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return types.ZeroCurrency, errors.New("could not find host in hostdb")
	}
	if host.Filtered {
		return types.ZeroCurrency, errHostBlocked
	}

	// Fetch the renter.
	c.mu.RLock()
	renter, err := c.managedFindRenter(contract.ID)
	c.mu.RUnlock()
	if err != nil {
		return types.ZeroCurrency, err
	}

	// Estimate the amount of money that's going to be needed for existing
	// storage.
	dataStored := contract.Transaction.FileContractRevisions[0].Filesize
	storageCost := types.NewCurrency64(dataStored).Mul64(allowance.Period).Mul(host.Settings.StoragePrice)

	// For the spending estimates, we're going to need to know the amount of
	// money that was spent on upload and download by this contract line in this
	// period. That's going to require iterating over the renew history of the
	// contract to get all the spending across any refreshes that occurred this
	// period.
	prevUploadSpending := contract.UploadSpending
	prevDownloadSpending := contract.DownloadSpending
	prevFundAccountSpending := contract.FundAccountSpending
	prevMaintenanceSpending := contract.MaintenanceSpending
	c.mu.Lock()
	currentID := contract.ID
	for i := 0; i < 10e3; i++ { // Prevent an infinite loop if there's an [impossible] contract cycle.
		// If there is no previous contract, nothing to do.
		var exists bool
		currentID, exists = c.renewedFrom[currentID]
		if !exists {
			break
		}

		// If the contract is not in oldContracts, that's probably a bug, but
		// nothing to do otherwise.
		currentContract, exists := c.staticContracts.OldContract(currentID)
		if !exists {
			c.log.Println("WARN: a known previous contract is not found in old contracts")
			break
		}

		// If the contract did not start in the current period, then it is not
		// relevant, and none of the previous contracts will be relevant either.
		if currentContract.StartHeight < renter.CurrentPeriod {
			break
		}

		// Add the historical spending metrics.
		prevUploadSpending = prevUploadSpending.Add(currentContract.UploadSpending)
		prevDownloadSpending = prevDownloadSpending.Add(currentContract.DownloadSpending)
		prevFundAccountSpending = prevFundAccountSpending.Add(currentContract.FundAccountSpending)
		prevMaintenanceSpending = prevMaintenanceSpending.Add(currentContract.MaintenanceSpending)
	}
	c.mu.Unlock()

	// Estimate the amount of money that's going to be needed for new storage
	// based on the amount of new storage added in the previous period. Account
	// for both the storage price as well as the upload price.
	prevUploadDataEstimate := prevUploadSpending
	if !host.Settings.UploadBandwidthPrice.IsZero() {
		// TODO: Because the host upload bandwidth price can change, this is not
		// the best way to estimate the amount of data that was uploaded to this
		// contract. Better would be to look at the amount of data stored in the
		// contract from the previous cycle and use that to determine how much
		// total data.
		prevUploadDataEstimate = prevUploadDataEstimate.Div(host.Settings.UploadBandwidthPrice)
	}
	// Sanity check - the host may have changed prices, make sure we aren't
	// assuming an unreasonable amount of data.
	if types.NewCurrency64(dataStored).Cmp(prevUploadDataEstimate) < 0 {
		prevUploadDataEstimate = types.NewCurrency64(dataStored)
	}
	// The estimated cost for new upload spending is the previous upload
	// bandwidth plus the implied storage cost for all of the new data.
	newUploadsCost := prevUploadSpending.Add(prevUploadDataEstimate.Mul64(allowance.Period).Mul(host.Settings.StoragePrice))

	// The download cost is assumed to be the same. Even if the user is
	// uploading more data, the expectation is that the download amounts will be
	// relatively constant. Add in the contract price as well.
	newDownloadsCost := prevDownloadSpending

	// The estimated cost for funding ephemeral accounts and performing RHP3
	// maintenance such as updating price tables and syncing the ephemeral
	// account balance is expected to remain identical.
	newFundAccountCost := prevFundAccountSpending
	newMaintenanceCost := prevMaintenanceSpending.Sum()

	contractPrice := host.Settings.ContractPrice

	// Aggregate all estimates so far to compute the estimated siafunds fees.
	// The transaction fees are not included in the siafunds estimate because
	// users are not charged siafund fees on money that doesn't go into the file
	// contract (and the transaction fee goes to the miners, not the file
	// contract).
	beforeSiafundFeesEstimate := storageCost.Add(newUploadsCost).Add(newDownloadsCost).Add(newFundAccountCost).Add(newMaintenanceCost).Add(contractPrice)
	afterSiafundFeesEstimate := modules.Tax(blockHeight, beforeSiafundFeesEstimate).Add(beforeSiafundFeesEstimate)

	// Get an estimate for how much money we will be charged before going into
	// the transaction pool.
	_, maxTxnFee := c.tpool.FeeEstimation()
	txnFees := maxTxnFee.Mul64(2 * modules.EstimatedFileContractTransactionSetSize)

	// Add them all up and then return the estimate plus 33% for error margin
	// and just general volatility of usage pattern.
	estimatedCost := afterSiafundFeesEstimate.Add(txnFees)
	estimatedCost = estimatedCost.Add(estimatedCost.Div64(3))

	// Check for a sane minimum. The contractor should not be forming contracts
	// with less than 'fileContractMinimumFunding / (num contracts)' of the
	// value of the allowance.
	minimum := modules.MulFloat(allowance.Funds, fileContractMinimumFunding).Div64(allowance.Hosts)
	if estimatedCost.Cmp(minimum) < 0 {
		estimatedCost = minimum
	}
	return estimatedCost, nil
}

// callInterruptContractMaintenance will issue an interrupt signal to any
// running maintenance, stopping that maintenance. If there are multiple threads
// running maintenance, they will all be stopped.
func (c *Contractor) callInterruptContractMaintenance() {
	// Spin up a thread to grab the maintenance lock. Signal that the lock was
	// acquired after the lock is acquired.
	gotLock := make(chan struct{})
	go func() {
		c.maintenanceLock.Lock()
		close(gotLock)
		c.maintenanceLock.Unlock()
	}()

	// There may be multiple threads contending for the maintenance lock. Issue
	// interrupts repeatedly until we get a signal that the maintenance lock has
	// been acquired.
	for {
		select {
		case <-gotLock:
			return
		case c.interruptMaintenance <- struct{}{}:
			c.log.Println("INFO: signal sent to interrupt contract maintenance")
		}
	}
}

// managedFindMinAllowedHostScores uses a set of random hosts from the hostdb to
// calculate minimum acceptable score for a host to be marked GFR and GFU.
func (c *Contractor) managedFindMinAllowedHostScores(rpk types.PublicKey) (types.Currency, types.Currency, error) {
	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.Currency{}, types.Currency{}, ErrRenterNotFound
	}

	// Pull a new set of hosts from the hostdb that could be used as a new set
	// to match the allowance. The lowest scoring host of these new hosts will
	// be used as a baseline for determining whether our existing contracts are
	// worthwhile.
	hostCount := int(renter.Allowance.Hosts)
	hosts, err := c.hdb.RandomHostsWithAllowance(hostCount+randomHostsBufferForScore, nil, nil, renter.Allowance)
	if err != nil {
		return types.Currency{}, types.Currency{}, err
	}

	if len(hosts) == 0 {
		return types.Currency{}, types.Currency{}, errors.New("no hosts returned in RandomHosts")
	}

	// Find the minimum score that a host is allowed to have to be considered
	// good for upload.
	var minScoreGFR, minScoreGFU types.Currency
	sb, err := c.hdb.ScoreBreakdown(hosts[0])
	if err != nil {
		return types.Currency{}, types.Currency{}, err
	}

	lowestScore := sb.Score
	for i := 1; i < len(hosts); i++ {
		score, err := c.hdb.ScoreBreakdown(hosts[i])
		if err != nil {
			return types.Currency{}, types.Currency{}, err
		}
		if score.Score.Cmp(lowestScore) < 0 {
			lowestScore = score.Score
		}
	}
	// Set the minimum acceptable score to a factor of the lowest score.
	minScoreGFR = lowestScore.Div(scoreLeewayGoodForRenew)
	minScoreGFU = lowestScore.Div(scoreLeewayGoodForUpload)

	return minScoreGFR, minScoreGFU, nil
}

// managedPruneRedundantAddressRange uses the hostdb to find hosts that
// violate the rules about address ranges and cancels them.
func (c *Contractor) managedPruneRedundantAddressRange() {
	// Get all contracts which are not canceled.
	allContracts := c.staticContracts.ViewAll()
	var contracts []modules.RenterContract
	for _, contract := range allContracts {
		if contract.Utility.Locked && !contract.Utility.GoodForRenew && !contract.Utility.GoodForUpload {
			// Contract is canceled.
			continue
		}
		contracts = append(contracts, contract)
	}

	// Get all the public keys and map them to contract ids.
	hosts := make(map[types.PublicKey]struct{})
	pks := make([]types.PublicKey, 0, len(allContracts))
	cids := make(map[types.PublicKey][]types.FileContractID)
	var fcids []types.FileContractID
	var exists bool
	for _, contract := range contracts {
		if _, exists := hosts[contract.HostPublicKey]; !exists {
			pks = append(pks, contract.HostPublicKey)
			hosts[contract.HostPublicKey] = struct{}{}
		}
		fcids, exists = cids[contract.HostPublicKey]
		if exists {
			cids[contract.HostPublicKey] = append(fcids, contract.ID)
		} else {
			cids[contract.HostPublicKey] = []types.FileContractID{contract.ID}
		}
	}

	// Let the hostdb filter out bad hosts and cancel contracts with those
	// hosts.
	badHosts, err := c.hdb.CheckForIPViolations(pks)
	if err != nil {
		c.log.Println("WARN: error checking for IP violations:", err)
		return
	}
	for _, host := range badHosts {
		// Multiple renters can have contracts with the same host, so we need
		// to iterate through those, too.
		for _, fcid := range cids[host] {
			if err := c.managedCancelContract(fcid); err != nil {
				c.log.Println("WARN: unable to cancel contract in managedPruneRedundantAddressRange", err)
			}
		}
	}
}

// managedAcquireAndUpdateContractUtility is a helper function that acquires
// a contract, updates its ContractUtility and returns the contract again.
func (c *Contractor) managedAcquireAndUpdateContractUtility(id types.FileContractID, utility modules.ContractUtility) error {
	fileContract, ok := c.staticContracts.Acquire(id)
	if !ok {
		return errors.New("failed to acquire contract for update")
	}
	defer c.staticContracts.Return(fileContract)

	return c.managedUpdateContractUtility(fileContract, utility)
}

// managedUpdateContractUtility is a helper function that updates the contract
// with the given utility.
func (c *Contractor) managedUpdateContractUtility(fileContract *contractset.FileContract, utility modules.ContractUtility) error {
	// Sanity check to verify that we aren't attempting to set a good utility on
	// a contract that has been renewed.
	c.mu.Lock()
	_, exists := c.renewedTo[fileContract.Metadata().ID]
	c.mu.Unlock()
	if exists && (utility.GoodForRenew || utility.GoodForUpload) {
		c.log.Println("CRITICAL: attempting to update contract utility on a contract that has been renewed")
	}

	return fileContract.UpdateUtility(utility)
}

// threadedContractMaintenance checks the set of contracts that the contractor
// has, dropping contracts which are no longer worthwhile.
//
// Between each network call, the thread checks whether a maintenance interrupt
// signal is being sent. If so, maintenance returns, yielding to whatever thread
// issued the interrupt.
func (c *Contractor) threadedContractMaintenance() {
	err := c.tg.Add()
	if err != nil {
		return
	}
	defer c.tg.Done()

	// No contract maintenance unless contractor is synced.
	if !c.managedSynced() {
		c.log.Println("INFO: skipping contract maintenance since consensus isn't synced yet")
		return
	}

	// Only one instance of this thread should be running at a time. It is
	// fine to return early if another thread is already doing maintenance.
	// The next block will trigger another round.
	if !c.maintenanceLock.TryLock() {
		c.log.Println("ERROR: maintenance lock could not be obtained")
		return
	}
	defer c.maintenanceLock.Unlock()

	// Get the current block height.
	c.mu.Lock()
	blockHeight := c.blockHeight
	c.mu.Unlock()

	// Perform general cleanup of the contracts. This includes archiving
	// contracts and other cleanup work.
	c.managedArchiveContracts()
	c.managedCheckForDuplicates()
	c.managedUpdatePubKeysToContractIDMap()
	c.managedPruneRedundantAddressRange()
	c.staticContracts.DeleteOldContracts(blockHeight)
	err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
	if err != nil {
		c.log.Println("ERROR: unable to update hostdb contracts:", err)
		return
	}

	// The total number of renews that failed for any reason.
	var numRenewFails int
	var renewErr error

	// Iterate through the active contracts and check which of them are
	// up for renewal. If the renter has opted in, add them to the renew
	// list.
	var renewSet []fileContractRenewal
	for _, contract := range c.staticContracts.ViewAll() {
		renter, err := c.managedFindRenter(contract.ID)
		if err != nil {
			c.log.Println("WARN: unable to find renter for contract", contract.ID)
			continue
		}

		// Check if the renter opted in for auto-renewals.
		if !renter.Settings.AutoRenewContracts {
			continue
		}

		// Check if the contract needs to be renewed because it is about to
		// expire.
		if blockHeight+renter.Allowance.RenewWindow >= contract.EndHeight {
			renewAmount, err := c.managedEstimateRenewFundingRequirements(contract, blockHeight, renter.Allowance)
			if err != nil {
				c.log.Println("WARN: contract skipped because there was an error estimating renew funding requirements", renewAmount, err)
				continue
			}
			renewSet = append(renewSet, fileContractRenewal{
				contract:     contract,
				amount:       renewAmount,
				renterPubKey: renter.PublicKey,
				secretKey:    renter.PrivateKey,
			})
		}
	}

	if len(renewSet) != 0 {
		c.log.Printf("INFO: renewing %v contracts\n", len(renewSet))
	}

	// Update the failed renew map so that it only contains contracts which we
	// are currently trying to renew. The failed renew map is a map that we use
	// to track how many times consecutively we failed to renew a contract with
	// a host, so that we know if we need to abandon that host.
	c.mu.Lock()
	newFirstFailedRenew := make(map[types.FileContractID]uint64)
	for _, r := range renewSet {
		if _, exists := c.numFailedRenews[r.contract.ID]; exists {
			newFirstFailedRenew[r.contract.ID] = c.numFailedRenews[r.contract.ID]
		}
	}
	c.numFailedRenews = newFirstFailedRenew
	c.mu.Unlock()

	// Go through the contracts we've assembled for renewal.
	hastings := modules.Float64(types.HastingsPerSiacoin)
	for _, renewal := range renewSet {
		// Return here if an interrupt or kill signal has been sent.
		select {
		case <-c.tg.StopChan():
			c.log.Println("INFO: returning because the contractor was stopped")
		default:
		}

		unlocked, err := c.wallet.Unlocked()
		if !unlocked || err != nil {
			c.log.Println("WARN: contractor is attempting to renew contracts that are about to expire, however the wallet is locked")
			return
		}

		// Get the renter. The error is ignored since we know already that
		// the renter exists.
		renter, _ := c.managedFindRenter(renewal.contract.ID)

		// Check if the renter has a sufficient balance.
		ub, err := c.m.GetBalance(renter.Email)
		if err != nil {
			c.log.Println("ERROR: couldn't get renter balance:", err)
			continue
		}
		cost := modules.Float64(renewal.amount)
		if ub.Balance < cost/hastings {
			c.log.Println("INFO: renewal skipped, because renter balance is insufficient")
			continue
		}

		// Renew one contract. The error is ignored because the renew function
		// already will have logged the error, and in the event of an error,
		// 'fundsSpent' will return '0'.
		fundsSpent, newContract, err := c.managedRenewContract(renewal.contract, renewal.renterPubKey, renewal.secretKey, renewal.amount, renter.ContractEndHeight())
		if modules.ContainsError(err, errContractNotGFR) {
			// Do not add a renewal error.
			c.log.Println("INFO: contract skipped because it is not good for renew", renewal.contract.ID)
		} else if err != nil {
			c.log.Println("ERROR: error renewing a contract", renewal.contract.ID, err)
			renewErr = modules.ComposeErrors(renewErr, err)
			numRenewFails++
		}

		if err == nil {
			// Lock the funds in the database.
			funds := modules.Float64(fundsSpent)
			amount := funds / hastings
			err = c.m.LockSiacoins(renter.Email, amount)
			if err != nil {
				c.log.Println("ERROR: couldn't lock funds")
			}
			// Increment the number of renewals in the database.
			err = c.m.IncrementStats(renter.Email, true)
			if err != nil {
				c.log.Println("ERROR: couldn't update stats")
			}

			// Add this contract to the contractor and save.
			err = c.managedAcquireAndUpdateContractUtility(newContract.ID, modules.ContractUtility{
				GoodForUpload: true,
				GoodForRenew:  true,
			})
			if err != nil {
				c.log.Println("ERROR: failed to update the contract utilities", err)
				continue
			}
			c.mu.Lock()
			err = c.save()
			c.mu.Unlock()
			if err != nil {
				c.log.Println("ERROR: unable to save the contractor:", err)
			}
		}
	}
}
