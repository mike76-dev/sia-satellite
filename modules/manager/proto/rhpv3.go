package proto

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	rhpv2 "go.sia.tech/core/rhp/v2"
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
func RPCRenewContract(ctx context.Context, t *rhpv3.Transport, renterKey types.PrivateKey, rev types.FileContractRevision, txnSet []types.Transaction, toSign []types.Hash256, ts transactionSigner) (_ rhpv2.ContractRevision, _ []types.Transaction, err error) {
	s := t.DialStream()
	defer s.Close()
	s.SetDeadline(time.Now().Add(5 * time.Minute))

	// Send an empty price table uid.
	var pt rhpv3.HostPriceTable
	if err = s.WriteRequest(rhpv3.RPCRenewContractID, &pt.UID); err != nil {
		return rhpv2.ContractRevision{}, nil, err
	}

	// The price table we sent contained a zero uid so we receive a
	// temporary one.
	var ptr rhpv3.RPCUpdatePriceTableResponse
	if err = s.ReadResponse(&ptr, 8192); err != nil {
		return rhpv2.ContractRevision{}, nil, err
	}
	err = json.Unmarshal(ptr.PriceTableJSON, &pt)
	if err != nil {
		return rhpv2.ContractRevision{}, nil, err
	}

	// Prepare the signed transaction that contains the final revision as well
	// as the new contract.
	parents, txn := txnSet[:len(txnSet) - 1], txnSet[len(txnSet) - 1]
	h := types.NewHasher()
	txn.FileContracts[0].EncodeTo(h.E)
	txn.FileContractRevisions[0].EncodeTo(h.E)
	finalRevisionSignature := renterKey.SignHash(h.Sum())

	// Send the request.
	req := rhpv3.RPCRenewContractRequest{
		TransactionSet:         txnSet,
		RenterKey:              rev.UnlockConditions.PublicKeys[0],
		FinalRevisionSignature: finalRevisionSignature,
	}
	if err = s.WriteResponse(&req); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to send RPCRenewContractRequest")
	}

	// Incorporate the host's additions.
	var hostAdditions rhpv3.RPCRenewContractHostAdditions
	if err = s.ReadResponse(&hostAdditions, 65536); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to read RPCRenewContractHostAdditions")
	}
	parents = append(parents, hostAdditions.Parents...)
	txn.SiacoinInputs = append(txn.SiacoinInputs, hostAdditions.SiacoinInputs...)
	txn.SiacoinOutputs = append(txn.SiacoinOutputs, hostAdditions.SiacoinOutputs...)

	finalRevRenterSig := types.TransactionSignature{
		ParentID:       types.Hash256(rev.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: finalRevisionSignature[:],
	}
	finalRevHostSig := types.TransactionSignature{
		ParentID:       types.Hash256(rev.ParentID),
		PublicKeyIndex: 1,
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: hostAdditions.FinalRevisionSignature[:],
	}
	txn.Signatures = []types.TransactionSignature{finalRevRenterSig, finalRevHostSig}

	// Sign the inputs we funded the txn with and cover the whole txn including
	// the existing signatures.
	cf := types.CoveredFields{
		WholeTransaction: true,
		Signatures:       []uint64{0, 1},
	}
	if err := ts.Sign(&txn, toSign, cf); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to sign transaction")
	}

	// Create a new no-op revision and sign it.
	fc := txn.FileContracts[0]
	noOpRevision := types.FileContractRevision{
		ParentID:          txn.FileContractID(0),
		UnlockConditions:  types.UnlockConditions{
			PublicKeys: []types.UnlockKey{
				renterKey.PublicKey().UnlockKey(),
				rev.UnlockConditions.PublicKeys[1],
			},
			SignaturesRequired: 2,
		},
		FileContract: types.FileContract{
			Filesize:           fc.Filesize,
			FileMerkleRoot:     fc.FileMerkleRoot,
			WindowStart:        fc.WindowStart,
			WindowEnd:          fc.WindowEnd,
			ValidProofOutputs:  fc.ValidProofOutputs,
			MissedProofOutputs: fc.MissedProofOutputs,
			UnlockHash:         fc.UnlockHash,
			RevisionNumber: 1,
		},
	}
	h = types.NewHasher()
	noOpRevision.EncodeTo(h.E)
	renterNoOpSig := renterKey.SignHash(h.Sum())
	renterNoOpRevisionSignature := types.TransactionSignature{
		ParentID:       types.Hash256(noOpRevision.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
		Signature: renterNoOpSig[:],
	}

	// Send the newly added signatures to the host and the signature for the
	// initial no-op revision.
	rs := rhpv3.RPCRenewSignatures{
		TransactionSignatures: txn.Signatures[2:],
		RevisionSignature:     renterNoOpRevisionSignature,
	}
	if err = s.WriteResponse(&rs); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to send RPCRenewSignatures")
	}

	// Receive the host's signatures.
	var hostSigs rhpv3.RPCRenewSignatures
	if err = s.ReadResponse(&hostSigs, 65536); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to read RPCRenewSignatures")
	}
	txn.Signatures = append(txn.Signatures, hostSigs.TransactionSignatures...)

	// Add the parents to get the full txnSet.
	txnSet = append(parents, txn)

	return rhpv2.ContractRevision{
		Revision:   noOpRevision,
		Signatures: [2]types.TransactionSignature{renterNoOpRevisionSignature, hostSigs.RevisionSignature},
	}, txnSet, nil
}

// RPCTrustlessRenewContract negotiates a contract renewal with the host
// using the new Renter-Satellite protocol.
func RPCTrustlessRenewContract(ctx context.Context, ss *modules.RPCSession, t *rhpv3.Transport, txnSet []types.Transaction, toSign []types.Hash256, ts transactionSigner) (_ rhpv2.ContractRevision, _ []types.Transaction, err error) {
	s := t.DialStream()
	defer s.Close()
	s.SetDeadline(time.Now().Add(5 * time.Minute))

	// Send an empty price table uid.
	var pt rhpv3.HostPriceTable
	if err := s.WriteRequest(rhpv3.RPCRenewContractID, &pt.UID); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to write price table uid")
	}

	// The price table we sent contained a zero uid so we receive a
	// temporary one.
	var ptr rhpv3.RPCUpdatePriceTableResponse
	if err = s.ReadResponse(&ptr, 8192); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to fetch temporary price table")
	}
	err = json.Unmarshal(ptr.PriceTableJSON, &pt)
	if err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to unmarshal temporary price table")
	}

	// Calculate the revision hash and send it to the renter.
	parents, txn := txnSet[:len(txnSet) - 1], txnSet[len(txnSet) - 1]
	fc := txn.FileContracts[0]
	rev := txn.FileContractRevisions[0]
	sr := signRequest{
		RevisionHash: hashContractAndRevision(fc, rev),
	}
	if err := ss.WriteResponse(&sr); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to send final revision to renter")
	}

	// Read the renter signature.
	var srr signResponse
	if err := ss.ReadResponse(&srr, 65536); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to read renter signature")
	}

	// Send the request.
	req := rhpv3.RPCRenewContractRequest{
		TransactionSet:         txnSet,
		RenterKey:              rev.UnlockConditions.PublicKeys[0],
		FinalRevisionSignature: srr.Signature,
	}
	if err := s.WriteResponse(&req); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to write RPCRenewContractRequest")
	}

	// Read the response. It contains the host's final revision sig and any
	// additions it made.
	var hostAdditions rhpv3.RPCRenewContractHostAdditions
	if err := s.ReadResponse(&hostAdditions, 65536); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to read RPCRenewContractHostAdditions")
	}

	// Incorporate host's modifications.
	parents = append(parents, hostAdditions.Parents...)
	txn.SiacoinInputs = append(txn.SiacoinInputs, hostAdditions.SiacoinInputs...)
	txn.SiacoinOutputs = append(txn.SiacoinOutputs, hostAdditions.SiacoinOutputs...)

	finalRevRenterSig := types.TransactionSignature{
		ParentID:       types.Hash256(rev.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: srr.Signature[:],
	}
	finalRevHostSig := types.TransactionSignature{
		ParentID:       types.Hash256(rev.ParentID),
		PublicKeyIndex: 1,
		CoveredFields:  types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: hostAdditions.FinalRevisionSignature[:],
	}

	// Add the revision signatures to the transaction set and sign it.
	txn.Signatures = []types.TransactionSignature{finalRevRenterSig, finalRevHostSig}
	cf := types.CoveredFields{
		WholeTransaction: true,
		Signatures:       []uint64{0, 1},
	}
	if err := ts.Sign(&txn, toSign, cf); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to sign transaction")
	}
	
	// Create initial (no-op) revision.
	noOpRevision := types.FileContractRevision{
		ParentID:          txn.FileContractID(0),
		UnlockConditions:  types.UnlockConditions{
			PublicKeys: rev.UnlockConditions.PublicKeys,
			SignaturesRequired: 2,
		},
		FileContract: types.FileContract{
			Filesize:           fc.Filesize,
			FileMerkleRoot:     fc.FileMerkleRoot,
			WindowStart:        fc.WindowStart,
			WindowEnd:          fc.WindowEnd,
			ValidProofOutputs:  fc.ValidProofOutputs,
			MissedProofOutputs: fc.MissedProofOutputs,
			UnlockHash:         fc.UnlockHash,
			RevisionNumber: 1,
		},
	}

	renterNoOpRevisionSignature := types.TransactionSignature{
		ParentID:       types.Hash256(noOpRevision.ParentID),
		PublicKeyIndex: 0,
		CoveredFields:  types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
	}

	// Calculate the revision hash and send it to the renter.
	sr = signRequest{
		RevisionHash: hashRevision(noOpRevision),
	}
	if err := ss.WriteResponse(&sr); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to send initial revision to renter")
	}

	// Read the renter signature.
	var srrNoOpRev signResponse
	if err := ss.ReadResponse(&srrNoOpRev, 65536); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to read renter signature")
	}

	// Send transaction signatures and no-op revision signature to host.
	renterNoOpRevisionSignature.Signature = srrNoOpRev.Signature[:]
	renterSigs := rhpv3.RPCRenewSignatures{
		TransactionSignatures: txn.Signatures[2:],
		RevisionSignature:     renterNoOpRevisionSignature,
	}
	if err := s.WriteResponse(&renterSigs); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to send RPCRenewSignatures to host")
	}

	// Read the host's signatures and add them to the transaction.
	var hostSigs rhpv3.RPCRenewSignatures
	if err := s.ReadResponse(&hostSigs, 65536); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to read RPCRenewSignatures from host")
	}

	txn.Signatures = append(txn.Signatures, hostSigs.TransactionSignatures...)
	txnSet = append(parents, txn)

	return rhpv2.ContractRevision{
		Revision:   noOpRevision,
		Signatures: [2]types.TransactionSignature{renterNoOpRevisionSignature, hostSigs.RevisionSignature},
	}, txnSet, nil
}

// RPCLatestRevision fetches the latest revision from the host.
func RPCLatestRevision(ctx context.Context, t *rhpv3.Transport, contractID types.FileContractID) (types.FileContractRevision, error) {
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

// hashRevision calculates the revision hash to be signed with the
// renter's private key.
func hashRevision(rev types.FileContractRevision) types.Hash256 {
	h := types.NewHasher()
	rev.EncodeTo(h.E)
	return h.Sum()
}

// hashContractAndRevision calculates the hash of the file contract and
// the revision to be signed with the renter's private key.
func hashContractAndRevision(fc types.FileContract, fcr types.FileContractRevision) types.Hash256 {
	h := types.NewHasher()
	fc.EncodeTo(h.E)
	fcr.EncodeTo(h.E)
	return h.Sum()
}
