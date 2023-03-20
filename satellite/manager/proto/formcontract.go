package proto

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

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

	// Calculate the anticipated transaction fee.
	_, maxFee := tpool.FeeEstimation()
	txnFee := maxFee.Mul64(smodules.EstimatedFileContractTransactionSetSize).MulFloat(1.5)

	// Calculate the payouts for the renter, host, and whole contract.
	period := endHeight - startHeight
	expectedStorage := allowance.ExpectedStorage / allowance.Hosts
	renterPayout, hostPayout, _, err := smodules.RenterPayoutsPreTax(host, funding, txnFee, types.ZeroCurrency, types.ZeroCurrency, period, expectedStorage)
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	totalPayout := renterPayout.Add(hostPayout)

	// Check for negative currency.
	if types.PostTax(startHeight, totalPayout).Cmp(hostPayout) < 0 {
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

	// Add FileContract identifier.
	fcTxn, _ := txnBuilder.View()
	si, hk := smodules.PrefixedSignedIdentifier(params.RenterSeed, fcTxn, host.PublicKey)
	_ = txnBuilder.AddArbitraryData(append(si[:], hk[:]...))
	// Create our key.
	renterSK, renterPK := crypto.GenerateKeyPairDeterministic([crypto.EntropySize]byte(params.RenterSeed))
	// Create unlock conditions.
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(renterPK),
			host.PublicKey,
		},
		SignaturesRequired: 2,
	}

	// Create file contract.
	renterPostTaxPayout := types.PostTax(startHeight, totalPayout).Sub(hostPayout)
	fc := types.FileContract{
		FileSize:       0,
		FileMerkleRoot: crypto.Hash{}, // No proof possible without data.
		WindowStart:    endHeight,
		WindowEnd:      endHeight + host.WindowSize,
		Payout:         totalPayout,
		UnlockHash:     uc.UnlockHash(),
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// Outputs need to account for tax.
			{Value: renterPostTaxPayout, UnlockHash: refundAddress}, // This is the renter payout, but with tax applied.
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

	// Initiate protocol.
	s, err := cs.NewRawSession(host, types.Ed25519PublicKey(renterPK), startHeight, hdb, cs.log, cancel)
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	defer func() {
		err = errors.Compose(err, s.Close())
	}()

	// Send the FormContract request.
	req := smodules.LoopFormContractRequest{
		Transactions: txnSet,
		RenterKey:    uc.PublicKeys[0],
	}
	if err := s.writeRequest(smodules.RPCLoopFormContract, req); err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Read the host's response.
	var resp smodules.LoopContractAdditions
	if err := s.readResponse(&resp, smodules.RPCMinLen); err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Incorporate host's modifications.
	txnBuilder.AddParents(resp.Parents)
	for _, input := range resp.Inputs {
		txnBuilder.AddSiacoinInput(input)
	}
	for _, output := range resp.Outputs {
		txnBuilder.AddSiacoinOutput(output)
	}

	// Sign the txn.
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		err = errors.New("failed to sign transaction: " + err.Error())
		smodules.WriteRPCResponse(s.conn, s.aead, nil, err)
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet) - 1].TransactionSignatures[i])
	}

	// Create initial (no-op) revision, transaction, and signature.
	initRevision := types.FileContractRevision{
		ParentID:          signedTxnSet[len(signedTxnSet) - 1].FileContractID(0),
		UnlockConditions:  uc,
		NewRevisionNumber: 1,

		NewFileSize:           fc.FileSize,
		NewFileMerkleRoot:     fc.FileMerkleRoot,
		NewWindowStart:        fc.WindowStart,
		NewWindowEnd:          fc.WindowEnd,
		NewValidProofOutputs:  fc.ValidProofOutputs,
		NewMissedProofOutputs: fc.MissedProofOutputs,
		NewUnlockHash:         fc.UnlockHash,
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

	// Send acceptance and signatures.
	renterSigs := smodules.LoopContractSignatures{
		ContractSignatures: addedSignatures,
		RevisionSignature:  revisionTxn.TransactionSignatures[0],
	}
	if err := smodules.WriteRPCResponse(s.conn, s.aead, renterSigs, nil); err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}

	// Read the host acceptance and signatures.
	var hostSigs smodules.LoopContractSignatures
	if err := s.readResponse(&hostSigs, smodules.RPCMinLen); err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	for _, sig := range hostSigs.ContractSignatures {
		txnBuilder.AddTransactionSignature(sig)
	}
	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)

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
	meta, err := cs.managedInsertContract(header) // No Merkle roots.
	if err != nil {
		return modules.RenterContract{}, nil, types.Transaction{}, nil, err
	}
	return meta, minSet, sweepTxn, sweepParents, nil
}
