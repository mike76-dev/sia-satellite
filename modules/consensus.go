package modules

import (
	"encoding/binary"
	"errors"
	"math/big"
	"time"

	"go.sia.tech/core/types"
)

const (
	// ConsensusDir is the name of the directory used for all of the consensus
	// persistence files.
	ConsensusDir = "consensus"

	// DiffApply indicates that a diff is being applied to the consensus set.
	DiffApply DiffDirection = true

	// DiffRevert indicates that a diff is being reverted from the consensus
	// set.
	DiffRevert DiffDirection = false
)

var (
	// ConsensusChangeBeginning is a special consensus change id that tells the
	// consensus set to provide all consensus changes starting from the very
	// first diff, which includes the genesis block diff.
	ConsensusChangeBeginning = ConsensusChangeID{}

	// ConsensusChangeRecent is a special consensus change id that tells the
	// consensus set to provide the most recent consensus change, instead of
	// starting from a specific value (which may not be known to the caller).
	ConsensusChangeRecent = ConsensusChangeID{1}

	// ErrBlockKnown is an error indicating that a block is already in the
	// database.
	ErrBlockKnown = errors.New("block already present in database")

	// ErrBlockUnsolved indicates that a block did not meet the required POW
	// target.
	ErrBlockUnsolved = errors.New("block does not meet target")

	// ErrInvalidConsensusChangeID indicates that ConsensusSetPersistSubscribe
	// was called with a consensus change id that is not recognized. Most
	// commonly, this means that the consensus set was deleted or replaced and
	// now the module attempting the subscription has desynchronized. This error
	// should be handled by the module, and not reported to the user.
	ErrInvalidConsensusChangeID = errors.New("consensus subscription has invalid id - files are inconsistent")

	// ErrNonExtendingBlock indicates that a block is valid but does not result
	// in a fork that is the heaviest known fork - the consensus set has not
	// changed as a result of seeing the block.
	ErrNonExtendingBlock = errors.New("block does not extend the longest fork")
)

type (
	// ConsensusChangeID is the id of a consensus change.
	ConsensusChangeID types.Hash256

	// A DiffDirection indicates the "direction" of a diff, either applied or
	// reverted. A bool is used to restrict the value to these two possibilities.
	DiffDirection bool

	// A ConsensusSetSubscriber is an object that receives updates to the consensus
	// set every time there is a change in consensus.
	ConsensusSetSubscriber interface {
		// ProcessConsensusChange sends a consensus update to a module through
		// a function call. Updates will always be sent in the correct order.
		// There may not be any reverted blocks, but there will always be
		// applied blocks.
		ProcessConsensusChange(ConsensusChange)
	}

	// ConsensusChangeDiffs is a collection of diffs caused by a single block.
	// If the block was reverted, the individual diff directions are inverted.
	// For example, a block that spends an output and creates a miner payout
	// would have one SiacoinOutputDiff with direction DiffRevert and one
	// DelayedSiacoinOutputDiff with direction DiffApply. If the same block
	// were reverted, the SCOD would have direction DiffApply and the DSCOD
	// would have direction DiffRevert.
	ConsensusChangeDiffs struct {
		SiacoinOutputDiffs        []SiacoinOutputDiff
		FileContractDiffs         []FileContractDiff
		SiafundOutputDiffs        []SiafundOutputDiff
		DelayedSiacoinOutputDiffs []DelayedSiacoinOutputDiff
		SiafundPoolDiffs          []SiafundPoolDiff
	}

	// A ConsensusChange enumerates a set of changes that occurred to the consensus set.
	ConsensusChange struct {
		// ID is a unique id for the consensus change derived from the reverted
		// and applied blocks.
		ID ConsensusChangeID

		// BlockHeight is the height of the chain after all blocks included in
		// this change have been reverted and applied.
		BlockHeight uint64

		// RevertedBlocks is the list of blocks that were reverted by the change.
		// The reverted blocks were always all reverted before the applied blocks
		// were applied. The revered blocks are presented in the order that they
		// were reverted.
		RevertedBlocks []types.Block

		// AppliedBlocks is the list of blocks that were applied by the change. The
		// applied blocks are always all applied after all the reverted blocks were
		// reverted. The applied blocks are presented in the order that they were
		// applied.
		AppliedBlocks []types.Block

		// RevertedDiffs is the set of diffs caused by reverted blocks. Each
		// element corresponds to a block in RevertedBlocks.
		RevertedDiffs []ConsensusChangeDiffs

		// AppliedDiffs is the set of diffs caused by applied blocks. Each
		// element corresponds to a block in AppliedBlocks.
		AppliedDiffs []ConsensusChangeDiffs

		// ConsensusChangeDiffs is the concatenation of all RevertedDiffs and
		// AppliedDiffs.
		ConsensusChangeDiffs

		// ChildTarget defines the target of any block that would be the child
		// of the block most recently appended to the consensus set.
		ChildTarget Target

		// MinimumValidChildTimestamp defines the minimum allowed timestamp for
		// any block that is the child of the block most recently appended to
		// the consensus set.
		MinimumValidChildTimestamp time.Time

		// Synced indicates whether or not the ConsensusSet is synced with its
		// peers.
		Synced bool

		// TryTransactionSet is an unlocked version of
		// ConsensusSet.TryTransactionSet. This allows the TryTransactionSet
		// function to be called by a subscriber during
		// ProcessConsensusChange.
		TryTransactionSet func([]types.Transaction) (ConsensusChange, error)
	}

	// A SiacoinOutputDiff indicates the addition or removal of a SiacoinOutput in
	// the consensus set.
	SiacoinOutputDiff struct {
		Direction     DiffDirection
		ID            types.SiacoinOutputID
		SiacoinOutput types.SiacoinOutput
	}

	// A FileContractDiff indicates the addition or removal of a FileContract in
	// the consensus set.
	FileContractDiff struct {
		Direction    DiffDirection
		ID           types.FileContractID
		FileContract types.FileContract
	}

	// A SiafundOutputDiff indicates the addition or removal of a SiafundOutput in
	// the consensus set.
	SiafundOutputDiff struct {
		Direction     DiffDirection
		ID            types.SiafundOutputID
		SiafundOutput types.SiafundOutput
		ClaimStart    types.Currency
	}

	// A DelayedSiacoinOutputDiff indicates the introduction of a siacoin output
	// that cannot be spent until after maturing for 144 blocks. When the output
	// has matured, a SiacoinOutputDiff will be provided.
	DelayedSiacoinOutputDiff struct {
		Direction      DiffDirection
		ID             types.SiacoinOutputID
		SiacoinOutput  types.SiacoinOutput
		MaturityHeight uint64
	}

	// A SiafundPoolDiff contains the value of the siafundPool before the block
	// was applied, and after the block was applied. When applying the diff, set
	// siafundPool to 'Adjusted'. When reverting the diff, set siafundPool to
	// 'Previous'.
	SiafundPoolDiff struct {
		Direction DiffDirection
		Previous  types.Currency
		Adjusted  types.Currency
	}

	// A ConsensusSet accepts blocks and builds an understanding of network
	// consensus.
	ConsensusSet interface {
		Alerter

		// AcceptBlock adds a block to consensus. An error will be returned if the
		// block is invalid, has been seen before, is an orphan, or doesn't
		// contribute to the heaviest fork known to the consensus set. If the block
		// does not become the head of the heaviest known fork but is otherwise
		// valid, it will be remembered by the consensus set but an error will
		// still be returned.
		AcceptBlock(types.Block) error

		// BlockAtHeight returns the block found at the input height, with a
		// bool to indicate whether that block exists.
		BlockAtHeight(uint64) (types.Block, bool)

		// BlocksByID returns a block found for a given ID and its height, with
		// a bool to indicate whether that block exists.
		BlockByID(types.BlockID) (types.Block, uint64, bool)

		// ChildTarget returns the target required to extend the current heaviest
		// fork. This function is typically used by miners looking to extend the
		// heaviest fork.
		ChildTarget(types.BlockID) (Target, bool)

		// Close will shut down the consensus set, giving the module enough time to
		// run any required closing routines.
		Close() error

		// ConsensusSetSubscribe adds a subscriber to the list of subscribers
		// and gives them every consensus change that has occurred since the
		// change with the provided id. There are a few special cases,
		// described by the ConsensusChangeX variables in this package.
		// A channel can be provided to abort the subscription process.
		ConsensusSetSubscribe(ConsensusSetSubscriber, ConsensusChangeID, <-chan struct{}) error

		// CurrentBlock returns the latest block in the heaviest known
		// blockchain.
		CurrentBlock() types.Block

		// Height returns the current height of consensus.
		Height() uint64

		// Synced returns true if the consensus set is synced with the network.
		Synced() bool

		// InCurrentPath returns true if the block id presented is found in the
		// current path, false otherwise.
		InCurrentPath(types.BlockID) bool

		// MinimumValidChildTimestamp returns the earliest timestamp that is
		// valid on the current longest fork according to the consensus set. This is
		// a required piece of information for the miner, who could otherwise be at
		// risk of mining invalid blocks.
		MinimumValidChildTimestamp(types.BlockID) (time.Time, bool)

		// StorageProofSegment returns the segment to be used in the storage proof for
		// a given file contract.
		StorageProofSegment(types.FileContractID) (uint64, error)

		// FoundationUnlockHashes returns the current primary and failsafe
		// Foundation UnlockHashes.
		FoundationUnlockHashes() (primary, failsafe types.Address)

		// TryTransactionSet checks whether the transaction set would be valid if
		// it were added in the next block. A consensus change is returned
		// detailing the diffs that would result from the application of the
		// transaction.
		TryTransactionSet([]types.Transaction) (ConsensusChange, error)

		// Unsubscribe removes a subscriber from the list of subscribers,
		// allowing for garbage collection and rescanning. If the subscriber is
		// not found in the subscriber database, no action is taken.
		Unsubscribe(ConsensusSetSubscriber)
	}
)

// AppendDiffs appends a set of diffs to cc.
func (cc *ConsensusChange) AppendDiffs(diffs ConsensusChangeDiffs) {
	cc.SiacoinOutputDiffs = append(cc.SiacoinOutputDiffs, diffs.SiacoinOutputDiffs...)
	cc.FileContractDiffs = append(cc.FileContractDiffs, diffs.FileContractDiffs...)
	cc.SiafundOutputDiffs = append(cc.SiafundOutputDiffs, diffs.SiafundOutputDiffs...)
	cc.DelayedSiacoinOutputDiffs = append(cc.DelayedSiacoinOutputDiffs, diffs.DelayedSiacoinOutputDiffs...)
	cc.SiafundPoolDiffs = append(cc.SiafundPoolDiffs, diffs.SiafundPoolDiffs...)
}

// InitialHeight returns the height of the consensus before blocks are applied.
func (cc *ConsensusChange) InitialHeight() uint64 {
	if cc.BlockHeight == 0 {
		return 0
	}
	return cc.BlockHeight - uint64(len(cc.AppliedBlocks))
}

// EncodeTo implements types.EncoderTo.
func (cc *ConsensusChange) EncodeTo(e *types.Encoder) {
	e.Write(cc.ID[:])
	e.WritePrefix(len(cc.RevertedBlocks))
	for _, rb := range cc.RevertedBlocks {
		rb.EncodeTo(e)
	}
	e.WritePrefix(len(cc.AppliedBlocks))
	for _, ab := range cc.AppliedBlocks {
		ab.EncodeTo(e)
	}
	e.WritePrefix(len(cc.RevertedDiffs))
	for _, rd := range cc.RevertedDiffs {
		rd.EncodeTo(e)
	}
	e.WritePrefix(len(cc.AppliedDiffs))
	for _, ad := range cc.AppliedDiffs {
		ad.EncodeTo(e)
	}
	e.Write(cc.ChildTarget[:])
	e.WriteTime(cc.MinimumValidChildTimestamp)
	e.WriteBool(cc.Synced)
}

// EncodeTo implements types.EncoderTo.
func (ccd *ConsensusChangeDiffs) EncodeTo(e *types.Encoder) {
	e.WritePrefix(len(ccd.SiacoinOutputDiffs))
	for _, sod := range ccd.SiacoinOutputDiffs {
		sod.EncodeTo(e)
	}
	e.WritePrefix(len(ccd.FileContractDiffs))
	for _, fcd := range ccd.FileContractDiffs {
		fcd.EncodeTo(e)
	}
	e.WritePrefix(len(ccd.SiafundOutputDiffs))
	for _, sfd := range ccd.SiafundOutputDiffs {
		sfd.EncodeTo(e)
	}
	e.WritePrefix(len(ccd.DelayedSiacoinOutputDiffs))
	for _, dsod := range ccd.DelayedSiacoinOutputDiffs {
		dsod.EncodeTo(e)
	}
	e.WritePrefix(len(ccd.SiafundPoolDiffs))
	for _, spd := range ccd.SiafundPoolDiffs {
		spd.EncodeTo(e)
	}
}

// EncodeTo implements types.EncoderTo.
func (sod *SiacoinOutputDiff) EncodeTo(e *types.Encoder) {
	e.WriteBool(bool(sod.Direction))
	sod.ID.EncodeTo(e)
	sod.SiacoinOutput.EncodeTo(e)
}

// EncodeTo implements types.EncoderTo.
func (fcd *FileContractDiff) EncodeTo(e *types.Encoder) {
	e.WriteBool(bool(fcd.Direction))
	fcd.ID.EncodeTo(e)
	fcd.FileContract.EncodeTo(e)
}

// EncodeTo implements types.EncoderTo.
func (sfd *SiafundOutputDiff) EncodeTo(e *types.Encoder) {
	e.WriteBool(bool(sfd.Direction))
	sfd.ID.EncodeTo(e)
	types.NewCurrency64(sfd.SiafundOutput.Value).EncodeTo(e)
	sfd.SiafundOutput.Address.EncodeTo(e)
	sfd.ClaimStart.EncodeTo(e)
}

// EncodeTo implements types.EncoderTo.
func (dsod *DelayedSiacoinOutputDiff) EncodeTo(e *types.Encoder) {
	e.WriteBool(bool(dsod.Direction))
	dsod.ID.EncodeTo(e)
	dsod.SiacoinOutput.EncodeTo(e)
	e.WriteUint64(dsod.MaturityHeight)
}

// EncodeTo implements types.EncoderTo.
func (spd *SiafundPoolDiff) EncodeTo(e *types.Encoder) {
	e.WriteBool(bool(spd.Direction))
	spd.Previous.EncodeTo(e)
	spd.Adjusted.EncodeTo(e)
}

// DecodeFrom implements types.DecoderFrom.
func (cc *ConsensusChange) DecodeFrom(d *types.Decoder) {
	d.Read(cc.ID[:])
	l := d.ReadPrefix()
	cc.RevertedBlocks = make([]types.Block, l)
	for i := 0; i < l; i++ {
		cc.RevertedBlocks[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	cc.AppliedBlocks = make([]types.Block, l)
	for i := 0; i < l; i++ {
		cc.AppliedBlocks[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	cc.RevertedDiffs = make([]ConsensusChangeDiffs, l)
	for i := 0; i < l; i++ {
		cc.RevertedDiffs[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	cc.AppliedDiffs = make([]ConsensusChangeDiffs, l)
	for i := 0; i < l; i++ {
		cc.AppliedDiffs[i].DecodeFrom(d)
	}
	d.Read(cc.ChildTarget[:])
	cc.MinimumValidChildTimestamp = d.ReadTime()
	cc.Synced = d.ReadBool()
}

// DecodeFrom implements types.DecoderFrom.
func (ccd *ConsensusChangeDiffs) DecodeFrom(d *types.Decoder) {
	l := d.ReadPrefix()
	ccd.SiacoinOutputDiffs = make([]SiacoinOutputDiff, l)
	for i := 0; i < l; i++ {
		ccd.SiacoinOutputDiffs[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	ccd.FileContractDiffs = make([]FileContractDiff, l)
	for i := 0; i < l; i++ {
		ccd.FileContractDiffs[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	ccd.SiafundOutputDiffs = make([]SiafundOutputDiff, l)
	for i := 0; i < l; i++ {
		ccd.SiafundOutputDiffs[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	ccd.DelayedSiacoinOutputDiffs = make([]DelayedSiacoinOutputDiff, l)
	for i := 0; i < l; i++ {
		ccd.DelayedSiacoinOutputDiffs[i].DecodeFrom(d)
	}
	l = d.ReadPrefix()
	ccd.SiafundPoolDiffs = make([]SiafundPoolDiff, l)
	for i := 0; i < l; i++ {
		ccd.SiafundPoolDiffs[i].DecodeFrom(d)
	}
}

// DecodeFrom implements types.DecoderFrom.
func (sod *SiacoinOutputDiff) DecodeFrom(d *types.Decoder) {
	sod.Direction = DiffDirection(d.ReadBool())
	sod.ID.DecodeFrom(d)
	sod.SiacoinOutput.DecodeFrom(d)
}

// DecodeFrom implements types.DecoderFrom.
func (fcd *FileContractDiff) DecodeFrom(d *types.Decoder) {
	fcd.Direction = DiffDirection(d.ReadBool())
	fcd.ID.DecodeFrom(d)
	fcd.FileContract.DecodeFrom(d)
}

// DecodeFrom implements types.DecoderFrom.
func (sfd *SiafundOutputDiff) DecodeFrom(d *types.Decoder) {
	sfd.Direction = DiffDirection(d.ReadBool())
	sfd.ID.DecodeFrom(d)
	var val types.Currency
	val.DecodeFrom(d)
	sfd.SiafundOutput.Value = val.Lo
	sfd.SiafundOutput.Address.DecodeFrom(d)
	sfd.ClaimStart.DecodeFrom(d)
}

// DecodeFrom implements types.DecoderFrom.
func (dsod *DelayedSiacoinOutputDiff) DecodeFrom(d *types.Decoder) {
	dsod.Direction = DiffDirection(d.ReadBool())
	dsod.ID.DecodeFrom(d)
	dsod.SiacoinOutput.DecodeFrom(d)
	dsod.MaturityHeight = d.ReadUint64()
}

// DecodeFrom implements types.DecoderFrom.
func (spd *SiafundPoolDiff) DecodeFrom(d *types.Decoder) {
	spd.Direction = DiffDirection(d.ReadBool())
	spd.Previous.DecodeFrom(d)
	spd.Adjusted.DecodeFrom(d)
}

// String returns the ConsensusChangeID as a string.
func (ccID ConsensusChangeID) String() string {
	return types.Hash256(ccID).String()
}

type (
	// A Target is a hash that a block's ID must be "less than" in order for
	// the block to be considered valid. Miners vary the block's 'Nonce' field
	// in order to brute-force such an ID. The inverse of a Target is called
	// the "difficulty," because it is proportional to the amount of time
	// required to brute-force the Target.
	Target types.Hash256
)

// AddDifficulties returns the resulting target with the difficulty of 'x' and
// 'y' are added together. Note that the difficulty is the inverse of the
// target. The sum is defined by:
//
//	sum(x, y) = 1/(1/x + 1/y)
func (x Target) AddDifficulties(y Target) (t Target) {
	sumDifficulty := new(big.Rat).Add(x.Inverse(), y.Inverse())
	return RatToTarget(new(big.Rat).Inv(sumDifficulty))
}

// Cmp compares the difficulties of two targets. Note that the difficulty is
// the inverse of the target. The results are as follows:
//
//	-1 if x <  y
//	 0 if x == y
//	+1 if x >  y
func (x Target) Cmp(y Target) int {
	return x.Int().Cmp(y.Int())
}

// Difficulty returns the difficulty associated with a given target.
func (x Target) Difficulty() types.Currency {
	buf := make([]byte, 16)
	if x == (Target{}) {
		rb := RootDepth.Int().Bytes()
		copy(buf[16 - len(rb):], rb[:])
		return types.NewCurrency(binary.BigEndian.Uint64(buf[8:]), binary.BigEndian.Uint64(buf[:8]))
	}
	b := new(big.Int).Div(RootDepth.Int(), x.Int()).Bytes()
	copy(buf[16 - len(b):], b[:])
	return types.NewCurrency(binary.BigEndian.Uint64(buf[8:]), binary.BigEndian.Uint64(buf[:8]))
}

// Int converts a Target to a big.Int.
func (x Target) Int() *big.Int {
	return new(big.Int).SetBytes(x[:])
}

// IntToTarget converts a big.Int to a Target.
func IntToTarget(i *big.Int) (t Target) {
	// Check for negatives.
	if i.Sign() < 0 {
		return Target{}
	} else {
		// In the event of overflow, return the maximum.
		if i.BitLen() > 256 {
			return RootDepth
		}
		b := i.Bytes()
		offset := len(t[:]) - len(b)
		copy(t[offset:], b)
	}
	return
}

// Inverse returns the inverse of a Target as a big.Rat
func (x Target) Inverse() *big.Rat {
	return new(big.Rat).Inv(x.Rat())
}

// MulDifficulty multiplies the difficulty of a target by y. The product is defined by:
// y / x
func (x Target) MulDifficulty(y *big.Rat) (t Target) {
	product := new(big.Rat).Mul(y, x.Inverse())
	product = product.Inv(product)
	return RatToTarget(product)
}

// Rat converts a Target to a big.Rat.
func (x Target) Rat() *big.Rat {
	return new(big.Rat).SetInt(x.Int())
}

// RatToTarget converts a big.Rat to a Target.
func RatToTarget(r *big.Rat) (t Target) {
	if r.Num().Sign() < 0 {
		return Target{}
	} else {
		i := new(big.Int).Div(r.Num(), r.Denom())
		t = IntToTarget(i)
	}
	return
}

// SubtractDifficulties returns the resulting target with the difficulty of 'x'
// is subtracted from the target with difficulty 'y'. Note that the difficulty
// is the inverse of the target. The difference is defined by:
//
//	sum(x, y) = 1/(1/x - 1/y)
func (x Target) SubtractDifficulties(y Target) (t Target) {
	sumDifficulty := new(big.Rat).Sub(x.Inverse(), y.Inverse())
	return RatToTarget(new(big.Rat).Inv(sumDifficulty))
}
