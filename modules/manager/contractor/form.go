package contractor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
)

// fundsToExpectedStorage returns how much storage a renter is expected to be
// able to afford given the provided funds.
func fundsToExpectedStorage(funds types.Currency, duration uint64, hostSettings rhpv2.HostSettings) uint64 {
	costPerByte := hostSettings.UploadBandwidthPrice
	costPerByte = costPerByte.Add(hostSettings.StoragePrice.Mul64(duration))
	costPerByte = costPerByte.Add(hostSettings.DownloadBandwidthPrice)

	// If storage is free, we can afford 'unlimited' data.
	if costPerByte.IsZero() {
		return math.MaxUint64
	}

	// Catch overflow.
	expectedStorage := funds.Div(costPerByte)
	if expectedStorage.Cmp(types.NewCurrency64(math.MaxUint64)) > 0 {
		return math.MaxUint64
	}
	return expectedStorage.Big().Uint64()
}

// prepareContractFormation creates a new contract and a formation
// transaction set.
func (c *Contractor) prepareContractFormation(rpk types.PublicKey, host modules.HostDBEntry, contractFunding, hostCollateral types.Currency, endHeight uint64, address types.Address) ([]types.Transaction, types.Transaction, []types.Transaction, types.Currency, types.Currency, types.Currency, error) {
	c.mu.RLock()
	blockHeight := c.blockHeight
	c.mu.RUnlock()

	// Prepare contract and transaction.
	fc := rhpv2.PrepareContractFormation(rpk, host.PublicKey, contractFunding, hostCollateral, endHeight, host.Settings, address)
	cost := fc.ValidRenterPayout().Add(host.Settings.ContractPrice)
	tax := modules.Tax(blockHeight, fc.Payout)
	txn := types.Transaction{
		FileContracts: []types.FileContract{fc},
	}
	_, txnFee := c.tpool.FeeEstimation()
	minerFee := txnFee.Mul64(uint64(types.EncodedLen(txn)))
	txn.MinerFees = []types.Currency{minerFee}
	totalCost := cost.Add(minerFee).Add(tax)
	parentTxn, err := c.wallet.FundTransaction(&txn, totalCost)
	if err != nil {
		c.wallet.ReleaseInputs(txn)
		return nil, types.Transaction{}, nil, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, modules.AddContext(err, "unable to fund transaction")
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

	// Sign the transaction.
	cf := modules.ExplicitCoveredFields(txn)
	err = c.wallet.SignTransaction(&txn, nil, cf)
	if err != nil {
		c.wallet.ReleaseInputs(txn)
		return nil, types.Transaction{}, nil, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, modules.AddContext(err, "unable to sign transaction")
	}

	return append([]types.Transaction{parentTxn}, txn), sweepTxn, sweepParents, totalCost, minerFee, tax, nil
}

// managedNewContract negotiates an initial file contract with the specified
// host, saves it, and returns it.
func (c *Contractor) managedNewContract(rpk types.PublicKey, rsk types.PrivateKey, host modules.HostDBEntry, contractFunding types.Currency, endHeight uint64) (_ types.Currency, _ modules.RenterContract, err error) {
	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	blockHeight := c.blockHeight
	c.mu.RUnlock()
	if !exists {
		return types.ZeroCurrency, modules.RenterContract{}, ErrRenterNotFound
	}

	// Check if the allowance is set.
	if reflect.DeepEqual(renter.Allowance, modules.Allowance{}) {
		return types.ZeroCurrency, modules.RenterContract{}, errors.New("called managedNewContract but allowance wasn't set")
	}

	// Create a context and set up its cancelling.
	ctx, cancel := context.WithTimeout(context.Background(), formContractTimeout)
	defer cancel()

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(host.PublicKey)
			err = fmt.Errorf("%v: %v", errHostFault, err)
		} else {
			c.hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Initiate the protocol.
	var txnSet, sweepParents []types.Transaction
	var sweepTxn types.Transaction
	var totalCost, contractPrice, minerFee, siafundFee types.Currency
	err = proto.WithTransportV2(ctx, host.Settings.NetAddress, host.PublicKey, func(t *rhpv2.Transport) error {
		// Get the host's settings.
		hostSettings, err := proto.RPCSettings(ctx, t)
		if err != nil {
			return modules.AddContext(err, "couldn't fetch host settings")
		}

		// NOTE: we overwrite the NetAddress with the host address here since we
		// just used it to dial the host we know it's valid.
		hostSettings.NetAddress = host.Settings.NetAddress

		// Check if the host is gouging.
		_, txnFee := c.tpool.FeeEstimation()
		if err := modules.CheckGouging(renter.Allowance, blockHeight, &hostSettings, nil, txnFee); err != nil {
			return modules.AddContext(err, "host is gouging")
		}

		// Derive ephemeral key.
		esk := modules.DeriveEphemeralKey(rsk, host.PublicKey)
		epk := esk.PublicKey()

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

		// Prepare and add file contract.
		expectedStorage := fundsToExpectedStorage(contractFunding, endHeight - blockHeight, hostSettings)
		hostCollateral := rhpv2.ContractFormationCollateral(renter.Allowance.Period, expectedStorage, hostSettings)
		contractPrice = host.Settings.ContractPrice
		var renterTxnSet []types.Transaction
		renterTxnSet, sweepTxn, sweepParents, totalCost, minerFee, siafundFee, err = c.prepareContractFormation(epk, host, contractFunding, hostCollateral, endHeight, uc.UnlockHash())
		if err != nil {
			return err
		}

		// Form the contract.
		_, txnSet, err = proto.RPCFormContract(ctx, t, esk, renterTxnSet)
		if err != nil {
			c.wallet.ReleaseInputs(renterTxnSet[len(renterTxnSet) - 1])
			return modules.AddContext(err, "couldn't form contract")
		}

		return nil
	})
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Submit to blockchain.
	err = c.tpool.AcceptTransactionSet(txnSet)
	if modules.ContainsError(err, errDuplicateTransactionSet) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Add contract to the set.
	contract, err := c.staticContracts.InsertContract(txnSet[len(txnSet) - 1], blockHeight, totalCost, contractPrice, minerFee, siafundFee, rpk)

	// Inform watchdog about the new contract.
	monitorContractArgs := monitorContractArgs{
		contract.ID,
		contract.Transaction,
		txnSet,
		sweepTxn,
		sweepParents,
		blockHeight,
	}
	err = c.staticWatchdog.callMonitorContract(monitorContractArgs)
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Add a mapping from the contract's id to the public keys of the host
	// and the renter.
	c.mu.Lock()
	_, exists = c.pubKeysToContractID[contract.RenterPublicKey.String() + contract.HostPublicKey.String()]
	if exists {
		c.mu.Unlock()
		c.wallet.ReleaseInputs(txnSet[len(txnSet) - 1])
		// We need to return a funding value because money was spent on this
		// host, even though the full process could not be completed.
		c.log.Println("WARN: attempted to form a new contract with a host that this renter already has a contract with.")
		return contractFunding, modules.RenterContract{}, fmt.Errorf("%v already has a contract with host %v", contract.RenterPublicKey, contract.HostPublicKey)
	}
	c.pubKeysToContractID[contract.RenterPublicKey.String() + contract.HostPublicKey.String()] = contract.ID
	c.mu.Unlock()

	c.log.Printf("INFO: formed contract %v with %v for %v\n", contract.ID, host.Settings.NetAddress, contract.RenterFunds)

	// Update the hostdb to include the new contract.
	err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
	if err != nil {
		c.log.Println("ERROR: unable to update hostdb contracts:", err)
	}
	return contractFunding, contract, nil
}

// FormContracts forms contracts according to the renter's allowance,
// puts them in the contract set, and returns them.
func (c *Contractor) FormContracts(rpk types.PublicKey, rsk types.PrivateKey) ([]modules.RenterContract, error) {
	// No contract formation until the contractor is synced.
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

	// Register or unregister and alerts related to contract formation.
	var registerLowFundsAlert bool
	defer func() {
		if registerLowFundsAlert {
			c.staticAlerter.RegisterAlert(modules.AlertIDRenterAllowanceLowFunds, AlertMSGAllowanceLowFunds, AlertCauseInsufficientAllowanceFunds, modules.SeverityWarning)
		} else {
			c.staticAlerter.UnregisterAlert(modules.AlertIDRenterAllowanceLowFunds)
		}
	}()

	// Check if the renter has enough contracts according to their allowance.
	fundsRemaining := renter.Allowance.Funds
	numHosts := renter.Allowance.Hosts
	if numHosts == 0 {
		return nil, errors.New("zero number of hosts specified")
	}
	endHeight := blockHeight + renter.Allowance.Period + renter.Allowance.RenewWindow

	// Create the contract set.
	neededContracts := int(renter.Allowance.Hosts)
	contractSet := make([]modules.RenterContract, 0, neededContracts)

	// Assemble two exclusion lists. The first one includes all hosts that we
	// already have contracts with and the second one includes all hosts we
	// have active contracts with. Then select a new batch of hosts to attempt
	// contract formation with.
	allContracts := c.staticContracts.ByRenter(rpk)
	var blacklist []types.PublicKey
	var addressBlacklist []types.PublicKey
	for _, contract := range allContracts {
		blacklist = append(blacklist, contract.HostPublicKey)
		if !contract.Utility.Locked || contract.Utility.GoodForRenew || contract.Utility.GoodForUpload {
			addressBlacklist = append(addressBlacklist, contract.HostPublicKey)
		}
	}

	// Determine the max and min initial contract funding based on the
	// allowance settings.
	maxInitialContractFunds := renter.Allowance.Funds.Div64(renter.Allowance.Hosts).Mul64(MaxInitialContractFundingMulFactor).Div64(MaxInitialContractFundingDivFactor)
	minInitialContractFunds := renter.Allowance.Funds.Div64(renter.Allowance.Hosts).Div64(MinInitialContractFundingDivFactor)

	// Get Hosts.
	hosts, err := c.hdb.RandomHostsWithAllowance(neededContracts * 4 + randomHostsBufferForScore, blacklist, addressBlacklist, renter.Allowance)
	if err != nil {
		return nil, err
	}

	// Calculate the anticipated transaction fee.
	_, maxFee := c.tpool.FeeEstimation()
	txnFee := maxFee.Mul64(modules.EstimatedFileContractTransactionSetSize)

	// Form contracts with the hosts one at a time, until we have enough contracts.
	for _, host := range hosts {
		// Return here if an interrupt or kill signal has been sent.
		select {
		case <-c.tg.StopChan():
			return nil, errors.New("the contractor was stopped")
		default:
		}

		// If no more contracts are needed, break.
		if neededContracts <= 0 {
			break
		}

		// Fetch the price table.
		pt, err := proto.FetchPriceTable(host)
		if err != nil {
			c.log.Printf("WARN: unable to fetch price table from %s: %v", host.Settings.NetAddress, err)
			continue
		}

		// Check if the host is gouging.
		err = modules.CheckGouging(renter.Allowance, blockHeight, nil, &pt, txnFee)
		if err != nil {
			c.log.Printf("WARN: gouging detected at %s: %v\n", host.Settings.NetAddress, err)
			continue
		}

		// Calculate the contract funding with the host.
		contractFunds := host.Settings.ContractPrice.Add(txnFee).Mul64(ContractFeeFundingMulFactor)

		// Check that the contract funding is reasonable compared to the max and
		// min initial funding. This is to protect against increases to
		// allowances being used up to fast and not being able to spread the
		// funds across new contracts properly, as well as protecting against
		// contracts renewing too quickly.
		if contractFunds.Cmp(maxInitialContractFunds) > 0 {
			contractFunds = maxInitialContractFunds
		}
		if contractFunds.Cmp(minInitialContractFunds) < 0 {
			contractFunds = minInitialContractFunds
		}

		// Confirm that the wallet is unlocked.
		unlocked, err := c.wallet.Unlocked()
		if !unlocked || err != nil {
			return nil, errors.New("the wallet is locked")
		}

		// Determine if we have enough money to form a new contract.
		if fundsRemaining.Cmp(contractFunds) < 0 {
			registerLowFundsAlert = true
			c.log.Println("WARN: need to form new contracts, but unable to because of a low allowance")
			break
		}

		// Attempt forming a contract with this host.
		start := time.Now()
		fundsSpent, newContract, err := c.managedNewContract(rpk, rsk, host, contractFunds, endHeight)
		if err != nil {
			c.log.Printf("WARN: attempted to form a contract with %v, time spent %v, but negotiation failed: %v\n", host.Settings.NetAddress, time.Since(start).Round(time.Millisecond), err)
			continue
		}
		fundsRemaining = fundsRemaining.Sub(fundsSpent)
		neededContracts--

		// Lock the funds in the database.
		funds := modules.Float64(fundsSpent)
		hastings := modules.Float64(types.HastingsPerSiacoin)
		amount := funds / hastings
		err = c.m.LockSiacoins(renter.Email, amount)
		if err != nil {
			c.log.Println("ERROR: couldn't lock funds:", err)
		}

		// Increment the number of formations in the database.
		err = c.m.IncrementStats(renter.Email, false)
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

	return contractSet, nil
}
