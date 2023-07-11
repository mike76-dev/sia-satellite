package contractor

import (
	"errors"
)

const (
	// If the watchdog sees one of its contractor's file contracts appear in a
	// reverted block, it will begin watching for it again with some flexibility
	// for when it appears in the future.
	reorgLeeway = 24
)

var (
	// waitTime is the number of blocks the watchdog will wait to see a
	// pendingContract onchain before double-spending it.
	waitTime = uint64(288)
)

var (
	errAlreadyWatchingContract = errors.New("Watchdog already watching contract with this ID")
	errTxnNotInSet             = errors.New("Transaction not in set; cannot remove from set.")
)
