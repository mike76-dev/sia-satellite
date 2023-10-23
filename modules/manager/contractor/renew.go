package contractor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

type (
	// fileContractRenewal is an instruction to renew a file contract.
	fileContractRenewal struct {
		contract     modules.RenterContract
		amount       types.Currency
		renterPubKey types.PublicKey
		secretKey    types.PrivateKey
		hostScore    types.Currency
	}
)

// prepareContractRenewal creates a renewed contract and a renewal
// transaction set.
func (c *Contractor) prepareContractRenewal(host modules.HostDBEntry, oldRev types.FileContractRevision, contractFunding, hostCollateral types.Currency, endHeight uint64, address types.Address) ([]types.Transaction, types.Transaction, []types.Transaction, types.Currency, types.Currency, types.Currency, []types.Hash256, error) {
	c.mu.RLock()
	blockHeight := c.blockHeight
	c.mu.RUnlock()

	// Create the final revision from the provided revision.
	finalRevision := oldRev
	finalRevision.MissedProofOutputs = finalRevision.ValidProofOutputs
	finalRevision.Filesize = 0
	finalRevision.FileMerkleRoot = types.Hash256{}
	finalRevision.RevisionNumber = math.MaxUint64

	// Prepare the new contract.
	fc, basePrice := rhpv3.PrepareContractRenewal(oldRev, host.Settings.Address, address, contractFunding, hostCollateral, host.PriceTable, endHeight)

	// RHP3 contains both the contract and final revision. So we double the
	// estimation.
	_, txnFee := c.tpool.FeeEstimation()
	minerFee := txnFee.Mul64(2 * modules.EstimatedFileContractTransactionSetSize)

	// Create the transaction containing both the final revision and new
	// contract.
	txn := types.Transaction{
		FileContracts:         []types.FileContract{fc},
		FileContractRevisions: []types.FileContractRevision{finalRevision},
		MinerFees:             []types.Currency{minerFee},
	}

	// Compute how much renter funds to put into the new contract.
	tax := modules.Tax(blockHeight, fc.Payout)
	cost := fc.ValidRenterPayout().Add(host.PriceTable.ContractPrice)
	totalCost := cost.Add(minerFee).Add(basePrice).Add(tax)

	// Fund the transaction.
	parentTxn, toSign, err := c.wallet.FundTransaction(&txn, totalCost)
	if err != nil {
		c.wallet.ReleaseInputs(append([]types.Transaction{parentTxn}, txn))
		return nil, types.Transaction{}, nil, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, nil, modules.AddContext(err, "unable to fund transaction")
	}

	// Make a copy of the transactions to be used to by the watchdog
	// to double spend these inputs in case the contract never appears on chain.
	sweepTxn := modules.CopyTransaction(txn)
	sweepParents := []types.Transaction{modules.CopyTransaction(parentTxn)}

	// Add an output that sends all funds back to the Satellite address.
	output := types.SiacoinOutput{
		Value:   totalCost,
		Address: address,
	}
	sweepTxn.SiacoinOutputs = append(sweepTxn.SiacoinOutputs, output)

	return append([]types.Transaction{parentTxn}, txn), sweepTxn, sweepParents, totalCost, minerFee, tax, toSign, nil
}

// managedRenewContract will try to renew a contract, returning the
// amount of money that was put into the contract for renewal.
func (c *Contractor) managedRenewContract(oldContract modules.RenterContract, rpk types.PublicKey, rsk types.PrivateKey, contractFunding types.Currency, endHeight uint64) (fundsSpent types.Currency, newContract modules.RenterContract, err error) {
	c.mu.RLock()
	blockHeight := c.blockHeight

	// Check if we know this renter.
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.ZeroCurrency, newContract, ErrRenterNotFound
	}

	id := oldContract.ID
	allowance := renter.Allowance
	if reflect.DeepEqual(allowance, modules.Allowance{}) {
		return types.ZeroCurrency, modules.RenterContract{}, errors.New("called managedRenewContract but allowance isn't set")
	}

	// Fetch the host.
	hostPubKey := oldContract.HostPublicKey
	host, exists, err := c.hdb.Host(hostPubKey)
	if err != nil {
		return types.ZeroCurrency, newContract, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return types.ZeroCurrency, newContract, errHostNotFound
	} else if host.Filtered {
		return types.ZeroCurrency, newContract, errHostBlocked
	}

	// Get the host settings and use the most recent hostSettings,
	// along with the hostDB entry.
	hostSettings, err := proto.HostSettings(string(host.Settings.NetAddress), hostPubKey)
	if err != nil {
		err = modules.AddContext(err, "unable to fetch host settings")
		return
	}
	host.Settings = hostSettings
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return types.ZeroCurrency, newContract, fmt.Errorf("failed to get host name: %v", err)
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Mark the contract as being renewed, and defer logic to unmark it
	// once renewing is complete.
	c.mu.Lock()
	c.renewing[id] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.renewing, id)
		c.mu.Unlock()
	}()

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), renewContractTimeout)
	defer cancelFunc()
	go func() {
		select {
		case <-c.tg.StopChan():
			cancelFunc()
		case <-ctx.Done():
		}
	}()

	// Increase Successful/Failed interactions accordingly.
	var hostFault bool
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(hostPubKey)
			if hostFault {
				err = fmt.Errorf("%v: %v", errHostFault, err)
			}
		} else {
			c.hdb.IncrementSuccessfulInteractions(hostPubKey)
		}
	}()

	// Perform the actual renewal. If the renewal succeeds, return the
	// contract. If the renewal fails we check how often it has failed
	// before. Once it has failed for a certain number of blocks in a
	// row and reached its second half of the renew window, we give up
	// on renewing it and set goodForRenew to false.
	var txnSet, sweepParents []types.Transaction
	var sweepTxn types.Transaction
	var contractPrice, minerFee, siafundFee types.Currency
	var toSign []types.Hash256
	var rev rhpv2.ContractRevision
	errRenew := proto.WithTransportV3(ctx, siamuxAddr, host.PublicKey, func(t *rhpv3.Transport) (err error) {
		// Fetch the price table.
		pt, err := proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) { return nil, nil })
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get price table")
		}
		host.PriceTable = pt

		// Check if the host is gouging.
		_, txnFee := c.tpool.FeeEstimation()
		if err := modules.CheckGouging(allowance, blockHeight, &hostSettings, &pt, txnFee); err != nil {
			hostFault = true
			return modules.AddContext(err, "host is gouging")
		}

		// Fetch the latest revision.
		oldRev, err := proto.RPCLatestRevision(ctx, t, id)
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get latest revision")
		}

		// Derive ephemeral key.
		esk := modules.DeriveEphemeralKey(rsk, host.PublicKey)

		// Get an address to use for negotiation.
		uc, err := c.wallet.NextAddress()
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				err = modules.ComposeErrors(err, c.wallet.MarkAddressUnused(uc))
			}
		}()

		// Avoid panic in rhpv2.ContractRenewalCollateral.
		if oldRev.FileContract.EndHeight() > endHeight {
			endHeight = oldRev.FileContract.EndHeight()
		}

		// Prepare the file contract and the final revision.
		expectedStorage := fundsToExpectedStorage(contractFunding, endHeight-blockHeight, hostSettings)
		hostCollateral := rhpv2.ContractRenewalCollateral(oldRev.FileContract, expectedStorage, hostSettings, blockHeight, endHeight)
		contractPrice = host.PriceTable.ContractPrice
		var renterTxnSet []types.Transaction
		renterTxnSet, sweepTxn, sweepParents, fundsSpent, minerFee, siafundFee, toSign, err = c.prepareContractRenewal(host, oldRev, contractFunding, hostCollateral, endHeight, uc.UnlockHash())
		if err != nil {
			return err
		}

		// Renew the contract.
		rev, txnSet, err = proto.RPCRenewContract(ctx, t, esk, oldRev, renterTxnSet, toSign, c.wallet)
		if err != nil {
			if !modules.ContainsError(err, errors.New("failed to sign transaction")) {
				hostFault = true
			}
			c.wallet.ReleaseInputs(renterTxnSet)
			return modules.AddContext(err, "couldn't renew contract")
		}

		// Submit to blockchain.
		err = c.tpool.AcceptTransactionSet(txnSet)
		if modules.ContainsError(err, errDuplicateTransactionSet) {
			// As long as it made it into the transaction pool, we're good.
			err = nil
		}
		if err != nil {
			c.wallet.ReleaseInputs(txnSet)
			return modules.AddContext(err, "couldn't broadcast transaction set")
		}

		return nil
	})

	oldFC, exists := c.staticContracts.Acquire(id)
	if !exists {
		return fundsSpent, modules.RenterContract{}, modules.AddContext(errContractNotFound, "failed to acquire oldContract after renewal")
	}

	if errRenew == nil {
		// Add contract to the set.
		revisionTxn := types.Transaction{
			FileContractRevisions: []types.FileContractRevision{rev.Revision},
			Signatures:            []types.TransactionSignature{rev.Signatures[0], rev.Signatures[1]},
		}
		newContract, err = c.staticContracts.InsertContract(revisionTxn, blockHeight, fundsSpent, contractPrice, minerFee, siafundFee, rpk, false)
		if err != nil {
			c.staticContracts.Return(oldFC)
			return fundsSpent, modules.RenterContract{}, modules.AddContext(err, "couldn't add the new contract to the contract set")
		}

		// Commit changes to old contract.
		if err := oldFC.Clear(txnSet[len(txnSet)-1]); err != nil {
			c.staticContracts.Return(oldFC)
			return fundsSpent, modules.RenterContract{}, modules.AddContext(err, "couldn't clear the old contract")
		}

		// Inform watchdog about the new contract.
		monitorContractArgs := monitorContractArgs{
			newContract.ID,
			revisionTxn,
			txnSet,
			sweepTxn,
			sweepParents,
			blockHeight,
		}
		err = c.staticWatchdog.callMonitorContract(monitorContractArgs)
		if err != nil {
			c.staticContracts.Return(oldFC)
			return fundsSpent, modules.RenterContract{}, err
		}

		// Add a mapping from the contract's id to the public keys of the renter
		// and the host. This will destroy the previous mapping from pubKey to
		// contract id but other modules are only interested in the most recent
		// contract anyway.
		c.mu.Lock()
		c.pubKeysToContractID[newContract.RenterPublicKey.String()+newContract.HostPublicKey.String()] = newContract.ID
		c.mu.Unlock()

		// Update the hostdb to include the new contract.
		err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
		if err != nil {
			c.log.Println("ERROR: unable to update hostdb contracts:", err)
		}
	}

	oldUtility := oldFC.Utility()
	if errRenew != nil {
		// Increment the number of failed renewals for the contract if it
		// was the host's fault.
		if hostFault {
			c.mu.Lock()
			totalFailures := c.numFailedRenews[id]
			totalFailures++
			c.numFailedRenews[id] = totalFailures
			c.mu.Unlock()
			c.log.Println("INFO: remote host determined to be at fault, tallying up failed renews", totalFailures, id)
		}

		// Check if contract has to be replaced.
		md := oldFC.Metadata()
		c.mu.RLock()
		numRenews, failedBefore := c.numFailedRenews[md.ID]
		c.mu.RUnlock()
		secondHalfOfWindow := blockHeight+allowance.RenewWindow/2 >= md.EndHeight
		replace := numRenews >= consecutiveRenewalsBeforeReplacement
		if failedBefore && secondHalfOfWindow && replace {
			oldUtility.GoodForRenew = false
			oldUtility.GoodForUpload = false
			oldUtility.Locked = true
			err := c.managedUpdateContractUtility(oldFC, oldUtility)
			if err != nil {
				c.log.Println("WARN: failed to mark contract as !goodForRenew:", err)
			}

			c.log.Printf("WARN: consistently failed to renew %v, marked as bad and locked: %v\n",
				hostPubKey, errRenew)
			c.staticContracts.Return(oldFC)

			return types.ZeroCurrency, newContract, modules.AddContext(errRenew, "contract marked as bad for too many consecutive failed renew attempts")
		}

		// Seems like it doesn't have to be replaced yet. Log the
		// failure and number of renews that have failed so far.
		c.log.Printf("WARN: failed to renew contract %v [%v]: '%v', current height: %v, proposed end height: %v", hostPubKey, numRenews, errRenew, blockHeight, endHeight)
		c.staticContracts.Return(oldFC)

		return types.ZeroCurrency, newContract, modules.AddContext(errRenew, "contract renewal with host was unsuccessful")
	}
	c.log.Printf("INFO: renewed contract %v\n", id)

	// Update the utility values for the new contract, and for the old
	// contract.
	newUtility := modules.ContractUtility{
		GoodForUpload: true,
		GoodForRenew:  true,
	}
	if err := c.managedAcquireAndUpdateContractUtility(newContract.ID, newUtility); err != nil {
		c.log.Println("ERROR: failed to update the contract utilities", err)
		c.staticContracts.Return(oldFC)
		return fundsSpent, newContract, nil
	}

	oldUtility.GoodForRenew = false
	oldUtility.GoodForUpload = false
	oldUtility.Locked = true
	if err := c.managedUpdateContractUtility(oldFC, oldUtility); err != nil {
		c.log.Println("ERROR: failed to update the contract utilities", err)
		c.staticContracts.Return(oldFC)
		return fundsSpent, newContract, nil
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
		c.log.Println("ERROR: failed to update contracts in the database.")
	}

	// Delete the old contract.
	c.staticContracts.Delete(oldFC)

	// Signal to the watchdog that it should immediately post the last
	// revision for this contract.
	go c.staticWatchdog.threadedSendMostRecentRevision(oldFC.Metadata())

	return fundsSpent, newContract, nil
}

// RenewContracts tries to renew a given set of contracts.
func (c *Contractor) RenewContracts(rpk types.PublicKey, rsk types.PrivateKey, contracts []types.FileContractID) ([]modules.RenterContract, error) {
	// No contract renewal until the contractor is synced.
	if !c.managedSynced() {
		return nil, errors.New("contractor isn't synced yet")
	}

	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	blockHeight := c.blockHeight
	c.mu.RUnlock()
	if !exists {
		return nil, ErrRenterNotFound
	}

	// The total number of renews that failed for any reason.
	var numRenewFails int
	var renewErr error

	// Register or unregister and alerts related to contract renewal.
	var registerLowFundsAlert bool
	defer func() {
		if registerLowFundsAlert {
			c.staticAlerter.RegisterAlert(modules.AlertIDRenterAllowanceLowFunds, AlertMSGAllowanceLowFunds, AlertCauseInsufficientAllowanceFunds, modules.SeverityWarning)
		} else {
			c.staticAlerter.UnregisterAlert(modules.AlertIDRenterAllowanceLowFunds)
		}
	}()

	var renewSet []fileContractRenewal
	var fundsRemaining types.Currency

	// Iterate through the contracts.
	contractSet := make([]modules.RenterContract, 0, len(contracts))
	for _, id := range contracts {
		rc, ok := c.staticContracts.View(id)
		r, err := c.managedFindRenter(id)
		if err != nil {
			c.log.Println("WARN: contract ID submitted that has no known renter associated with it:", id)
			continue
		}
		if !ok || r.PublicKey != rpk {
			c.log.Println("WARN: contract ID submitted that doesn't belong to this renter:", id, renter.PublicKey)
			continue
		}

		cu, ok := c.managedContractUtility(id)

		// Create the renewSet. Depend on the PeriodSpending function to get
		// a breakdown of spending in the contractor. Then use that to
		// determine how many funds remain available in the allowance for
		// renewals.
		spending, err := c.PeriodSpending(renter.PublicKey)
		if err != nil {
			// This should only error if the contractor is shutting down.
			c.log.Println("WARN: error getting period spending:", err)
			return nil, err
		}

		// Check for an underflow. This can happen if the user reduced their
		// allowance at some point to less than what we've already spent.
		fundsRemaining = renter.Allowance.Funds
		if spending.TotalAllocated.Cmp(fundsRemaining) < 0 {
			fundsRemaining = fundsRemaining.Sub(spending.TotalAllocated)
		}

		// Skip any host that does not match our whitelist/blacklist filter
		// settings.
		host, _, err := c.hdb.Host(rc.HostPublicKey)
		if err != nil {
			c.log.Println("WARN: error getting host", err)
			continue
		}
		if host.Filtered {
			c.log.Println("INFO: contract skipped because it is filtered")
			continue
		}

		// Skip hosts that can't use the current renter-host protocol.
		if build.VersionCmp(host.Settings.Version, minimumSupportedRenterHostProtocolVersion) < 0 {
			c.log.Println("INFO: contract skipped because host is using an outdated version", host.Settings.Version)
			continue
		}

		// Skip contracts which do not exist or are otherwise unworthy for
		// renewal.
		if renter.Settings.AutoRenewContracts && (!ok || !cu.GoodForRenew) {
			c.log.Println("INFO: contract skipped because it is not good for renew (utility.GoodForRenew, exists)", cu.GoodForRenew, ok)
			continue
		}

		// Calculate a spending for the contract that is proportional to how
		// much money was spend on the contract throughout this billing cycle
		// (which is now ending).
		renewAmount, err := c.managedEstimateRenewFundingRequirements(rc, blockHeight, renter.Allowance)
		if err != nil {
			c.log.Println("WARN: contract skipped because there was an error estimating renew funding requirements", renewAmount, err)
			continue
		}
		renewSet = append(renewSet, fileContractRenewal{
			contract:     rc,
			amount:       renewAmount,
			renterPubKey: rpk,
			secretKey:    rsk,
		})
		c.log.Println("INFO: contract has been added to the renew set")
	}
	if len(renewSet) != 0 {
		c.log.Printf("INFO: renewing %v contracts\n", len(renewSet))
	}

	// Go through the contracts we've assembled for renewal.
	for _, renewal := range renewSet {
		// Return here if an interrupt or kill signal has been sent.
		select {
		case <-c.tg.StopChan():
			c.log.Println("INFO: returning because the manager was stopped")
			return nil, errors.New("the manager was stopped")
		default:
		}

		unlocked, err := c.wallet.Unlocked()
		if !unlocked || err != nil {
			c.log.Println("ERROR: contractor is attempting to renew contracts that are about to expire, however the wallet is locked")
			return nil, err
		}

		// Skip this renewal if we don't have enough funds remaining.
		if renewal.amount.Cmp(fundsRemaining) > 0 {
			c.log.Println("WARN: skipping renewal because there are not enough funds remaining in the allowance", renewal.contract.ID, renewal.amount, fundsRemaining)
			registerLowFundsAlert = true
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
			c.log.Println("ERROR: error renewing contract", renewal.contract.ID, err)
			renewErr = modules.ComposeErrors(renewErr, err)
			numRenewFails++
		}
		fundsRemaining = fundsRemaining.Sub(fundsSpent)

		if err == nil {
			// Lock the funds in the database.
			funds := modules.Float64(fundsSpent)
			hastings := modules.Float64(types.HastingsPerSiacoin)
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
			contractSet = append(contractSet, newContract)
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

	// Update the failed renew map so that it only contains contracts which we
	// are currently trying to renew. The failed renew map is a map that we
	// use to track how many times consecutively we failed to renew a contract
	// with a host, so that we know if we need to abandon that host.
	c.mu.Lock()
	newFirstFailedRenew := make(map[types.FileContractID]uint64)
	for _, r := range renewSet {
		if _, exists := c.numFailedRenews[r.contract.ID]; exists {
			newFirstFailedRenew[r.contract.ID] = c.numFailedRenews[r.contract.ID]
		}
	}
	c.numFailedRenews = newFirstFailedRenew
	c.mu.Unlock()

	return contractSet, nil
}

// managedTrustlessRenewContract will try to renew a contract using the new
// Renter-Satellite protocol.
func (c *Contractor) managedTrustlessRenewContract(s *modules.RPCSession, rpk types.PublicKey, oldContract modules.RenterContract, contractFunding types.Currency, endHeight uint64) (fundsSpent types.Currency, newContract modules.RenterContract, err error) {
	c.mu.RLock()
	blockHeight := c.blockHeight

	// Check if we know this renter.
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.ZeroCurrency, newContract, ErrRenterNotFound
	}

	id := oldContract.ID
	allowance := renter.Allowance
	if reflect.DeepEqual(allowance, modules.Allowance{}) {
		return types.ZeroCurrency, modules.RenterContract{}, errors.New("called managedTrustlessRenewContract but allowance isn't set")
	}

	// Fetch the host.
	hostPubKey := oldContract.HostPublicKey
	host, exists, err := c.hdb.Host(hostPubKey)
	if err != nil {
		return types.ZeroCurrency, newContract, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return types.ZeroCurrency, newContract, errHostNotFound
	} else if host.Filtered {
		return types.ZeroCurrency, newContract, errHostBlocked
	}

	// Get the host settings and use the most recent hostSettings,
	// along with the hostDB entry.
	hostSettings, err := proto.HostSettings(string(host.Settings.NetAddress), hostPubKey)
	if err != nil {
		err = modules.AddContext(err, "unable to fetch host settings")
		return
	}
	host.Settings = hostSettings
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return types.ZeroCurrency, newContract, fmt.Errorf("failed to get host name: %v", err)
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Mark the contract as being renewed, and defer logic to unmark it
	// once renewing is complete.
	c.mu.Lock()
	c.renewing[id] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.renewing, id)
		c.mu.Unlock()
	}()

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), renewContractTimeout)
	defer cancelFunc()
	go func() {
		select {
		case <-c.tg.StopChan():
			cancelFunc()
		case <-ctx.Done():
		}
	}()

	// Increase Successful/Failed interactions accordingly.
	var hostFault bool
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(hostPubKey)
			if hostFault {
				err = fmt.Errorf("%v: %v", errHostFault, err)
			}
		} else {
			c.hdb.IncrementSuccessfulInteractions(hostPubKey)
		}
	}()

	// Perform the actual renewal. If the renewal succeeds, return the
	// contract. If the renewal fails we check how often it has failed
	// before. Once it has failed for a certain number of blocks in a
	// row and reached its second half of the renew window, we give up
	// on renewing it and set goodForRenew to false.
	var txnSet, sweepParents []types.Transaction
	var sweepTxn types.Transaction
	var contractPrice, minerFee, siafundFee types.Currency
	var toSign []types.Hash256
	var rev rhpv2.ContractRevision
	errRenew := proto.WithTransportV3(ctx, siamuxAddr, host.PublicKey, func(t *rhpv3.Transport) (err error) {
		// Fetch the price table.
		pt, err := proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) { return nil, nil })
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get price table")
		}
		host.PriceTable = pt

		// Check if the host is gouging.
		_, txnFee := c.tpool.FeeEstimation()
		if err := modules.CheckGouging(allowance, blockHeight, &hostSettings, &pt, txnFee); err != nil {
			hostFault = true
			return modules.AddContext(err, "host is gouging")
		}

		// Fetch the latest revision.
		oldRev, err := proto.RPCLatestRevision(ctx, t, id)
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get latest revision")
		}

		// Get an address to use for negotiation.
		uc, err := c.wallet.NextAddress()
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				err = modules.ComposeErrors(err, c.wallet.MarkAddressUnused(uc))
			}
		}()

		// Avoid panic in rhpv2.ContractRenewalCollateral.
		if oldRev.FileContract.EndHeight() > endHeight {
			endHeight = oldRev.FileContract.EndHeight()
		}

		// Prepare the file contract and the final revision.
		expectedStorage := fundsToExpectedStorage(contractFunding, endHeight-blockHeight, hostSettings)
		hostCollateral := rhpv2.ContractRenewalCollateral(oldRev.FileContract, expectedStorage, hostSettings, blockHeight, endHeight)
		contractPrice = host.PriceTable.ContractPrice
		var renterTxnSet []types.Transaction
		renterTxnSet, sweepTxn, sweepParents, fundsSpent, minerFee, siafundFee, toSign, err = c.prepareContractRenewal(host, oldRev, contractFunding, hostCollateral, endHeight, uc.UnlockHash())
		if err != nil {
			return err
		}

		// Renew the contract.
		rev, txnSet, err = proto.RPCTrustlessRenewContract(ctx, s, t, renterTxnSet, toSign, c.wallet)
		if err != nil {
			if !modules.ContainsError(err, errors.New("failed to sign transaction")) && !modules.ContainsError(err, errors.New("invalid renter signature")) {
				hostFault = true
			}
			c.wallet.ReleaseInputs(renterTxnSet)
			return modules.AddContext(err, "couldn't renew contract")
		}

		// Submit to blockchain.
		err = c.tpool.AcceptTransactionSet(txnSet)
		if modules.ContainsError(err, errDuplicateTransactionSet) {
			// As long as it made it into the transaction pool, we're good.
			err = nil
		}
		if err != nil {
			c.wallet.ReleaseInputs(txnSet)
			return modules.AddContext(err, "couldn't broadcast transaction set")
		}

		return nil
	})

	oldFC, exists := c.staticContracts.Acquire(id)
	if !exists {
		return fundsSpent, modules.RenterContract{}, modules.AddContext(errContractNotFound, "failed to acquire oldContract after renewal")
	}

	if errRenew == nil {
		// Add contract to the set.
		revisionTxn := types.Transaction{
			FileContractRevisions: []types.FileContractRevision{rev.Revision},
			Signatures:            []types.TransactionSignature{rev.Signatures[0], rev.Signatures[1]},
		}
		newContract, err = c.staticContracts.InsertContract(revisionTxn, blockHeight, fundsSpent, contractPrice, minerFee, siafundFee, rpk, false)
		if err != nil {
			c.staticContracts.Return(oldFC)
			return fundsSpent, modules.RenterContract{}, modules.AddContext(err, "couldn't add the new contract to the contract set")
		}

		// Commit changes to old contract.
		if err := oldFC.Clear(txnSet[len(txnSet)-1]); err != nil {
			c.staticContracts.Return(oldFC)
			return fundsSpent, modules.RenterContract{}, modules.AddContext(err, "couldn't clear the old contract")
		}

		// Inform watchdog about the new contract.
		monitorContractArgs := monitorContractArgs{
			newContract.ID,
			revisionTxn,
			txnSet,
			sweepTxn,
			sweepParents,
			blockHeight,
		}
		err = c.staticWatchdog.callMonitorContract(monitorContractArgs)
		if err != nil {
			c.staticContracts.Return(oldFC)
			return fundsSpent, modules.RenterContract{}, err
		}

		// Add a mapping from the contract's id to the public keys of the renter
		// and the host. This will destroy the previous mapping from pubKey to
		// contract id but other modules are only interested in the most recent
		// contract anyway.
		c.mu.Lock()
		c.pubKeysToContractID[newContract.RenterPublicKey.String()+newContract.HostPublicKey.String()] = newContract.ID
		c.mu.Unlock()

		// Update the hostdb to include the new contract.
		err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
		if err != nil {
			c.log.Println("ERROR: unable to update hostdb contracts:", err)
		}
	}

	oldUtility := oldFC.Utility()
	if errRenew != nil {
		// Increment the number of failed renewals for the contract if it
		// was the host's fault.
		if hostFault {
			c.mu.Lock()
			c.numFailedRenews[id]++
			totalFailures := c.numFailedRenews[id]
			c.mu.Unlock()
			c.log.Println("INFO: remote host determined to be at fault, tallying up failed renews", totalFailures, id)
		}

		// Check if contract has to be replaced.
		md := oldFC.Metadata()
		c.mu.RLock()
		numRenews, failedBefore := c.numFailedRenews[md.ID]
		c.mu.RUnlock()
		secondHalfOfWindow := blockHeight+allowance.RenewWindow/2 >= md.EndHeight
		replace := numRenews >= consecutiveRenewalsBeforeReplacement
		if failedBefore && secondHalfOfWindow && replace {
			oldUtility.GoodForRenew = false
			oldUtility.GoodForUpload = false
			oldUtility.Locked = true
			err := c.managedUpdateContractUtility(oldFC, oldUtility)
			if err != nil {
				c.log.Println("WARN: failed to mark contract as !goodForRenew:", err)
			}

			c.log.Printf("WARN: consistently failed to renew %v, marked as bad and locked: %v\n",
				hostPubKey, errRenew)
			c.staticContracts.Return(oldFC)

			return types.ZeroCurrency, newContract, modules.AddContext(errRenew, "contract marked as bad for too many consecutive failed renew attempts")
		}

		// Seems like it doesn't have to be replaced yet. Log the
		// failure and number of renews that have failed so far.
		c.log.Printf("WARN: failed to renew contract %v [%v]: '%v', current height: %v, proposed end height: %v", hostPubKey, numRenews, errRenew, blockHeight, endHeight)
		c.staticContracts.Return(oldFC)

		return types.ZeroCurrency, newContract, modules.AddContext(errRenew, "contract renewal with host was unsuccessful")
	}
	c.log.Printf("INFO: renewed contract %v\n", id)

	// Update the utility values for the new contract, and for the old
	// contract.
	newUtility := modules.ContractUtility{
		GoodForUpload: true,
		GoodForRenew:  true,
	}
	if err := c.managedAcquireAndUpdateContractUtility(newContract.ID, newUtility); err != nil {
		c.log.Println("ERROR: failed to update the contract utilities", err)
		c.staticContracts.Return(oldFC)
		return fundsSpent, newContract, nil
	}

	oldUtility.GoodForRenew = false
	oldUtility.GoodForUpload = false
	oldUtility.Locked = true
	if err := c.managedUpdateContractUtility(oldFC, oldUtility); err != nil {
		c.log.Println("ERROR: failed to update the contract utilities", err)
		c.staticContracts.Return(oldFC)
		return fundsSpent, newContract, nil
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
		c.log.Println("ERROR: failed to update contracts in the database.")
	}

	// Delete the old contract.
	c.staticContracts.Delete(oldFC)

	// Signal to the watchdog that it should immediately post the last
	// revision for this contract.
	go c.staticWatchdog.threadedSendMostRecentRevision(oldFC.Metadata())

	return fundsSpent, newContract, nil
}

// RenewContract tries to renew the given contract using RSP2.
func (c *Contractor) RenewContract(s *modules.RPCSession, rpk types.PublicKey, contract modules.RenterContract, funding types.Currency, endHeight uint64) (modules.RenterContract, error) {
	// No contract renewal until the contractor is synced.
	if !c.managedSynced() {
		return modules.RenterContract{}, errors.New("contractor isn't synced yet")
	}

	// Find the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
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
	if r.PublicKey != rpk {
		c.log.Println("WARN: contract ID submitted that doesn't belong to this renter:", contract.ID, renter.PublicKey)
		return modules.RenterContract{}, errors.New("contract doesn't belong to this renter")
	}

	// Check if the wallet is unlocked.
	unlocked, err := c.wallet.Unlocked()
	if !unlocked || err != nil {
		c.log.Println("ERROR: contractor is attempting to renew a contract but the wallet is locked")
		return modules.RenterContract{}, err
	}

	// Renew the contract.
	fundsSpent, newContract, err := c.managedTrustlessRenewContract(s, rpk, contract, funding, endHeight)
	if err != nil {
		c.log.Printf("WARN: attempted to renew a contract with %v, but renewal failed: %v\n", contract.HostPublicKey, err)
		return modules.RenterContract{}, err
	}

	// Lock the funds in the database.
	funds := modules.Float64(fundsSpent)
	hastings := modules.Float64(types.HastingsPerSiacoin)
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
		return modules.RenterContract{}, err
	}
	c.mu.Lock()
	err = c.save()
	c.mu.Unlock()
	if err != nil {
		c.log.Println("ERROR: unable to save the contractor:", err)
	}

	return newContract, nil
}
