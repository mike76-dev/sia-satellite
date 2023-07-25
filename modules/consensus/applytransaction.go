package consensus

import (
	"bytes"
	"database/sql"
	"errors"
	"io"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// applySiacoinInputs takes all of the siacoin inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiacoinInputs(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	// Remove all siacoin inputs from the unspent siacoin outputs list.
	for _, sci := range t.SiacoinInputs {
		sco, exists, err := findSiacoinOutput(tx, sci.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("no output found for the Siacoin input")
		}
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sci.ParentID,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		err = commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
		if err != nil {
			return err
		}
	}
	return nil
}

// applySiacoinOutputs takes all of the siacoin outputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiacoinOutputs(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	// Add all siacoin outputs to the unspent siacoin outputs list.
	for i, sco := range t.SiacoinOutputs {
		scoid := t.SiacoinOutputID(i)
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffApply,
			ID:            scoid,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		err := commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
		if err != nil {
			return err
		}
	}
	return nil
}

// applyFileContracts iterates through all of the file contracts in a
// transaction and applies them to the state, updating the diffs in the proccesed
// block.
func applyFileContracts(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	for i, fc := range t.FileContracts {
		fcid := t.FileContractID(i)
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcid,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		err := commitFileContractDiff(tx, fcd, modules.DiffApply)
		if err != nil {
			return err
		}

		// Get the portion of the contract that goes into the siafund pool and
		// add it to the siafund pool.
		sfp := getSiafundPool(tx)
		sfpd := modules.SiafundPoolDiff{
			Direction: modules.DiffApply,
			Previous:  sfp,
			Adjusted:  sfp.Add(modules.Tax(blockHeight(tx), fc.Payout)),
		}
		pb.SiafundPoolDiffs = append(pb.SiafundPoolDiffs, sfpd)
		err = commitSiafundPoolDiff(tx, sfpd, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyFileContractRevisions iterates through all of the file contract
// revisions in a transaction and applies them to the state, updating the diffs
// in the processed block.
func applyFileContractRevisions(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	for _, fcr := range t.FileContractRevisions {
		fc, exists, err := findFileContract(tx, fcr.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("no file contract found for the revision")
		}

		// Add the diff to delete the old file contract.
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           fcr.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		err = commitFileContractDiff(tx, fcd, modules.DiffApply)
		if err != nil {
			return err
		}

		// Add the diff to add the revised file contract.
		newFC := types.FileContract{
			Filesize:           fcr.FileContract.Filesize,
			FileMerkleRoot:     fcr.FileContract.FileMerkleRoot,
			WindowStart:        fcr.FileContract.WindowStart,
			WindowEnd:          fcr.FileContract.WindowEnd,
			Payout:             fc.Payout,
			ValidProofOutputs:  fcr.FileContract.ValidProofOutputs,
			MissedProofOutputs: fcr.FileContract.MissedProofOutputs,
			UnlockHash:         fcr.FileContract.UnlockHash,
			RevisionNumber:     fcr.FileContract.RevisionNumber,
		}
		fcd = modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcr.ParentID,
			FileContract: newFC,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		err = commitFileContractDiff(tx, fcd, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyStorageProofs iterates through all of the storage proofs in a
// transaction and applies them to the state, updating the diffs in the processed
// block.
func applyStorageProofs(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	for _, sp := range t.StorageProofs {
		fc, exists, err := findFileContract(tx, sp.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("no file contract found for the storage proof")
		}

		// Add all of the outputs in the ValidProofOutputs of the contract.
		for i, vpo := range fc.ValidProofOutputs {
			spoid := modules.StorageProofOutputID(sp.ParentID, true, i)
			dscod := modules.DelayedSiacoinOutputDiff{
				Direction:      modules.DiffApply,
				ID:             spoid,
				SiacoinOutput:  vpo,
				MaturityHeight: pb.Height + modules.MaturityDelay,
			}
			pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
			err := commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
			if err != nil {
				return err
			}
		}

		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           sp.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		err = commitFileContractDiff(tx, fcd, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	return nil
}

// applySiafundInputs takes all of the siafund inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiafundInputs(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	for _, sfi := range t.SiafundInputs {
		// Calculate the volume of siacoins to put in the claim output.
		sfo, claimStart, exists, err := findSiafundOutput(tx, sfi.ParentID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("no output found for the Siafund input")
		}
		claimPortion := getSiafundPool(tx).Sub(claimStart).Div64(modules.SiafundCount).Mul64(sfo.Value)

		// Add the claim output to the delayed set of outputs.
		sco := types.SiacoinOutput{
			Value:   claimPortion,
			Address: sfi.ClaimAddress,
		}
		sfoid := sfi.ParentID.ClaimOutputID()
		dscod := modules.DelayedSiacoinOutputDiff{
			Direction:      modules.DiffApply,
			ID:             sfoid,
			SiacoinOutput:  sco,
			MaturityHeight: pb.Height + modules.MaturityDelay,
		}
		pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
		err = commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
		if err != nil {
			return err
		}

		// Create the siafund output diff and remove the output from the
		// consensus set.
		sfod := modules.SiafundOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sfi.ParentID,
			SiafundOutput: sfo,
			ClaimStart:    claimStart,
		}
		pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
		err = commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	return nil
}

// applySiafundOutputs applies a siafund output to the consensus set.
func applySiafundOutputs(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	for i, sfo := range t.SiafundOutputs {
		sfoid := t.SiafundOutputID(i)
		sfod := modules.SiafundOutputDiff{
			Direction:     modules.DiffApply,
			ID:            sfoid,
			SiafundOutput: sfo,
			ClaimStart:    getSiafundPool(tx),
		}
		pb.SiafundOutputDiffs = append(pb.SiafundOutputDiffs, sfod)
		err := commitSiafundOutputDiff(tx, sfod, modules.DiffApply)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyArbitraryData applies arbitrary data to the consensus set. ArbitraryData
// is a field of the Transaction type whose structure is not fixed. This means
// that, via hardfork, new types of transaction can be introduced with minimal
// breakage by updating consensus code to recognize and act upon values encoded
// within the ArbitraryData field.
//
// Accordingly, this function dispatches on the various ArbitraryData values
// that are recognized by consensus. Currently, types.FoundationUnlockHashUpdate
// is the only recognized value.
func applyArbitraryData(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	// No ArbitraryData values were recognized prior to the Foundation hardfork.
	if pb.Height < modules.FoundationHardforkHeight {
		return nil
	}
	for _, arb := range t.ArbitraryData {
		if bytes.HasPrefix(arb, types.SpecifierFoundation[:]) {
			var update types.FoundationAddressUpdate
			var buf bytes.Buffer
			buf.Write(arb[16:])
			d := types.NewDecoder(io.LimitedReader{R: &buf, N: int64(len(arb)) - 16})
			update.DecodeFrom(d)
			if err := d.Err(); err != nil {
				return err
			}

			// Apply the update. First, save a copy of the old (i.e. current)
			// unlock hashes, so that we can revert later. Then set the new
			// unlock hashes.
			//
			// Importantly, we must only do this once per block; otherwise, for
			// complicated reasons involving diffs, we would not be able to
			// revert updates safely. So if we see that a copy has already been
			// recorded, we simply ignore the update; i.e. only the first update
			// in a block will be applied.
			_, _, exists, err := getPriorFoundationUnlockHashes(tx, pb.Height)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			err = setPriorFoundationUnlockHashes(tx, pb.Height)
			if err != nil {
				return err
			}
			err = setFoundationUnlockHashes(tx, update.NewPrimary, update.NewFailsafe)
			if err != nil {
				return err
			}
			err = transferFoundationOutputs(tx, pb.Height, update.NewPrimary)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// transferFoundationOutputs transfers all unspent subsidy outputs to
// newPrimary. This allows subsidies to be recovered in the event that the
// primary key is lost or unusable when a subsidy is created.
func transferFoundationOutputs(tx *sql.Tx, currentHeight uint64, newPrimary types.Address) error {
	for height := modules.FoundationHardforkHeight; height < currentHeight; height += modules.FoundationSubsidyFrequency {
		blockID, err := getBlockAtHeight(tx, height)
		if err != nil {
			continue
		}
		id := blockID.FoundationOutputID()
		sco, exists, err := findSiacoinOutput(tx, id)
		if err != nil {
			return err
		}
		if !exists {
			continue // Output has already been spent.
		}
		sco.Address = newPrimary
		err = removeSiacoinOutput(tx, id)
		if err != nil {
			return err
		}
		err = addSiacoinOutput(tx, id, sco)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyTransaction applies the contents of a transaction to the ConsensusSet.
// This produces a set of diffs, which are stored in the blockNode containing
// the transaction. No verification is done by this function.
func applyTransaction(tx *sql.Tx, pb *processedBlock, t types.Transaction) error {
	err := applySiacoinInputs(tx, pb, t)
	if err != nil {
		return err
	}
	err = applySiacoinOutputs(tx, pb, t)
	if err != nil {
		return err
	}
	err = applyFileContracts(tx, pb, t)
	if err != nil {
		return err
	}
	err = applyFileContractRevisions(tx, pb, t)
	if err != nil {
		return err
	}
	err = applyStorageProofs(tx, pb, t)
	if err != nil {
		return err
	}
	err = applySiafundInputs(tx, pb, t)
	if err != nil {
		return err
	}
	err = applySiafundOutputs(tx, pb, t)
	if err != nil {
		return err
	}
	err = applyArbitraryData(tx, pb, t)
	if err != nil {
		return err
	}
	return nil
}
