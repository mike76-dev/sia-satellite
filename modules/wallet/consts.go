package wallet

import (
	"go.sia.tech/core/types"
)

const (
	// defragBatchSize defines how many outputs are combined during one defrag.
	defragBatchSize = 35

	// defragStartIndex is the number of outputs to skip over when performing a
	// defrag.
	defragStartIndex = 10

	// defragThreshold is the number of outputs a wallet is allowed before it is
	// defragmented.
	defragThreshold = 50
)

const (
	// lookaheadBuffer together with lookaheadRescanThreshold defines the constant part
	// of the maxLookahead.
	lookaheadBuffer = uint64(4000)

	// lookaheadRescanThreshold is the number of keys in the lookahead that will be
	// generated before a complete wallet rescan is initialized.
	lookaheadRescanThreshold = uint64(1000)
)

var (
	// Specifiers.
	specifierMinerPayout  = types.NewSpecifier("miner payout")
	specifierMinerFee     = types.NewSpecifier("miner fee")
	specifierSiacoinInput = types.NewSpecifier("siacoin input")
	specifierSiafundInput = types.NewSpecifier("siafund input")
	specifierClaimOutput  = types.NewSpecifier("claim output")
)

// maxLookahead returns the size of the lookahead for a given seed progress
// which usually is the current primarySeedProgress.
func maxLookahead(start uint64) uint64 {
	return start + lookaheadRescanThreshold + lookaheadBuffer + start / 10
}
