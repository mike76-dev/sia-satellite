package consensus

import (
	"database/sql"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"
)

var (
	errInvalidSuccessor = errors.New("generating diffs for a block that's an invalid successsor to the current block")
)

// commitSiacoinOutputDiff applies or reverts a SiacoinOutputDiff.
func commitSiacoinOutputDiff(tx *sql.Tx, scod modules.SiacoinOutputDiff, dir modules.DiffDirection) error {
	if scod.Direction == dir {
		return addSiacoinOutput(tx, scod.ID, scod.SiacoinOutput)
	} else {
		return removeSiacoinOutput(tx, scod.ID)
	}
}

// commitFileContractDiff applies or reverts a FileContractDiff.
func commitFileContractDiff(tx *sql.Tx, fcd modules.FileContractDiff, dir modules.DiffDirection) error {
	if fcd.Direction == dir {
		return addFileContract(tx, fcd.ID, fcd.FileContract)
	} else {
		return removeFileContract(tx, fcd.ID)
	}
}

// commitSiafundOutputDiff applies or reverts a Siafund output diff.
func commitSiafundOutputDiff(tx *sql.Tx, sfod modules.SiafundOutputDiff, dir modules.DiffDirection) error {
	if sfod.Direction == dir {
		return addSiafundOutput(tx, sfod.ID, sfod.SiafundOutput, sfod.ClaimStart)
	} else {
		return removeSiafundOutput(tx, sfod.ID)
	}
}

// commitDelayedSiacoinOutputDiff applies or reverts a delayedSiacoinOutputDiff.
func commitDelayedSiacoinOutputDiff(tx *sql.Tx, dscod modules.DelayedSiacoinOutputDiff, dir modules.DiffDirection) error {
	if dscod.Direction == dir {
		return addDSCO(tx, dscod.MaturityHeight, dscod.ID, dscod.SiacoinOutput)
	} else {
		return removeDSCO(tx, dscod.MaturityHeight, dscod.ID)
	}
}

// commitSiafundPoolDiff applies or reverts a SiafundPoolDiff.
func commitSiafundPoolDiff(tx *sql.Tx, sfpd modules.SiafundPoolDiff, dir modules.DiffDirection) error {
	if dir == modules.DiffApply {
		return setSiafundPool(tx, sfpd.Adjusted)
	} else {
		return setSiafundPool(tx, sfpd.Previous)
	}
}

// commitNodeDiffs commits all of the diffs in a block node.
func commitNodeDiffs(tx *sql.Tx, pb *processedBlock, dir modules.DiffDirection) (err error) {
	if dir == modules.DiffApply {
		for _, scod := range pb.SiacoinOutputDiffs {
			if err := commitSiacoinOutputDiff(tx, scod, dir); err != nil {
				return err
			}
		}
		for _, fcd := range pb.FileContractDiffs {
			if err := commitFileContractDiff(tx, fcd, dir); err != nil {
				return err
			}
		}
		for _, sfod := range pb.SiafundOutputDiffs {
			if err := commitSiafundOutputDiff(tx, sfod, dir); err != nil {
				return err
			}
		}
		for _, dscod := range pb.DelayedSiacoinOutputDiffs {
			if err := commitDelayedSiacoinOutputDiff(tx, dscod, dir); err != nil {
				return err
			}
		}
		for _, sfpd := range pb.SiafundPoolDiffs {
			if err := commitSiafundPoolDiff(tx, sfpd, dir); err != nil {
				return err
			}
		}
	} else {
		for i := len(pb.SiacoinOutputDiffs) - 1; i >= 0; i-- {
			if err := commitSiacoinOutputDiff(tx, pb.SiacoinOutputDiffs[i], dir); err != nil {
				return err
			}
		}
		for i := len(pb.FileContractDiffs) - 1; i >= 0; i-- {
			if err := commitFileContractDiff(tx, pb.FileContractDiffs[i], dir); err != nil {
				return err
			}
		}
		for i := len(pb.SiafundOutputDiffs) - 1; i >= 0; i-- {
			if err := commitSiafundOutputDiff(tx, pb.SiafundOutputDiffs[i], dir); err != nil {
				return err
			}
		}
		for i := len(pb.DelayedSiacoinOutputDiffs) - 1; i >= 0; i-- {
			if err := commitDelayedSiacoinOutputDiff(tx, pb.DelayedSiacoinOutputDiffs[i], dir); err != nil {
				return err
			}
		}
		for i := len(pb.SiafundPoolDiffs) - 1; i >= 0; i-- {
			if err := commitSiafundPoolDiff(tx, pb.SiafundPoolDiffs[i], dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// updateCurrentPath updates the current path after applying a diff set.
func updateCurrentPath(tx *sql.Tx, pb *processedBlock, dir modules.DiffDirection) error {
	// Update the current path.
	if dir == modules.DiffApply {
		return pushPath(tx, pb.Block.ID())
	} else {
		return popPath(tx)
	}
}

// commitFoundationUpdate updates the current Foundation unlock hashes in
// accordance with the specified block and direction.
//
// Because these updates do not have associated diffs, we cannot apply multiple
// updates per block. Instead, we apply the first update and ignore the rest.
func commitFoundationUpdate(tx *sql.Tx, pb *processedBlock, dir modules.DiffDirection) (err error) {
	if dir == modules.DiffApply {
		for i := range pb.Block.Transactions {
			if err := applyArbitraryData(tx, pb, pb.Block.Transactions[i]); err != nil {
				return err
			}
		}
	} else {
		// Look for a set of prior unlock hashes for this height.
		primary, failsafe, exists, err := getPriorFoundationUnlockHashes(tx, pb.Height)
		if err != nil {
			return err
		}
		if exists {
			if err := setFoundationUnlockHashes(tx, primary, failsafe); err != nil {
				return err
			}
			if err := deletePriorFoundationUnlockHashes(tx, pb.Height); err != nil {
				return err
			}
			if err := transferFoundationOutputs(tx, pb.Height, primary); err != nil {
				return err
			}
		}
	}
	return nil
}

// commitDiffSet applies or reverts the diffs in a blockNode.
func commitDiffSet(tx *sql.Tx, pb *processedBlock, dir modules.DiffDirection) (err error) {
	if err := commitNodeDiffs(tx, pb, dir); err != nil {
		return err
	}
	if err := commitFoundationUpdate(tx, pb, dir); err != nil {
		return err
	}
	return updateCurrentPath(tx, pb, dir)
}

// generateAndApplyDiff will verify the block and then integrate it into the
// consensus state. These two actions must happen at the same time because
// transactions are allowed to depend on each other. We can't be sure that a
// transaction is valid unless we have applied all of the previous transactions
// in the block, which means we need to apply while we verify.
func generateAndApplyDiff(tx *sql.Tx, pb *processedBlock) error {
	// Sanity check - the block being applied should have the current block as
	// a parent.
	if pb.Block.ParentID != currentBlockID(tx) {
		return errInvalidSuccessor
	}

	// Validate and apply each transaction in the block. They cannot be
	// validated all at once because some transactions may not be valid until
	// previous transactions have been applied.
	for _, txn := range pb.Block.Transactions {
		if err := validTransaction(tx, txn); err != nil {
			return err
		}
		if err := applyTransaction(tx, pb, txn); err != nil {
			return err
		}
	}

	// After all of the transactions have been applied, 'maintenance' is
	// applied on the block. This includes adding any outputs that have reached
	// maturity, applying any contracts with missed storage proofs, and adding
	// the miner payouts and Foundation subsidy to the list of delayed outputs.
	if err := applyMaintenance(tx, pb); err != nil {
		return err
	}

	// DiffsGenerated are only set to true after the block has been fully
	// validated and integrated. This is required to prevent later blocks from
	// being accepted on top of an invalid block - if the consensus set ever
	// forks over an invalid block, 'DiffsGenerated' will be set to 'false',
	// requiring validation to occur again. when 'DiffsGenerated' is set to
	// true, validation is skipped, therefore the flag should only be set to
	// true on fully validated blocks.
	pb.DiffsGenerated = true

	// Add the block to the current path and block map.
	bid := pb.Block.ID()
	if err := updateCurrentPath(tx, pb, modules.DiffApply); err != nil {
		return err
	}

	return saveBlock(tx, bid, pb)
}
