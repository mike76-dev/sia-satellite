package proto

import (
	"context"
	"fmt"
	"math"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
	"go.sia.tech/siad/types/typesutil"
)

// Renew negotiates a new contract for data already stored with a host, and
// submits the new contract transaction to tpool. The new contract is added to
// the ContractSet and its metadata is returned.
func (cs *ContractSet) Renew(oldFC *FileContract, params modules.ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, formationTxnSet []types.Transaction, err error) {
	// For convenience.
	oldContract := oldFC.header
	oldRev := oldContract.LastRevision()

	// Extract vars from params, for convenience.
	fcTxn, _ := txnBuilder.View()
	host, funding, startHeight, endHeight := params.Host, params.Funding, params.StartHeight, params.EndHeight
	renterSKOld := oldContract.SecretKey
	renterSKNew, renterPKNew := crypto.GenerateKeyPairDeterministic([crypto.EntropySize]byte(params.RenterSeed))

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

	// Create the new file contract.
	uc := createFileContractUnlockConds(host.PublicKey, renterPKNew)
	uh := uc.UnlockHash()
	fc, err := createRenewedContract(oldRev, uh, params, txnFee, basePrice, baseCollateral, tpool)
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("unable to create new contract: %s", err)
	}

	// Add both the new final revision and the new contract to the same
	// transaction.
	txnBuilder.AddFileContractRevision(finalRev)
	txnBuilder.AddFileContract(fc)

	// Add the fee to the transaction.
	txnBuilder.AddMinerFee(txnFee)

	// Add FileContract identifier.
	si, hk := smodules.PrefixedSignedIdentifier(smodules.EphemeralRenterSeed(params.RenterSeed), fcTxn, host.PublicKey)
	_ = txnBuilder.AddArbitraryData(append(si[:], hk[:]...))

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

	// Sign the final revision.
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
	finalRevRenterSigRaw := crypto.SignHash(finalRevTxn.SigHash(0, pt.HostBlockHeight), renterSKOld)
	finalRevTxn.TransactionSignatures[0].Signature = finalRevRenterSigRaw[:]

	// Initiate protocol.
	hostName, _, err := net.SplitHostPort(string(host.NetAddress))
	if err != nil {
		return modules.RenterContract{}, nil, fmt.Errorf("failed to get host name: %s", err)
	}
	siamuxAddr := net.JoinHostPort(hostName, host.SiaMuxPort)

	var noOpRevTxn types.Transaction
	err = WithTransportV3(ctx, siamuxAddr, host.PublicKey, func(t *rhpv3.Transport) (err error) {
		noOpRevTxn, err = RPCRenewContract(t, txnBuilder, txnSet, renterSKNew, host.PublicKey, finalRevTxn, startHeight)
		return err
	})

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
		SecretKey:       renterSKNew,
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
	meta, err := cs.managedInsertContract(header)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Commit changes to old contract.
	if err := oldFC.managedCommitClearContract(finalRevTxn, renewCost); err != nil {
		return modules.RenterContract{}, nil, err
	}
	return meta, txnSet, nil
}

// prepareFinalRevision creates a new revision for a contract which transfers
// the given amount of payment, clears the contract and sets the missed outputs
// to equal the valid outputs.
func prepareFinalRevision(contract contractHeader, payment types.Currency) (types.FileContractRevision, error) {
	finalRev, err := contract.LastRevision().PaymentRevision(payment)
	if err != nil {
		return types.FileContractRevision{}, err
	}

	// Clear the revision.
	finalRev.NewFileSize = 0
	finalRev.NewFileMerkleRoot = crypto.Hash{}
	finalRev.NewRevisionNumber = math.MaxUint64

	// The valid proof outputs become the missed ones since the host won't need
	// to provide a storage proof.
	finalRev.NewMissedProofOutputs = finalRev.NewValidProofOutputs
	return finalRev, nil
}

// prepareTransactionSet prepares a transaction set from the given builder to be
// sent to the host. It includes all unconfirmed parents and has all
// non-essential txns trimmed from it.
func prepareTransactionSet(txnBuilder transactionBuilder) ([]types.Transaction, error) {
	txn, parentTxns := txnBuilder.View()
	unconfirmedParents, err := txnBuilder.UnconfirmedParents()
	if err != nil {
		return nil, err
	}
	txnSet := append(unconfirmedParents, parentTxns...)
	txnSet = typesutil.MinimumTransactionSet([]types.Transaction{txn}, txnSet)
	return txnSet, nil
}

// createRenewedContract creates a new contract from another contract's last
// revision given some additional renewal parameters.
func createRenewedContract(lastRev types.FileContractRevision, uh types.UnlockHash, params modules.ContractParams, txnFee, basePrice, baseCollateral types.Currency, tpool transactionPool) (types.FileContract, error) {
	allowance, startHeight, endHeight, host, funding := params.Allowance, params.StartHeight, params.EndHeight, params.Host, params.Funding

	// Calculate the payouts for the renter, host, and whole contract.
	period := endHeight - startHeight
	renterPayout, hostPayout, hostCollateral, err := smodules.RenterPayoutsPreTax(host, funding, txnFee, basePrice, baseCollateral, period, allowance.ExpectedStorage/allowance.Hosts)
	if err != nil {
		return types.FileContract{}, err
	}
	totalPayout := renterPayout.Add(hostPayout)

	// check for negative currency
	if hostCollateral.Cmp(baseCollateral) < 0 {
		baseCollateral = hostCollateral
	}
	if types.PostTax(params.StartHeight, totalPayout).Cmp(hostPayout) < 0 {
		return types.FileContract{}, errors.New("insufficient funds to pay both siafund fee and also host payout")
	}

	fc := types.FileContract{
		FileSize:       lastRev.NewFileSize,
		FileMerkleRoot: lastRev.NewFileMerkleRoot,
		WindowStart:    params.EndHeight,
		WindowEnd:      params.EndHeight + params.Host.WindowSize,
		Payout:         totalPayout,
		UnlockHash:     uh,
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// Renter.
			{Value: types.PostTax(params.StartHeight, totalPayout).Sub(hostPayout), UnlockHash: params.RefundAddress},
			// Host.
			{Value: hostPayout, UnlockHash: params.Host.UnlockHash},
		},
		MissedProofOutputs: []types.SiacoinOutput{
			// Renter.
			{Value: types.PostTax(params.StartHeight, totalPayout).Sub(hostPayout), UnlockHash: params.RefundAddress},
			// Host gets its unused collateral back, plus the contract price.
			{Value: hostCollateral.Sub(baseCollateral).Add(params.Host.ContractPrice), UnlockHash: params.Host.UnlockHash},
			// Void gets the spent storage fees, plus the collateral being risked.
			{Value: basePrice.Add(baseCollateral), UnlockHash: types.UnlockHash{}},
		},
	}
	return fc, nil
}

// createFileContractUnlockConds is a helper method to create unlock conditions
// for forming and renewing a contract.
func createFileContractUnlockConds(hpk types.SiaPublicKey, renterPK crypto.PublicKey) types.UnlockConditions {
	return types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(renterPK),
			hpk,
		},
		SignaturesRequired: 2,
	}
}

// FetchPriceTable retrieves the price table from the host.
func FetchPriceTable(host smodules.HostDBEntry) (rhpv3.HostPriceTable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), contractHostPriceTableTimeout)
	defer cancel()

	hostName, _, err := net.SplitHostPort(string(host.NetAddress))
	if err != nil {
		return rhpv3.HostPriceTable{}, err
	}
	siamuxAddr := net.JoinHostPort(hostName, host.SiaMuxPort)

	var pt rhpv3.HostPriceTable
	err = WithTransportV3(ctx, siamuxAddr, host.PublicKey, func(t *rhpv3.Transport) (err error) {
		pt, err = RPCPriceTable(t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
			return nil, nil
		})
		return
	})

	return pt, err
}
