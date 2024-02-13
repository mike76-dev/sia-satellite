package proto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/consensus"
	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
	"go.sia.tech/mux/v1"
)

const (
	// responseLeeway is the amount of leeway given to the maxLen when we read
	// the response in the ReadSector RPC.
	responseLeeway = 1 << 12 // 4 KiB
)

var (
	// errMaxRevisionReached occurs when trying to revise a contract that has
	// already reached the highest possible revision number. Usually happens
	// when trying to use a renewed contract.
	errMaxRevisionReached = errors.New("contract has reached the maximum number of revisions")
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
func RPCRenewContract(ctx context.Context, t *rhpv3.Transport, renterKey types.PrivateKey, rev types.FileContractRevision, txnSet []types.Transaction, toSign []types.Hash256, ts transactionSigner, cs consensus.State) (_ rhpv2.ContractRevision, _ []types.Transaction, err error) {
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
	parents, txn := txnSet[:len(txnSet)-1], txnSet[len(txnSet)-1]
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

	// Sign the inputs we funded the txn with.
	if err := ts.Sign(cs, &txn, toSign); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to sign transaction")
	}

	// Create a new no-op revision and sign it.
	fc := txn.FileContracts[0]
	noOpRevision := types.FileContractRevision{
		ParentID: txn.FileContractID(0),
		UnlockConditions: types.UnlockConditions{
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
			RevisionNumber:     1,
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
func RPCTrustlessRenewContract(ctx context.Context, ss *modules.RPCSession, t *rhpv3.Transport, txnSet []types.Transaction, toSign []types.Hash256, ts transactionSigner, cs consensus.State) (_ rhpv2.ContractRevision, _ []types.Transaction, err error) {
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
	parents, txn := txnSet[:len(txnSet)-1], txnSet[len(txnSet)-1]
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
		CoveredFields: types.CoveredFields{
			FileContracts:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
		Signature: hostAdditions.FinalRevisionSignature[:],
	}

	// Add the revision signatures to the transaction set and sign it.
	txn.Signatures = []types.TransactionSignature{finalRevRenterSig, finalRevHostSig}
	if err := ts.Sign(cs, &txn, toSign); err != nil {
		return rhpv2.ContractRevision{}, nil, modules.AddContext(err, "failed to sign transaction")
	}

	// Create initial (no-op) revision.
	noOpRevision := types.FileContractRevision{
		ParentID: txn.FileContractID(0),
		UnlockConditions: types.UnlockConditions{
			PublicKeys:         rev.UnlockConditions.PublicKeys,
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
			RevisionNumber:     1,
		},
	}

	renterNoOpRevisionSignature := types.TransactionSignature{
		ParentID:       types.Hash256(noOpRevision.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
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

// RPCAccountBalance fetches the balance of an ephemeral account.
func RPCAccountBalance(ctx context.Context, t *rhpv3.Transport, payment rhpv3.PaymentMethod, account rhpv3.Account, settingsID rhpv3.SettingsID) (bal types.Currency, err error) {
	s := t.DialStream()
	defer s.Close()

	req := rhpv3.RPCAccountBalanceRequest{
		Account: account,
	}
	var resp rhpv3.RPCAccountBalanceResponse
	if err := s.WriteRequest(rhpv3.RPCAccountBalanceID, &settingsID); err != nil {
		return types.ZeroCurrency, err
	} else if err := processPayment(s, payment); err != nil {
		return types.ZeroCurrency, err
	} else if err := s.WriteResponse(&req); err != nil {
		return types.ZeroCurrency, err
	} else if err := s.ReadResponse(&resp, 128); err != nil {
		return types.ZeroCurrency, err
	}
	return resp.Balance, nil
}

// RPCFundAccount funds an ephemeral account.
func RPCFundAccount(ctx context.Context, t *rhpv3.Transport, payment rhpv3.PaymentMethod, account rhpv3.Account, settingsID rhpv3.SettingsID) (err error) {
	s := t.DialStream()
	defer s.Close()

	req := rhpv3.RPCFundAccountRequest{
		Account: account,
	}
	var resp rhpv3.RPCFundAccountResponse
	if err := s.WriteRequest(rhpv3.RPCFundAccountID, &settingsID); err != nil {
		return err
	} else if err := s.WriteResponse(&req); err != nil {
		return err
	} else if err := processPayment(s, payment); err != nil {
		return err
	} else if err := s.ReadResponse(&resp, 65536); err != nil {
		return err
	}
	return nil
}

// RPCReadSector calls the ExecuteProgram RPC with a ReadSector instruction.
func RPCReadSector(ctx context.Context, t *rhpv3.Transport, w io.Writer, pt rhpv3.HostPriceTable, payment rhpv3.PaymentMethod, offset, length uint32, merkleRoot types.Hash256) (cost, refund types.Currency, err error) {
	s := t.DialStream()
	defer s.Close()

	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	e.WriteUint64(uint64(length))
	e.WriteUint64(uint64(offset))
	merkleRoot.EncodeTo(e)
	e.Flush()

	req := rhpv3.RPCExecuteProgramRequest{
		FileContractID: types.FileContractID{},
		Program: []rhpv3.Instruction{&rhpv3.InstrReadSector{
			LengthOffset:     0,
			OffsetOffset:     8,
			MerkleRootOffset: 16,
			ProofRequired:    true,
		}},
		ProgramData: buf.Bytes(),
	}

	var cancellationToken types.Specifier
	var resp rhpv3.RPCExecuteProgramResponse
	if err = s.WriteRequest(rhpv3.RPCExecuteProgramID, &pt.UID); err != nil {
		return
	} else if err = processPayment(s, payment); err != nil {
		return
	} else if err = s.WriteResponse(&req); err != nil {
		return
	} else if err = s.ReadResponse(&cancellationToken, 16); err != nil {
		return
	} else if err = s.ReadResponse(&resp, rhpv2.SectorSize+responseLeeway); err != nil {
		return
	}

	// Check response error.
	if err = resp.Error; err != nil {
		refund = resp.FailureRefund
		return
	}
	cost = resp.TotalCost

	// Verify proof.
	proofStart := int(offset) / modules.SegmentSize
	proofEnd := int(offset+length) / modules.SegmentSize
	if !modules.VerifyRangeProof(resp.Output, resp.Proof, proofStart, proofEnd, merkleRoot) {
		err = errors.New("proof verification failed")
		return
	}

	_, err = w.Write(resp.Output)
	return
}

// RPCAppendSector calls the ExecuteProgram RPC with an AppendSector instruction.
func RPCAppendSector(ctx context.Context, t *rhpv3.Transport, renterKey types.PrivateKey, pt rhpv3.HostPriceTable, rev types.FileContractRevision, payment rhpv3.PaymentMethod, sector *[rhpv2.SectorSize]byte) (sectorRoot types.Hash256, cost types.Currency, err error) {
	// Sanity check revision first.
	if rev.RevisionNumber == math.MaxUint64 {
		return types.Hash256{}, types.ZeroCurrency, errMaxRevisionReached
	}

	s := t.DialStream()
	defer s.Close()

	req := rhpv3.RPCExecuteProgramRequest{
		FileContractID: rev.ParentID,
		Program: []rhpv3.Instruction{&rhpv3.InstrAppendSector{
			SectorDataOffset: 0,
			ProofRequired:    true,
		}},
		ProgramData: (*sector)[:],
	}

	var cancellationToken types.Specifier
	var executeResp rhpv3.RPCExecuteProgramResponse
	if err = s.WriteRequest(rhpv3.RPCExecuteProgramID, &pt.UID); err != nil {
		return
	} else if err = processPayment(s, payment); err != nil {
		return
	} else if err = s.WriteResponse(&req); err != nil {
		return
	} else if err = s.ReadResponse(&cancellationToken, 16); err != nil {
		return
	} else if err = s.ReadResponse(&executeResp, 65536); err != nil {
		return
	}

	// Compute expected collateral and refund.
	expectedCost, expectedCollateral, expectedRefund, err := UploadSectorCost(pt, rev.WindowEnd)
	if err != nil {
		return types.Hash256{}, types.ZeroCurrency, err
	}

	// Apply leeways.
	// TODO: remove once most hosts use hostd. Then we can check for exact values.
	expectedCollateral = expectedCollateral.Mul64(9).Div64(10)
	expectedCost = expectedCost.Mul64(11).Div64(10)
	expectedRefund = expectedRefund.Mul64(9).Div64(10)

	// Check if the cost, collateral and refund match our expectation.
	if executeResp.TotalCost.Cmp(expectedCost) > 0 {
		return types.Hash256{}, types.ZeroCurrency, fmt.Errorf("cost exceeds expectation: %v > %v", executeResp.TotalCost, expectedCost)
	}
	if executeResp.FailureRefund.Cmp(expectedRefund) < 0 {
		return types.Hash256{}, types.ZeroCurrency, fmt.Errorf("insufficient refund: %v < %v", executeResp.FailureRefund, expectedRefund)
	}
	if executeResp.AdditionalCollateral.Cmp(expectedCollateral) < 0 {
		return types.Hash256{}, types.ZeroCurrency, fmt.Errorf("insufficient collateral: %v < %v", executeResp.AdditionalCollateral, expectedCollateral)
	}

	// Set the cost and refund.
	cost = executeResp.TotalCost
	defer func() {
		if err != nil {
			cost = types.ZeroCurrency
			if executeResp.FailureRefund.Cmp(cost) < 0 {
				cost = cost.Sub(executeResp.FailureRefund)
			}
		}
	}()

	// Check response error.
	if err = executeResp.Error; err != nil {
		return
	}
	cost = executeResp.TotalCost

	// Include the refund in the collateral.
	collateral := executeResp.AdditionalCollateral.Add(executeResp.FailureRefund)

	// Check proof.
	sectorRoot = rhpv2.SectorRoot(sector)
	if rev.Filesize == 0 {
		// For the first upload to a contract we don't get a proof. So we just
		// assert that the new contract root matches the root of the sector.
		if rev.Filesize == 0 && executeResp.NewMerkleRoot != sectorRoot {
			return types.Hash256{}, types.ZeroCurrency, fmt.Errorf("merkle root doesn't match the sector root upon first upload to contract: %v != %v", executeResp.NewMerkleRoot, sectorRoot)
		}
	} else {
		// Otherwise we make sure the proof was transmitted and verify it.
		actions := []rhpv2.RPCWriteAction{{Type: rhpv2.RPCWriteActionAppend}} // TODO: change once rhpv3 support is available.
		if !rhpv2.VerifyDiffProof(actions, rev.Filesize/rhpv2.SectorSize, executeResp.Proof, []types.Hash256{}, rev.FileMerkleRoot, executeResp.NewMerkleRoot, []types.Hash256{sectorRoot}) {
			return types.Hash256{}, types.ZeroCurrency, errors.New("proof verification failed")
		}
	}

	// Finalize the program with a new revision.
	newRevision := rev
	newValid, newMissed, err := updateRevisionOutputs(&newRevision, types.ZeroCurrency, collateral)
	if err != nil {
		return types.Hash256{}, types.ZeroCurrency, err
	}
	newRevision.Filesize += rhpv2.SectorSize
	newRevision.RevisionNumber++
	newRevision.FileMerkleRoot = executeResp.NewMerkleRoot

	finalizeReq := rhpv3.RPCFinalizeProgramRequest{
		Signature:         renterKey.SignHash(hashRevision(newRevision)),
		ValidProofValues:  newValid,
		MissedProofValues: newMissed,
		RevisionNumber:    newRevision.RevisionNumber,
	}

	var finalizeResp rhpv3.RPCFinalizeProgramResponse
	if err = s.WriteResponse(&finalizeReq); err != nil {
		return
	} else if err = s.ReadResponse(&finalizeResp, 64); err != nil {
		return
	}

	// Read one more time to receive a potential error in case finalising the
	// contract fails after receiving the RPCFinalizeProgramResponse. This also
	// guarantees that the program is finalised before we return.
	// TODO: remove once most hosts use hostd.
	errFinalise := s.ReadResponse(&finalizeResp, 64)
	if errFinalise != nil &&
		!errors.Is(errFinalise, io.EOF) &&
		!errors.Is(errFinalise, mux.ErrClosedConn) &&
		!errors.Is(errFinalise, mux.ErrClosedStream) &&
		!errors.Is(errFinalise, mux.ErrPeerClosedStream) &&
		!errors.Is(errFinalise, mux.ErrPeerClosedConn) {
		err = errFinalise
		return
	}
	return
}

// padBandwitdh pads the bandwidth to the next multiple of 1460 bytes.  1460
// bytes is the maximum size of a TCP packet when using IPv4.
// TODO: once hostd becomes the only host implementation we can simplify this.
func padBandwidth(pt rhpv3.HostPriceTable, rc rhpv3.ResourceCost) rhpv3.ResourceCost {
	padCost := func(cost, paddingSize types.Currency) types.Currency {
		if paddingSize.IsZero() {
			return cost // Might happen if bandwidth is free.
		}
		return cost.Add(paddingSize).Sub(types.NewCurrency64(1)).Div(paddingSize).Mul(paddingSize)
	}
	minPacketSize := uint64(1460)
	minIngress := pt.UploadBandwidthCost.Mul64(minPacketSize)
	minEgress := pt.DownloadBandwidthCost.Mul64(3*minPacketSize + responseLeeway)
	rc.Ingress = padCost(rc.Ingress, minIngress)
	rc.Egress = padCost(rc.Egress, minEgress)
	return rc
}

// ReadSectorCost returns an overestimate for the cost of reading a sector from a host.
func ReadSectorCost(pt rhpv3.HostPriceTable, length uint64) (types.Currency, error) {
	rc := pt.BaseCost()
	rc = rc.Add(pt.ReadSectorCost(length))
	rc = padBandwidth(pt, rc)
	cost, _ := rc.Total()

	// Overestimate the cost by 10%.
	cost, overflow := cost.Mul64WithOverflow(11)
	if overflow {
		return types.ZeroCurrency, errors.New("overflow occurred while adding leeway to read sector cost")
	}
	return cost.Div64(10), nil
}

// UploadSectorCost returns an overestimate for the cost of uploading a sector
// to a host.
func UploadSectorCost(pt rhpv3.HostPriceTable, windowEnd uint64) (cost, collateral, storage types.Currency, _ error) {
	rc := pt.BaseCost()
	rc = rc.Add(pt.AppendSectorCost(windowEnd - pt.HostBlockHeight))
	rc = padBandwidth(pt, rc)
	cost, collateral = rc.Total()

	// Overestimate the cost by 10%.
	cost, overflow := cost.Mul64WithOverflow(11)
	if overflow {
		return types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, errors.New("overflow occurred while adding leeway to read sector cost")
	}
	return cost.Div64(10), collateral, rc.Storage, nil
}
