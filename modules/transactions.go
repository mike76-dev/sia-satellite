package modules

import (
	"bytes"
	"errors"
	"io"

	"go.sia.tech/core/types"
)

var (
	// ErrInvalidSignature is returned if a signature is provided that does not
	// match the data and public key.
	ErrInvalidSignature = errors.New("invalid signature")
	// ErrEntropyKey is the error when a transaction tries to sign an entropy
	// public key.
	ErrEntropyKey = errors.New("transaction tries to sign an entropy public key")
	// ErrFrivolousSignature is the error when a transaction contains a frivolous
	// signature.
	ErrFrivolousSignature = errors.New("transaction contains a frivolous signature")
	// ErrInvalidPubKeyIndex is the error when a transaction contains a signature
	// that points to a nonexistent public key.
	ErrInvalidPubKeyIndex = errors.New("transaction contains a signature that points to a nonexistent public key")
	// ErrInvalidUnlockHashChecksum is the error when the provided unlock hash has
	// an invalid checksum.
	ErrInvalidUnlockHashChecksum = errors.New("provided unlock hash has an invalid checksum")
	// ErrMissingSignatures is the error when a transaction has inputs with missing
	// signatures.
	ErrMissingSignatures = errors.New("transaction has inputs with missing signatures")
	// ErrPrematureSignature is the error when the timelock on signature has not
	// expired.
	ErrPrematureSignature = errors.New("timelock on signature has not expired")
	// ErrPublicKeyOveruse is the error when public key was used multiple times while
	// signing transaction.
	ErrPublicKeyOveruse = errors.New("public key was used multiple times while signing transaction")
	// ErrSortedUniqueViolation is the error when a sorted unique violation occurs.
	ErrSortedUniqueViolation = errors.New("sorted unique violation")
	// ErrUnlockHashWrongLen is the error when a marshalled unlock hash is the wrong
	// length.
	ErrUnlockHashWrongLen = errors.New("marshalled unlock hash is the wrong length")
	// ErrWholeTransactionViolation is the error when there's a covered fields violation.
	ErrWholeTransactionViolation = errors.New("covered fields violation")
	// ErrDoubleSpend is an error when a transaction uses a parent object
	// twice.
	ErrDoubleSpend = errors.New("transaction uses a parent object twice")
	// ErrFileContractOutputSumViolation is an error when a file contract
	// has invalid output sums.
	ErrFileContractOutputSumViolation = errors.New("file contract has invalid output sums")
	// ErrFileContractWindowEndViolation is an error when a file contract
	// window must end at least one block after it starts.
	ErrFileContractWindowEndViolation = errors.New("file contract window must end at least one block after it starts")
	// ErrFileContractWindowStartViolation is an error when a file contract
	// window must start in the future.
	ErrFileContractWindowStartViolation = errors.New("file contract window must start in the future")
	// ErrNonZeroClaimStart is an error when a transaction has a siafund
	// output with a non-zero Siafund claim.
	ErrNonZeroClaimStart = errors.New("transaction has a siafund output with a non-zero siafund claim")
	// ErrNonZeroRevision is an error when a new file contract has a
	// nonzero revision number.
	ErrNonZeroRevision = errors.New("new file contract has a nonzero revision number")
	// ErrStorageProofWithOutputs is an error when a transaction has both
	// a storage proof and other outputs.
	ErrStorageProofWithOutputs = errors.New("transaction has both a storage proof and other outputs")
	// ErrTimelockNotSatisfied is an error when a timelock has not been met.
	ErrTimelockNotSatisfied = errors.New("timelock has not been met")
	// ErrTransactionTooLarge is an error when a transaction is too large
	// to fit in a block.
	ErrTransactionTooLarge = errors.New("transaction is too large to fit in a block")
	// ErrZeroMinerFee is an error when a transaction has a zero value miner
	// fee.
	ErrZeroMinerFee = errors.New("transaction has a zero value miner fee")
	// ErrZeroOutput is an error when a transaction cannot have an output
	// or payout that has zero value.
	ErrZeroOutput = errors.New("transaction cannot have an output or payout that has zero value")
	// ErrZeroRevision is an error when a transaction has a file contract
	// revision with RevisionNumber=0.
	ErrZeroRevision = errors.New("transaction has a file contract revision with RevisionNumber=0")
	// ErrInvalidFoundationUpdateEncoding is returned when a transaction
	// contains an improperly-encoded FoundationUnlockHashUpdate.
	ErrInvalidFoundationUpdateEncoding = errors.New("transaction contains an improperly-encoded FoundationUnlockHashUpdate")
	// ErrUninitializedFoundationUpdate is returned when a transaction contains
	// an uninitialized FoundationUnlockHashUpdate. To prevent accidental
	// misuse, updates cannot set the Foundation addresses to the empty ("void")
	// UnlockHash.
	ErrUninitializedFoundationUpdate = errors.New("transaction contains an uninitialized FoundationUnlockHashUpdate")

	// These Specifiers enumerate the types of signatures that are recognized
	// by this implementation. If a signature's type is unrecognized, the
	// signature is treated as valid. Signatures using the special "entropy"
	// type are always treated as invalid; see Consensus.md for more details.

	// SignatureEd25519 is a specifier for Ed22519
	SignatureEd25519 = types.NewSpecifier("ed25519")
	// SignatureEntropy is a specifier for entropy
	SignatureEntropy = types.NewSpecifier("entropy")
)

// SiacoinOutputSum returns the sum of all the Siacoin outputs in the
// transaction, which must match the sum of all the Siacoin inputs. Siacoin
// outputs created by storage proofs and Siafund outputs are not considered, as
// they were considered when the contract responsible for funding them was
// created.
func SiacoinOutputSum(t types.Transaction) (sum types.Currency) {
	// Add the siacoin outputs.
	for _, sco := range t.SiacoinOutputs {
		sum = sum.Add(sco.Value)
	}

	// Add the file contract payouts.
	for _, fc := range t.FileContracts {
		sum = sum.Add(fc.Payout)
	}

	// Add the miner fees.
	for _, fee := range t.MinerFees {
		sum = sum.Add(fee)
	}

	return
}

// correctFileContracts checks that the file contracts adhere to the file
// contract rules.
func correctFileContracts(t types.Transaction, currentHeight uint64) error {
	// Check that FileContract rules are being followed.
	for _, fc := range t.FileContracts {
		// Check that start and expiration are reasonable values.
		if fc.WindowStart <= currentHeight {
			return ErrFileContractWindowStartViolation
		}
		if fc.WindowEnd <= fc.WindowStart {
			return ErrFileContractWindowEndViolation
		}

		// Check that the proof outputs sum to the payout after the
		// Siafund fee has been applied.
		var validProofOutputSum, missedProofOutputSum types.Currency
		for _, output := range fc.ValidProofOutputs {
			validProofOutputSum = validProofOutputSum.Add(output.Value)
		}
		for _, output := range fc.MissedProofOutputs {
			missedProofOutputSum = missedProofOutputSum.Add(output.Value)
		}
		outputPortion := PostTax(currentHeight, fc.Payout)
		if validProofOutputSum.Cmp(outputPortion) != 0 {
			return ErrFileContractOutputSumViolation
		}
		if missedProofOutputSum.Cmp(outputPortion) != 0 {
			return ErrFileContractOutputSumViolation
		}
	}
	return nil
}

// correctFileContractRevisions checks that any file contract revisions adhere
// to the revision rules.
func correctFileContractRevisions(t types.Transaction, currentHeight uint64) error {
	for _, fcr := range t.FileContractRevisions {
		// Check that start and expiration are reasonable values.
		if fcr.WindowStart <= currentHeight {
			return ErrFileContractWindowStartViolation
		}
		if fcr.WindowEnd <= fcr.WindowStart {
			return ErrFileContractWindowEndViolation
		}

		// Check that the valid outputs and missed outputs sum to the same
		// value.
		var validProofOutputSum, missedProofOutputSum types.Currency
		for _, output := range fcr.ValidProofOutputs {
			validProofOutputSum = validProofOutputSum.Add(output.Value)
		}
		for _, output := range fcr.MissedProofOutputs {
			missedProofOutputSum = missedProofOutputSum.Add(output.Value)
		}
		if validProofOutputSum.Cmp(missedProofOutputSum) != 0 {
			return ErrFileContractOutputSumViolation
		}
	}
	return nil
}

// correctArbitraryData checks that any consensus-recognized ArbitraryData
// values are correctly encoded.
func correctArbitraryData(t types.Transaction, currentHeight uint64) error {
	if currentHeight < FoundationHardforkHeight {
		return nil
	}
	for _, arb := range t.ArbitraryData {
		if bytes.HasPrefix(arb, types.SpecifierFoundation[:]) {
			var update types.FoundationAddressUpdate
			buf := bytes.NewBuffer(arb[16:])
			d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(arb) - 16)})
			update.DecodeFrom(d)
			if err := d.Err(); err != nil {
				return ErrInvalidFoundationUpdateEncoding
			} else if update.NewPrimary == (types.Address{}) || update.NewFailsafe == (types.Address{}) {
				return ErrUninitializedFoundationUpdate
			}
		}
	}
	return nil
}

// fitsInABlock checks if the transaction is likely to fit in a block. After
// OakHardforkHeight, transactions must be smaller than 64 KiB.
func fitsInABlock(t types.Transaction, currentHeight uint64) error {
	// Check that the transaction will fit inside of a block, leaving 5kb for
	// overhead.
	size := uint64(types.EncodedLen(t))
	if size > BlockSizeLimit - 5e3 {
		return ErrTransactionTooLarge
	}
	if currentHeight >= OakHardforkBlock {
		if size > OakHardforkTxnSizeLimit {
			return ErrTransactionTooLarge
		}
	}
	return nil
}

// followsMinimumValues checks that all outputs adhere to the rules for the
// minimum allowed value (generally 1).
func followsMinimumValues(t types.Transaction) error {
	for _, sco := range t.SiacoinOutputs {
		if sco.Value.IsZero() {
			return ErrZeroOutput
		}
	}
	for _, fc := range t.FileContracts {
		if fc.Payout.IsZero() {
			return ErrZeroOutput
		}
	}
	for _, sfo := range t.SiafundOutputs {
		if sfo.Value == 0 {
			return ErrZeroOutput
		}
	}
	for _, fee := range t.MinerFees {
		if fee.IsZero() {
			return ErrZeroMinerFee
		}
	}
	return nil
}

// followsStorageProofRules checks that a transaction follows the limitations
// placed on transactions that have storage proofs.
func followsStorageProofRules(t types.Transaction) error {
	// No storage proofs, no problems.
	if len(t.StorageProofs) == 0 {
		return nil
	}

	// If there are storage proofs, there can be no Siacoin outputs, Siafund
	// outputs, new file contracts, or file contract terminations. These
	// restrictions are in place because a storage proof can be invalidated by
	// a simple reorg, which will also invalidate the rest of the transaction.
	// These restrictions minimize blockchain turbulence. These other types
	// cannot be invalidated by a simple reorg, and must instead by replaced by
	// a conflicting transaction.
	if len(t.SiacoinOutputs) != 0 {
		return ErrStorageProofWithOutputs
	}
	if len(t.FileContracts) != 0 {
		return ErrStorageProofWithOutputs
	}
	if len(t.FileContractRevisions) != 0 {
		return ErrStorageProofWithOutputs
	}
	if len(t.SiafundOutputs) != 0 {
		return ErrStorageProofWithOutputs
	}

	return nil
}

// noRepeats checks that a transaction does not spend multiple outputs twice,
// submit two valid storage proofs for the same file contract, etc. We
// frivolously check that a file contract termination and storage proof don't
// act on the same file contract. There is very little overhead for doing so,
// and the check is only frivolous because of the current rule that file
// contract terminations are not valid after the proof window opens.
func noRepeats(t types.Transaction) error {
	// Check that there are no repeat instances of Siacoin outputs, storage
	// proofs, contract terminations, or Siafund outputs.
	siacoinInputs := make(map[types.SiacoinOutputID]struct{})
	for _, sci := range t.SiacoinInputs {
		_, exists := siacoinInputs[sci.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		siacoinInputs[sci.ParentID] = struct{}{}
	}
	doneFileContracts := make(map[types.FileContractID]struct{})
	for _, sp := range t.StorageProofs {
		_, exists := doneFileContracts[sp.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		doneFileContracts[sp.ParentID] = struct{}{}
	}
	for _, fcr := range t.FileContractRevisions {
		_, exists := doneFileContracts[fcr.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		doneFileContracts[fcr.ParentID] = struct{}{}
	}
	siafundInputs := make(map[types.SiafundOutputID]struct{})
	for _, sfi := range t.SiafundInputs {
		_, exists := siafundInputs[sfi.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		siafundInputs[sfi.ParentID] = struct{}{}
	}
	return nil
}

// validUC checks that the conditions of uc have been met. The height is taken
// as input so that modules who might be at a different height can do the
// verification without needing to use their own function. Additionally, it
// means that the function does not need to be a method of the consensus set.
func validUC(uc types.UnlockConditions, currentHeight uint64) (err error) {
	if uc.Timelock > currentHeight {
		return ErrTimelockNotSatisfied
	}
	return
}

// validUnlockConditions checks that all of the unlock conditions in the
// transaction are valid.
func validUnlockConditions(t types.Transaction, currentHeight uint64) (err error) {
	for _, sci := range t.SiacoinInputs {
		err = validUC(sci.UnlockConditions, currentHeight)
		if err != nil {
			return
		}
	}
	for _, fcr := range t.FileContractRevisions {
		err = validUC(fcr.UnlockConditions, currentHeight)
		if err != nil {
			return
		}
	}
	for _, sfi := range t.SiafundInputs {
		err = validUC(sfi.UnlockConditions, currentHeight)
		if err != nil {
			return
		}
	}
	return
}

// sortedUnique checks that 'elems' is sorted, contains no repeats, and that no
// element is larger than or equal to 'max'.
func sortedUnique(elems []uint64, max int) bool {
	if len(elems) == 0 {
		return true
	}

	biggest := elems[0]
	for _, elem := range elems[1:] {
		if elem <= biggest {
			return false
		}
		biggest = elem
	}
	if biggest >= uint64(max) {
		return false
	}
	return true
}

// validCoveredFields makes sure that all covered fields objects in the
// signatures follow the rules. This means that if 'WholeTransaction' is set to
// true, all fields except for 'Signatures' must be empty. All fields must be
// sorted numerically, and there can be no repeats.
func validCoveredFields(t types.Transaction) error {
	for _, sig := range t.Signatures {
		// Convenience variables.
		cf := sig.CoveredFields
		fieldMaxs := []struct {
			field []uint64
			max   int
		}{
			{cf.SiacoinInputs, len(t.SiacoinInputs)},
			{cf.SiacoinOutputs, len(t.SiacoinOutputs)},
			{cf.FileContracts, len(t.FileContracts)},
			{cf.FileContractRevisions, len(t.FileContractRevisions)},
			{cf.StorageProofs, len(t.StorageProofs)},
			{cf.SiafundInputs, len(t.SiafundInputs)},
			{cf.SiafundOutputs, len(t.SiafundOutputs)},
			{cf.MinerFees, len(t.MinerFees)},
			{cf.ArbitraryData, len(t.ArbitraryData)},
			{cf.Signatures, len(t.Signatures)},
		}

		if cf.WholeTransaction {
			// If WholeTransaction is set, all fields must be
			// empty, except TransactionSignatures.
			for _, fieldMax := range fieldMaxs[:len(fieldMaxs) - 1] {
				if len(fieldMax.field) != 0 {
					return ErrWholeTransactionViolation
				}
			}
		} else {
			// If WholeTransaction is not set, at least one field
			// must be non-empty.
			allEmpty := true
			for _, fieldMax := range fieldMaxs {
				if len(fieldMax.field) != 0 {
					allEmpty = false
					break
				}
			}
			if allEmpty {
				return ErrWholeTransactionViolation
			}
		}

		// Check that all fields are sorted, and without repeat values, and
		// that all elements point to objects that exists within the
		// transaction. If there are repeats, it means a transaction is trying
		// to sign the same object twice. This is unncecessary, and opens up a
		// DoS vector where the transaction asks the verifier to verify many GB
		// of data.
		for _, fieldMax := range fieldMaxs {
			if !sortedUnique(fieldMax.field, fieldMax.max) {
				return ErrSortedUniqueViolation
			}
		}
	}

	return nil
}

// SigHash returns the hash of the fields in a transaction covered by a given
// signature.
func SigHash(t types.Transaction, i int, height uint64) (hash types.Hash256) {
	sig := t.Signatures[i]
	if sig.CoveredFields.WholeTransaction {
		return wholeSigHash(t, sig, height)
	}
	return partialSigHash(t, sig.CoveredFields, height)
}

// replayPrefix returns the replay protection prefix for the specified height.
// These prefixes are included in a transaction's SigHash; a new prefix is used
// after each hardfork to prevent replay attacks.
func replayPrefix(height uint64) []byte {
	switch {
	case height >= FoundationHardforkHeight:
		return FoundationHardforkReplayProtectionPrefix
	case height >= ASICHardforkHeight:
		return ASICHardforkReplayProtectionPrefix
	default:
		return nil
	}
}

// wholeSigHash calculates the hash for a signature that specifies
// WholeTransaction = true.
func wholeSigHash(t types.Transaction, sig types.TransactionSignature, height uint64) (hash types.Hash256) {
	h := types.NewHasher()

	h.E.WritePrefix(len((t.SiacoinInputs)))
	for i := range t.SiacoinInputs {
		h.E.Write(replayPrefix(height))
		t.SiacoinInputs[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.SiacoinOutputs)))
	for i := range t.SiacoinOutputs {
		t.SiacoinOutputs[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.FileContracts)))
	for i := range t.FileContracts {
		t.FileContracts[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.FileContractRevisions)))
	for i := range t.FileContractRevisions {
		t.FileContractRevisions[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.StorageProofs)))
	for i := range t.StorageProofs {
		t.StorageProofs[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.SiafundInputs)))
	for i := range t.SiafundInputs {
		h.E.Write(replayPrefix(height))
		t.SiafundInputs[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.SiafundOutputs)))
	for i := range t.SiafundOutputs {
		t.SiafundOutputs[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.MinerFees)))
	for i := range t.MinerFees {
		t.MinerFees[i].EncodeTo(h.E)
	}
	h.E.WritePrefix(len((t.ArbitraryData)))
	for i := range t.ArbitraryData {
		h.E.WriteBytes(t.ArbitraryData[i])
	}

	h.E.Write(sig.ParentID[:])
	h.E.WriteUint64(sig.PublicKeyIndex)
	h.E.WriteUint64(uint64(sig.Timelock))

	for _, i := range sig.CoveredFields.Signatures {
		t.Signatures[i].EncodeTo(h.E)
	}

	return h.Sum()
}

// partialSigHash calculates the hash of the fields of the transaction
// specified in cf.
func partialSigHash(t types.Transaction, cf types.CoveredFields, height uint64) (hash types.Hash256) {
	h := types.NewHasher()

	for _, input := range cf.SiacoinInputs {
		h.E.Write(replayPrefix(height))
		t.SiacoinInputs[input].EncodeTo(h.E)
	}
	for _, output := range cf.SiacoinOutputs {
		t.SiacoinOutputs[output].EncodeTo(h.E)
	}
	for _, contract := range cf.FileContracts {
		t.FileContracts[contract].EncodeTo(h.E)
	}
	for _, revision := range cf.FileContractRevisions {
		t.FileContractRevisions[revision].EncodeTo(h.E)
	}
	for _, storageProof := range cf.StorageProofs {
		t.StorageProofs[storageProof].EncodeTo(h.E)
	}
	for _, siafundInput := range cf.SiafundInputs {
		h.E.Write(replayPrefix(height))
		t.SiafundInputs[siafundInput].EncodeTo(h.E)
	}
	for _, siafundOutput := range cf.SiafundOutputs {
		t.SiafundOutputs[siafundOutput].EncodeTo(h.E)
	}
	for _, minerFee := range cf.MinerFees {
		t.MinerFees[minerFee].EncodeTo(h.E)
	}
	for _, arbData := range cf.ArbitraryData {
		h.E.WriteBytes(t.ArbitraryData[arbData])
	}
	for _, sig := range cf.Signatures {
		t.Signatures[sig].EncodeTo(h.E)
	}

	return h.Sum()
}

// Each input has a list of public keys and a required number of signatures.
// inputSignatures keeps track of which public keys have been used and how many
// more signatures are needed.
type inputSignatures struct {
	remainingSignatures uint64
	possibleKeys        []types.UnlockKey
	usedKeys            map[uint64]struct{}
	index               int
}

// validSignatures checks the validaty of all signatures in a transaction.
func validSignatures(t types.Transaction, currentHeight uint64) error {
	// Check that all covered fields objects follow the rules.
	err := validCoveredFields(t)
	if err != nil {
		return err
	}

	// Create the inputSignatures object for each input.
	sigMap := make(map[types.Hash256]*inputSignatures)
	for i, input := range t.SiacoinInputs {
		id := types.Hash256(input.ParentID)
		_, exists := sigMap[id]
		if exists {
			return ErrDoubleSpend
		}

		sigMap[id] = &inputSignatures{
			remainingSignatures: input.UnlockConditions.SignaturesRequired,
			possibleKeys:        input.UnlockConditions.PublicKeys,
			usedKeys:            make(map[uint64]struct{}),
			index:               i,
		}
	}
	for i, revision := range t.FileContractRevisions {
		id := types.Hash256(revision.ParentID)
		_, exists := sigMap[id]
		if exists {
			return ErrDoubleSpend
		}

		sigMap[id] = &inputSignatures{
			remainingSignatures: revision.UnlockConditions.SignaturesRequired,
			possibleKeys:        revision.UnlockConditions.PublicKeys,
			usedKeys:            make(map[uint64]struct{}),
			index:               i,
		}
	}
	for i, input := range t.SiafundInputs {
		id := types.Hash256(input.ParentID)
		_, exists := sigMap[id]
		if exists {
			return ErrDoubleSpend
		}

		sigMap[id] = &inputSignatures{
			remainingSignatures: input.UnlockConditions.SignaturesRequired,
			possibleKeys:        input.UnlockConditions.PublicKeys,
			usedKeys:            make(map[uint64]struct{}),
			index:               i,
		}
	}

	// Check all of the signatures for validity.
	for i, sig := range t.Signatures {
		// Check that sig corresponds to an entry in sigMap.
		inSig, exists := sigMap[types.Hash256(sig.ParentID)]
		if !exists || inSig.remainingSignatures == 0 {
			return ErrFrivolousSignature
		}
		// Check that sig's key hasn't already been used.
		_, exists = inSig.usedKeys[sig.PublicKeyIndex]
		if exists {
			return ErrPublicKeyOveruse
		}
		// Check that the public key index refers to an existing public key.
		if sig.PublicKeyIndex >= uint64(len(inSig.possibleKeys)) {
			return ErrInvalidPubKeyIndex
		}
		// Check that the timelock has expired.
		if sig.Timelock > currentHeight {
			return ErrPrematureSignature
		}

		// Check that the signature verifies. Multiple signature schemes are
		// supported.
		publicKey := inSig.possibleKeys[sig.PublicKeyIndex]
		switch publicKey.Algorithm {
		case SignatureEntropy:
			// Entropy cannot ever be used to sign a transaction.
			return ErrEntropyKey

		case SignatureEd25519:
			// Decode the public key and signature.
			var edPK types.PublicKey
			copy(edPK[:], publicKey.Key)
			var edSig types.Signature
			copy(edSig[:], sig.Signature)

			sigHash := SigHash(t, i, currentHeight)
			ok := edPK.VerifyHash(sigHash, edSig)
			if !ok {
				return ErrInvalidSignature
			}

		default:
			// If the identifier is not recognized, assume that the signature
			// is valid. This allows more signature types to be added via soft
			// forking.
		}

		inSig.usedKeys[sig.PublicKeyIndex] = struct{}{}
		inSig.remainingSignatures--
	}

	// Check that all inputs have been sufficiently signed.
	for _, reqSigs := range sigMap {
		if reqSigs.remainingSignatures != 0 {
			return ErrMissingSignatures
		}
	}

	return nil
}

// StandaloneValid returns an error if a transaction is not valid in any
// context, for example if the same output is spent twice in the same
// transaction. StandaloneValid will not check that all outputs being spent are
// legal outputs, as it has no confirmed or unconfirmed set to look at.
func StandaloneValid(t types.Transaction, currentHeight uint64) (err error) {
	err = fitsInABlock(t, currentHeight)
	if err != nil {
		return
	}
	err = followsStorageProofRules(t)
	if err != nil {
		return
	}
	err = noRepeats(t)
	if err != nil {
		return
	}
	err = followsMinimumValues(t)
	if err != nil {
		return
	}
	err = correctFileContracts(t, currentHeight)
	if err != nil {
		return
	}
	err = correctFileContractRevisions(t, currentHeight)
	if err != nil {
		return
	}
	err = correctArbitraryData(t, currentHeight)
	if err != nil {
		return
	}
	err = validUnlockConditions(t, currentHeight)
	if err != nil {
		return
	}
	err = validSignatures(t, currentHeight)
	if err != nil {
		return
	}
	return
}