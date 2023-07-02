package wallet

import (
	"bytes"
	"database/sql"
	"errors"
	"sort"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
)

const (
	// RespendTimeout records the number of blocks that the wallet will wait
	// before spending an output that has been spent in the past. If the
	// transaction spending the output has not made it to the transaction pool
	// after the limit, the assumption is that it never will.
	RespendTimeout = 100
)

var (
	errNilDB           = errors.New("wallet cannot initialize with a nil database")
	errNilConsensusSet = errors.New("wallet cannot initialize with a nil consensus set")
	errNilTpool        = errors.New("wallet cannot initialize with a nil transaction pool")
)

// spendableKey is a set of secret keys plus the corresponding unlock
// conditions.  The public key can be derived from the secret key and then
// matched to the corresponding public keys in the unlock conditions. All
// addresses that are to be used in 'FundTransaction' must conform to this
// form of spendable key.
type spendableKey struct {
	UnlockConditions types.UnlockConditions
	SecretKeys       []types.PrivateKey
}

// EncodeTo implements types.EncoderTo.
func (sk *spendableKey) EncodeTo(e *types.Encoder) {
	sk.UnlockConditions.EncodeTo(e)
	e.WritePrefix(len(sk.SecretKeys))
	for _, key := range sk.SecretKeys {
		e.WriteBytes(key[:])
	}
}

// DecodeFrom implements types.DecoderFrom.
func (sk *spendableKey) DecodeFrom(d *types.Decoder) {
	sk.UnlockConditions.DecodeFrom(d)
	sk.SecretKeys = make([]types.PrivateKey, d.ReadPrefix())
	for i := 0; i < len(sk.SecretKeys); i++ {
		sk.SecretKeys[i] = types.PrivateKey(d.ReadBytes())
	}
}

// Wallet is an object that tracks balances, creates keys and addresses,
// manages building and sending transactions.
type Wallet struct {
	// encrypted indicates whether the wallet has been encrypted (i.e.
	// initialized). unlocked indicates whether the wallet is currently
	// storing secret keys in memory. subscribed indicates whether the wallet
	// has subscribed to the consensus set yet - the wallet is unable to
	// subscribe to the consensus set until it has been unlocked for the first
	// time. The primary seed is used to generate new addresses for the
	// wallet.
	encrypted   bool
	unlocked    bool
	primarySeed modules.Seed

	// Fields that handle the subscriptions to the cs and tpool. subscribedMu
	// needs to be locked when subscribed is accessed and while calling the
	// subscribing methods on the tpool and consensusset.
	subscribedMu sync.Mutex
	subscribed   bool

	// The wallet's dependencies.
	db    *sql.DB
	cs    modules.ConsensusSet
	tpool modules.TransactionPool

	// The following set of fields are responsible for tracking the confirmed
	// outputs, and for being able to spend them. The seeds are used to derive
	// the keys that are tracked on the blockchain. All keys are pregenerated
	// from the seeds, when checking new outputs or spending outputs, the seeds
	// are not referenced at all. The seeds are only stored so that the user
	// may access them.
	seeds        []modules.Seed
	unusedKeys   map[types.Address]types.UnlockConditions
	keys         map[types.Address]spendableKey
	lookahead    map[types.Address]uint64
	watchedAddrs map[types.Address]struct{}

	// unconfirmedProcessedTransactions tracks unconfirmed transactions.
	unconfirmedSets                  map[modules.TransactionSetID][]types.TransactionID
	unconfirmedProcessedTransactions processedTransactionList

	// The wallet's database tracks its seeds, keys, outputs, and
	// transactions. A global db transaction is maintained in memory to avoid
	// excessive disk writes. Any operations involving dbTx must hold an
	// exclusive lock.
	//
	// If dbRollback is set, then when the database syncs it will perform a
	// rollback instead of a commit. For safety reasons, the db will close and
	// the wallet will close if a rollback is performed.
	dbRollback bool
	dbTx       *sql.Tx

	log *persist.Logger
	mu  sync.RWMutex

	// A separate TryMutex is used to protect against concurrent unlocking or
	// initialization.
	scanLock siasync.TryMutex

	// The wallet's ThreadGroup tells tracked functions to shut down and
	// blocks until they have all exited before returning from Close.
	tg siasync.ThreadGroup

	// defragDisabled determines if the wallet is set to defrag outputs once it
	// reaches a certain threshold
	defragDisabled bool
}

// Height return the internal processed consensus height of the wallet.
func (w *Wallet) Height() (uint64, error) {
	if err := w.tg.Add(); err != nil {
		return 0, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.syncDB()
	if err != nil {
		return 0, err
	}

	var height uint64
	tx, err := w.db.Begin()
	if err != nil {
		return 0, err
	}
	err = tx.QueryRow("SELECT height FROM wt_info WHERE id = 1").Scan(&height)
	tx.Commit()
	if err != nil {
		return 0, err
	}

	return height, nil
}

// LastAddresses returns the last n addresses starting at the last seedProgress
// for which an address was generated. If n is greater than the current
// progress, fewer than n keys will be returned. That means all addresses can
// be retrieved in reverse order by simply supplying math.MaxUint64 for n.
func (w *Wallet) LastAddresses(n uint64) ([]types.Address, error) {
	if err := w.tg.Add(); err != nil {
		return nil, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Get the current seed progress from disk.
	var seedProgress uint64
	tx, err := w.db.Begin()
	if err != nil {
		return []types.Address{}, err
	}
	err = tx.QueryRow("SELECT progress FROM wt_info WHERE id = 1").Scan(&seedProgress)
	tx.Commit()
	if err != nil {
		return []types.Address{}, err
	}

	// At most seedProgess addresses can be requested.
	if n > seedProgress {
		n = seedProgress
	}
	start := seedProgress - n

	// Generate the keys.
	keys := generateKeys(w.primarySeed, start, n)
	uhs := make([]types.Address, 0, len(keys))
	for i := len(keys) - 1; i >= 0; i-- {
		uhs = append(uhs, keys[i].UnlockConditions.UnlockHash())
	}

	return uhs, nil
}

// New creates a new wallet. Keys and addresses are not loaded into the
// wallet during the call to 'New', but rather during the call to 'Unlock'.
func New(db *sql.DB, cs modules.ConsensusSet, tpool modules.TransactionPool, dir string) (*Wallet, error) {
	// Check for nil dependencies.
	if db == nil {
		return nil, errNilDB
	}
	if cs == nil {
		return nil, errNilConsensusSet
	}
	if tpool == nil {
		return nil, errNilTpool
	}

	// Initialize the data structure.
	w := &Wallet{
		db:    db,
		cs:    cs,
		tpool: tpool,

		keys:         make(map[types.Address]spendableKey),
		lookahead:    make(map[types.Address]uint64),
		unusedKeys:   make(map[types.Address]types.UnlockConditions),
		watchedAddrs: make(map[types.Address]struct{}),

		unconfirmedSets: make(map[modules.TransactionSetID][]types.TransactionID),
	}
	err := w.initPersist(dir)
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Close terminates all ongoing processes involving the wallet, enabling
// garbage collection.
func (w *Wallet) Close() error {
	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)
	var lockErr error
	// Lock the wallet outside of mu.Lock because Lock uses its own mu.Lock.
	// Once the wallet is locked it cannot be unlocked except using the
	// unexported unlock method (w.Unlock returns an error if the wallet's
	// ThreadGroup is stopped).
	if w.managedUnlocked() {
		lockErr = w.managedLock()
	}
	return modules.ComposeErrors(lockErr, w.tg.Stop())
}

// AllAddresses returns all addresses that the wallet is able to spend from,
// including unseeded addresses. Addresses are returned sorted in byte-order.
func (w *Wallet) AllAddresses() ([]types.Address, error) {
	if err := w.tg.Add(); err != nil {
		return []types.Address{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.RLock()
	defer w.mu.RUnlock()

	addrs := make([]types.Address, 0, len(w.keys))
	for addr := range w.keys {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs, nil
}

// Rescanning reports whether the wallet is currently rescanning the
// blockchain.
func (w *Wallet) Rescanning() (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	rescanning := !w.scanLock.TryLock()
	if !rescanning {
		w.scanLock.Unlock()
	}
	return rescanning, nil
}

// Settings returns the wallet's current settings.
func (w *Wallet) Settings() (modules.WalletSettings, error) {
	if err := w.tg.Add(); err != nil {
		return modules.WalletSettings{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()
	return modules.WalletSettings{
		NoDefrag: w.defragDisabled,
	}, nil
}

// SetSettings will update the settings for the wallet.
func (w *Wallet) SetSettings(s modules.WalletSettings) error {
	if err := w.tg.Add(); err != nil {
		return modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	w.defragDisabled = s.NoDefrag
	w.mu.Unlock()
	return nil
}

// managedCanSpendUnlockHash returns true if and only if the the wallet
// has keys to spend from outputs with the given address.
func (w *Wallet) managedCanSpendUnlockHash(unlockHash types.Address) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	_, isSpendable := w.keys[unlockHash]
	return isSpendable
}
