package proto

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
	"go.sia.tech/siad/types/typesutil"
)

// signRequest is used to send the revision hash to the renter.
type signRequest struct {
	RevisionHash crypto.Hash
}

// DecodeFrom implements modules.RequestBody.
func (sr *signRequest) DecodeFrom(d *core.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements modules.RequestBody.
func (sr *signRequest) EncodeTo(e *core.Encoder) {
	e.Write(sr.RevisionHash[:])
}

// signResponse is used to receive the revision signature from the renter.
type signResponse struct {
	Signature crypto.Signature
}

// DecodeFrom implements modules.RequestBody.
func (sr *signResponse) DecodeFrom(d *core.Decoder) {
	d.Read(sr.Signature[:])
}

// EncodeTo implements modules.RequestBody.
func (sr *signResponse) EncodeTo(e *core.Encoder) {
	// Nothing to do here.
}

// FormNewContract forms a contract with a host and submits the contract
// transaction to tpool. The contract is added to the ContractSet and its
// metadata is returned. The new Renter-Satellite protocol is used.
func (cs *ContractSet) FormNewContract(s *modules.RPCSession, pk, rpk types.SiaPublicKey, host smodules.HostDBEntry, startHeight, endHeight types.BlockHeight, funding types.Currency, refundAddress types.UnlockHash, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, formationTxnSet []types.Transaction, sweepTxn types.Transaction, sweepParents []types.Transaction, err error) {
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

	// Create unlock conditions.
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			rpk,
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
			err = fmt.Errorf("%s: %s", err, smodules.ErrHostFault)
		} else {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Send the FormContract request.
	var revisionTxn types.Transaction
	err = WithTransportV2(ctx, string(host.NetAddress), host.PublicKey, func(t *rhpv2.Transport) (err error) {
		revisionTxn, err = RPCFormNewContract(ctx, t, s, rpk, txnBuilder, txnSet, startHeight)
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
	if errors.Is(err, smodules.ErrDuplicateTransactionSet) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Construct contract header.
	header := contractHeader{
		Transaction: revisionTxn,
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
	meta, err := cs.managedInsertContract(header, pk) // No Merkle roots.
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	return meta, minSet, sweepTxn, sweepParents, nil
}

// RenewContract negotiates a new contract for data already stored with
// a host, and submits the new contract transaction to tpool. The new
// contract is added to the ContractSet and its metadata is returned.
// The new Renter-Satellite protocol is used.
func (cs *ContractSet) RenewContract(s *modules.RPCSession, oldFC *FileContract, pk, renterKey types.SiaPublicKey, host smodules.HostDBEntry, startHeight, endHeight types.BlockHeight, funding types.Currency, refundAddress types.UnlockHash, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, formationTxnSet []types.Transaction, err error) {
	// For convenience.
	oldContract := oldFC.header
	oldRev := oldContract.LastRevision()

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), contractHostRenewTimeout)
	go func() {
		select {
		case <-cancel:
			cancelFunc()
		case <-ctx.Done():
		}
	}()

	// Fetch the price table.
	rhp3pt, err := FetchPriceTable(host)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}
	pt := modules.ConvertPriceTable(rhp3pt)

	// RHP3 contains both the contract and final revision. So we double the
	// estimation.
	txnFee := pt.TxnFeeMaxRecommended.Mul64(2 * smodules.EstimatedFileContractTransactionSetSize)

	// Calculate the base cost. This includes the RPC cost.
	basePrice, baseCollateral := smodules.RenewBaseCosts(oldRev, &pt, endHeight)

	// Create the final revision of the old contract.
	renewCost := types.ZeroCurrency
	finalRev, err := prepareFinalRevision(oldContract, renewCost)
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("unable to create final revision: %s", err)
	}

	// Calculate the payouts for the renter, host, and whole contract.
	period := endHeight - startHeight
	expectedStorage := fundsToExpectedStorage(funding, period, host)
	renterPayout, hostPayout, hostCollateral, err := smodules.RenterPayoutsPreTax(host, funding, txnFee, basePrice, baseCollateral, period, expectedStorage)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}
	totalPayout := renterPayout.Add(hostPayout)

	// Check for negative currency.
	if hostCollateral.Cmp(baseCollateral) < 0 {
		baseCollateral = hostCollateral
	}
	if types.PostTax(startHeight, totalPayout).Cmp(hostPayout) < 0 {
		return modules.RenterContract{}, nil, errors.New("insufficient funds to pay both siafund fee and also host payout")
	}

	// Create unlock conditions.
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			renterKey,
			host.PublicKey,
		},
		SignaturesRequired: 2,
	}

	// Create file contract.
	renterPostTaxPayout := types.PostTax(startHeight, totalPayout).Sub(hostPayout)
	fc := types.FileContract{
		FileSize:       oldRev.NewFileSize,
		FileMerkleRoot: oldRev.NewFileMerkleRoot,
		WindowStart:    endHeight,
		WindowEnd:      endHeight + host.WindowSize,
		Payout:         totalPayout,
		UnlockHash:     uc.UnlockHash(),
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// Renter.
			{Value: renterPostTaxPayout, UnlockHash: refundAddress},
			// Host.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
		},
		MissedProofOutputs: []types.SiacoinOutput{
			// Renter.
			{Value: renterPostTaxPayout, UnlockHash: refundAddress},
			// Host gets its unused collateral back, plus the contract price.
			{Value: hostCollateral.Sub(baseCollateral).Add(host.ContractPrice), UnlockHash: host.UnlockHash},
			// Void gets the spent storage fees, plus the collateral being risked.
			{Value: basePrice.Add(baseCollateral), UnlockHash: types.UnlockHash{}},
		},
	}

	// Add both the new final revision and the new contract to the same
	// transaction.
	txnBuilder.AddFileContractRevision(finalRev)
	txnBuilder.AddFileContract(fc)

	// Add the fee to the transaction.
	txnBuilder.AddMinerFee(txnFee)

	// Create transaction set.
	txnSet, err := prepareTransactionSet(txnBuilder)
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("failed to prepare txnSet with finalRev and new contract: %s", err)
	}

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			hdb.IncrementFailedInteractions(host.PublicKey)
			err = fmt.Errorf("%s: %s", smodules.ErrHostFault, err)
		} else if err == nil {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Prepare the final revision.
	finalRevRenterSig := types.TransactionSignature{
		ParentID:       crypto.Hash(finalRev.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
	}
	finalRevTxn, _ := txnBuilder.View()
	finalRevTxn.TransactionSignatures = append(finalRevTxn.TransactionSignatures, finalRevRenterSig)

	// Calculate the txn hash and send it to the renter.
	sr := &signRequest{
		RevisionHash: finalRevTxn.SigHash(0, startHeight),
	}
	if err := s.WriteResponse(sr); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Read the renter signature and add it to the txn.
	var srr signResponse
	if err := s.ReadResponse(&srr, 65536); err != nil {
		return modules.RenterContract{}, nil, err
	}
	finalRevTxn.TransactionSignatures[0].Signature = srr.Signature[:]

	// Initiate protocol.
	hostName, _, err := net.SplitHostPort(string(host.NetAddress))
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("failed to get host name: %s", err)
	}
	siamuxAddr := net.JoinHostPort(hostName, host.SiaMuxPort)

	var noOpRevTxn types.Transaction
	err = WithTransportV3(ctx, siamuxAddr, host.PublicKey, func(t *rhpv3.Transport) (err error) {
		noOpRevTxn, err = RPCRenewOldContract(s, t, txnBuilder, txnSet, renterKey, host.PublicKey, finalRevTxn, startHeight)
		return err
	})
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("failed to renew contract: %s", err)
	}

	// Construct the final transaction.
	txnSet, err = prepareTransactionSet(txnBuilder)
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("failed to prepare txnSet with finalRev and new contract: %s", err)
	}

	// Submit to blockchain.
	err = tpool.AcceptTransactionSet(txnSet)
	if err == smodules.ErrDuplicateTransactionSet {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("failed to submit txnSet for renewal to blockchain: %s", err)
	}

	// Construct contract header.
	header := contractHeader{
		Transaction:     noOpRevTxn,
		StartHeight:     startHeight,
		TotalCost:       funding,
		ContractFee:     host.ContractPrice,
		TxnFee:          txnFee,
		SiafundFee:      types.Tax(startHeight, fc.Payout),
		StorageSpending: basePrice,
		Utility: smodules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
	}

	// Add contract to the set.
	meta, err := cs.managedInsertContract(header, pk)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Commit changes to old contract.
	if err := oldFC.managedCommitClearContract(finalRevTxn, renewCost); err != nil {
		return modules.RenterContract{}, nil, err
	}
	return meta, txnSet, nil
}
