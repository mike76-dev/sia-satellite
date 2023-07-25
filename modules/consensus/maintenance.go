package consensus

import (
	"bytes"
	"database/sql"
	"errors"
	"io"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

var (
	errOutputAlreadyMature = errors.New("delayed siacoin output is already in the matured outputs set")
	errPayoutsAlreadyPaid  = errors.New("payouts are already in the consensus set")
	errStorageProofTiming  = errors.New("missed proof triggered for file contract that is not expiring")
)

// applyFoundationSubsidy adds a Foundation subsidy to the consensus set as a
// delayed siacoin output. If no subsidy is due on the given block, no output is
// added.
func applyFoundationSubsidy(tx *sql.Tx, pb *processedBlock) error {
	// NOTE: this conditional is split up to better visualize test coverage.
	if pb.Height < modules.FoundationHardforkHeight {
		return nil
	} else if (pb.Height - modules.FoundationHardforkHeight) % modules.FoundationSubsidyFrequency != 0 {
		return nil
	}
	value := modules.FoundationSubsidyPerBlock.Mul64(modules.FoundationSubsidyFrequency)
	if pb.Height == modules.FoundationHardforkHeight {
		value = modules.InitialFoundationSubsidy
	}

	// The subsidy is always sent to the primary address.
	addr, _, err := getFoundationUnlockHashes(tx)
	if err != nil {
		return err
	}
	dscod := modules.DelayedSiacoinOutputDiff{
		Direction: modules.DiffApply,
		ID:        pb.Block.ID().FoundationOutputID(),
		SiacoinOutput: types.SiacoinOutput{
			Value:   value,
			Address: addr,
		},
		MaturityHeight: pb.Height + modules.MaturityDelay,
	}

	pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
	return commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
}

// applyMinerPayouts adds a block's miner payouts to the consensus set as
// delayed siacoin outputs.
func applyMinerPayouts(tx *sql.Tx, pb *processedBlock) error {
	for i := range pb.Block.MinerPayouts {
		mpid := pb.Block.ID().MinerOutputID(i)
		dscod := modules.DelayedSiacoinOutputDiff{
			Direction:      modules.DiffApply,
			ID:             mpid,
			SiacoinOutput:  pb.Block.MinerPayouts[i],
			MaturityHeight: pb.Height + modules.MaturityDelay,
		}
		pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
		if err := commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply); err != nil {
			return err
		}
	}
	return nil
}

// applyMaturedSiacoinOutputs goes through the list of siacoin outputs that
// have matured and adds them to the consensus set. This also updates the block
// node diff set.
func applyMaturedSiacoinOutputs(tx *sql.Tx, pb *processedBlock) error {
	// Skip this step if the blockchain is not old enough to have maturing
	// outputs.
	if pb.Height < modules.MaturityDelay {
		return nil
	}

	// Iterate through the list of delayed siacoin outputs.
	rows, err := tx.Query("SELECT scoid, bytes FROM cs_dsco WHERE height = ?", pb.Height)
	if err != nil {
		return err
	}

	var scods []modules.SiacoinOutputDiff
	var dscods []modules.DelayedSiacoinOutputDiff
	for rows.Next() {
		var scoid types.SiacoinOutputID
		id := make([]byte, 32)
		scoBytes := make([]byte, 0, 56)
		var sco types.SiacoinOutput
		if err := rows.Scan(&id, &scoBytes); err != nil {
			rows.Close()
			return err
		}

		copy(scoid[:], id[:])
		buf := bytes.NewBuffer(scoBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(scoBytes))})
		sco.DecodeFrom(d)
		if err := d.Err(); err != nil {
			rows.Close()
			return err
		}

		// Add the output to the ConsensusSet and record the diff in the
		// blockNode.
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffApply,
			ID:            scoid,
			SiacoinOutput: sco,
		}
		scods = append(scods, scod)

		// Create the dscod and add it to the list of dscods that should be
		// deleted.
		dscod := modules.DelayedSiacoinOutputDiff{
			Direction:      modules.DiffRevert,
			ID:             scoid,
			SiacoinOutput:  sco,
			MaturityHeight: pb.Height,
		}
		dscods = append(dscods, dscod)
	}
	rows.Close()

	// Sanity check - the output should not already be in siacoinOuptuts.
	for _, scod := range scods {
		if isSiacoinOutput(tx, scod.ID) {
			return errOutputAlreadyMature
		}
	}

	for _, scod := range scods {
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		if err := commitSiacoinOutputDiff(tx, scod, modules.DiffApply); err != nil {
			return err
		}
	}
	for _, dscod := range dscods {
		pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
		if err := commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply); err != nil {
			return err
		}
	}

	// Delete the delayed SC outputs.
	_, err = tx.Exec("DELETE FROM cs_dsco WHERE height = ?", pb.Height)
	return err
}

// applyMissedStorageProof adds the outputs and diffs that result from a file
// contract expiring.
func applyMissedStorageProof(tx *sql.Tx, pb *processedBlock, fcid types.FileContractID) (dscods []modules.DelayedSiacoinOutputDiff, fcd modules.FileContractDiff, err error) {
	// Sanity checks.
	fc, exists, err := findFileContract(tx, fcid)
	if err != nil {
		return nil, modules.FileContractDiff{}, err
	}
	if !exists {
		return nil, modules.FileContractDiff{}, errors.New("no file contract found")
	}

	// Add all of the outputs in the missed proof outputs to the consensus set.
	for i, mpo := range fc.MissedProofOutputs {
		// Sanity check - output should not already exist.
		spoid := modules.StorageProofOutputID(fcid, false, i)
		if isSiacoinOutput(tx, spoid) {
			return nil, modules.FileContractDiff{}, errPayoutsAlreadyPaid
		}

		// Don't add the output if the value is zero.
		dscod := modules.DelayedSiacoinOutputDiff{
			Direction:      modules.DiffApply,
			ID:             spoid,
			SiacoinOutput:  mpo,
			MaturityHeight: pb.Height + modules.MaturityDelay,
		}
		dscods = append(dscods, dscod)
	}

	// Remove the file contract from the consensus set and record the diff in
	// the blockNode.
	fcd = modules.FileContractDiff{
		Direction:    modules.DiffRevert,
		ID:           fcid,
		FileContract: fc,
	}
	return dscods, fcd, nil
}

// applyFileContractMaintenance looks for all of the file contracts that have
// expired without an appropriate storage proof, and calls 'applyMissedProof'
// for the file contract.
func applyFileContractMaintenance(tx *sql.Tx, pb *processedBlock) error {
	// Get all of the expiring file contracts.
	rows, err := tx.Query("SELECT fcid FROM cs_fcex WHERE height = ?", pb.Height)
	if err != nil {
		return err
	}

	// Iterate through the expired contracts and add them to the list.
	var dscods []modules.DelayedSiacoinOutputDiff
	var fcds []modules.FileContractDiff
	var fcids []types.FileContractID
	for rows.Next() {
		var fcid types.FileContractID
		id := make([]byte, 32)
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		copy(fcid[:], id[:])
		fcids = append(fcids, fcid)
	}
	rows.Close()

	for _, fcid := range fcids {
		amspDSCODS, fcd, err := applyMissedStorageProof(tx, pb, fcid)
		if err != nil {
			return err
		}
		fcds = append(fcds, fcd)
		dscods = append(dscods, amspDSCODS...)
	}

	for _, dscod := range dscods {
		pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
		if err := commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply); err != nil {
			return err
		}
	}
	for _, fcd := range fcds {
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		if err := commitFileContractDiff(tx, fcd, modules.DiffApply); err != nil {
			return err
		}
	}

	// Delete expired file contracts.
	_, err = tx.Exec("DELETE FROM cs_fcex WHERE height = ?", pb.Height)
	return err
}

// applyMaintenance applies block-level alterations to the consensus set.
// Maintenance is applied after all of the transactions for the block have been
// applied.
func applyMaintenance(tx *sql.Tx, pb *processedBlock) error {
	if err := applyMinerPayouts(tx, pb); err != nil {
		return err
	}
	if err := applyFoundationSubsidy(tx, pb); err != nil {
		return err
	}
	if err := applyMaturedSiacoinOutputs(tx, pb); err != nil {
		return err
	}
	return applyFileContractMaintenance(tx, pb)
}
