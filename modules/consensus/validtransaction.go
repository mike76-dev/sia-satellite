package consensus

import (
	"bytes"
	"database/sql"
	"errors"
	"math/big"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

var (
	errAlteredRevisionPayouts     = errors.New("file contract revision has altered payout volume")
	errInvalidStorageProof        = errors.New("provided storage proof is invalid")
	errLateRevision               = errors.New("file contract revision submitted after deadline")
	errLowRevisionNumber          = errors.New("transaction has a file contract with an outdated revision number")
	errMissingSiacoinOutput       = errors.New("transaction spends a nonexisting siacoin output")
	errSiacoinInputOutputMismatch = errors.New("siacoin inputs do not equal siacoin outputs for transaction")
	errSiafundInputOutputMismatch = errors.New("siafund inputs do not equal siafund outputs for transaction")
	errUnfinishedFileContract     = errors.New("file contract window has not yet openend")
	errUnrecognizedFileContractID = errors.New("cannot fetch storage proof segment for unknown file contract")
	errWrongUnlockConditions      = errors.New("transaction contains incorrect unlock conditions")
	errUnsignedFoundationUpdate   = errors.New("transaction contains an Foundation UnlockHash update with missing or invalid signatures")
)

// validSiacoins checks that the siacoin inputs and outputs are valid in the
// context of the current consensus set.
func (cs *ConsensusSet) validSiacoins(tx *sql.Tx, t types.Transaction) error {
	var inputSum types.Currency
	for _, sci := range t.SiacoinInputs {
		// Check that the input spends an existing output.
		sco, exists, err := cs.findSiacoinOutput(tx, sci.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errMissingSiacoinOutput
		}

		// Check that the unlock conditions match the required unlock hash.
		if sci.UnlockConditions.UnlockHash() != sco.Address {
			return errWrongUnlockConditions
		}

		inputSum = inputSum.Add(sco.Value)
	}
	if !inputSum.Equals(modules.SiacoinOutputSum(t)) {
		return errSiacoinInputOutputMismatch
	}
	return nil
}

// storageProofSegment returns the index of the segment that needs to be proven
// exists in a file contract.
func (cs *ConsensusSet) storageProofSegment(tx *sql.Tx, fcid types.FileContractID) (uint64, error) {
	// Check that the parent file contract exists.
	fc, exists, err := cs.findFileContract(tx, fcid)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, errUnrecognizedFileContractID
	}

	// Get the trigger block id.
	triggerHeight := fc.WindowStart - 1
	if triggerHeight > blockHeight(tx) {
		return 0, errUnfinishedFileContract
	}
	triggerID, err := getBlockAtHeight(tx, triggerHeight)
	if err != nil {
		return 0, err
	}

	// Get the index by appending the file contract ID to the trigger block and
	// taking the hash, then converting the hash to a numerical value and
	// modding it against the number of segments in the file. The result is a
	// random number in range [0, numSegments]. The probability is very
	// slightly weighted towards the beginning of the file, but because the
	// size difference between the number of segments and the random number
	// being modded, the difference is too small to make any practical
	// difference.
	h := types.NewHasher()
	triggerID.EncodeTo(h.E)
	fcid.EncodeTo(h.E)
	seed := h.Sum()
	numSegments := int64(modules.CalculateLeaves(fc.Filesize))
	seedInt := new(big.Int).SetBytes(seed[:])
	index := seedInt.Mod(seedInt, big.NewInt(numSegments)).Uint64()
	return index, nil
}

// validStorageProofs checks that the storage proofs are valid in the context
// of the consensus set.
func (cs *ConsensusSet) validStorageProofs(tx *sql.Tx, t types.Transaction) error {
	for _, sp := range t.StorageProofs {
		// Check that the storage proof itself is valid.
		segmentIndex, err := cs.storageProofSegment(tx, sp.ParentID)
		if err != nil {
			return err
		}

		fc, exists, err := cs.findFileContract(tx, sp.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("storage contract not found")
		}

		leaves := modules.CalculateLeaves(fc.Filesize)
		segmentLen := uint64(modules.SegmentSize)

		// If this segment chosen is the final segment, it should only be as
		// long as necessary to complete the filesize.
		height := blockHeight(tx)
		if segmentIndex == leaves - 1 && height >= 21e3 {
			segmentLen = fc.Filesize % modules.SegmentSize
		}
		if segmentLen == 0 {
			segmentLen = uint64(modules.SegmentSize)
		}

		verified := modules.VerifySegment(
			sp.Leaf[:segmentLen],
			sp.Proof,
			leaves,
			segmentIndex,
			fc.FileMerkleRoot,
		)
		if !verified && fc.Filesize > 0 {
			return errInvalidStorageProof
		}
	}

	return nil
}

// validFileContractRevision checks that each file contract revision is valid
// in the context of the current consensus set.
func (cs *ConsensusSet) validFileContractRevisions(tx *sql.Tx, t types.Transaction) error {
	for _, fcr := range t.FileContractRevisions {
		fc, exists, err := cs.findFileContract(tx, fcr.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("storage contract not found")
		}

		// Check that the height is less than fc.WindowStart - revisions are
		// not allowed to be submitted once the storage proof window has
		// opened.  This reduces complexity for unconfirmed transactions.
		if blockHeight(tx) > fc.WindowStart {
			return errLateRevision
		}

		// Check that the revision number of the revision is greater than the
		// revision number of the existing file contract.
		if fc.RevisionNumber >= fcr.RevisionNumber {
			return errLowRevisionNumber
		}

		// Check that the unlock conditions match the unlock hash.
		if fcr.UnlockConditions.UnlockHash() != types.Address(fc.UnlockHash) {
			return errWrongUnlockConditions
		}

		// Check that the payout of the revision matches the payout of the
		// original, and that the payouts match each other.
		var valid, missed types.Currency
		for _, output := range fcr.ValidProofOutputs {
			valid = valid.Add(output.Value)
		}
		for _, output := range fcr.MissedProofOutputs {
			missed = missed.Add(output.Value)
		}
		var oldPayout types.Currency
		for _, output := range fc.ValidProofOutputs {
			oldPayout = oldPayout.Add(output.Value)
		}
		if !valid.Equals(oldPayout) {
			return errAlteredRevisionPayouts
		}
		if !missed.Equals(oldPayout) {
			return errAlteredRevisionPayouts
		}
	}
	return nil
}

// validSiafunds checks that the siafund portions of the transaction are valid
// in the context of the consensus set.
func validSiafunds(tx *sql.Tx, t types.Transaction) (err error) {
	// Compare the number of input siafunds to the output siafunds.
	var siafundInputSum uint64
	var siafundOutputSum uint64
	for _, sfi := range t.SiafundInputs {
		sfo, _, exists, err := findSiafundOutput(tx, sfi.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("unable to find SF output")
		}

		// Check the unlock conditions match the unlock hash.
		if sfi.UnlockConditions.UnlockHash() != sfo.Address {
			return errWrongUnlockConditions
		}

		siafundInputSum = siafundInputSum + sfo.Value
	}
	for _, sfo := range t.SiafundOutputs {
		siafundOutputSum = siafundOutputSum + sfo.Value
	}
	if siafundOutputSum != siafundInputSum {
		return errSiafundInputOutputMismatch
	}
	return
}

// validArbitraryData checks that the ArbitraryData portions of the transaction are
// valid in the context of the consensus set. Currently, only ArbitraryData with
// the types.SpecifierFoundation prefix is examined.
func validArbitraryData(tx *sql.Tx, t types.Transaction, currentHeight uint64) error {
	if currentHeight < modules.FoundationHardforkHeight {
		return nil
	}
	for _, arb := range t.ArbitraryData {
		if bytes.HasPrefix(arb, types.SpecifierFoundation[:]) {
			// NOTE: modules.StandaloneValid ensures that the update is correctly encoded.
			if !foundationUpdateIsSigned(tx, t) {
				return errUnsignedFoundationUpdate
			}
		}
	}
	return nil
}

// foundationUpdateIsSigned checks that the transaction has a signature that
// covers a SiacoinInput controlled by the primary or failsafe UnlockHash. To
// minimize surface area, the signature must cover the whole transaction.
//
// This function does not actually validate the signature. By the time
// foundationUpdateIsSigned is called, all of the transaction's signatures have
// already been validated by StandaloneValid.
func foundationUpdateIsSigned(tx *sql.Tx, t types.Transaction) bool {
	primary, failsafe, err := getFoundationUnlockHashes(tx)
	if err != nil {
		return false
	}
	for _, sci := range t.SiacoinInputs {
		// NOTE: this conditional is split up to better visualize test coverage.
		if uh := sci.UnlockConditions.UnlockHash(); uh != primary {
			if uh != failsafe {
				continue
			}
		}
		// Locate the corresponding signature.
		for _, sig := range t.Signatures {
			if sig.ParentID == types.Hash256(sci.ParentID) && sig.CoveredFields.WholeTransaction {
				return true
			}
		}
	}
	return false
}

// validTransaction checks that all fields are valid within the current
// consensus state. If not an error is returned.
func (cs *ConsensusSet) validTransaction(tx *sql.Tx, t types.Transaction) error {
	// StandaloneValid will check things like signatures and properties that
	// should be inherent to the transaction. (storage proof rules, etc.)
	currentHeight := blockHeight(tx)
	if err := modules.StandaloneValid(t, currentHeight); err != nil {
		return err
	}

	// Check that each portion of the transaction is legal given the current
	// consensus set.
	if err := cs.validSiacoins(tx, t); err != nil {
		return err
	}
	if err := cs.validStorageProofs(tx, t); err != nil {
		return err
	}
	if err := cs.validFileContractRevisions(tx, t); err != nil {
		return err
	}
	if err := validSiafunds(tx, t); err != nil {
		return err
	}
	if err := validArbitraryData(tx, t, currentHeight); err != nil {
		return err
	}
	return nil
}

// tryTransactionSet applies the input transactions to the consensus set to
// determine if they are valid. An error is returned IF they are not a valid
// set in the current consensus set. The size of the transactions and the set
// is not checked. After the transactions have been validated, a consensus
// change is returned detailing the diffs that the transactions set would have.
func (cs *ConsensusSet) tryTransactionSet(txns []types.Transaction) (modules.ConsensusChange, error) {
	tx, err := cs.db.Begin()
	if err != nil {
		return modules.ConsensusChange{}, err
	}

	// applyTransaction will apply the diffs from a transaction and store them
	// in a block node. diffHolder is the blockNode that tracks the temporary
	// changes. At the end of the function, all changes that were made to the
	// consensus set get reverted.
	diffHolder := new(processedBlock)
	diffHolder.Height = blockHeight(tx)
	for _, txn := range txns {
		if err := cs.validTransaction(tx, txn); err != nil {
			tx.Rollback()
			return modules.ConsensusChange{}, err
		}
		if err := cs.applyTransaction(tx, diffHolder, txn); err != nil {
			tx.Rollback()
			return modules.ConsensusChange{}, err
		}
	}
	cc := modules.ConsensusChange{
		ConsensusChangeDiffs: modules.ConsensusChangeDiffs{
			SiacoinOutputDiffs:        diffHolder.SiacoinOutputDiffs,
			FileContractDiffs:         diffHolder.FileContractDiffs,
			SiafundOutputDiffs:        diffHolder.SiafundOutputDiffs,
			DelayedSiacoinOutputDiffs: diffHolder.DelayedSiacoinOutputDiffs,
			SiafundPoolDiffs:          diffHolder.SiafundPoolDiffs,
		},
	}

	if err := tx.Commit(); err != nil {
		return modules.ConsensusChange{}, err
	}
	
	return cc, nil
}

// TryTransactionSet applies the input transactions to the consensus set to
// determine if they are valid. An error is returned IF they are not a valid
// set in the current consensus set. The size of the transactions and the set
// is not checked. After the transactions have been validated, a consensus
// change is returned detailing the diffs that the transactions set would have.
func (cs *ConsensusSet) TryTransactionSet(txns []types.Transaction) (modules.ConsensusChange, error) {
	err := cs.tg.Add()
	if err != nil {
		return modules.ConsensusChange{}, err
	}
	defer cs.tg.Done()
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tryTransactionSet(txns)
}

// LockedTryTransactionSet calls fn while under read-lock, passing it a
// version of TryTransactionSet that can be called under read-lock. This fixes
// an edge case in the transaction pool.
func (cs *ConsensusSet) LockedTryTransactionSet(fn func(func(txns []types.Transaction) (modules.ConsensusChange, error)) error) error {
	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return fn(cs.tryTransactionSet)
}
