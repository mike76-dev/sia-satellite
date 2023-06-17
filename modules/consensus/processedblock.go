package consensus

import (
	"database/sql"
	"math/big"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// SurpassThreshold is a percentage that dictates how much heavier a competing
// chain has to be before the node will switch to mining on that chain. This is
// not a consensus rule. This percentage is only applied to the most recent
// block, not the entire chain; see blockNode.heavierThan.
//
// If no threshold were in place, it would be possible to manipulate a block's
// timestamp to produce a sufficiently heavier block.
var SurpassThreshold = big.NewRat(20, 100)

// processedBlock is a copy/rename of blockNode, with the pointers to
// other blockNodes replaced with block ID's, and all the fields
// exported, so that a block node can be marshalled.
type processedBlock struct {
	Block       types.Block
	Height      uint64
	Depth       modules.Target
	ChildTarget modules.Target

	DiffsGenerated            bool
	SiacoinOutputDiffs        []modules.SiacoinOutputDiff
	FileContractDiffs         []modules.FileContractDiff
	SiafundOutputDiffs        []modules.SiafundOutputDiff
	DelayedSiacoinOutputDiffs []modules.DelayedSiacoinOutputDiff
	SiafundPoolDiffs          []modules.SiafundPoolDiff

	ConsensusChecksum types.Hash256
}

// EncodeTo implements types.EncoderTo.
func (pb *processedBlock) EncodeTo(e *types.Encoder) {
	pb.Block.EncodeTo(e)
	e.WriteUint64(pb.Height)
	e.Write(pb.Depth[:])
	e.Write(pb.ChildTarget[:])
	e.WriteBool(pb.DiffsGenerated)
	e.WritePrefix(len(pb.SiacoinOutputDiffs))
	for _, sod := range pb.SiacoinOutputDiffs {
		sod.EncodeTo(e)
	}
	e.WritePrefix(len(pb.FileContractDiffs))
	for _, fcd := range pb.FileContractDiffs {
		fcd.EncodeTo(e)
	}
	e.WritePrefix(len(pb.SiafundOutputDiffs))
	for _, sfd := range pb.SiafundOutputDiffs {
		sfd.EncodeTo(e)
	}
	e.WritePrefix(len(pb.DelayedSiacoinOutputDiffs))
	for _, dsod := range pb.DelayedSiacoinOutputDiffs {
		dsod.EncodeTo(e)
	}
	e.WritePrefix(len(pb.SiafundPoolDiffs))
	for _, spd := range pb.SiafundPoolDiffs {
		spd.EncodeTo(e)
	}
}

// DecodeFrom implements types.DecoderFrom.
func (pb *processedBlock) DecodeFrom(d *types.Decoder) {
	pb.Block.DecodeFrom(d)
	pb.Height = d.ReadUint64()
	d.Read(pb.Depth[:])
	d.Read(pb.ChildTarget[:])
	pb.DiffsGenerated = d.ReadBool()
	pb.SiacoinOutputDiffs = make([]modules.SiacoinOutputDiff, d.ReadPrefix())
	for i := 0; i < len(pb.SiacoinOutputDiffs); i++ {
		pb.SiacoinOutputDiffs[i].DecodeFrom(d)
	}
	pb.FileContractDiffs = make([]modules.FileContractDiff, d.ReadPrefix())
	for i := 0; i < len(pb.FileContractDiffs); i++ {
		pb.FileContractDiffs[i].DecodeFrom(d)
	}
	pb.SiafundOutputDiffs = make([]modules.SiafundOutputDiff, d.ReadPrefix())
	for i := 0; i < len(pb.SiafundOutputDiffs); i++ {
		pb.SiafundOutputDiffs[i].DecodeFrom(d)
	}
	pb.DelayedSiacoinOutputDiffs = make([]modules.DelayedSiacoinOutputDiff, d.ReadPrefix())
	for i := 0; i < len(pb.DelayedSiacoinOutputDiffs); i++ {
		pb.DelayedSiacoinOutputDiffs[i].DecodeFrom(d)
	}
	pb.SiafundPoolDiffs = make([]modules.SiafundPoolDiff, d.ReadPrefix())
	for i := 0; i < len(pb.SiafundPoolDiffs); i++ {
		pb.SiafundPoolDiffs[i].DecodeFrom(d)
	}
}

// heavierThan returns true if the blockNode is sufficiently heavier than
// 'cmp'. 'cmp' is expected to be the current block node. "Sufficient" means
// that the weight of 'bn' exceeds the weight of 'cmp' by:
//
//	(the target of 'cmp' * 'Surpass Threshold')
func (pb *processedBlock) heavierThan(cmp *processedBlock) bool {
	requirement := cmp.Depth.AddDifficulties(cmp.ChildTarget.MulDifficulty(SurpassThreshold))
	return requirement.Cmp(pb.Depth) > 0 // Inversed, because the smaller target is actually heavier.
}

// childDepth returns the depth of a blockNode's child nodes. The depth is the
// "sum" of the current depth and current difficulty. See target.Add for more
// detailed information.
func (pb *processedBlock) childDepth() modules.Target {
	return pb.Depth.AddDifficulties(pb.ChildTarget)
}

// targetAdjustmentBase returns the magnitude that the target should be
// adjusted by before a clamp is applied.
func (cs *ConsensusSet) targetAdjustmentBase(tx *sql.Tx, pb *processedBlock) *big.Rat {
	// Grab the block that was generated 'TargetWindow' blocks prior to the
	// parent. If there are not 'TargetWindow' blocks yet, stop at the genesis
	// block.
	var windowSize uint64
	parentID := pb.Block.ParentID
	currentID := cs.blockID(pb.Block)
	var err error
	for windowSize = 0; windowSize < modules.TargetWindow && parentID != (types.BlockID{}); windowSize++ {
		currentID = parentID
		parentID, _, err = cs.getParentID(tx, parentID)
		if err != nil {
			cs.log.Println("ERROR: unable to find parent ID:", err)
			return nil
		}
	}

	current, exists, err := cs.findBlockByID(tx, currentID)
	if err != nil || !exists {
		cs.log.Println("ERROR: unable to find block:", err)
		return nil
	}
	timestamp := current.Block.Timestamp

	// The target of a child is determined by the amount of time that has
	// passed between the generation of its immediate parent and its
	// TargetWindow'th parent. The expected amount of seconds to have passed is
	// TargetWindow*BlockFrequency. The target is adjusted in proportion to how
	// time has passed vs. the expected amount of time to have passed.
	//
	// The target is converted to a big.Rat to provide infinite precision
	// during the calculation. The big.Rat is just the int representation of a
	// target.
	timePassed := pb.Block.Timestamp.Unix() - timestamp.Unix()
	expectedTimePassed := modules.BlockFrequency * windowSize
	return big.NewRat(int64(timePassed), int64(expectedTimePassed))
}

// clampTargetAdjustment returns a clamped version of the base adjustment
// value. The clamp keeps the maximum adjustment to ~7x every 2000 blocks. This
// ensures that raising and lowering the difficulty requires a minimum amount
// of total work, which prevents certain classes of difficulty adjusting
// attacks.
func clampTargetAdjustment(base *big.Rat) *big.Rat {
	if base.Cmp(modules.MaxTargetAdjustmentUp) > 0 {
		return modules.MaxTargetAdjustmentUp
	} else if base.Cmp(modules.MaxTargetAdjustmentDown) < 0 {
		return modules.MaxTargetAdjustmentDown
	}
	return base
}

// setChildTarget computes the target of a blockNode's child. All children of a node
// have the same target.
func (cs *ConsensusSet) setChildTarget(tx *sql.Tx, pb *processedBlock) {
	// Fetch the parent block.
	parent, exists, err := cs.findBlockByID(tx, pb.Block.ParentID)
	if err != nil || !exists {
		cs.log.Println("ERROR: unable to find block:", err)
		return
	}

	if pb.Height % (modules.TargetWindow / 2) != 0 {
		pb.ChildTarget = parent.ChildTarget
		return
	}

	adjustment := clampTargetAdjustment(cs.targetAdjustmentBase(tx, pb))
	adjustedRatTarget := new(big.Rat).Mul(parent.ChildTarget.Rat(), adjustment)
	pb.ChildTarget = modules.RatToTarget(adjustedRatTarget)
}

// newChild creates a blockNode from a block and adds it to the parent's set of
// children. The new node is also returned. It necessarily modifies the database.
func (cs *ConsensusSet) newChild(tx *sql.Tx, pb *processedBlock, b types.Block) (*processedBlock, error) {
	// Create the child node.
	childID := cs.blockID(b)
	child := &processedBlock{
		Block:  b,
		Height: pb.Height + 1,
		Depth:  pb.childDepth(),
	}

	// Push the total values for this block into the oak difficulty adjustment
	// bucket. The previous totals are required to compute the new totals.
	prevTotalTime, prevTotalTarget, err := cs.getBlockTotals(tx, b.ParentID)
	if err != nil {
		cs.log.Println("ERROR: couldn't retrieve block totals:", err)
		return nil, err
	}
	_, _, err = cs.storeBlockTotals(tx, child.Height, childID, prevTotalTime, pb.Block.Timestamp, b.Timestamp, prevTotalTarget, pb.ChildTarget)
	if err != nil {
		cs.log.Println("ERROR: couldn't save block totals:", err)
		return nil, err
	}

	// Use the difficulty adjustment algorithm to set the target of the child
	// block and put the new processed block into the database.
	if pb.Height < modules.OakHardforkBlock {
		cs.setChildTarget(tx, child)
	} else {
		child.ChildTarget = cs.childTargetOak(prevTotalTime, prevTotalTarget, pb.ChildTarget, pb.Height, pb.Block.Timestamp)
	}
	err = cs.saveBlock(tx, childID, child)
	if err != nil {
		cs.log.Println("ERROR: couldn't save new block:", err)
		return nil, err
	}

	return child, nil
}
