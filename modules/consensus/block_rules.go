package consensus

import (
	"database/sql"
	"sort"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

type (
	// timestampSlice is an array of timestamps.
	timestampSlice []time.Time
)

// Len is part of sort.Interface.
func (ts timestampSlice) Len() int {
	return len(ts)
}

// Less is part of sort.Interface.
func (ts timestampSlice) Less(i, j int) bool {
	return ts[i].Before(ts[j])
}

// Swap is part of sort.Interface.
func (ts timestampSlice) Swap(i, j int) {
	ts[i], ts[j] = ts[j], ts[i]
}

// minimumValidChildTimestamp returns the earliest timestamp that a child node
// can have while still being valid.
func (cs *ConsensusSet) minimumValidChildTimestamp(tx *sql.Tx, pb *processedBlock) time.Time {
	// Get the previous MedianTimestampWindow timestamps.
	windowTimes := make(timestampSlice, modules.MedianTimestampWindow)
	windowTimes[0] = pb.Block.Timestamp
	parentID := pb.Block.ParentID
	var timestamp time.Time
	var err error
	for i := uint64(1); i < modules.MedianTimestampWindow; i++ {
		// If the genesis block is 'parent', use the genesis block timestamp
		// for all remaining times.
		if parentID == (types.BlockID{}) {
			windowTimes[i] = windowTimes[i - 1]
			continue
		}

		// Get the next parent.
		parentID, timestamp, err = cs.getParentID(tx, parentID)
		if err != nil {
			cs.log.Println("ERROR: unable to get parent ID:", err)
		}
		windowTimes[i] = timestamp
	}
	sort.Sort(windowTimes)

	// Return the median of the sorted timestamps.
	return windowTimes[len(windowTimes) / 2]
}
