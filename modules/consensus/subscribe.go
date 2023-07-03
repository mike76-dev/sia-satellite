package consensus

import (
	"database/sql"
	"errors"

	"github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
)

// computeConsensusChangeDiffs computes the ConsensusChangeDiffs for the
// provided block.
func computeConsensusChangeDiffs(pb *processedBlock, apply bool) modules.ConsensusChangeDiffs {
	if apply {
		return modules.ConsensusChangeDiffs{
			SiacoinOutputDiffs:        pb.SiacoinOutputDiffs,
			FileContractDiffs:         pb.FileContractDiffs,
			SiafundOutputDiffs:        pb.SiafundOutputDiffs,
			DelayedSiacoinOutputDiffs: pb.DelayedSiacoinOutputDiffs,
			SiafundPoolDiffs:          pb.SiafundPoolDiffs,
		}
	}
	// The order of the diffs needs to be flipped and the direction of the
	// diffs also needs to be flipped.
	cd := modules.ConsensusChangeDiffs{
		SiacoinOutputDiffs:        make([]modules.SiacoinOutputDiff, len(pb.SiacoinOutputDiffs)),
		FileContractDiffs:         make([]modules.FileContractDiff, len(pb.FileContractDiffs)),
		SiafundOutputDiffs:        make([]modules.SiafundOutputDiff, len(pb.SiafundOutputDiffs)),
		DelayedSiacoinOutputDiffs: make([]modules.DelayedSiacoinOutputDiff, len(pb.DelayedSiacoinOutputDiffs)),
		SiafundPoolDiffs:          make([]modules.SiafundPoolDiff, len(pb.SiafundPoolDiffs)),
	}
	for i, d := range pb.SiacoinOutputDiffs {
		d.Direction = !d.Direction
		cd.SiacoinOutputDiffs[len(cd.SiacoinOutputDiffs) - i - 1] = d
	}
	for i, d := range pb.FileContractDiffs {
		d.Direction = !d.Direction
		cd.FileContractDiffs[len(cd.FileContractDiffs) - i - 1] = d
	}
	for i, d := range pb.SiafundOutputDiffs {
		d.Direction = !d.Direction
		cd.SiafundOutputDiffs[len(cd.SiafundOutputDiffs) - i - 1] = d
	}
	for i, d := range pb.DelayedSiacoinOutputDiffs {
		d.Direction = !d.Direction
		cd.DelayedSiacoinOutputDiffs[len(cd.DelayedSiacoinOutputDiffs) - i - 1] = d
	}
	for i, d := range pb.SiafundPoolDiffs {
		d.Direction = !d.Direction
		cd.SiafundPoolDiffs[len(cd.SiafundPoolDiffs) - i - 1] = d
	}
	return cd
}

// computeConsensusChange computes the consensus change from the change entry
// at index 'i' in the change log. If i is out of bounds, an error is returned.
func (cs *ConsensusSet) computeConsensusChange(tx *sql.Tx, ce changeEntry) (modules.ConsensusChange, error) {
	cc := modules.ConsensusChange{
		ID: ce.ID(),
	}
	for _, revertedBlockID := range ce.RevertedBlocks {
		revertedBlock, exists, err := findBlockByID(tx, revertedBlockID)
		if err != nil || !exists {
			cs.log.Println("CRITICAL: unable to find block in computeConsensusChange:", err)
			return modules.ConsensusChange{}, err
		}
		cc.RevertedBlocks = append(cc.RevertedBlocks, revertedBlock.Block)
		diffs := computeConsensusChangeDiffs(revertedBlock, false)
		cc.RevertedDiffs = append(cc.RevertedDiffs, diffs)
		cc.AppendDiffs(diffs)
	}
	for _, appliedBlockID := range ce.AppliedBlocks {
		appliedBlock, exists, err := findBlockByID(tx, appliedBlockID)
		if err != nil || !exists {
			cs.log.Println("CRITICAL: unable to find block in computeConsensusChange:", err)
			return modules.ConsensusChange{}, err
		}
		cc.AppliedBlocks = append(cc.AppliedBlocks, appliedBlock.Block)
		diffs := computeConsensusChangeDiffs(appliedBlock, true)
		cc.AppliedDiffs = append(cc.AppliedDiffs, diffs)
		cc.AppendDiffs(diffs)
	}

	// Grab the child target and the minimum valid child timestamp.
	recentBlock := ce.AppliedBlocks[len(ce.AppliedBlocks) - 1]
	pb, exists, err := findBlockByID(tx, recentBlock)
	if err != nil || !exists {
		cs.log.Println("CRITICAL: could not find process block for known block:", err)
		return modules.ConsensusChange{}, err
	}

	cc.ChildTarget = pb.ChildTarget
	cc.MinimumValidChildTimestamp = cs.minimumValidChildTimestamp(tx, pb)
	cc.BlockHeight = pb.Height

	currentBlock := currentBlockID(tx)
	if cs.synced && recentBlock == currentBlock {
		cc.Synced = true
	}

	// Add the unexported tryTransactionSet function.
	cc.TryTransactionSet = cs.tryTransactionSet

	return cc, nil
}

// updateSubscribers will inform all subscribers of a new update to the
// consensus set. updateSubscribers does not alter the changelog, the changelog
// must be updated beforehand.
func (cs *ConsensusSet) updateSubscribers(ce changeEntry) {
	if len(cs.subscribers) == 0 {
		return
	}

	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return
	}

	// Compute the consensus change so it can be sent to subscribers.
	cc, err := cs.computeConsensusChange(tx, ce)
	if err != nil {
		cs.log.Println("CRITICAL: computeConsensusChange failed:", err)
		tx.Rollback()
		return
	}

	// Log re-orgs.
	tx.Commit()
	if len(cc.RevertedBlocks) > 0 {
		cs.log.Println("INFO: ConsensusChange with re-org detected: ", cc.ID, len(cc.RevertedBlocks))
	}

	// Send the consensus change to all subscribers.
	for _, subscriber := range cs.subscribers {
		subscriber.ProcessConsensusChange(cc)
	}
}

// managedInitializeSubscribe will take a subscriber and feed them all of the
// consensus changes that have occurred since the change provided.
//
// As a special case, using an empty id as the start will have all the changes
// sent to the modules starting with the genesis block.
func (cs *ConsensusSet) managedInitializeSubscribe(subscriber modules.ConsensusSetSubscriber, start modules.ConsensusChangeID, cancel <-chan struct{}) (modules.ConsensusChangeID, error) {
	if start == modules.ConsensusChangeRecent {
		cs.mu.RLock()
		defer cs.mu.RUnlock()
		return cs.recentConsensusChangeID()
	}

	// 'exists' and 'entry' are going to be pointed to the first entry that
	// has not yet been seen by subscriber.
	var exists bool
	var entry changeEntry
	cs.mu.RLock()
	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return modules.ConsensusChangeID{}, err
	}
	if start == modules.ConsensusChangeBeginning {
		// Special case: for modules.ConsensusChangeBeginning, create an
		// initial node pointing to the genesis block. The subscriber will
		// receive the diffs for all blocks in the consensus set, including
		// the genesis block.
		entry = cs.genesisEntry()
		exists = true
	} else {
		// The subscriber has provided an existing consensus change.
		// Because the subscriber already has this consensus change,
		// 'entry' and 'exists' need to be pointed at the next consensus
		// change.
		entry, exists = getEntry(tx, start)
		if !exists {
			// modules.ErrInvalidConsensusChangeID is a named error that
			// signals a break in synchronization between the consensus set
			// persistence and the subscriber persistence. Typically,
			// receiving this error means that the subscriber needs to
			// perform a rescan of the consensus set.
			cs.mu.RUnlock()
			tx.Rollback()
			return modules.ConsensusChangeID{}, modules.ErrInvalidConsensusChangeID
		}
		entry, exists = entry.NextEntry(tx)
	}
	cs.mu.RUnlock()

	// Nothing to do if the changeEntry doesn't exist.
	if !exists {
		tx.Rollback()
		return start, nil
	}

	// Send all remaining consensus changes to the subscriber.
	latestChangeID := entry.ID()
	for exists {
		// Send changes in batches of 100 so that we don't hold the
		// lock for too long.
		cs.mu.RLock()
		for i := 0; i < 100 && exists; i++ {
			latestChangeID = entry.ID()
			select {
			case <-cancel:
				tx.Rollback()
				return modules.ConsensusChangeID{}, sync.ErrStopped
			default:
			}
			cc, err := cs.computeConsensusChange(tx, entry)
			if err != nil {
				cs.mu.RUnlock()
				tx.Rollback()
				return modules.ConsensusChangeID{}, err
			}
			subscriber.ProcessConsensusChange(cc)
			entry, exists = entry.NextEntry(tx)
		}
		cs.mu.RUnlock()
	}

	tx.Commit()
	return latestChangeID, nil
}

// recentConsensusChangeID gets the ConsensusChangeID of the most recent
// change.
func (cs *ConsensusSet) recentConsensusChangeID() (cid modules.ConsensusChangeID, err error) {
	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return modules.ConsensusChangeID{}, err
	}

	id := make([]byte, 32)
	err = tx.QueryRow("SELECT bytes FROM cs_changelog WHERE id = 1").Scan(&id)
	if err != nil {
		tx.Rollback()
		return modules.ConsensusChangeID{}, err
	}

	tx.Commit()
	copy(cid[:], id[:])
	return
}

// ConsensusSetSubscribe adds a subscriber to the list of subscribers, and
// gives them every consensus change that has occurred since the change with
// the provided id.
//
// As a special case, using an empty id as the start will have all the changes
// sent to the modules starting with the genesis block.
func (cs *ConsensusSet) ConsensusSetSubscribe(subscriber modules.ConsensusSetSubscriber, start modules.ConsensusChangeID,
	cancel <-chan struct{}) error {
	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()

	// Call managedInitializeSubscribe until the new module is up-to-date.
	for {
		start, err = cs.managedInitializeSubscribe(subscriber, start, cancel)
		if err != nil {
			return err
		}

		// Check if the start equals the most recent change id. If it does we
		// are done. If it doesn't, we need to call managedInitializeSubscribe
		// again.
		cs.mu.Lock()
		recentID, err := cs.recentConsensusChangeID()
		if err != nil {
			cs.mu.Unlock()
			return err
		}
		if start == recentID {
			// break out of the loop while still holding to lock to avoid
			// updating subscribers before the new module is appended to the
			// list of subscribers.
			defer cs.mu.Unlock()
			break
		}
		cs.mu.Unlock()

		// Check for shutdown.
		select {
		case <-cs.tg.StopChan():
			return sync.ErrStopped
		default:
		}
	}

	// Add the module to the list of subscribers.
	// Sanity check - subscriber should not be already subscribed.
	for _, s := range cs.subscribers {
		if s == subscriber {
			cs.log.Println("CRITICAL: refusing to double-subscribe subscriber")
			return errors.New("subscriber already registered")
		}
	}
	cs.subscribers = append(cs.subscribers, subscriber)
	return nil
}

// Unsubscribe removes a subscriber from the list of subscribers, allowing for
// garbage collection and rescanning. If the subscriber is not found in the
// subscriber database, no action is taken.
func (cs *ConsensusSet) Unsubscribe(subscriber modules.ConsensusSetSubscriber) {
	if cs.tg.Add() != nil {
		return
	}
	defer cs.tg.Done()
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Search for the subscriber in the list of subscribers and remove it if
	// found.
	for i := range cs.subscribers {
		if cs.subscribers[i] == subscriber {
			// nil the subscriber entry (otherwise it will not be GC'd if it's
			// at the end of the subscribers slice).
			cs.subscribers[i] = nil
			// Delete the entry from the slice.
			cs.subscribers = append(cs.subscribers[0:i], cs.subscribers[i + 1:]...)
			break
		}
	}
}
