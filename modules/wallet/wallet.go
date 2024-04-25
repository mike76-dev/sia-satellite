package wallet

import (
	"bytes"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.uber.org/zap"
)

type (
	// Wallet manages funds and signs transactions.
	Wallet struct {
		cm  *chain.Manager
		s   modules.Syncer
		db  *sql.DB
		tx  *sql.Tx
		log *zap.Logger

		mu      sync.Mutex
		tg      siasync.ThreadGroup
		closeFn func()

		seed         modules.Seed
		addrs        map[types.Address]uint64
		keys         map[types.Address]types.PrivateKey
		unusedKeys   map[types.Address]types.UnlockConditions
		lookahead    map[types.Address]uint64
		watchedAddrs map[types.Address]uint64
		sces         map[types.Address]types.SiacoinElement
		sfes         map[types.Address]types.SiafundElement
		used         map[types.Hash256]bool
		tip          types.ChainIndex
		dbError      bool
	}
)

// Close shuts down the wallet.
func (w *Wallet) Close() error {
	err := w.tg.Stop()
	if err != nil {
		w.log.Error("couldn't stop threads", zap.Error(err))
	}
	err = w.save()
	w.closeFn()
	return err
}

// Tip returns the current tip of the wallet.
func (w *Wallet) Tip() types.ChainIndex {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tip
}

// LastAddresses returns the last n addresses starting at the last seedProgress
// for which an address was generated. If n is greater than the current
// progress, fewer than n keys will be returned. That means all addresses can
// be retrieved in reverse order by simply supplying math.MaxUint64 for n.
func (w *Wallet) LastAddresses(n uint64) ([]types.Address, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Get the current seed progress from disk.
	tx, err := w.db.Begin()
	if err != nil {
		return nil, err
	}
	seedProgress, err := w.getSeedProgress()
	tx.Commit()
	if err != nil {
		return nil, err
	}

	// At most seedProgess addresses can be requested.
	if n > seedProgress {
		n = seedProgress
	}
	start := seedProgress - n

	// Generate the keys.
	keys := generateKeys(w.seed, start, n)
	addrs := make([]types.Address, 0, len(keys))
	for i := len(keys) - 1; i >= 0; i-- {
		addrs = append(addrs, types.StandardUnlockHash(keys[i].PublicKey()))
	}

	return addrs, nil
}

// AllAddresses returns all addresses that the wallet is able to spend from.
// Addresses are returned sorted in byte-order.
func (w *Wallet) AllAddresses() ([]types.Address, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	addrs := make([]types.Address, 0, len(w.keys))
	for addr := range w.keys {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs, nil
}

// UnspentSiacoinOutputs returns the unspent SC outputs of the wallet.
func (w *Wallet) UnspentSiacoinOutputs() (sces []types.SiacoinElement) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, sce := range w.sces {
		sces = append(sces, sce)
	}

	return
}

// UnspentSiafundOutputs returns the unspent SF outputs of the wallet.
func (w *Wallet) UnspentSiafundOutputs() (sfes []types.SiafundElement) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, sfe := range w.sfes {
		sfes = append(sfes, sfe)
	}

	return
}

// Annotate annotates the given transactions with the wallet.
func (w *Wallet) Annotate(pool []types.Transaction) []modules.PoolTransaction {
	w.mu.Lock()
	defer w.mu.Unlock()

	var annotated []modules.PoolTransaction
	for _, txn := range pool {
		ptxn := Annotate(txn, func(a types.Address) bool {
			_, ok := w.addrs[a]
			return ok
		})
		if ptxn.Type != "unrelated" {
			annotated = append(annotated, ptxn)
		}
	}

	return annotated
}

func (w *Wallet) sync(index types.ChainIndex) error {
	for index != w.cm.Tip() {
		select {
		case <-w.tg.StopChan():
			return nil
		default:
		}
		crus, caus, err := w.cm.UpdatesSince(index, 100)
		if err != nil {
			w.log.Error("failed to subscribe to chain manager", zap.Error(err))
			return err
		} else if err := w.UpdateChainState(crus, caus); err != nil {
			w.log.Error("failed to update chain state", zap.Error(err))
			return err
		}
		if len(caus) > 0 {
			index = caus[len(caus)-1].State.Index
		}
	}
	return nil
}

func (w *Wallet) subscribe() {
	if err := w.sync(w.tip); err != nil {
		return
	}

	reorgChan := make(chan types.ChainIndex, 1)
	unsubscribe := w.cm.OnReorg(func(index types.ChainIndex) {
		select {
		case reorgChan <- index:
		default:
		}
	})
	defer unsubscribe()

	for {
		select {
		case <-w.tg.StopChan():
			return
		case <-reorgChan:
		}

		if err := w.sync(w.tip); err != nil {
			w.log.Error("failed to sync wallet", zap.Error(err))
		}
	}
}

// New creates a new wallet.
func New(db *sql.DB, cm *chain.Manager, s modules.Syncer, seed, dir string) (*Wallet, error) {
	var entropy modules.Seed
	if err := modules.SeedFromPhrase(&entropy, seed); err != nil {
		return nil, modules.AddContext(err, "unable to decode seed phrase")
	}

	logger, closeFn, err := persist.NewFileLogger(filepath.Join(dir, "wallet.log"))
	if err != nil {
		return nil, modules.AddContext(err, "unable to create logger")
	}

	w := &Wallet{
		cm:           cm,
		s:            s,
		db:           db,
		log:          logger,
		closeFn:      closeFn,
		used:         make(map[types.Hash256]bool),
		addrs:        make(map[types.Address]uint64),
		keys:         make(map[types.Address]types.PrivateKey),
		lookahead:    make(map[types.Address]uint64),
		unusedKeys:   make(map[types.Address]types.UnlockConditions),
		watchedAddrs: make(map[types.Address]uint64),
		sces:         make(map[types.Address]types.SiacoinElement),
		sfes:         make(map[types.Address]types.SiafundElement),
	}

	if err := w.load(); err != nil {
		return nil, modules.AddContext(err, "unable to load wallet")
	}

	go w.threadedSaveWallet()

	if entropy != w.seed {
		w.log.Info("new seed detected, rescanning")
		w.tip = types.ChainIndex{}
		w.addrs = make(map[types.Address]uint64)
		w.keys = make(map[types.Address]types.PrivateKey)
		w.lookahead = make(map[types.Address]uint64)
		w.sces = make(map[types.Address]types.SiacoinElement)
		w.sfes = make(map[types.Address]types.SiafundElement)
		if err := w.reset(); err != nil {
			return nil, modules.AddContext(err, "couldn't reset database before rescanning")
		}

		go func() {
			if err := w.tg.Add(); err != nil {
				w.log.Error("couldn't start thread", zap.Error(err))
				return
			}
			defer w.tg.Done()

			fmt.Println("Wallet: waiting for the consensus to sync before scanning...")
			for {
				if w.synced() {
					break
				}
				select {
				case <-w.tg.StopChan():
					return
				case <-time.After(5 * time.Second):
				}
			}

			copy(w.seed[:], entropy[:])
			dustThreshold := w.DustThreshold()
			scanner := newSeedScanner(w.seed, dustThreshold)
			if err := scanner.scan(cm, w.tg.StopChan()); err != nil {
				w.log.Error("blockchain scan failed", zap.Error(err))
				return
			}

			progress := scanner.largestIndexSeen + 1
			progress += progress / 10
			w.log.Info("blockchain scan finished", zap.Uint64("index", scanner.largestIndexSeen), zap.Uint64("progress", progress))
			w.generate(progress)
			if err := w.saveSeed(progress); err != nil {
				w.log.Error("couldn't save new seed", zap.Error(err))
				return
			}

			w.subscribe()
		}()

		return w, nil
	} else {
		go w.subscribe()
	}

	return w, nil
}

// synced returns true if we are synced with the blockchain.
func (w *Wallet) synced() bool {
	lastBlockTimestamp := w.cm.TipState().PrevTimestamps[0]
	return time.Since(lastBlockTimestamp) < 24*time.Hour && w.s.Synced()
}

// threadedSaveWallet periodically saves the wallet state.
func (w *Wallet) threadedSaveWallet() {
	err := w.tg.Add()
	if err != nil {
		return
	}
	defer w.tg.Done()

	for {
		select {
		case <-w.tg.StopChan():
			return
		case <-time.After(2 * time.Minute):
		}
		w.mu.Lock()
		if err := w.save(); err != nil {
			w.log.Error("couldn't save wallet", zap.Error(err))
		}
		w.mu.Unlock()
	}
}
