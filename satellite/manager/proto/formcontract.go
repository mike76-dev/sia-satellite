package proto

import (
	"context"
	"fmt"
	"math"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/siad/build"
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
	"go.sia.tech/siad/types/typesutil"
)

// FormContract forms a contract with a host and submits the contract
// transaction to tpool. The contract is added to the ContractSet and its
// metadata is returned.
func (cs *ContractSet) FormContract(params modules.ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, formationTxnSet []types.Transaction, sweepTxn types.Transaction, sweepParents []types.Transaction, err error) {
	// Check that the host version is high enough. This should never happen
	// because hosts with old versions should be filtered / blocked by the
	// contractor anyway.
	if build.VersionCmp(params.Host.Version, smodules.MinimumSupportedRenterHostProtocolVersion) < 0 {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, ErrBadHostVersion
	}

	// Extract vars from params, for convenience.
	allowance, host, funding, startHeight, endHeight, refundAddress := params.Allowance, params.Host, params.Funding, params.StartHeight, params.EndHeight, params.RefundAddress

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), contractHostFormTimeout)
	go func() {
		select {
		case <-cancel:
			cancelFunc()
		case <-ctx.Done():
		}
	}()

	// Calculate the anticipated transaction fee.
	_, maxFee := tpool.FeeEstimation()
	txnFee := maxFee.Mul64(smodules.EstimatedFileContractTransactionSetSize).MulFloat(3)

	// Calculate the payouts.
	period := endHeight - startHeight
	expectedStorage := fundsToExpectedStorage(funding, period, host)
	renterPayout, hostPayout, _, err := smodules.RenterPayoutsPreTax(host, funding, txnFee, types.ZeroCurrency, types.ZeroCurrency, period, expectedStorage)
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	payout := renterPayout.Add(hostPayout)

	// Check for negative currency.
	if types.PostTax(startHeight, payout).Cmp(hostPayout) < 0 {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, errors.New("not enough money to pay both siafund fee and also host payout")
	}

	// Fund the transaction.
	err = txnBuilder.FundSiacoins(funding)
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Make a copy of the transaction builder so far, to be used to by the watchdog
	// to double spend these inputs in case the contract never appears on chain.
	sweepBuilder := txnBuilder.Copy()
	// Add an output that sends all funds back to the refundAddress.
	// Note that in order to send this transaction, a miner fee will have to be subtracted.
	output := types.SiacoinOutput{
		Value:      funding,
		UnlockHash: refundAddress,
	}
	sweepBuilder.AddSiacoinOutput(output)
	sweepTxn, sweepParents = sweepBuilder.View()

	// Create our keys.
	renterSK := params.SecretKey
	renterPK := renterSK.PublicKey()

	// Create unlock conditions.
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(renterPK),
			host.PublicKey,
		},
		SignaturesRequired: 2,
	}

	// Create file contract.
	renterPostTaxPayout := types.PostTax(startHeight, payout).Sub(hostPayout)
	fc := types.FileContract{
		FileSize:       0,
		FileMerkleRoot: crypto.Hash{}, // No proof possible without data.
		WindowStart:    endHeight,
		WindowEnd:      endHeight + host.WindowSize,
		Payout:         payout,
		UnlockHash:     uc.UnlockHash(),
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// Outputs need to account for tax.
			{Value: renterPostTaxPayout, UnlockHash: refundAddress},
			// Collateral is returned to host.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
		},
		MissedProofOutputs: []types.SiacoinOutput{
			// Same as above.
			{Value: renterPostTaxPayout, UnlockHash: refundAddress},
			// Same as above.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
			// Once we start doing revisions, we'll move some coins to the host and some to the void.
			{Value: types.ZeroCurrency, UnlockHash: types.UnlockHash{}},
		},
	}

	// Add file contract.
	txnBuilder.AddFileContract(fc)
	// Add miner fee.
	txnBuilder.AddMinerFee(txnFee)

	// Create initial transaction set.
	txn, parentTxns := txnBuilder.View()
	unconfirmedParents, err := txnBuilder.UnconfirmedParents()
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	txnSet := append(unconfirmedParents, append(parentTxns, txn)...)
	txnSet = typesutil.MinimumTransactionSet([]types.Transaction{txn}, txnSet)

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			hdb.IncrementFailedInteractions(host.PublicKey)
			err = errors.Extend(err, smodules.ErrHostFault)
		} else {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Send the FormContract request.
	var revisionTxn types.Transaction
	err = WithTransportV2(ctx, string(host.NetAddress), host.PublicKey, func(t *rhpv2.Transport) (err error) {
		// Fetch the host's settings.
		hes, err := RPCSettings(ctx, t)
		if err != nil {
			cs.log.Printf("ERROR: couldn't fetch host settings from %s: %v\n", host.NetAddress, err)
			return
		}

		// Check if the host is gouging.
		err = modules.CheckGouging(allowance, startHeight, &hes, nil, txnFee)
		if err != nil {
			cs.log.Printf("WARN: gouging detected at %s: %v\n", host.NetAddress, err)
			return
		}

		// Form a contract.
		revisionTxn, err = RPCFormContract(ctx, t, txnBuilder, renterSK, txnSet, startHeight)
		return
	})
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, fmt.Errorf("couldn't form contract: %s", err)
	}

	// Construct the final transaction, and then grab the minimum necessary
	// final set to submit to the transaction pool. Minimizing the set will
	// greatly improve the chances of the transaction propagating through an
	// actively attacked network.
	txn, parentTxns = txnBuilder.View()
	minSet := typesutil.MinimumTransactionSet([]types.Transaction{txn}, parentTxns)

	// Submit to blockchain.
	err = tpool.AcceptTransactionSet(minSet)
	if errors.Contains(err, smodules.ErrDuplicateTransactionSet) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Construct contract header.
	header := contractHeader{
		Transaction: revisionTxn,
		SecretKey:   renterSK,
		StartHeight: startHeight,
		TotalCost:   funding,
		ContractFee: host.ContractPrice,
		TxnFee:      txnFee,
		SiafundFee:  types.Tax(startHeight, fc.Payout),
		Utility: smodules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
	}

	// Add contract to set.
	meta, err := cs.managedInsertContract(header, params.PublicKey) // No Merkle roots.
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	return meta, minSet, sweepTxn, sweepParents, nil
}

// fundsToExpectedStorage returns how much storage a renter is expected to be
// able to afford given the provided funds.
func fundsToExpectedStorage(funds types.Currency, duration types.BlockHeight, host smodules.HostDBEntry) uint64 {
	costPerByte := host.UploadBandwidthPrice.Add(host.StoragePrice.Mul64(uint64(duration))).Add(host.DownloadBandwidthPrice)
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
