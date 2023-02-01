package proto

import (
	"math"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/build"
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
	"go.sia.tech/siad/types/typesutil"
)

// Renew negotiates a new contract for data already stored with a host, and
// submits the new contract transaction to tpool. The new contract is added to
// the ContractSet and its metadata is returned.
func (cs *ContractSet) Renew(oldContract *FileContract, params smodules.ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, formationTxnSet []types.Transaction, err error) {
	// Check that the host version is high enough as belt-and-suspenders. This
	// should never happen, because hosts with old versions should be blacklisted
	// by the contractor.
	if build.VersionCmp(params.Host.Version, "1.4.4") < 0 {
		return modules.RenterContract{}, nil, ErrBadHostVersion
	}
	return cs.managedNewRenewAndClear(oldContract, params, txnBuilder, tpool, hdb, cancel)
}

// managedNewRenewAndClear uses the new RPC to renew a contract, creating a new
// contract that is identical to the old one, and then clears the old one to be
// empty.
func (cs *ContractSet) managedNewRenewAndClear(oldContract *FileContract, params smodules.ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, formationTxnSet []types.Transaction, err error) {
	// For convenience.
	contract := oldContract.header

	// Extract vars from params, for convenience.
	fcTxn, _ := txnBuilder.View()
	host, funding, startHeight, endHeight := params.Host, params.Funding, params.StartHeight, params.EndHeight
	renterSKOld := contract.SecretKey
	renterSKNew, renterPKNew := smodules.GenerateContractKeyPair(params.RenterSeed, fcTxn)
	lastRev := contract.LastRevision()

	// Calculate the anticipated transaction fee.
	_, maxFee := tpool.FeeEstimation()
	txnFee := maxFee.Mul64(smodules.EstimatedFileContractTransactionSetSize)

	// Calculate the base cost.
	basePrice, baseCollateral := rhp2BaseCosts(lastRev, host, endHeight)

	// Create file contract and add it together with the fee to the builder.
	uc := createFileContractUnlockConds(host.PublicKey, renterPKNew)
	uh := uc.UnlockHash()
	fc, err := createRenewedContract(lastRev, uh, params, txnFee, basePrice, baseCollateral, tpool)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}
	txnBuilder.AddFileContract(fc)
	txnBuilder.AddMinerFee(txnFee)

	// Add FileContract identifier.
	si, hk := smodules.PrefixedSignedIdentifier(params.RenterSeed, fcTxn, host.PublicKey)
	_ = txnBuilder.AddArbitraryData(append(si[:], hk[:]...))

	// Create initial transaction set.
	txnSet, err := prepareTransactionSet(txnBuilder)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		// A revision mismatch might not be the host's fault.
		if err != nil && !IsRevisionMismatch(err) {
			hdb.IncrementFailedInteractions(contract.HostPublicKey())
			err = errors.Extend(err, smodules.ErrHostFault)
		} else if err == nil {
			hdb.IncrementSuccessfulInteractions(contract.HostPublicKey())
		}
	}()

	// Initiate protocol.
	s, err := cs.NewRawSession(host, startHeight, hdb, cancel)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}
	defer func() {
		err = errors.Compose(err, s.Close())
	}()

	// Lock the contract and resynchronize if necessary.
	rev, sigs, err := s.Lock(contract.ID(), renterSKOld)
	if err != nil {
		return modules.RenterContract{}, nil, err
	} else if err := oldContract.managedSyncRevision(rev, sigs); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Create the final revision of the old contract.
	bandwidthCost := host.BaseRPCPrice
	finalRev, err := prepareFinalRevision(contract, bandwidthCost)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "Unable to create final revision")
	}

	// Create the RenewContract request.
	req := smodules.LoopRenewAndClearContractRequest{
		Transactions: txnSet,
		RenterKey:    types.Ed25519PublicKey(renterPKNew),
	}
	for _, vpo := range finalRev.NewValidProofOutputs {
		req.FinalValidProofValues = append(req.FinalValidProofValues, vpo.Value)
	}
	for _, mpo := range finalRev.NewMissedProofOutputs {
		req.FinalMissedProofValues = append(req.FinalMissedProofValues, mpo.Value)
	}

	// Send the request.
	if err := s.writeRequest(smodules.RPCLoopRenewClearContract, req); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Read the host's response.
	var resp smodules.LoopContractAdditions
	if err := s.readResponse(&resp, smodules.RPCMinLen); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Incorporate host's modifications.
	txnBuilder.AddParents(resp.Parents)
	for _, input := range resp.Inputs {
		txnBuilder.AddSiacoinInput(input)
	}
	for _, output := range resp.Outputs {
		txnBuilder.AddSiacoinOutput(output)
	}

	// Sign the final revision of the old contract.
	rev.NewFileMerkleRoot = crypto.Hash{}
	finalRevTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{finalRev},
		TransactionSignatures: []types.TransactionSignature{
			{
				ParentID:       crypto.Hash(finalRev.ParentID),
				CoveredFields:  types.CoveredFields{FileContractRevisions: []uint64{0}},
				PublicKeyIndex: 0, // Renter key is always first -- see formContract.
			},
			{
				ParentID:       crypto.Hash(finalRev.ParentID),
				PublicKeyIndex: 1,
				CoveredFields:  types.CoveredFields{FileContractRevisions: []uint64{0}},
				Signature:      nil, // To be provided by the host.
			},
		},
	}
	finalRevSig := crypto.SignHash(finalRevTxn.SigHash(0, s.height), renterSKOld)
	finalRevTxn.TransactionSignatures[0].Signature = finalRevSig[:]

	// Sign the txn.
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		err = errors.New("failed to sign transaction: " + err.Error())
		smodules.WriteRPCResponse(s.conn, s.aead, nil, err)
		return modules.RenterContract{}, nil, err
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet) - 1].TransactionSignatures[i])
	}

	// Create initial (no-op) revision, transaction, and signature.
	revisionTxn := prepareInitRevisionTxn(lastRev, uc, fc, startHeight, renterSKNew, signedTxnSet[len(signedTxnSet) - 1].FileContractID(0))

	// Send acceptance and signatures.
	renterSigs := smodules.LoopRenewAndClearContractSignatures{
		ContractSignatures: addedSignatures,
		RevisionSignature:  revisionTxn.TransactionSignatures[0],

		FinalRevisionSignature: finalRevSig[:],
	}
	if err := smodules.WriteRPCResponse(s.conn, s.aead, renterSigs, nil); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Read the host acceptance and signatures.
	var hostSigs smodules.LoopRenewAndClearContractSignatures
	if err := s.readResponse(&hostSigs, smodules.RPCMinLen); err != nil {
		return modules.RenterContract{}, nil, err
	}
	for _, sig := range hostSigs.ContractSignatures {
		txnBuilder.AddTransactionSignature(sig)
	}
	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)
	finalRevTxn.TransactionSignatures[1].Signature = hostSigs.FinalRevisionSignature

	// Construct the final transaction.
	txnSet, err = prepareTransactionSet(txnBuilder)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Submit to blockchain.
	err = tpool.AcceptTransactionSet(txnSet)
	if errors.Contains(err, smodules.ErrDuplicateTransactionSet) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, nil, err
	}
	err = tpool.AcceptTransactionSet([]types.Transaction{finalRevTxn})
	if errors.Contains(err, smodules.ErrDuplicateTransactionSet) {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Construct contract header.
	header := contractHeader{
		Transaction:     revisionTxn,
		SecretKey:       renterSKNew,
		StartHeight:     startHeight,
		TotalCost:       funding,
		ContractFee:     host.ContractPrice,
		TxnFee:          txnFee,
		SiafundFee:      types.Tax(startHeight, fc.Payout),
		StorageSpending: basePrice,
		Utility: modules.ContractUtility{
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
	if err := oldContract.managedCommitClearContract(finalRevTxn, bandwidthCost); err != nil {
		return modules.RenterContract{}, nil, err
	}
	return meta, txnSet, nil
}

// rhp2BaseCosts computes the base costs for renewing a contract.
func rhp2BaseCosts(lastRev types.FileContractRevision, host smodules.HostDBEntry, endHeight types.BlockHeight) (basePrice, baseCollateral types.Currency) {
	// If the contract height did not increase, basePrice and baseCollateral are
	// zero.
	if endHeight + host.WindowSize > lastRev.NewWindowEnd {
		timeExtension := uint64((endHeight + host.WindowSize) - lastRev.NewWindowEnd)
		basePrice = host.StoragePrice.Mul64(lastRev.NewFileSize).Mul64(timeExtension)    // Cost of already uploaded data that needs to be covered by the renewed contract.
		baseCollateral = host.Collateral.Mul64(lastRev.NewFileSize).Mul64(timeExtension) // Same as basePrice.
	}
	return
}

// prepareInitRevisionTxn creates the initRevision, places it in a transaction and
// adds the signature or the revision to the transaction.
func prepareInitRevisionTxn(lastRev types.FileContractRevision, uc types.UnlockConditions, newContract types.FileContract, startHeight types.BlockHeight, renterSK crypto.SecretKey, parentID types.FileContractID) types.Transaction {
	initRevision := types.FileContractRevision{
		ParentID:          parentID,
		UnlockConditions:  uc,
		NewRevisionNumber: 1,

		NewFileSize:           newContract.FileSize,
		NewFileMerkleRoot:     newContract.FileMerkleRoot,
		NewWindowStart:        newContract.WindowStart,
		NewWindowEnd:          newContract.WindowEnd,
		NewValidProofOutputs:  newContract.ValidProofOutputs,
		NewMissedProofOutputs: newContract.MissedProofOutputs,
		NewUnlockHash:         newContract.UnlockHash,
	}

	renterRevisionSig := types.TransactionSignature{
		ParentID:       crypto.Hash(initRevision.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
	}

	revisionTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{initRevision},
		TransactionSignatures: []types.TransactionSignature{renterRevisionSig},
	}

	encodedSig := crypto.SignHash(revisionTxn.SigHash(0, startHeight), renterSK)
	revisionTxn.TransactionSignatures[0].Signature = encodedSig[:]
	return revisionTxn
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
func createRenewedContract(lastRev types.FileContractRevision, uh types.UnlockHash, params smodules.ContractParams, txnFee, basePrice, baseCollateral types.Currency, tpool transactionPool) (types.FileContract, error) {
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

// RenewContract takes an established connection to a host and renews the
// contract with that host.
func (cs *ContractSet) RenewContract(conn net.Conn, fcid types.FileContractID, params smodules.ContractParams, txnBuilder smodules.TransactionBuilder, tpool smodules.TransactionPool, hdb hostDB, pt *smodules.RPCPriceTable) (_ modules.RenterContract, _ []types.Transaction, err error) {
	// Fetch the contract.
	oldSC, ok := cs.Acquire(fcid)
	if !ok {
		return modules.RenterContract{}, nil, errors.New("RenewContract: failed to acquire contract to renew")
	}
	defer cs.Return(oldSC)
	oldContract := oldSC.header
	oldRev := oldContract.LastRevision()

	// Extract vars from params, for convenience.
	fcTxn, _ := txnBuilder.View()
	host, funding, startHeight, endHeight := params.Host, params.Funding, params.StartHeight, params.EndHeight
	renterSKOld := oldContract.SecretKey
	renterSKNew, renterPKNew := smodules.GenerateContractKeyPair(params.RenterSeed, fcTxn)

	// RHP3 contains both the contract and final revision. So we double the
	// estimation.
	txnFee := pt.TxnFeeMaxRecommended.Mul64(2 * smodules.EstimatedFileContractTransactionSetSize)

	// Calculate the base cost. This includes the RPC cost.
	basePrice, baseCollateral := smodules.RenewBaseCosts(oldRev, pt, endHeight)

	// Create the final revision of the old contract.
	renewCost := types.ZeroCurrency
	finalRev, err := prepareFinalRevision(oldContract, renewCost)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "Unable to create final revision")
	}

	// Create the new file contract.
	uc := createFileContractUnlockConds(host.PublicKey, renterPKNew)
	uh := uc.UnlockHash()
	fc, err := createRenewedContract(oldRev, uh, params, txnFee, basePrice, baseCollateral, tpool)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "Unable to create new contract")
	}

	// Add both the new final revision and the new contract to the same
	// transaction.
	txnBuilder.AddFileContractRevision(finalRev)
	txnBuilder.AddFileContract(fc)

	// Add the fee to the transaction.
	txnBuilder.AddMinerFee(txnFee)

	// Add FileContract identifier.
	si, hk := modules.PrefixedSignedIdentifier(params.RenterSeed, fcTxn, host.PublicKey)
	_ = txnBuilder.AddArbitraryData(append(si[:], hk[:]...))

	// Create transaction set.
	txnSet, err := prepareTransactionSet(txnBuilder)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to prepare txnSet with finalRev and new contract")
	}

	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			hdb.IncrementFailedInteractions(host.PublicKey)
			err = errors.Compose(err, smodules.ErrHostFault)
		} else if err == nil {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Sign the final revision.
	finalRevRenterSig := types.TransactionSignature{
		ParentID:       crypto.Hash(finalRev.ParentID),
		PublicKeyIndex: 0, // renter key is first
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
	}
	finalRevTxn, _ := txnBuilder.View()
	finalRevTxn.TransactionSignatures = append(finalRevTxn.TransactionSignatures, finalRevRenterSig)
	finalRevRenterSigRaw := crypto.SignHash(finalRevTxn.SigHash(0, pt.HostBlockHeight), renterSKOld)
	finalRevRenterSig.Signature = finalRevRenterSigRaw[:]
	// Write the request.
	err = smodules.RPCWrite(conn, smodules.RPCRenewContractRequest{
		TSet:        txnSet,
		RenterPK:    types.Ed25519PublicKey(renterPKNew),
		FinalRevSig: finalRevRenterSigRaw,
	})
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to write RPCRenewContractRequest")
	}

	// Read the response. It contains the host's final revision sig and any
	// additions it made.
	var resp smodules.RPCRenewContractCollateralResponse
	err = smodules.RPCReadMaxLen(conn, &resp, smodules.RenewDecodeMaxLen)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to read RPCRenewContractCollateralResponse")
	}

	// Incorporate host's modifications.
	txnBuilder.AddParents(resp.NewParents)
	for _, input := range resp.NewInputs {
		txnBuilder.AddSiacoinInput(input)
	}
	for _, output := range resp.NewOutputs {
		txnBuilder.AddSiacoinOutput(output)
	}

	// Create the host sig for the final revision.
	finalRevHostSigRaw := resp.FinalRevSig
	finalRevHostSig := types.TransactionSignature{
		ParentID:       crypto.Hash(finalRev.ParentID),
		PublicKeyIndex: 1,
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: finalRevHostSigRaw[:],
	}

	// Add the revision signatures to the transaction set and sign it.
	_ = txnBuilder.AddTransactionSignature(finalRevRenterSig)
	_ = txnBuilder.AddTransactionSignature(finalRevHostSig)
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to sign transaction set")
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet) - 1].TransactionSignatures[i])
	}

	// Create initial (no-op) revision, transaction, and signature.
	noOpRevTxn := prepareInitRevisionTxn(oldRev, uc, fc, startHeight, renterSKNew, signedTxnSet[len(signedTxnSet) - 1].FileContractID(0))
	// Send transaction signatures and no-op revision signature to host.
	err = smodules.RPCWrite(conn, smodules.RPCRenewContractRenterSignatures{
		RenterNoOpRevisionSig: noOpRevTxn.RenterSignature(),
		RenterTxnSigs:         addedSignatures,
	})
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to send RPCRenewContractRenterSignatures to host")
	}

	// Read the host's signatures and add them to the transactions.
	var hostSignatureResp smodules.RPCRenewContractHostSignatures
	err = smodules.RPCReadMaxLen(conn, &hostSignatureResp, smodules.RenewDecodeMaxLen)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to read RPCRenewContractHostSignatures from host")
	}
	for _, sig := range hostSignatureResp.ContractSignatures {
		_ = txnBuilder.AddTransactionSignature(sig)
	}
	noOpRevTxn.TransactionSignatures = append(noOpRevTxn.TransactionSignatures, hostSignatureResp.NoOpRevisionSignature)

	// Construct the final transaction.
	txnSet, err = prepareTransactionSet(txnBuilder)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to prepare txnSet with finalRev and new contract")
	}

	// Submit the txn set with the final revision and new contract to the blockchain.
	err = tpool.AcceptTransactionSet(txnSet)
	if err == smodules.ErrDuplicateTransactionSet {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "failed to submit txnSet for renewal to blockchain")
	}

	// Construct contract header.
	header := contractHeader{
		Transaction:     noOpRevTxn,
		SecretKey:       renterSKNew,
		StartHeight:     startHeight,
		TotalCost:       funding,
		ContractFee:     pt.ContractPrice,
		TxnFee:          txnFee,
		SiafundFee:      types.Tax(startHeight, fc.Payout),
		StorageSpending: basePrice,
		Utility: smodules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
	}

	// Add contract to set.
	newContract, err := cs.managedInsertContract(header) // No Merkle roots.
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Commit changes to old contract.
	if err := oldSC.managedCommitClearContract(finalRevTxn, renewCost); err != nil {
		return modules.RenterContract{}, nil, err
	}
	return newContract, txnSet, nil
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
