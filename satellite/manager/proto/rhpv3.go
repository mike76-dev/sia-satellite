package proto

import (
	"encoding/json"
	"fmt"
	"time"

	rhpv3 "go.sia.tech/core/rhp/v3"
	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
)

// PriceTablePaymentFunc is a function that can be passed in to RPCPriceTable.
// It is called after the price table is received from the host and supposed to
// create a payment for that table and return it. It can also be used to perform
// gouging checks before paying for the table.
type PriceTablePaymentFunc func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error)

// RPCPriceTable negotiates a price table with the host.
func RPCPriceTable(t *rhpv3.Transport, paymentFunc PriceTablePaymentFunc) (pt rhpv3.HostPriceTable, err error) {
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
	var paymentType core.Specifier
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
func RPCRenewContract(t *rhpv3.Transport, txnBuilder transactionBuilder, txnSet []types.Transaction, renterKey crypto.SecretKey, hostPK types.SiaPublicKey, finalRevTxn types.Transaction, height types.BlockHeight) (types.Transaction, error) {
	s := t.DialStream()
	defer s.Close()
	s.SetDeadline(time.Now().Add(5 * time.Minute))

	// Create request.
	renterPK := renterKey.PublicKey()
	req := &rpcRenewContractRequest{
		TransactionSet:         txnSet,
		RenterKey:              types.Ed25519PublicKey(renterPK),
		FinalRevisionSignature: finalRevTxn.TransactionSignatures[0],
	}

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
	finalRevHostSig := resp.FinalRevisionSignature

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
		CoveredFields: types.CoveredFields{
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
		fmt.Printf("Write renew response: %v\n", err) //TODO
		return types.Transaction{}, fmt.Errorf("failed to send RPCRenewSignatures to host: %s", err)
	}
	fmt.Println("Write renew response: success") //TODO

	// Read the host's signatures and add them to the transactions.
	var hostSigs rpcRenewSignatures
	if err := s.ReadResponse(&hostSigs, 65536); err != nil {
		fmt.Printf("Host signatures: %v\n", err) //TODO
		return types.Transaction{}, fmt.Errorf("failed to read RPCRenewSignatures from host: %s", err)
	}
	fmt.Printf("Host signatures: %+v\n", hostSigs) //TODO
	for _, sig := range hostSigs.TransactionSignatures {
		_ = txnBuilder.AddTransactionSignature(sig)
	}

	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)

	return revisionTxn, nil
}
