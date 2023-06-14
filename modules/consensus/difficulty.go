package consensus

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"math/big"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// Errors returned by this file.
var (
	// errOakHardforkIncompatibility is the error returned if Oak initialization
	// cannot begin because the consensus database was not upgraded before the
	// hardfork height.
	errOakHardforkIncompatibility = errors.New("difficulty adjustment hardfork incompatibility detected")
)

// childTargetOak sets the child target based on the total time delta and total
// hashrate of the parent block. The deltas are known for the child block,
// however we do not use the child block deltas because that would allow the
// child block to influence the target of the following block, which makes abuse
// easier in selfish mining scenarios.
func (cs *ConsensusSet) childTargetOak(parentTotalTime int64, parentTotalTarget, currentTarget modules.Target, parentHeight uint64, parentTimestamp time.Time) modules.Target {
	// Determine the delta of the current total time vs. the desired total time.
	// The desired total time is the difference between the genesis block
	// timestamp and the current block timestamp.
	var delta int64
	if parentHeight < modules.OakHardforkFixBlock {
		// This is the original code. It is incorrect, because it is comparing
		// 'expectedTime', an absolute value, to 'parentTotalTime', a value
		// which gets compressed every block. The result is that 'expectedTime'
		// is substantially larger than 'parentTotalTime' always, and that the
		// shifter is always reading that blocks have been coming out far too
		// quickly.
		expectedTime := int64(modules.BlockFrequency * parentHeight)
		delta = expectedTime - parentTotalTime
	} else {
		// This is the correct code. The expected time is an absolute time based
		// on the genesis block, and the delta is an absolute time based on the
		// timestamp of the parent block.
		//
		// Rules elsewhere in consensus ensure that the timestamp of the parent
		// block has not been manipulated by more than a few hours, which is
		// accurate enough for this logic to be safe.
		expectedTime := int64(modules.BlockFrequency * parentHeight) + modules.GenesisTimestamp.Unix()
		delta = expectedTime - parentTimestamp.Unix()
	}
	// Convert the delta in to a target block time.
	square := delta * delta
	if delta < 0 {
		// If the delta is negative, restore the negative value.
		square *= -1
	}
	shift := square / 10e6 // 10e3 second delta leads to 10 second shift.
	targetBlockTime := int64(modules.BlockFrequency) + shift

	// Clamp the block time to 1/3 and 3x the target block time.
	if targetBlockTime < int64(modules.BlockFrequency) / modules.OakMaxBlockShift {
		targetBlockTime = int64(modules.BlockFrequency) / modules.OakMaxBlockShift
	}
	if targetBlockTime > int64(modules.BlockFrequency) * modules.OakMaxBlockShift {
		targetBlockTime = int64(modules.BlockFrequency) * modules.OakMaxBlockShift
	}

	// Determine the hashrate using the total time and total target. Set a
	// minimum total time of 1 to prevent divide by zero and underflows.
	if parentTotalTime < 1 {
		parentTotalTime = 1
	}
	visibleHashrate := parentTotalTarget.Difficulty().Div64(uint64(parentTotalTime)) // Hashes per second.
	// Handle divide by zero risks.
	if visibleHashrate.IsZero() {
		visibleHashrate = visibleHashrate.Add(types.NewCurrency64(1))
	}
	if targetBlockTime == 0 {
		// This code can only possibly be triggered if the block frequency is
		// less than 3, but during testing the block frequency is 1.
		targetBlockTime = 1
	}

	// Determine the new target by multiplying the visible hashrate by the
	// target block time. Clamp it to a 0.4% difficulty adjustment.
	maxNewTarget := currentTarget.MulDifficulty(modules.OakMaxRise) // Max = difficulty increase (target decrease)
	minNewTarget := currentTarget.MulDifficulty(modules.OakMaxDrop) // Min = difficulty decrease (target increase)
	newTarget := modules.RatToTarget(new(big.Rat).SetFrac(modules.RootDepth.Int(), visibleHashrate.Mul64(uint64(targetBlockTime)).Big()))
	if newTarget.Cmp(maxNewTarget) < 0 && parentHeight + 1 != modules.ASICHardforkHeight {
		newTarget = maxNewTarget
	}
	if newTarget.Cmp(minNewTarget) > 0 && parentHeight + 1 != modules.ASICHardforkHeight {
		// This can only possibly trigger if the BlockFrequency is less than 3
		// seconds.
		newTarget = minNewTarget
	}
	return newTarget
}

// getBlockTotals returns the block totals values that get stored in
// storeBlockTotals.
func (cs *ConsensusSet) getBlockTotals(tx *sql.Tx, id types.BlockID) (totalTime int64, totalTarget modules.Target) {
	totalsBytes := make([]byte, 40)
	err := tx.QueryRow(`SELECT bytes FROM cs_oak WHERE bid = ?`, id[:]).Scan(&totalsBytes)
	if err != nil {
		cs.log.Println("ERROR: unable to retrieve Oak data:", err)
		return
	}
	totalTime = int64(binary.LittleEndian.Uint64(totalsBytes[:8]))
	copy(totalTarget[:], totalsBytes[8:])
	return
}

// storeBlockTotals computes the new total time and total target for the current
// block and stores that new time in the database. It also returns the new
// totals.
func (cs *ConsensusSet) storeBlockTotals(tx *sql.Tx, currentHeight uint64, currentBlockID types.BlockID, prevTotalTime int64, parentTimestamp, currentTimestamp time.Time, prevTotalTarget, targetOfCurrentBlock modules.Target) (newTotalTime int64, newTotalTarget modules.Target, err error) {
	// Reset the prevTotalTime to a delta of zero just before the hardfork.
	//
	// NOTICE: This code is broken, an incorrectly executed hardfork. The
	// correct thing to do was to not put in these 3 lines of code. It is
	// correct to not have them.
	//
	// This code is incorrect, and introduces an unfortunate drop in difficulty,
	// because this is an uncompreesed prevTotalTime, but really it should be
	// getting set to a compressed prevTotalTime. And, actually, a compressed
	// prevTotalTime doesn't have much meaning, so this code block shouldn't be
	// here at all. But... this is the code that was running for the block
	// 135,000 hardfork, so this code needs to stay. With the standard
	// constants, it should cause a disruptive bump that lasts only a few days.
	//
	// The disruption will be complete well before we can deploy a fix, so
	// there's no point in fixing it.
	if currentHeight == modules.OakHardforkBlock - 1 {
		prevTotalTime = int64(modules.BlockFrequency * currentHeight)
	}

	// For each value, first multiply by the decay, and then add in the new
	// delta.
	newTotalTime = (prevTotalTime * modules.OakDecayNum / modules.OakDecayDenom) + currentTimestamp.Unix() - parentTimestamp.Unix()
	newTotalTarget = prevTotalTarget.MulDifficulty(big.NewRat(modules.OakDecayNum, modules.OakDecayDenom)).AddDifficulties(targetOfCurrentBlock)

	// At the hardfork height to adjust the acceptable nonce conditions, reset
	// the total time and total target.
	if currentHeight + 1 == modules.ASICHardforkHeight {
		newTotalTime = modules.ASICHardforkTotalTime
		newTotalTarget = modules.ASICHardforkTotalTarget
	}

	// Store the new total time and total target in the database at the
	// appropriate id.
	totalsBytes := make([]byte, 40)
	binary.LittleEndian.PutUint64(totalsBytes[:8], uint64(newTotalTime))
	copy(totalsBytes[8:], newTotalTarget[:])
	_, err = tx.Exec(`
		INSERT INTO cs_oak (bid, bytes) VALUES (?, ?)
	`, currentBlockID[:], totalsBytes)
	if err != nil {
		return 0, modules.Target{}, modules.AddContext(err, "unable to store total time values")
	}
	return newTotalTime, newTotalTarget, nil
}

// initOak will initialize all of the Oak difficulty adjustment related fields.
// This is separate from the initialization process for compatibility reasons -
// some databases will not have these fields at start, so it must be checked.
//
// After oak initialization is complete, a specific field  is marked so that
// oak initialization can be skipped in the future.
func (cs *ConsensusSet) initOak(tx *sql.Tx) error {
	// Check whether the init field is set.
	var init bool
	err := tx.QueryRow("SELECT init FROM cs_oak_init WHERE id = 1").Scan(&init)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if init {
		// The Oak fields have been initialized, nothing to do.
		return nil
	}

	// If the current height is greater than the hardfork trigger date, return
	// an error and refuse to initialize.
	height := blockHeight(tx)
	if height > modules.OakHardforkBlock {
		return errOakHardforkIncompatibility
	}

	// Store base values for the genesis block.
	totalTime, totalTarget, err := cs.storeBlockTotals(tx, 0, modules.GenesisID, 0, modules.GenesisTimestamp, modules.GenesisTimestamp, modules.RootDepth, modules.RootTarget)
	if err != nil {
		return modules.AddContext(err, "unable to store genesis block totals")
	}

	// The Oak fields has not been initialized, scan through the consensus set
	// and set the fields for each block.
	parentTimestamp := modules.GenesisTimestamp
	parentChildTarget := modules.RootTarget
	for i := uint64(1); i <= height; i++ { // Skip Genesis block.
		// Fetch the processed block for the current block.
		id, err := getBlockAtHeight(tx, i)
		if err != nil {
			return modules.AddContext(err, "unable to find block at height")
		}
		pb, exists, err := cs.findBlockByID(tx, id)
		if err != nil {
			return modules.AddContext(err, "unable to find block from id")
		}
		if !exists {
			return errors.New("unable to find block from id")
		}

		// Calculate and store the new block totals.
		totalTime, totalTarget, err = cs.storeBlockTotals(tx, i, id, totalTime, parentTimestamp, pb.Block.Timestamp, totalTarget, parentChildTarget)
		if err != nil {
			return modules.AddContext(err, "unable to store updated block totals")
		}
		// Update the previous values.
		parentTimestamp = pb.Block.Timestamp
		parentChildTarget = pb.ChildTarget
	}

	// Tag the initialization field, indicating that initialization has
	// completed.
	_, err = tx.Exec("REPLACE INTO cs_oak_init (id, init) VALUES (1, TRUE)")

	return err
}
