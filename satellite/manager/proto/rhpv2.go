package proto

import (
	"context"
	"encoding/json"
	"fmt"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// RPCSettings returns the host's reported settings.
func RPCSettings(ctx context.Context, t *rhpv2.Transport) (hes modules.HostExternalSettings, err error) {
	var resp rhpv2.RPCSettingsResponse
	var settings rhpv2.HostSettings
	if err := t.Call(rhpv2.RPCSettingsID, nil, &resp); err != nil {
		return modules.HostExternalSettings{}, err
	} else if err := json.Unmarshal(resp.Settings, &settings); err != nil {
		return modules.HostExternalSettings{}, fmt.Errorf("couldn't unmarshal json: %w", err)
	}

	hes = modules.HostExternalSettings{
		AcceptingContracts:         settings.AcceptingContracts,
		MaxDownloadBatchSize:       settings.MaxDownloadBatchSize,
		MaxDuration:                types.BlockHeight(settings.MaxDuration),
		MaxReviseBatchSize:         settings.MaxReviseBatchSize,
		NetAddress:                 modules.NetAddress(settings.NetAddress),
		RemainingStorage:           settings.RemainingStorage,
		SectorSize:                 settings.SectorSize,
		TotalStorage:               settings.TotalStorage,
		UnlockHash:                 types.UnlockHash(settings.Address),
		WindowSize:                 types.BlockHeight(settings.WindowSize),
		Collateral:                 types.NewCurrency(settings.Collateral.Big()),
		MaxCollateral:              types.NewCurrency(settings.MaxCollateral.Big()),
		BaseRPCPrice:               types.NewCurrency(settings.BaseRPCPrice.Big()),
		ContractPrice:              types.NewCurrency(settings.ContractPrice.Big()),
		DownloadBandwidthPrice:     types.NewCurrency(settings.DownloadBandwidthPrice.Big()),
		SectorAccessPrice:          types.NewCurrency(settings.SectorAccessPrice.Big()),
		StoragePrice:               types.NewCurrency(settings.StoragePrice.Big()),
		UploadBandwidthPrice:       types.NewCurrency(settings.UploadBandwidthPrice.Big()),
		EphemeralAccountExpiry:     settings.EphemeralAccountExpiry,
		MaxEphemeralAccountBalance: types.NewCurrency(settings.MaxEphemeralAccountBalance.Big()),
		RevisionNumber:             settings.RevisionNumber,
		Version:                    settings.Version,
		SiaMuxPort:                 settings.SiaMuxPort,
	}
	
	return hes, nil
}

// RPCFormContract forms a contract with the host.
func RPCFormContract(ctx context.Context, t *rhpv2.Transport, txnBuilder transactionBuilder, renterKey crypto.SecretKey, txnSet []types.Transaction, height types.BlockHeight) (types.Transaction, error) {
	// Create request.
	renterPubkey := renterKey.PublicKey()
	req := &rpcFormContractRequest{
		Transactions: txnSet,
		RenterKey:    types.Ed25519PublicKey(renterPubkey),
	}
	if err := t.WriteRequest(rhpv2.RPCFormContractID, req); err != nil {
		return types.Transaction{}, err
	}

	// Read host's response.
	var resp rpcFormContractAdditions
	if err := t.ReadResponse(&resp, 65536); err != nil {
		return types.Transaction{}, err
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
		err = fmt.Errorf("failed to sign transaction: %v", err)
		t.WriteResponseErr(err)
		return types.Transaction{}, err
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet) - 1].TransactionSignatures[i])
	}

	// Create initial (no-op) revision, transaction, and signature.
	hostKey := crypto.PublicKey(t.HostKey())
	fc := signedTxnSet[len(signedTxnSet) - 1].FileContracts[0]
	initRevision := types.FileContractRevision{
		ParentID: signedTxnSet[len(signedTxnSet) - 1].FileContractID(0),
		UnlockConditions: types.UnlockConditions{
			PublicKeys: []types.SiaPublicKey{
				types.Ed25519PublicKey(renterPubkey),
				types.Ed25519PublicKey(hostKey),
			},
			SignaturesRequired: 2,
		},
		NewRevisionNumber:     1,
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
		CoveredFields:  types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
	}
	revisionTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{initRevision},
		TransactionSignatures: []types.TransactionSignature{renterRevisionSig},
	}
	encodedSig := crypto.SignHash(revisionTxn.SigHash(0, height), renterKey)
	revisionTxn.TransactionSignatures[0].Signature = encodedSig[:]

	// Write our signatures.
	renterSigs := &rpcFormContractSignatures{
		ContractSignatures: addedSignatures,
		RevisionSignature:  revisionTxn.TransactionSignatures[0],
	}
	if err := t.WriteResponse(renterSigs); err != nil {
		return types.Transaction{}, err
	}

	// Read the host's signatures and merge them with our own.
	var hostSigs rpcFormContractSignatures
	if err := t.ReadResponse(&hostSigs, 4096); err != nil {
		return types.Transaction{}, err
	}
	for _, sig := range hostSigs.ContractSignatures {
		txnBuilder.AddTransactionSignature(sig)
	}

	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)

	return revisionTxn, nil
}
