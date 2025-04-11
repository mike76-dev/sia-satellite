package wallet

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/syncer"
	"go.uber.org/zap"
)

// Wallet is a multi-address wallet. It doesn't support Siafunds
// and Foundation subsidies.
type Wallet struct {
	chain  *chain.Manager
	syncer *syncer.Syncer
	store  *DBStore
	log    *zap.Logger
	mu     sync.Mutex
	ctx    context.Context
	cancel func()
	locked map[types.SiacoinOutputID]time.Time
}

// New returns an initialized Wallet.
func New(cm *chain.Manager, s *syncer.Syncer, db *sql.DB, seedPhrase string, logger *zap.Logger) (*Wallet, error) {
	ctx, cancel := context.WithCancel(context.Background())
	store, tip, err := NewDBStore(db, seedPhrase, logger)
	if err != nil {
		cancel()
		return nil, utils.AddContext(err, "couldn't initialize store")
	}

	w := &Wallet{
		chain:  cm,
		syncer: s,
		store:  store,
		log:    logger,
		locked: make(map[types.SiacoinOutputID]time.Time),
		ctx:    ctx,
		cancel: cancel,
	}

	reorgCh := make(chan struct{}, 1)
	reorgCh <- struct{}{}
	stop := cm.OnReorg(func(index types.ChainIndex) {
		select {
		case reorgCh <- struct{}{}:
		default:
		}
	})

	syncWallet := func() {
		defer stop()

		for cm.Tip().Height <= tip.Height {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(5 * time.Second)
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-reorgCh:
				index := store.lastSyncedIndex()
				if err := w.syncStore(index); err != nil {
					w.log.Error("failed to sync database", zap.Error(err))
				}
			}
		}
	}

	if len(store.addresses) == 0 { // rescan needed
		scanner := newScanner(store.seed)
		go func() {
			// Wait until synced.
			for {
				if w.synced() {
					break
				}

				time.Sleep(time.Second)
			}

			addrs, err := scanner.scan(ctx, cm, defaultAddresses)
			if err != nil {
				logger.Error("failed to scan blockchain for addresses", zap.Error(err))
				return
			}

			for _, index := range addrs {
				_, err = store.insertAddress(index)
				if err != nil {
					logger.Error("failed to insert address", zap.Error(err))
					return
				}
			}

			if len(store.addresses) == 0 { // insert at least one address
				_, err = store.insertAddress(0)
				if err != nil {
					logger.Error("failed to insert root address", zap.Error(err))
					return
				}
			}

			go syncWallet()
		}()
	} else {
		go syncWallet()
	}

	return w, nil
}

// syncStore is called to apply the changed blockchain state to the wallet store.
func (w *Wallet) syncStore(index types.ChainIndex) error {
	for index != w.chain.Tip() {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		reverted, applied, err := w.chain.UpdatesSince(index, 100)
		if err != nil && strings.Contains(err.Error(), "missing block at index") {
			w.log.Warn("missing block at index, resetting chain state", zap.Uint64("height", index.Height))
			if err := w.store.resetChainState(); err != nil {
				return utils.AddContext(err, "failed to reset consensus state")
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to get updates since %v: %w", index, err)
		} else if len(reverted) == 0 && len(applied) == 0 {
			return nil
		}

		if err := w.updateChainState(reverted, applied); err != nil {
			return utils.AddContext(err, "failed to update wallet state")
		}

		if len(applied) > 0 {
			index = applied[len(applied)-1].State.Index
		} else {
			index = reverted[len(reverted)-1].State.Index
		}

		if err := w.store.updateChainState(index, true); err != nil {
			w.log.Error("couldn't update index", zap.Error(err))
			return err
		}
	}

	return nil
}

// Close shuts down the wallet.
func (w *Wallet) Close() {
	w.cancel()
	w.store.close()
}

// synced returns true if the wallet is synced to the blockchain.
func (w *Wallet) synced() bool {
	var count int
	for _, p := range w.syncer.Peers() {
		if p.Synced() {
			count++
		}
	}

	return count >= 5 && time.Since(w.chain.TipState().PrevTimestamps[0]) < 24*time.Hour
}
