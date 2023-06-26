package consensus

import (
	"bytes"
	"database/sql"
	"errors"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

var (
	errDoSBlock       = errors.New("block is known to be invalid")
	errDatabaseError  = errors.New("error querying database")
	errNonLinearChain = errors.New("block set is not a contiguous chain")
	errOrphan         = errors.New("block has no known parent")
)

// managedBroadcastBlock will broadcast a block to the consensus set's peers.
func (cs *ConsensusSet) managedBroadcastBlock(b types.Block) {
	// Broadcast the block header to all peers.
	go cs.gateway.Broadcast("RelayHeader", b.Header(), cs.gateway.Peers())
}

// validateHeaderAndBlock does some early, low computation verification on the
// block. Callers should not assume that validation will happen in a particular
// order.
func (cs *ConsensusSet) validateHeaderAndBlock(tx *sql.Tx, b types.Block, id types.BlockID) (parent *processedBlock, err error) {
	// Check if the block is a DoS block - a known invalid block that is expensive
	// to validate.
	exists, err := checkDoSBlock(tx, id)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errDoSBlock
	}

	// Check if the block is already known.
	_, exists, err = findBlockByID(tx, id)
	if err != nil {
		return nil, errDatabaseError
	}
	if exists {
		return nil, modules.ErrBlockKnown
	}

	// Check for the parent.
	parent, exists, err = findBlockByID(tx, b.ParentID)
	if err != nil {
		return nil, errDatabaseError
	}
	if !exists {
		return nil, errOrphan
	}

	// Check that the timestamp is not too far in the past to be acceptable.
	minTimestamp := cs.minimumValidChildTimestamp(tx, parent)

	err = cs.validateBlock(b, id, minTimestamp, parent.ChildTarget, parent.Height + 1)
	if err != nil {
		return nil, err
	}
	return parent, nil
}

// checkHeaderTarget returns true if the header's ID meets the given target.
func (cs *ConsensusSet) checkHeaderTarget(h types.BlockHeader, target modules.Target) bool {
	blockHash := h.ID()
	return bytes.Compare(target[:], blockHash[:]) >= 0
}

// validateHeader does some early, low computation verification on the header
// to determine if the block should be downloaded. Callers should not assume
// that validation will happen in a particular order.
func (cs *ConsensusSet) validateHeader(tx *sql.Tx, h types.BlockHeader) error {
	// Check if the block is a DoS block - a known invalid block that is expensive
	// to validate.
	id := h.ID()
	exists, err := checkDoSBlock(tx, id)
	if err != nil {
		return err
	}
	if exists {
		return errDoSBlock
	}

	// Check if the block is already known.
	_, exists, err = findBlockByID(tx, id)
	if err != nil {
		return errDatabaseError
	}
	if exists {
		return modules.ErrBlockKnown
	}

	// Check for the parent.
	parent, exists, err := findBlockByID(tx, h.ParentID)
	if err != nil {
		return errDatabaseError
	}
	if !exists {
		return errOrphan
	}

	// Check that the nonce is a legal nonce.
	if parent.Height + 1 >= modules.ASICHardforkHeight && h.Nonce % modules.ASICHardforkFactor != 0 {
		return errors.New("block does not meet nonce requirements")
	}
	// Check that the target of the new block is sufficient.
	if !cs.checkHeaderTarget(h, parent.ChildTarget) {
		return modules.ErrBlockUnsolved
	}

	// TODO: check if the block is a non extending block once headers-first
	// downloads are implemented.

	// Check that the timestamp is not too far in the past to be acceptable.
	minTimestamp := cs.minimumValidChildTimestamp(tx, parent)
	if minTimestamp.After(h.Timestamp) {
		return ErrEarlyTimestamp
	}

	// Check if the block is in the extreme future. We make a distinction between
	// future and extreme future because there is an assumption that by the time
	// the extreme future arrives, this block will no longer be a part of the
	// longest fork because it will have been ignored by all of the miners.
	if h.Timestamp.Unix() > types.CurrentTimestamp().Unix() + modules.ExtremeFutureThreshold {
		return ErrExtremeFutureTimestamp
	}

	// We do not check if the header is in the near future here, because we want
	// to get the corresponding block as soon as possible, even if the block is in
	// the near future.

	return nil
}

// addBlockToTree inserts a block into the blockNode tree by adding it to its
// parent's list of children. If the new blockNode is heavier than the current
// node, the blockchain is forked to put the new block and its parents at the
// tip. An error will be returned if block verification fails or if the block
// does not extend the longest fork.
//
// addBlockToTree might need to modify the database while returning an error
// on the block. Such errors are handled outside by the caller.
func (cs *ConsensusSet) addBlockToTree(tx *sql.Tx, b types.Block, parent *processedBlock) (ce changeEntry, err error) {
	// Prepare the child processed block associated with the parent block.
	newNode, err := cs.newChild(tx, parent, b)
	if err != nil {
		return
	}

	// Check whether the new node is part of a chain that is heavier than the
	// current node. If not, return ErrNonExtending and don't fork the
	// blockchain.
	currentNode := currentProcessedBlock(tx)
	if !newNode.heavierThan(currentNode) {
		return changeEntry{}, modules.ErrNonExtendingBlock
	}

	// Fork the blockchain and put the new heaviest block at the tip of the
	// chain.
	var revertedBlocks, appliedBlocks []*processedBlock
	revertedBlocks, appliedBlocks, err = cs.forkBlockchain(tx, newNode)
	if err != nil {
		return changeEntry{}, err
	}
	for _, rn := range revertedBlocks {
		ce.RevertedBlocks = append(ce.RevertedBlocks, rn.Block.ID())
	}
	for _, an := range appliedBlocks {
		ce.AppliedBlocks = append(ce.AppliedBlocks, an.Block.ID())
	}
	err = appendChangeLog(tx, ce)
	if err != nil {
		return changeEntry{}, err
	}

	return ce, nil
}

// threadedSleepOnFutureBlock will sleep until the timestamp of a future block
// has arrived.
//
// TODO: An attacker can broadcast a future block multiple times, resulting in a
// goroutine spinup for each future block. Need to prevent that.
//
// TODO: An attacker could produce a very large number of future blocks,
// consuming memory. Need to prevent that.
func (cs *ConsensusSet) threadedSleepOnFutureBlock(b types.Block) {
	// Add this thread to the threadgroup.
	err := cs.tg.Add()
	if err != nil {
		return
	}
	defer cs.tg.Done()

	// Perform a soft-sleep while we wait for the block to become valid.
	select {
	case <-cs.tg.StopChan():
		return
	case <-time.After(time.Duration(b.Timestamp.Unix() - (types.CurrentTimestamp().Unix() + modules.FutureThreshold)) * time.Second):
		_, err := cs.managedAcceptBlocks([]types.Block{b})
		if err != nil {
			cs.log.Println("WARN: failed to accept a future block:", err)
		}
		cs.managedBroadcastBlock(b)
	}
}

// managedAcceptBlocks will try to add blocks to the consensus set. If the
// blocks do not extend the longest currently known chain, an error is
// returned but the blocks are still kept in memory. If the blocks extend a fork
// such that the fork becomes the longest currently known chain, the consensus
// set will reorganize itself to recognize the new longest fork. Accepted
// blocks are not relayed.
//
// Typically AcceptBlock should be used so that the accepted block is relayed.
// This method is typically only be used when there would otherwise be multiple
// consecutive calls to AcceptBlock with each successive call accepting the
// child block of the previous call.
func (cs *ConsensusSet) managedAcceptBlocks(blocks []types.Block) (blockchainExtended bool, err error) {
	// Grab a lock on the consensus set.
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Make sure that blocks are consecutive. Though this isn't a strict
	// requirement, if blocks are not consecutive then it becomes a lot harder
	// to maintain correctness when adding multiple blocks.
	//
	// This is the first time that IDs on the blocks have been computed.
	blockIDs := make([]types.BlockID, 0, len(blocks))
	for i := 0; i < len(blocks); i++ {
		blockIDs = append(blockIDs, blocks[i].ID())
		if i > 0 && blocks[i].ParentID != blockIDs[i - 1] {
			return false, errNonLinearChain
		}
	}

	// Verify the headers for every block, throw out known blocks, and the
	// invalid blocks (which includes the children of invalid blocks).
	chainExtended := false
	changes := make([]changeEntry, 0, len(blocks))
	tx, err := cs.db.Begin()
	if err != nil {
		return false, err
	}
	var setErr error
	for i := 0; i < len(blocks); i++ {
		// Start by checking the header of the block.
		parent, err := cs.validateHeaderAndBlock(tx, blocks[i], blockIDs[i])
		if modules.ContainsError(err, modules.ErrBlockKnown) {
			// Skip over known blocks.
			continue
		}
		if modules.ContainsError(err, ErrFutureTimestamp) {
			// Queue the block to be tried again if it is a future block.
			go cs.threadedSleepOnFutureBlock(blocks[i])
		}
		if err != nil {
			setErr = err
			break
		}

		// Try adding the block to consensus.
		changeEntry, err := cs.addBlockToTree(tx, blocks[i], parent)
		if err == nil {
			changes = append(changes, changeEntry)
			chainExtended = true
			var applied, reverted []string
			for _, b := range changeEntry.AppliedBlocks {
					applied = append(applied, b.String()[:6])
			}
			for _, b := range changeEntry.RevertedBlocks {
				reverted = append(reverted, b.String()[:6])
			}
		}
		if modules.ContainsError(err, modules.ErrNonExtendingBlock) {
			err = nil
		}
		if err != nil {
			setErr = err
			break
		}

		// Sanity check - we should never apply fewer blocks than we revert.
		if len(changeEntry.AppliedBlocks) < len(changeEntry.RevertedBlocks) {
			err = errors.New("after adding a change entry, there are more reverted blocks than applied ones")
			cs.log.Severe(err)
			setErr = err
			break
		}
	}

	if setErr != nil {
		if len(changes) == 0 {
			cs.log.Println("Consensus received an invalid block:", setErr)
		} else {
			cs.log.Println("Consensus received a chain of blocks, where one was valid, but others were not:", setErr)
		}
		tx.Rollback()
		return false, setErr
	}

	// Stop here if the blocks did not extend the longest blockchain.
	if !chainExtended {
		tx.Rollback()
		return false, modules.ErrNonExtendingBlock
	}

	// Commit all changes before updating the subscribers.
	if err := tx.Commit(); err != nil {
		return false, err
	}

	// Send any changes to subscribers.
	for i := 0; i < len(changes); i++ {
		cs.updateSubscribers(changes[i])
	}

	return chainExtended, nil
}

// AcceptBlock will try to add a block to the consensus set. If the block does
// not extend the longest currently known chain, an error is returned but the
// block is still kept in memory. If the block extends a fork such that the
// fork becomes the longest currently known chain, the consensus set will
// reorganize itself to recognize the new longest fork. If a block is accepted
// without error, it will be relayed to all connected peers. This function
// should only be called for new blocks.
func (cs *ConsensusSet) AcceptBlock(b types.Block) error {
	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()

	chainExtended, err := cs.managedAcceptBlocks([]types.Block{b})
	if err != nil {
		return err
	}
	if chainExtended {
		cs.managedBroadcastBlock(b)
	}
	return nil
}
