package proto

import (
	"context"
	"encoding/json"
	//"fmt"
	//"math"
	"time"

	//"github.com/mike76-dev/sia-satellite/modules"

	//rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

// PriceTablePaymentFunc is a function that can be passed in to RPCPriceTable.
// It is called after the price table is received from the host and supposed to
// create a payment for that table and return it. It can also be used to perform
// gouging checks before paying for the table.
type PriceTablePaymentFunc func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error)

// RPCPriceTable negotiates a price table with the host.
func RPCPriceTable(ctx context.Context, t *rhpv3.Transport, paymentFunc PriceTablePaymentFunc) (pt rhpv3.HostPriceTable, err error) {
	s := t.DialStream()
	defer s.Close()

	s.SetDeadline(time.Now().Add(5 * time.Second))
	const maxPriceTableSize = 16 * 1024
	var ptr rhpv3.RPCUpdatePriceTableResponse
	if err := s.WriteRequest(rhpv3.RPCUpdatePriceTableID, nil); err != nil {
		return rhpv3.HostPriceTable{}, err
	} else if err := s.ReadResponse(&ptr, maxPriceTableSize); err != nil {
		return rhpv3.HostPriceTable{}, err
	} else if err := json.Unmarshal(ptr.PriceTableJSON, &pt); err != nil {
		return rhpv3.HostPriceTable{}, err
	} else if payment, err := paymentFunc(pt); err != nil {
		return rhpv3.HostPriceTable{}, err
	} else if payment == nil {
		return pt, nil // Intended not to pay.
	} else if err := processPayment(s, payment); err != nil {
		return rhpv3.HostPriceTable{}, err
	} else if err := s.ReadResponse(&rhpv3.RPCPriceTableResponse{}, 0); err != nil {
		return rhpv3.HostPriceTable{}, err
	}
	return pt, nil
}

// processPayment carries out the payment for an RPC.
func processPayment(s *rhpv3.Stream, payment rhpv3.PaymentMethod) error {
	var paymentType types.Specifier
	switch payment.(type) {
	case *rhpv3.PayByContractRequest:
		paymentType = rhpv3.PaymentTypeContract
	case *rhpv3.PayByEphemeralAccountRequest:
		paymentType = rhpv3.PaymentTypeEphemeralAccount
	default:
		panic("unhandled payment method")
	}
	if err := s.WriteResponse(&paymentType); err != nil {
		return err
	} else if err := s.WriteResponse(payment); err != nil {
		return err
	}
	if _, ok := payment.(*rhpv3.PayByContractRequest); ok {
		var pr rhpv3.PaymentResponse
		if err := s.ReadResponse(&pr, 4096); err != nil {
			return err
		}
		// TODO: return host signature.
	}
	return nil
}

// RPCRenewContract negotiates a contract renewal with the host.
/*func RPCRenewContract(t *rhpv3.Transport, txnBuilder transactionBuilder, renterKey types.PrivateKey, hostPK types.PublicKey, hostAddress, renterAddress types.Address, finalRevTxn types.Transaction, funding, newCollateral types.Currency, endHeight uint64) (_ rhpv2.ContractRevision, _ []types.Transaction, _ types.Transaction, _ []types.Transaction, err error) {
	s, err := t.DialStream()
	if err != nil {
		return rhpv2.ContractRevision{}, nil, types.Transaction{}, nil, err
	}
	defer s.Close()

	// Send the ptUID.
	var ptUID rhpv3.SettingsID
	if err = s.WriteRequest(rhpv3.RPCRenewContractID, &ptUID); err != nil {
		return rhpv2.ContractRevision{}, nil, types.Transaction{}, nil, err
	}

	// The price table we sent contained a zero uid so we receive a
	// temporary one.
	var ptResp rhpv3.RPCUpdatePriceTableResponse
	if err = s.ReadResponse(&ptResp, 4096); err != nil {
		return rhpv2.ContractRevision{}, nil, types.Transaction{}, nil, err
	}
	err = json.Unmarshal(ptr.PriceTableJSON, &pt)
	if err != nil {
		return rhpv2.ContractRevision{}, nil, types.Transaction{}, nil, err
	}

	// Prepare the signed transaction that contains the final revision as well
	// as the new contract.
	rev := finalRevTxn.FileContractRevisions[0]
	txnSet, err := prepareRenewal(txnBuilder, rev, hostAddress, renterAddress, funding, newCollateral, pt, endHeight)
	if err != nil {
		return rhpv2.ContractRevision{}, nil, types.Transaction{}, nil, err
	}
	parents, txn := txnSet[:len(txnSet) - 1], txnSet[len(txnSet) - 1]

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
	sweepTxn := modules.ConvertToCore(st)
	sweepParents := modules.ConvertToCore(sp)


	// Create request.
	renterPK := renterKey.PublicKey()
	req := &rpcRenewContractRequest{
		TransactionSet: txnSet,
		RenterKey:      types.Ed25519PublicKey(renterPK),
	}
	copy(req.FinalRevisionSignature[:], finalRevTxn.TransactionSignatures[0].Signature)

	// Send request.
	if err := s.WriteResponse(req); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to write RPCRenewContractRequest: %s", err)
	}

	// Read the response. It contains the host's final revision sig and any
	// additions it made.
	var resp rpcRenewContractHostAdditions
	if err := s.ReadResponse(&resp, 65536); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to read RPCRenewContractHostAdditions: %s", err)
	}

	// Incorporate host's modifications.
	txnBuilder.AddParents(resp.Parents)
	for _, input := range resp.SiacoinInputs {
		txnBuilder.AddSiacoinInput(input)
	}
	for _, output := range resp.SiacoinOutputs {
		txnBuilder.AddSiacoinOutput(output)
	}

	// Get the host sig for the final revision.
	finalRevHostSigRaw := resp.FinalRevisionSignature
	finalRevHostSig := types.TransactionSignature{
		ParentID:       crypto.Hash(finalRevTxn.FileContractRevisions[0].ParentID),
		PublicKeyIndex: 1,
		CoveredFields:  types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: finalRevHostSigRaw[:],
	}

	// Add the revision signatures to the transaction set and sign it.
	_ = txnBuilder.AddTransactionSignature(finalRevTxn.TransactionSignatures[0])
	_ = txnBuilder.AddTransactionSignature(finalRevHostSig)
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		return types.Transaction{}, fmt.Errorf("failed to sign transaction set: %s", err)
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet) - 1].TransactionSignatures[i])
	}

	// Create initial (no-op) revision, transaction, and signature.
	fc := signedTxnSet[len(signedTxnSet) - 1].FileContracts[0]
	initRevision := types.FileContractRevision{
		ParentID:          signedTxnSet[len(signedTxnSet) - 1].FileContractID(0),
		UnlockConditions:  types.UnlockConditions{
			PublicKeys: []types.SiaPublicKey{
				types.Ed25519PublicKey(renterPK),
				hostPK,
			},
			SignaturesRequired: 2,
		},
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

	// Send transaction signatures and no-op revision signature to host.
	renterSigs := &rpcRenewSignatures{
		TransactionSignatures: addedSignatures,
		RevisionSignature:     revisionTxn.TransactionSignatures[0],
	}
	if err := s.WriteResponse(renterSigs); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to send RPCRenewSignatures to host: %s", err)
	}

	// Read the host's signatures and add them to the transactions.
	var hostSigs rpcRenewSignatures
	if err := s.ReadResponse(&hostSigs, 65536); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to read RPCRenewSignatures from host: %s", err)
	}
	for _, sig := range hostSigs.TransactionSignatures {
		_ = txnBuilder.AddTransactionSignature(sig)
	}

	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)

	return revisionTxn, nil
}*/

// RPCRenewOldContract negotiates a contract renewal with the host using
// the new Renter-Satellite protocol.
/*func RPCRenewOldContract(ss *modules.RPCSession, t *rhpv3.Transport, txnBuilder transactionBuilder, txnSet []types.Transaction, renterPK types.SiaPublicKey, hostPK types.SiaPublicKey, finalRevTxn types.Transaction, height types.BlockHeight) (types.Transaction, error) {
	s := t.DialStream()
	defer s.Close()
	s.SetDeadline(time.Now().Add(5 * time.Minute))

	// Send an empty price table uid.
	var pt rhpv3.HostPriceTable
	if err := s.WriteRequest(rhpv3.RPCRenewContractID, &pt.UID); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to write price table uid: %s", err)
	}

	// If the price table we sent contained a zero uid, we receive a temporary
	// one.
	var ptr rhpv3.RPCUpdatePriceTableResponse
	err := s.ReadResponse(&ptr, 8192)
	if err != nil {
		return types.Transaction{}, fmt.Errorf("failed to fetch temporary price table: %s", err)
	}
	err = json.Unmarshal(ptr.PriceTableJSON, &pt)
	if err != nil {
		return types.Transaction{}, fmt.Errorf("failed to unmarshal temporary price table: %s", err)
	}

	// Create request.
	req := &rpcRenewContractRequest{
		TransactionSet: txnSet,
		RenterKey:      renterPK,
	}
	copy(req.FinalRevisionSignature[:], finalRevTxn.TransactionSignatures[0].Signature)

	// Send request.
	if err := s.WriteResponse(req); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to write RPCRenewContractRequest: %s", err)
	}

	// Read the response. It contains the host's final revision sig and any
	// additions it made.
	var resp rpcRenewContractHostAdditions
	if err := s.ReadResponse(&resp, 65536); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to read RPCRenewContractHostAdditions: %s", err)
	}

	// Incorporate host's modifications.
	txnBuilder.AddParents(resp.Parents)
	for _, input := range resp.SiacoinInputs {
		txnBuilder.AddSiacoinInput(input)
	}
	for _, output := range resp.SiacoinOutputs {
		txnBuilder.AddSiacoinOutput(output)
	}

	// Get the host sig for the final revision.
	finalRevHostSigRaw := resp.FinalRevisionSignature
	finalRevHostSig := types.TransactionSignature{
		ParentID:       crypto.Hash(finalRevTxn.FileContractRevisions[0].ParentID),
		PublicKeyIndex: 1,
		CoveredFields:  types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: finalRevHostSigRaw[:],
	}

	// Add the revision signatures to the transaction set and sign it.
	_ = txnBuilder.AddTransactionSignature(finalRevTxn.TransactionSignatures[0])
	_ = txnBuilder.AddTransactionSignature(finalRevHostSig)
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		return types.Transaction{}, fmt.Errorf("failed to sign transaction set: %s", err)
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet) - 1].TransactionSignatures[i])
	}

	// Create initial (no-op) revision and transaction.
	fc := signedTxnSet[len(signedTxnSet) - 1].FileContracts[0]
	initRevision := types.FileContractRevision{
		ParentID:          signedTxnSet[len(signedTxnSet) - 1].FileContractID(0),
		UnlockConditions:  types.UnlockConditions{
			PublicKeys: []types.SiaPublicKey{
				renterPK,
				hostPK,
			},
			SignaturesRequired: 2,
		},
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
		CoveredFields:  types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
	}
	revisionTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{initRevision},
		TransactionSignatures: []types.TransactionSignature{renterRevisionSig},
	}

	// Calculate the txn hash and send it to the renter.
	sr := &signRequest{
		RevisionHash: revisionTxn.SigHash(0, height),
	}
	if err := ss.WriteResponse(sr); err != nil {
		return types.Transaction{}, err
	}

	// Read the renter signature and add it to the txn.
	var srr signResponse
	if err := ss.ReadResponse(&srr, 65536); err != nil {
		return types.Transaction{}, err
	}
	revisionTxn.TransactionSignatures[0].Signature = srr.Signature[:]

	// Send transaction signatures and no-op revision signature to host.
	renterSigs := &rpcRenewSignatures{
		TransactionSignatures: addedSignatures,
		RevisionSignature:     revisionTxn.TransactionSignatures[0],
	}
	if err := s.WriteResponse(renterSigs); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to send RPCRenewSignatures to host: %s", err)
	}

	// Read the host's signatures and add them to the transactions.
	var hostSigs rpcRenewSignatures
	if err := s.ReadResponse(&hostSigs, 65536); err != nil {
		return types.Transaction{}, fmt.Errorf("failed to read RPCRenewSignatures from host: %s", err)
	}
	for _, sig := range hostSigs.TransactionSignatures {
		_ = txnBuilder.AddTransactionSignature(sig)
	}

	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)

	return revisionTxn, nil
}*/

// RPCLatestRevision fetches the latest revision from the host.
func RPCLatestRevision(t *rhpv3.Transport, contractID types.FileContractID) (types.FileContractRevision, error) {
	s := t.DialStream()
	defer s.Close()

	s.SetDeadline(time.Now().Add(15 * time.Second))
	req := rhpv3.RPCLatestRevisionRequest{
		ContractID: contractID,
	}
	var resp rhpv3.RPCLatestRevisionResponse
	pt := rhpv3.HostPriceTable{}
	if err := s.WriteRequest(rhpv3.RPCLatestRevisionID, &req); err != nil {
		return types.FileContractRevision{}, err
	} else if err := s.ReadResponse(&resp, 4096); err != nil {
		return types.FileContractRevision{}, err
	} else if err := s.WriteResponse(&pt.UID); err != nil {
		return types.FileContractRevision{}, err
	}
	return resp.Revision, nil
}

// prepareRenewal prepares the final revision and a new contract.
/*func prepareRenewal(txnBuilder transactionBuilder, rev types.FileContractRevision, hostAddress, renterAddress types.Address, renterFunds, newCollateral types.Currency, pt rhpv3.HostPriceTable, endHeight uint64) ([]types.Transaction, error) {
	// Create the final revision from the provided revision.
	finalRevision := rev
	finalRevision.MissedProofOutputs = finalRevision.ValidProofOutputs
	finalRevision.Filesize = 0
	finalRevision.FileMerkleRoot = types.Hash256{}
	finalRevision.RevisionNumber = math.MaxUint64

	// Prepare the new contract.
	fc, basePrice := rhpv3.PrepareContractRenewal(rev, hostAddress, renterAddress, renterFunds, newCollateral, pt, endHeight)

	// Add revision, contract, and mining fee.
	txnBuilder.AddFileContractRevision(modules.ConvertToSiad(finalRev))
	txnBuilder.AddFileContract(modules.ConvertToSiad(fc))
	minerFee := pt.TxnFeeMaxRecommended.Mul64(4096)
	txnBuilder.AddMinerFee(siad.NewCurrency(minerFee.Big()))

	// Compute how much renter funds to put into the new contract.
	cost := fc.ValidRenterPayout().Add(pt.ContractPrice).Add(minerFee)
	cost = cost.Add(basePrice).Add(modules.Tax(fc.Payout))

	// Fund the transaction.
	err = txnBuilder.FundSiacoins(siad.NewCurrency(cost.Big()))
	if err != nil {
		return nil, fmt.Errorf("couldn't fund transaction: %s", err)
	}

	// Add any required parents.
	up, err := txnBuilder.UnconfirmedParents()
	if err != nil {
		txnBuilder.Drop()
		return nil, fmt.Errorf("couldn't load transaction dependencies: %s", err)
	}

	txn, parents := txnBuilder.View()
	return append(modules.ConvertToCore(up), append(modules.ConvertToCore(parents), modules.ConvertToCore(txn))), nil
}*/
