package consensus

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	
	"go.sia.tech/core/types"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
)

var (
	errNilGateway = errors.New("cannot have a nil gateway as input")
)

// The ConsensusSet is the object responsible for tracking the current status
// of the blockchain. Broadly speaking, it is responsible for maintaining
// consensus.  It accepts blocks and constructs a blockchain, forking when
// necessary.
type ConsensusSet struct {
	// The gateway manages peer connections and keeps the consensus set
	// synchronized to the rest of the network.
	gateway modules.Gateway

	// The block root contains the genesis block.
	blockRoot processedBlock

	// Subscribers to the consensus set will receive a changelog every time
	// there is an update to the consensus set. At initialization, they receive
	// all changes that they are missing.
	//
	// Memory: A consensus set typically has fewer than 10 subscribers, and
	// subscription typically happens entirely at startup. This slice is
	// unlikely to grow beyond 1kb, and cannot by manipulated by an attacker as
	// the function of adding a subscriber should not be exposed.
	subscribers []modules.ConsensusSetSubscriber

	// checkingConsistency is a bool indicating whether or not a consistency
	// check is in progress. The consistency check logic call itself, resulting
	// in infinite loops. This bool prevents that while still allowing for full
	// granularity consistency checks. Previously, consistency checks were only
	// performed after a full reorg, but now they are performed after every
	// block.
	checkingConsistency bool

	// synced is true if initial blockchain download has finished. It indicates
	// whether the consensus set is synced with the network.
	synced bool

	// Caches keeps the most recently accessed items.
	scoCache *siacoinOutputCache
	fcCache  *fileContractCache
	pbCache  *blockCache

	// Utilities.
	db  *sql.DB
	log *persist.Logger
	mu  sync.RWMutex
	tg  siasync.ThreadGroup
}

// consensusSetBlockingStartup handles the blocking portion of New.
func consensusSetBlockingStartup(gateway modules.Gateway, db *sql.DB, dir string) (*ConsensusSet, error) {
	// Check for nil dependencies.
	if gateway == nil {
		return nil, errNilGateway
	}

	// Create the ConsensusSet object.
	cs := &ConsensusSet{
		gateway: gateway,
		db:      db,

		blockRoot: processedBlock{
			Block:       modules.GenesisBlock,
			ChildTarget: modules.RootTarget,
			Depth:       modules.RootDepth,

			DiffsGenerated: true,
		},

		scoCache: newSiacoinOutputCache(),
		fcCache:  newFileContractCache(),
		pbCache:  newBlockCache(),
	}

	// Create the diffs for the genesis transaction outputs.
	for _, transaction := range modules.GenesisBlock.Transactions {
		// Create the diffs for the genesis siacoin outputs.
		for i, siacoinOutput := range transaction.SiacoinOutputs {
			scid := transaction.SiacoinOutputID(i)
			scod := modules.SiacoinOutputDiff{
				Direction:     modules.DiffApply,
				ID:            scid,
				SiacoinOutput: siacoinOutput,
			}
			cs.blockRoot.SiacoinOutputDiffs = append(cs.blockRoot.SiacoinOutputDiffs, scod)
		}

		// Create the diffs for the genesis siafund outputs.
		for i, siafundOutput := range transaction.SiafundOutputs {
			sfid := transaction.SiafundOutputID(i)
			sfod := modules.SiafundOutputDiff{
				Direction:     modules.DiffApply,
				ID:            sfid,
				SiafundOutput: siafundOutput,
			}
			cs.blockRoot.SiafundOutputDiffs = append(cs.blockRoot.SiafundOutputDiffs, sfod)
		}
	}

	// Initialize the consensus persistence structures.
	err := cs.initPersist(dir)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

// consensusSetAsyncStartup handles the async portion of New.
func consensusSetAsyncStartup(cs *ConsensusSet, bootstrap bool) error {
	// Sync with the network.
	if bootstrap {
		err := cs.managedInitialBlockchainDownload()
		if err != nil {
			return err
		}
	}

	// Register RPCs.
	cs.gateway.RegisterRPC("SendBlocks", cs.rpcSendBlocks)
	cs.gateway.RegisterRPC("RelayHeader", cs.threadedRPCRelayHeader)
	cs.gateway.RegisterRPC("SendBlk", cs.rpcSendBlk)
	cs.gateway.RegisterConnectCall("SendBlocks", cs.threadedReceiveBlocks)
	cs.tg.OnStop(func() {
		cs.gateway.UnregisterRPC("SendBlocks")
		cs.gateway.UnregisterRPC("RelayHeader")
		cs.gateway.UnregisterRPC("SendBlk")
		cs.gateway.UnregisterConnectCall("SendBlocks")
	})

	// Mark that we are synced with the network.
	cs.mu.Lock()
	cs.synced = true
	cs.mu.Unlock()
	return nil
}

// New returns a new ConsensusSet, containing at least the genesis block. If
// there is an existing block database, it will be loaded.
func New(db *sql.DB, gateway modules.Gateway, bootstrap bool, dir string) (*ConsensusSet, <-chan error) {
	// Handle blocking consensus startup first.
	errChan := make(chan error, 1)
	cs, err := consensusSetBlockingStartup(gateway, db, dir)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Non-blocking consensus startup.
	go func() {
		defer close(errChan)
		err := cs.tg.Add()
		if err != nil {
			errChan <- err
			return
		}
		defer cs.tg.Done()

		err = consensusSetAsyncStartup(cs, bootstrap)
		if err != nil {
			errChan <- err
			return
		}
	}()
	return cs, errChan
}

// BlockAtHeight returns the block at a given height.
func (cs *ConsensusSet) BlockAtHeight(height uint64) (block types.Block, exists bool) {
	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return types.Block{}, false
	}

	id, err := getBlockAtHeight(tx, height)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			cs.log.Println("ERROR: unable to find block:", err)
		}
		tx.Rollback()
		return types.Block{}, false
	}

	pb, exists, err := cs.findBlockByID(tx, id)
	if err != nil {
		cs.log.Println("ERROR: unable to find block:", err)
		tx.Rollback()
		return types.Block{}, false
	}

	tx.Commit()
	return pb.Block, exists
}

// BlockByID returns the block for a given BlockID.
func (cs *ConsensusSet) BlockByID(id types.BlockID) (block types.Block, height uint64, exists bool) {
	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return types.Block{}, 0, false
	}

	pb, exists, err := cs.findBlockByID(tx, id)
	if err != nil {
		cs.log.Println("ERROR: unable to find block:", err)
		tx.Rollback()
		return types.Block{}, 0, false
	}

	tx.Commit()
	return pb.Block, pb.Height, exists
}

// ChildTarget returns the target for the child of a block.
func (cs *ConsensusSet) ChildTarget(id types.BlockID) (target modules.Target, exists bool) {
	err := cs.tg.Add()
	if err != nil {
		return modules.Target{}, false
	}
	defer cs.tg.Done()

	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return modules.Target{}, false
	}

	pb, exists, err := cs.findBlockByID(tx, id)
	if err != nil {
		cs.log.Println("ERROR: unable to find block:", err)
		tx.Rollback()
		return modules.Target{}, false
	}

	tx.Commit()
	return pb.ChildTarget, exists
}

// Close safely closes the consensus set.
func (cs *ConsensusSet) Close() error {
	return cs.tg.Stop()
}

// managedCurrentBlock returns the latest block in the heaviest known blockchain.
func (cs *ConsensusSet) managedCurrentBlock() (block types.Block) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return types.Block{}
	}

	pb := cs.currentProcessedBlock(tx)
	if pb == nil {
		tx.Rollback()
		return types.Block{}
	}

	tx.Commit()
	return pb.Block
}

// CurrentBlock returns the latest block in the heaviest known blockchain.
func (cs *ConsensusSet) CurrentBlock() (block types.Block) {
	err := cs.tg.Add()
	if err != nil {
		return types.Block{}
	}
	defer cs.tg.Done()

	// Block until a lock can be grabbed on the consensus set, indicating that
	// all modules have received the most recent block. The lock is held so that
	// there are no race conditions when trying to synchronize nodes.
	cs.mu.Lock()
	defer cs.mu.Unlock()

	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return types.Block{}
	}

	pb := cs.currentProcessedBlock(tx)
	if pb == nil {
		tx.Rollback()
		return types.Block{}
	}

	tx.Commit()
	return pb.Block
}

// Height returns the height of the consensus set.
func (cs *ConsensusSet) Height() (height uint64) {
	err := cs.tg.Add()
	if err != nil {
		return 0
	}
	defer cs.tg.Done()

	// Block until a lock can be grabbed on the consensus set, indicating that
	// all modules have received the most recent block. The lock is held so that
	// there are no race conditions when trying to synchronize nodes.
	cs.mu.Lock()
	defer cs.mu.Unlock()

	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return 0
	}

	height = blockHeight(tx)
	tx.Commit()

	return height
}

// InCurrentPath returns true if the block presented is in the current path,
// false otherwise.
func (cs *ConsensusSet) InCurrentPath(id types.BlockID) (inPath bool) {
	err := cs.tg.Add()
	if err != nil {
		return false
	}
	defer cs.tg.Done()

	tx, err := cs.db.Begin()
	if err != nil {
		return false
	}

	pb, exists, err := cs.findBlockByID(tx, id)
	if err != nil || !exists {
		tx.Rollback()
		return false
	}

	pathID, err := getBlockAtHeight(tx, pb.Height)
	if err != nil {
		tx.Rollback()
		return false
	}

	tx.Commit()
	return pathID == id
}

// MinimumValidChildTimestamp returns the earliest timestamp that the next block
// can have in order for it to be considered valid.
func (cs *ConsensusSet) MinimumValidChildTimestamp(id types.BlockID) (timestamp time.Time, exists bool) {
	err := cs.tg.Add()
	if err != nil {
		return
	}
	defer cs.tg.Done()

	tx, err := cs.db.Begin()
	if err != nil {
		cs.log.Println("ERROR: unable to start transaction:", err)
		return
	}

	pb, exists, err := cs.findBlockByID(tx, id)
	if err != nil {
		tx.Rollback()
		return
	}

	tx.Commit()
	timestamp = cs.minimumValidChildTimestamp(tx, pb)
	exists = true

	return
}

// StorageProofSegment returns the segment to be used in the storage proof for
// a given file contract.
func (cs *ConsensusSet) StorageProofSegment(fcid types.FileContractID) (index uint64, err error) {
	err = cs.tg.Add()
	if err != nil {
		return 0, err
	}
	defer cs.tg.Done()

	tx, err := cs.db.Begin()
	if err != nil {
		return 0, err
	}

	index, err = cs.storageProofSegment(tx, fcid)
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	tx.Commit()
	return index, nil
}

// FoundationUnlockHashes returns the current primary and failsafe Foundation
// UnlockHashes.
func (cs *ConsensusSet) FoundationUnlockHashes() (primary, failsafe types.Address) {
	if err := cs.tg.Add(); err != nil {
		return
	}
	defer cs.tg.Done()

	tx, err := cs.db.Begin()
	if err != nil {
		return
	}

	primary, failsafe, err = getFoundationUnlockHashes(tx)
	if err != nil {
		cs.log.Println("ERROR: unable to get the Foundation unlock hashes:", err)
		tx.Rollback()
		return
	}

	tx.Commit()
	return
}
