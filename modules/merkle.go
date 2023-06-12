package modules

import (
	"bytes"

	"gitlab.com/NebulousLabs/merkletree/merkletree-blake"

	"go.sia.tech/core/types"
)

const (
	// SegmentSize is the chunk size that is used when taking the Merkle root
	// of a file. 64 is chosen because bandwidth is scarce and it optimizes for
	// the smallest possible storage proofs. Using a larger base, even 256
	// bytes, would result in substantially faster hashing, but the bandwidth
	// tradeoff was deemed to be more important, as blockchain space is scarce.
	SegmentSize = 64
)

// MerkleTree wraps merkletree.Tree, changing some of the function definitions
// to assume sia-specific constants and return sia-specific types.
type MerkleTree struct {
	merkletree.Tree
}

// NewTree returns a MerkleTree, which can be used for getting Merkle roots and
// Merkle proofs on data. See merkletree.Tree for more details.
func NewTree() *MerkleTree {
	return &MerkleTree{*merkletree.New()}
}

// PushObject encodes and adds the hash of the encoded object to the tree as a
// leaf.
func (t *MerkleTree) PushObject(obj types.EncoderTo) {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	obj.EncodeTo(e)
	e.Flush()
	t.Push(buf.Bytes())
}

// Root is a redefinition of merkletree.Tree.Root, returning a types.Hash256
// instead of a []byte.
func (t *MerkleTree) Root() (h types.Hash256) {
	return types.Hash256(t.Tree.Root())
}

// CalculateLeaves calculates the number of leaves that would be pushed from
// data of size 'dataSize'.
func CalculateLeaves(dataSize uint64) uint64 {
	numSegments := dataSize / SegmentSize
	if dataSize == 0 || dataSize % SegmentSize != 0 {
		numSegments++
	}
	return numSegments
}

// VerifySegment will verify that a segment, given the proof, is a part of a
// Merkle root.
func VerifySegment(base []byte, hashSet []types.Hash256, numSegments, proofIndex uint64, root types.Hash256) bool {
	// Convert base and hashSet to proofSet
	proofSet := make([][32]byte, len(hashSet) + 1)
	proofSet[0] = merkletree.LeafSum(base)
	for i := range hashSet {
		proofSet[i + 1] = [32]byte(hashSet[i])
	}
	return merkletree.VerifyProof(root, proofSet, proofIndex, numSegments)
}
