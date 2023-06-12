package contractor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
	siad "go.sia.tech/siad/types"
)

// fundsToExpectedStorage returns how much storage a renter is expected to be
// able to afford given the provided funds.
func fundsToExpectedStorage(funds types.Currency, duration uint64, host modules.HostDBEntry) uint64 {
	costPerByte := host.UploadBandwidthPrice.Add(host.StoragePrice.Mul64(duration)).Add(host.DownloadBandwidthPrice)
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

// managedNewContract negotiates an initial file contract with the specified
// host, saves it, and returns it.
func (c *Contractor) managedNewContract(rpk types.PublicKey, rsk types.PrivateKey, host modules.HostDBEntry, contractFunding types.Currency, endHeight uint64) (_ types.Currency, _ modules.RenterContract, err error) {
	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk.String()]
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
	ctx, cancel := context.WithTimeout(context.Background(), contractFormTimeout)
	defer cancel()

	// Get an address to use for negotiation.
	uc, err := c.wallet.NextAddress()
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}
	defer func() {
		if err != nil {
			err = modules.ComposeErrors(err, c.wallet.MarkAddressUnused(uc))
		}
	}()

	// Create transaction builder and trigger contract formation.
	txnBuilder, err := c.wallet.StartTransaction()
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(host.PublicKey)
			err = fmt.Errorf("%s: %s", errHostFault, err)
		} else {
			c.hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Initiate the protocol.
	var formationTxnSet, sweepParents []types.Transaction
	var sweepTxn types.Transaction
	var totalCost, contractPrice, minerFee, tax types.Currency
	err = proto.WithTransportV2(ctx, host.NetAddress, host.PublicKey, fn func(*rhpv2.Transport) error {
		// Get the host's settings.
		hostSettings, err := proto.RPCSettings(ctx, t)
		if err != nil {
			return fmt.Errorf("couldn't fetch host settings: %s", err)
		}
		// NOTE: we overwrite the NetAddress with the host address here since we
		// just used it to dial the host we know it's valid.
		hostSettings.NetAddress = host.NetAddress

		// Check if the host is gouging.
		if err := modules.CheckGouging(renter.Allowance, blockHeight, hostSettings, nil, txnFee); err != nil {
			return fmt.Errorf("host is gouging: %s", err)
		}

		// Derive ephemeral key.
		esk := modules.DeriveEphemeralKey(rsk, host.PublicKey)
		epk := esk.PublicKey()

		// Prepare and add file contract.
		expectedStorage := fundsToExpectedStorage(contractFunding, endHeight - blockHeight, hostSettings)
		hostCollateral := rhpv2.ContractFormationCollateral(renter.Allowance.Period, expectedStorage, hostSettings)
		contractPrice = hostSettings.ContractPrice
		fc := rhpv2.PrepareContractFormation(epk, host.PublicKey, contractFunding, hostCollateral, endHeight, hostSettings, types.Address(uc.UnlockHash()))
		txnBuilder.AddFileContract(modules.ConvertToSiad(fc))

		// Calculate and add mining fee.
		_, maxFee := c.tpool.FeeEstimation()
		txnFee := maxFee.Mul64(modules.EstimatedFileContractTransactionSetSize)
		minerFee = modules.ConvertCurrency(txnFee)
		txnBuilder.AddMinerFee(txnFee)

		// Fund the transaction.
		tax = modules.Tax(fc.Payout)
		cost := fc.ValidRenterPayout().Add(hostSettings.ContractPrice).Add(tax)
		funding := siad.NewCurrency(cost.Big()).Add(txnFee)
		totalCost = modules.ConvertCurrency(funding)
		err = txnBuilder.FundSiacoins(funding)
		if err != nil {
			return fmt.Errorf("couldn't fund transaction: %s", err)
		}

		// Make a copy of the transaction builder so far, to be used to by the watchdog
		// to double spend these inputs in case the contract never appears on chain.
		sweepBuilder := txnBuilder.Copy()

		// Add an output that sends all funds back to the refundAddress.
		// Note that in order to send this transaction, a miner fee will have to be subtracted.
		output := siad.SiacoinOutput{
			Value:      funding,
			UnlockHash: uc.UnlockHash(),
		}
		sweepBuilder.AddSiacoinOutput(output)
		st, sp := sweepBuilder.View()
		sweepTxn = modules.ConvertToCore(st)
		sweepParents = modules.ConvertToCore(sp)

		// Sign the transaction.
		sts, err := txnBuilder.Sign(true)
		if err != nil {
			txnBuilder.Drop()
			return fmt.Errorf("couldn't sign transaction: %s", err)
		}
		signedTxnSet := modules.ConvertToCore(sts)

		// Form the contract.
		_, formationTxnSet, err = proto.RPCFormContract(ctx, t, esk, signedTxnSet)
		if err != nil {
			txnBuilder.Drop()
			return fmt.Errorf("couldn't form contract: %s", err)
		}

		return nil
	}
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Submit to blockchain.
	err = c.tpool.AcceptTransactionSet(modules.ConvertToSiad(formationTxnSet))
	if strings.Contains(err.Error(), errDuplicateTransactionSet.Error()) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Add contract to the set.
	contract, err := c.staticContracts.InsertContract(formationTxnSet[len(formationTxnSet) - 1], blockHeight, totalCost, contractPrice, minerFee, tax, rpk)

	// Inform watchdog about the new contract.
	monitorContractArgs := monitorContractArgs{
		false,
		contract.ID,
		contract.Transaction,
		formationTxnSet,
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
		txnBuilder.Drop()
		// We need to return a funding value because money was spent on this
		// host, even though the full process could not be completed.
		c.log.Println("WARN: Attempted to form a new contract with a host that this renter already has a contract with.")
		return contractFunding, modules.RenterContract{}, fmt.Errorf("%v already has a contract with host %v", contract.RenterPublicKey.String(), contract.HostPublicKey.String())
	}
	c.pubKeysToContractID[contract.RenterPublicKey.String() + contract.HostPublicKey.String()] = contract.ID
	c.mu.Unlock()

	contractValue := contract.RenterFunds
	c.log.Printf("INFO: formed contract %v with %v for %v\n", contract.ID, host.NetAddress, contractValue)

	// Update the hostdb to include the new contract.
	err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
	if err != nil {
		c.log.Println("ERROR: unable to update hostdb contracts:", err)
	}
	return contractFunding, contract, nil
}

// managedTrustlessNewContract negotiates an initial file contract with
// the specified host using the RSP2 protocol, saves it, and returns it.
func (c *Contractor) managedTrustlessNewContract(s *modules.RPCSession, pk, rpk types.PublicKey, host modules.HostDBEntry, contractFunding types.Currency, endHeight uint64) (_ types.Currency, _ modules.RenterContract, err error) {
	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[pk.String()]
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
	ctx, cancel := context.WithTimeout(context.Background(), contractFormTimeout)
	defer cancel()

	// Get an address to use for negotiation.
	uc, err := c.wallet.NextAddress()
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}
	defer func() {
		if err != nil {
			err = modules.ComposeErrors(err, c.wallet.MarkAddressUnused(uc))
		}
	}()

	// Create transaction builder and trigger contract formation.
	txnBuilder, err := c.wallet.StartTransaction()
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(host.PublicKey)
			err = fmt.Errorf("%s: %s", errHostFault, err)
		} else {
			c.hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Initiate the protocol.
	var formationTxnSet, sweepParents []types.Transaction
	var sweepTxn types.Transaction
	var totalCost, contractPrice, minerFee, tax types.Currency
	err = proto.WithTransportV2(ctx, host.NetAddress, host.PublicKey, fn func(*rhpv2.Transport) error {
		// Get the host's settings.
		hostSettings, err := proto.RPCSettings(ctx, t)
		if err != nil {
			return fmt.Errorf("couldn't fetch host settings: %s", err)
		}
		// NOTE: we overwrite the NetAddress with the host address here since we
		// just used it to dial the host we know it's valid.
		hostSettings.NetAddress = host.NetAddress

		// Check if the host is gouging.
		if err := modules.CheckGouging(renter.Allowance, blockHeight, hostSettings, nil, txnFee); err != nil {
			return fmt.Errorf("host is gouging: %s", err)
		}

		// Prepare and add file contract.
		expectedStorage := fundsToExpectedStorage(contractFunding, endHeight - blockHeight, hostSettings)
		hostCollateral := rhpv2.ContractFormationCollateral(renter.Allowance.Period, expectedStorage, hostSettings)
		contractPrice = hostSettings.ContractPrice
		fc := rhpv2.PrepareContractFormation(rpk, host.PublicKey, contractFunding, hostCollateral, endHeight, hostSettings, types.Address(uc.UnlockHash()))
		txnBuilder.AddFileContract(modules.ConvertToSiad(fc))

		// Calculate and add mining fee.
		_, maxFee := c.tpool.FeeEstimation()
		txnFee := maxFee.Mul64(modules.EstimatedFileContractTransactionSetSize)
		minerFee = modules.ConvertCurrency(txnFee)
		txnBuilder.AddMinerFee(txnFee)

		// Fund the transaction.
		tax = modules.Tax(fc.Payout)
		cost := fc.ValidRenterPayout().Add(hostSettings.ContractPrice).Add(tax)
		funding := siad.NewCurrency(cost.Big()).Add(txnFee)
		totalCost = modules.ConvertCurrency(funding)
		err = txnBuilder.FundSiacoins(funding)
		if err != nil {
			return fmt.Errorf("couldn't fund transaction: %s", err)
		}

		// Make a copy of the transaction builder so far, to be used to by the watchdog
		// to double spend these inputs in case the contract never appears on chain.
		sweepBuilder := txnBuilder.Copy()

		// Add an output that sends all funds back to the refundAddress.
		// Note that in order to send this transaction, a miner fee will have to be subtracted.
		output := siad.SiacoinOutput{
			Value:      funding,
			UnlockHash: uc.UnlockHash(),
		}
		sweepBuilder.AddSiacoinOutput(output)
		st, sp := sweepBuilder.View()
		sweepTxn = modules.ConvertToCore(st)
		sweepParents = modules.ConvertToCore(sp)

		// Sign the transaction.
		sts, err := txnBuilder.Sign(true)
		if err != nil {
			txnBuilder.Drop()
			return fmt.Errorf("couldn't sign transaction: %s", err)
		}
		signedTxnSet := modules.ConvertToCore(sts)

		// Form the contract.
		_, formationTxnSet, err = proto.RPCTrustlessFormContract(ctx, t, s, rpk, signedTxnSet)
		if err != nil {
			txnBuilder.Drop()
			return fmt.Errorf("couldn't form contract: %s", err)
		}

		return nil
	}
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Submit to blockchain.
	err = c.tpool.AcceptTransactionSet(modules.ConvertToSiad(formationTxnSet))
	if strings.Contains(err.Error(), errDuplicateTransactionSet.Error()) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return types.ZeroCurrency, modules.RenterContract{}, err
	}

	// Add contract to the set.
	contract, err := c.staticContracts.InsertContract(formationTxnSet[len(formationTxnSet) - 1], blockHeight, totalCost, contractPrice, minerFee, tax, pk)

	// Inform watchdog about the new contract.
	monitorContractArgs := monitorContractArgs{
		false,
		contract.ID,
		contract.Transaction,
		formationTxnSet,
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
		txnBuilder.Drop()
		// We need to return a funding value because money was spent on this
		// host, even though the full process could not be completed.
		c.log.Println("WARN: Attempted to form a new contract with a host that this renter already has a contract with.")
		return contractFunding, modules.RenterContract{}, fmt.Errorf("%v already has a contract with host %v", contract.RenterPublicKey.String(), contract.HostPublicKey.String())
	}
	c.pubKeysToContractID[contract.RenterPublicKey.String() + contract.HostPublicKey.String()] = contract.ID
	c.mu.Unlock()

	contractValue := contract.RenterFunds
	c.log.Printf("INFO: formed contract %v with %v for %v\n", contract.ID, host.NetAddress, contractValue)

	// Update the hostdb to include the new contract.
	err = c.hdb.UpdateContracts(c.staticContracts.ViewAll())
	if err != nil {
		c.log.Println("ERROR: unable to update hostdb contracts:", err)
	}
	return contractFunding, contract, nil
}
