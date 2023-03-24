package proto

import (
	"github.com/mike76-dev/sia-satellite/modules"

	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
)

type (
	// rpcFormContractRequest contains the request parameters for the
	// FormContract and RenewContract RPCs.
	rpcFormContractRequest struct {
		Transactions []types.Transaction
		RenterKey    types.SiaPublicKey
	}

	// rpcFormContractAdditions contains the parent transaction, inputs,
	// and outputs added by the host when negotiating a file contract.
	rpcFormContractAdditions struct {
		Parents []types.Transaction
		Inputs  []types.SiacoinInput
		Outputs []types.SiacoinOutput
	}

	// rpcFormContractSignatures contains the signatures for a contract
	// transaction and initial revision. These signatures are sent by both
	// the renter and the host during contract formation and renewal.
	rpcFormContractSignatures struct {
		ContractSignatures []types.TransactionSignature
		RevisionSignature  types.TransactionSignature
	}
)

// EncodeTo implements ProtocolObject.
func (r *rpcFormContractRequest) EncodeTo(e *core.Encoder) {
	e.WritePrefix(len(r.Transactions))
	for i := range r.Transactions {
		encodeTransaction(r.Transactions[i], e)
	}
	encodeSiaPublicKey(r.RenterKey, e)
}

// DecodeFrom implements ProtocolObject.
func (r *rpcFormContractRequest) DecodeFrom(d *core.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements ProtocolObject.
func (r *rpcFormContractAdditions) EncodeTo(e *core.Encoder) {
	// Nothing to do here.
}

// DecodeFrom implements ProtocolObject.
func (r *rpcFormContractAdditions) DecodeFrom(d *core.Decoder) {
	r.Parents = make([]types.Transaction, d.ReadPrefix())
	for i := range r.Parents {
		r.Parents[i] = decodeTransaction(d)
	}
	r.Inputs = make([]types.SiacoinInput, d.ReadPrefix())
	for i := range r.Inputs {
		r.Inputs[i] = decodeSiacoinInput(d)
	}
	r.Outputs = make([]types.SiacoinOutput, d.ReadPrefix())
	for i := range r.Outputs {
		r.Outputs[i] = decodeSiacoinOutput(d)
	}
}

// EncodeTo implements ProtocolObject.
func (r *rpcFormContractSignatures) EncodeTo(e *core.Encoder) {
	e.WritePrefix(len(r.ContractSignatures))
	for i := range r.ContractSignatures {
		encodeSignature(r.ContractSignatures[i], e)
	}
	encodeSignature(r.RevisionSignature, e)
}

// DecodeFrom implements ProtocolObject.
func (r *rpcFormContractSignatures) DecodeFrom(d *core.Decoder) {
	r.ContractSignatures = make([]types.TransactionSignature, d.ReadPrefix())
	for i := range r.ContractSignatures {
		r.ContractSignatures[i] = decodeSignature(d)
	}
	r.RevisionSignature = decodeSignature(d)
}

func encodeSiaPublicKey(spk types.SiaPublicKey, e *core.Encoder) {
	e.Write(spk.Algorithm[:])
	e.WriteBytes(spk.Key)
}

func decodeSiaPublicKey(d *core.Decoder) types.SiaPublicKey {
	var spk types.SiaPublicKey
	d.Read(spk.Algorithm[:])
	spk.Key = d.ReadBytes()
	return spk
}

func encodeSignature(ts types.TransactionSignature, e *core.Encoder) {
	e.Write(ts.ParentID[:])
	e.WriteUint64(ts.PublicKeyIndex)
	e.WriteUint64(uint64(ts.Timelock))
	encodeCoveredFields(ts.CoveredFields, e)
	e.WriteBytes(ts.Signature)
}

func decodeSignature(d *core.Decoder) types.TransactionSignature {
	var ts types.TransactionSignature
	d.Read(ts.ParentID[:])
	ts.PublicKeyIndex = d.ReadUint64()
	ts.Timelock = types.BlockHeight(d.ReadUint64())
	ts.CoveredFields = decodeCoveredFields(d)
	ts.Signature = d.ReadBytes()
	return ts
}

func encodeTransaction(txn types.Transaction, e *core.Encoder) {
	encodeNoSignatures(txn, e)
	e.WritePrefix(len((txn.TransactionSignatures)))
	for i := range txn.TransactionSignatures {
		encodeSignature(txn.TransactionSignatures[i], e)
	}
}

func encodeNoSignatures(txn types.Transaction, e *core.Encoder) {
	e.WritePrefix(len((txn.SiacoinInputs)))
	for i := range txn.SiacoinInputs {
		encodeSiacoinInput(txn.SiacoinInputs[i], e)
	}
	e.WritePrefix(len((txn.SiacoinOutputs)))
	for i := range txn.SiacoinOutputs {
		encodeSiacoinOutput(txn.SiacoinOutputs[i], e)
	}
	e.WritePrefix(len((txn.FileContracts)))
	for i := range txn.FileContracts {
		encodeFileContract(txn.FileContracts[i], e)
	}
	e.WritePrefix(len((txn.FileContractRevisions)))
	for i := range txn.FileContractRevisions {
		encodeFileContractRevision(txn.FileContractRevisions[i], e)
	}
	e.WritePrefix(len((txn.StorageProofs)))
	for i := range txn.StorageProofs {
		encodeStorageProof(txn.StorageProofs[i], e)
	}
	e.WritePrefix(len((txn.SiafundInputs)))
	for i := range txn.SiafundInputs {
		encodeSiafundInput(txn.SiafundInputs[i], e)
	}
	e.WritePrefix(len((txn.SiafundOutputs)))
	for i := range txn.SiafundOutputs {
		encodeSiafundOutput(txn.SiafundOutputs[i], e)
	}
	e.WritePrefix(len((txn.MinerFees)))
	for i := range txn.MinerFees {
		modules.ConvertCurrency(txn.MinerFees[i]).EncodeTo(e)
	}
	e.WritePrefix(len((txn.ArbitraryData)))
	for i := range txn.ArbitraryData {
		e.WriteBytes(txn.ArbitraryData[i])
	}
}

func decodeTransaction(d *core.Decoder) types.Transaction {
	var txn types.Transaction
	txn.SiacoinInputs = make([]types.SiacoinInput, d.ReadPrefix())
	for i := range txn.SiacoinInputs {
		txn.SiacoinInputs[i] = decodeSiacoinInput(d)
	}
	txn.SiacoinOutputs = make([]types.SiacoinOutput, d.ReadPrefix())
	for i := range txn.SiacoinOutputs {
		txn.SiacoinOutputs[i] = decodeSiacoinOutput(d)
	}
	txn.FileContracts = make([]types.FileContract, d.ReadPrefix())
	for i := range txn.FileContracts {
		txn.FileContracts[i] = decodeFileContract(d)
	}
	txn.FileContractRevisions = make([]types.FileContractRevision, d.ReadPrefix())
	for i := range txn.FileContractRevisions {
		txn.FileContractRevisions[i] = decodeFileContractRevision(d)
	}
	txn.StorageProofs = make([]types.StorageProof, d.ReadPrefix())
	for i := range txn.StorageProofs {
		txn.StorageProofs[i] = decodeStorageProof(d)
	}
	txn.SiafundInputs = make([]types.SiafundInput, d.ReadPrefix())
	for i := range txn.SiafundInputs {
		txn.SiafundInputs[i] = decodeSiafundInput(d)
	}
	txn.SiafundOutputs = make([]types.SiafundOutput, d.ReadPrefix())
	for i := range txn.SiafundOutputs {
		txn.SiafundOutputs[i] = decodeSiafundOutput(d)
	}
	txn.MinerFees = make([]types.Currency, d.ReadPrefix())
	var mf core.Currency
	for i := range txn.MinerFees {
		mf.DecodeFrom(d)
		txn.MinerFees[i] = types.NewCurrency(mf.Big())
	}
	txn.ArbitraryData = make([][]byte, d.ReadPrefix())
	for i := range txn.ArbitraryData {
		txn.ArbitraryData[i] = d.ReadBytes()
	}
	txn.TransactionSignatures = make([]types.TransactionSignature, d.ReadPrefix())
	for i := range txn.TransactionSignatures {
		txn.TransactionSignatures[i] = decodeSignature(d)
	}
	return txn
}

func encodeSiacoinInput(in types.SiacoinInput, e *core.Encoder) {
	e.Write(in.ParentID[:])
	encodeUnlockConditions(in.UnlockConditions, e)
}

func decodeSiacoinInput(d *core.Decoder) types.SiacoinInput {
	var si types.SiacoinInput
	d.Read(si.ParentID[:])
	si.UnlockConditions = decodeUnlockConditions(d)
	return si
}

func encodeSiacoinOutput(sco types.SiacoinOutput, e *core.Encoder) {
	modules.ConvertCurrency(sco.Value).EncodeTo(e)
	e.Write(sco.UnlockHash[:])
}

func decodeSiacoinOutput(d *core.Decoder) types.SiacoinOutput {
	var v core.Currency
	var uh types.UnlockHash
	v.DecodeFrom(d)
	d.Read(uh[:])
	return types.SiacoinOutput{
		Value:      types.NewCurrency(v.Big()),
		UnlockHash: uh,
	}
}

func encodeSiafundInput(in types.SiafundInput, e *core.Encoder) {
	e.Write(in.ParentID[:])
	encodeUnlockConditions(in.UnlockConditions, e)
	e.Write(in.ClaimUnlockHash[:])
}

func decodeSiafundInput(d *core.Decoder) types.SiafundInput {
	var in types.SiafundInput
	d.Read(in.ParentID[:])
	in.UnlockConditions = decodeUnlockConditions(d)
	d.Read(in.ClaimUnlockHash[:])
	return in
}

func encodeSiafundOutput(sfo types.SiafundOutput, e *core.Encoder) {
	modules.ConvertCurrency(sfo.Value).EncodeTo(e)
	e.Write(sfo.UnlockHash[:])
	(core.Currency{}).EncodeTo(e)
}

func decodeSiafundOutput(d *core.Decoder) types.SiafundOutput {
	var sfo types.SiafundOutput
	var v core.Currency
	v.DecodeFrom(d)
	sfo.Value = types.NewCurrency(v.Big())
	d.Read(sfo.UnlockHash[:])
	(&core.Currency{}).DecodeFrom(d)
	return sfo
}

func encodeFileContract(fc types.FileContract, e *core.Encoder) {
	e.WriteUint64(fc.FileSize)
	e.Write(fc.FileMerkleRoot[:])
	e.WriteUint64(uint64(fc.WindowStart))
	e.WriteUint64(uint64(fc.WindowEnd))
	modules.ConvertCurrency(fc.Payout).EncodeTo(e)
	e.WritePrefix(len(fc.ValidProofOutputs))
	for _, sco := range fc.ValidProofOutputs {
		encodeSiacoinOutput(sco, e)
	}
	e.WritePrefix(len(fc.MissedProofOutputs))
	for _, sco := range fc.MissedProofOutputs {
		encodeSiacoinOutput(sco, e)
	}
	e.Write(fc.UnlockHash[:])
	e.WriteUint64(fc.RevisionNumber)
}

func decodeFileContract(d *core.Decoder) types.FileContract {
	var fc types.FileContract
	fc.FileSize = d.ReadUint64()
	d.Read(fc.FileMerkleRoot[:])
	fc.WindowStart = types.BlockHeight(d.ReadUint64())
	fc.WindowEnd = types.BlockHeight(d.ReadUint64())
	var payout core.Currency
	payout.DecodeFrom(d)
	fc.Payout = types.NewCurrency(payout.Big())
	fc.ValidProofOutputs = make([]types.SiacoinOutput, d.ReadPrefix())
	for i := range fc.ValidProofOutputs {
		fc.ValidProofOutputs[i] = decodeSiacoinOutput(d)
	}
	fc.MissedProofOutputs = make([]types.SiacoinOutput, d.ReadPrefix())
	for i := range fc.MissedProofOutputs {
		fc.MissedProofOutputs[i] = decodeSiacoinOutput(d)
	}
	d.Read(fc.UnlockHash[:])
	fc.RevisionNumber = d.ReadUint64()
	return fc
}

func encodeFileContractRevision(rev types.FileContractRevision, e *core.Encoder) {
	e.Write(rev.ParentID[:])
	encodeUnlockConditions(rev.UnlockConditions, e)
	e.WriteUint64(rev.NewRevisionNumber)
	e.WriteUint64(rev.NewFileSize)
	e.Write(rev.NewFileMerkleRoot[:])
	e.WriteUint64(uint64(rev.NewWindowStart))
	e.WriteUint64(uint64(rev.NewWindowEnd))
	e.WritePrefix(len(rev.NewValidProofOutputs))
	for _, sco := range rev.NewValidProofOutputs {
		encodeSiacoinOutput(sco, e)
	}
	e.WritePrefix(len(rev.NewMissedProofOutputs))
	for _, sco := range rev.NewMissedProofOutputs {
		encodeSiacoinOutput(sco, e)
	}
	e.Write(rev.NewUnlockHash[:])
}

func decodeFileContractRevision(d *core.Decoder) types.FileContractRevision {
	var rev types.FileContractRevision
	d.Read(rev.ParentID[:])
	rev.UnlockConditions = decodeUnlockConditions(d)
	rev.NewRevisionNumber = d.ReadUint64()
	rev.NewFileSize = d.ReadUint64()
	d.Read(rev.NewFileMerkleRoot[:])
	rev.NewWindowStart = types.BlockHeight(d.ReadUint64())
	rev.NewWindowEnd = types.BlockHeight(d.ReadUint64())
	rev.NewValidProofOutputs = make([]types.SiacoinOutput, d.ReadPrefix())
	for i := range rev.NewValidProofOutputs {
		rev.NewValidProofOutputs[i] = decodeSiacoinOutput(d)
	}
	rev.NewMissedProofOutputs = make([]types.SiacoinOutput, d.ReadPrefix())
	for i := range rev.NewMissedProofOutputs {
		rev.NewMissedProofOutputs[i] = decodeSiacoinOutput(d)
	}
	d.Read(rev.NewUnlockHash[:])
	return rev
}

func encodeStorageProof(sp types.StorageProof, e *core.Encoder) {
	e.Write(sp.ParentID[:])
	e.Write(sp.Segment[:])
	e.WritePrefix(len(sp.HashSet))
	for _, h := range sp.HashSet {
		e.Write(h[:])
	}
}

func decodeStorageProof(d *core.Decoder) types.StorageProof {
	var sp types.StorageProof
	d.Read(sp.ParentID[:])
	d.Read(sp.Segment[:])
	sp.HashSet = make([]crypto.Hash, d.ReadPrefix())
	for i := range sp.HashSet {
		d.Read(sp.HashSet[i][:])
	}
	return sp
}

func encodeUnlockConditions(uc types.UnlockConditions, e *core.Encoder) {
	e.WriteUint64(uint64(uc.Timelock))
	e.WritePrefix(len(uc.PublicKeys))
	for _, pk := range uc.PublicKeys {
		encodeSiaPublicKey(pk, e)
	}
	e.WriteUint64(uc.SignaturesRequired)
}

func decodeUnlockConditions(d *core.Decoder) types.UnlockConditions {
	var uc types.UnlockConditions
	uc.Timelock = types.BlockHeight(d.ReadUint64())
	uc.PublicKeys = make([]types.SiaPublicKey, d.ReadPrefix())
	for i := range uc.PublicKeys {
		uc.PublicKeys[i] = decodeSiaPublicKey(d)
	}
	uc.SignaturesRequired = d.ReadUint64()
	return uc
}

func encodeCoveredFields(cf types.CoveredFields, e *core.Encoder) {
	e.WriteBool(cf.WholeTransaction)
	for _, f := range [][]uint64{
		cf.SiacoinInputs,
		cf.SiacoinOutputs,
		cf.FileContracts,
		cf.FileContractRevisions,
		cf.StorageProofs,
		cf.SiafundInputs,
		cf.SiafundOutputs,
		cf.MinerFees,
		cf.ArbitraryData,
		cf.TransactionSignatures,
	} {
		e.WritePrefix(len(f))
		for _, i := range f {
			e.WriteUint64(i)
		}
	}
}

func decodeCoveredFields(d *core.Decoder) types.CoveredFields {
	var cf types.CoveredFields
	cf.WholeTransaction = d.ReadBool()
	for _, f := range []*[]uint64{
		&cf.SiacoinInputs,
		&cf.SiacoinOutputs,
		&cf.FileContracts,
		&cf.FileContractRevisions,
		&cf.StorageProofs,
		&cf.SiafundInputs,
		&cf.SiafundOutputs,
		&cf.MinerFees,
		&cf.ArbitraryData,
		&cf.TransactionSignatures,
	} {
		*f = make([]uint64, d.ReadPrefix())
		for i := range *f {
			(*f)[i] = d.ReadUint64()
		}
	}
	return cf
}
