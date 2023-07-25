package transactionpool

import (
	"database/sql"
	"errors"
	"sync"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"
)

var (
	errNilDB      = errors.New("transaction pool cannot initialize with a nil database")
	errNilCS      = errors.New("transaction pool cannot initialize with a nil consensus set")
	errNilGateway = errors.New("transaction pool cannot initialize with a nil gateway")
)

type (
	// ObjectID is the ID of an object such as Siacoin output and file
	// contracts, and is used to see if there is are conflicts or overlaps within
	// the transaction pool.
	ObjectID types.Hash256

	// The TransactionPool tracks incoming transactions, accepting them or
	// rejecting them based on internal criteria such as fees and unconfirmed
	// double spends.
	TransactionPool struct {
		// Dependencies of the transaction pool.
		consensusSet modules.ConsensusSet
		gateway      modules.Gateway

		// To prevent double spends in the unconfirmed transaction set, the
		// transaction pool keeps a list of all objects that have either been
		// created or consumed by the current unconfirmed transaction pool. All
		// transactions with overlaps are rejected. This model is
		// over-aggressive - one transaction set may create an object that
		// another transaction set spends. This is done to minimize the
		// computation and memory load on the transaction pool. Dependent
		// transactions should be lumped into a single transaction set.
		//
		// transactionSetDiffs map form a transaction set id to the set of
		// diffs that resulted from the transaction set.
		knownObjects        map[ObjectID]modules.TransactionSetID
		subscriberSets      map[modules.TransactionSetID]*modules.UnconfirmedTransactionSet
		transactionHeights  map[types.TransactionID]uint64
		transactionSets     map[modules.TransactionSetID][]types.Transaction
		transactionSetDiffs map[modules.TransactionSetID]*modules.ConsensusChange
		transactionListSize int

		// Variables related to the blockchain.
		blockHeight     uint64
		recentMedians   []types.Currency
		recentMedianFee types.Currency // SC per byte.

		// The consensus change index tracks how many consensus changes have
		// been sent to the transaction pool. When a new subscriber joins the
		// transaction pool, all prior consensus changes are sent to the new
		// subscriber.
		subscribers []modules.TransactionPoolSubscriber

		// Utilities.
		db      *sql.DB
		dbTx    *sql.Tx
		log     *persist.Logger
		outerMu sync.Mutex
		innerMu sync.RWMutex
		tg      siasync.ThreadGroup
	}
)

// Enforce that TransactionPool satisfies the modules.TransactionPool interface.
var _ modules.TransactionPool = (*TransactionPool)(nil)

// New creates a transaction pool that is ready to receive transactions.
func New(db *sql.DB, cs modules.ConsensusSet, g modules.Gateway, dir string) (*TransactionPool, error) {
	// Check that the input modules are non-nil.
	if db == nil {
		return nil, errNilDB
	}
	if cs == nil {
		return nil, errNilCS
	}
	if g == nil {
		return nil, errNilGateway
	}

	// Initialize a transaction pool.
	tp := &TransactionPool{
		db:           db,
		consensusSet: cs,
		gateway:      g,

		knownObjects:        make(map[ObjectID]modules.TransactionSetID),
		subscriberSets:      make(map[modules.TransactionSetID]*modules.UnconfirmedTransactionSet),
		transactionHeights:  make(map[types.TransactionID]uint64),
		transactionSets:     make(map[modules.TransactionSetID][]types.Transaction),
		transactionSetDiffs: make(map[modules.TransactionSetID]*modules.ConsensusChange),
	}

	// Open the tpool database.
	err := tp.initPersist(dir)
	if err != nil {
		return nil, err
	}

	// Register RPCs.
	g.RegisterRPC("RelayTransactionSet", tp.relayTransactionSet)
	tp.tg.OnStop(func() {
		tp.gateway.UnregisterRPC("RelayTransactionSet")
	})

	return tp, nil
}

// Close releases any resources held by the transaction pool, stopping all of
// its worker threads.
func (tp *TransactionPool) Close() error {
	return tp.tg.Stop()
}

// FeeEstimation returns an estimation for what fee should be applied to
// transactions. It returns a minimum and maximum estimated fee per transaction
// byte.
func (tp *TransactionPool) FeeEstimation() (min, max types.Currency) {
	err := tp.tg.Add()
	if err != nil {
		return
	}
	defer tp.tg.Done()
	tp.outerMu.Lock()
	tp.innerMu.Lock()
	defer func() {
		tp.innerMu.Unlock()
		tp.outerMu.Unlock()
	}()

	// Use three methods to determine an acceptable fee. The first method looks
	// at what fee is required to get into a block on the blockchain based on
	// the actual fees of transactions confirmed in recent blocks. The second
	// method looks at the current tpool and performs fee estimation based on
	// the other transactions in the tpool. The third method is an absolute
	// minimum.

	// First method: use the median fees calculated while looking at
	// transactions that have been confirmed in the recent blocks.
	feeByBlockchain := tp.recentMedianFee

	// Second method: use the median fees calculated while looking at the
	// current size of the transaction pool. For the min fee, use a size that's
	// a fixed size larger than the current pool, and then also add some
	// proportional padding. The fixed size handles cases where the tpool is
	// really small, and a low number of transactions can move the fee
	// substantially. The proportional padding is for when the tpool is large
	// and there is a lot of activity which is adding to the tpool.
	//
	// The sizes for proportional and constant are computed independently, and
	// then the max is taken of the two.
	sizeAfterConstantPadding := tp.transactionListSize + feeEstimationConstantPadding
	sizeAfterProportionalPadding := int(float64(tp.transactionListSize) * float64(feeEstimationProportionalPadding))
	var feeByCurrentTpoolSize types.Currency
	if sizeAfterConstantPadding > sizeAfterProportionalPadding {
		feeByCurrentTpoolSize = requiredFeesToExtendTpoolAtSize(sizeAfterConstantPadding)
	} else {
		feeByCurrentTpoolSize = requiredFeesToExtendTpoolAtSize(sizeAfterProportionalPadding)
	}

	// Pick the larger of the first two methods to be compared with the third
	// method.
	if feeByBlockchain.Cmp(feeByCurrentTpoolSize) > 0 {
		min = feeByBlockchain
	} else {
		min = feeByCurrentTpoolSize
	}

	// Third method: ensure the fee is above an absolute minimum.
	if min.Cmp(minEstimation) < 0 {
		min = minEstimation
	}
	max = min.Mul64(maxMultiplier)
	return
}

// TransactionList returns a list of all transactions in the transaction pool.
// The transactions are provided in an order that can acceptably be put into a
// block.
func (tp *TransactionPool) TransactionList() []types.Transaction {
	tp.outerMu.Lock()
	tp.innerMu.Lock()
	defer func() {
		tp.innerMu.Unlock()
		tp.outerMu.Unlock()
	}()

	var txns []types.Transaction
	for _, tSet := range tp.transactionSets {
		txns = append(txns, tSet...)
	}
	return txns
}

// Transaction returns the transaction with the provided txid, its parents, and
// a bool indicating if it exists in the transaction pool.
func (tp *TransactionPool) Transaction(id types.TransactionID) (types.Transaction, []types.Transaction, bool) {
	tp.outerMu.Lock()
	tp.innerMu.Lock()
	defer func() {
		tp.innerMu.Unlock()
		tp.outerMu.Unlock()
	}()

	// Find the transaction.
	exists := false
	var txn types.Transaction
	var allParents []types.Transaction
	for _, tSet := range tp.transactionSets {
		for i, t := range tSet {
			if t.ID() == id {
				txn = t
				allParents = tSet[:i]
				exists = true
				break
			}
		}
	}

	// prune unneeded parents
	parentIDs := make(map[ObjectID]struct{})
	addOutputIDs := func(txn types.Transaction) {
		for _, input := range txn.SiacoinInputs {
			parentIDs[ObjectID(input.ParentID)] = struct{}{}
		}
		for _, fcr := range txn.FileContractRevisions {
			parentIDs[ObjectID(fcr.ParentID)] = struct{}{}
		}
		for _, input := range txn.SiafundInputs {
			parentIDs[ObjectID(input.ParentID)] = struct{}{}
		}
		for _, proof := range txn.StorageProofs {
			parentIDs[ObjectID(proof.ParentID)] = struct{}{}
		}
		for _, sig := range txn.Signatures {
			parentIDs[ObjectID(sig.ParentID)] = struct{}{}
		}
	}
	isParent := func(t types.Transaction) bool {
		for i := range t.SiacoinOutputs {
			if _, exists := parentIDs[ObjectID(t.SiacoinOutputID(i))]; exists {
				return true
			}
		}
		for i := range t.FileContracts {
			if _, exists := parentIDs[ObjectID(t.SiacoinOutputID(i))]; exists {
				return true
			}
		}
		for i := range t.SiafundOutputs {
			if _, exists := parentIDs[ObjectID(t.SiacoinOutputID(i))]; exists {
				return true
			}
		}
		return false
	}

	addOutputIDs(txn)
	var necessaryParents []types.Transaction
	for i := len(allParents) - 1; i >= 0; i-- {
		parent := allParents[i]

		if isParent(parent) {
			necessaryParents = append([]types.Transaction{parent}, necessaryParents...)
			addOutputIDs(parent)
		}
	}

	return txn, necessaryParents, exists
}

// Transactions returns the transactions of the transaction pool.
func (tp *TransactionPool) Transactions() []types.Transaction {
	tp.innerMu.RLock()
	defer tp.innerMu.RUnlock()
	var txns []types.Transaction
	for _, set := range tp.transactionSets {
		txns = append(txns, set...)
	}
	return txns
}

// TransactionSet returns the transaction set the provided object appears in.
func (tp *TransactionPool) TransactionSet(oid types.Hash256) []types.Transaction {
	tp.innerMu.RLock()
	defer tp.innerMu.RUnlock()
	// Define txns as to not use the memory that stores the actual map
	var txns []types.Transaction
	tSetID, exists := tp.knownObjects[ObjectID(oid)]
	if !exists {
		return nil
	}
	tSet, exists := tp.transactionSets[tSetID]
	if !exists {
		return nil
	}
	txns = append(txns, tSet...)
	return txns
}

// Broadcast broadcasts a transaction set to all of the transaction pool's
// peers.
func (tp *TransactionPool) Broadcast(ts []types.Transaction) {
	go tp.gateway.Broadcast("RelayTransactionSet", transactionSet(ts), tp.gateway.Peers())
}
