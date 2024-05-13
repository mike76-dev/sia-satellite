package portal

import (
	"database/sql"
	"net"
	"path/filepath"
	"sync"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/mail"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.uber.org/zap"
)

// Portal contains the information related to the server.
type Portal struct {
	// Dependencies.
	db       *sql.DB
	cm       *chain.Manager
	w        modules.Wallet
	manager  modules.Manager
	provider modules.Provider

	// Server-related fields.
	apiPort string
	satAddr string
	muxAddr string

	// Atomic stats.
	authStats map[string]authenticationStats
	credits   modules.CreditData
	tip       types.ChainIndex

	// Watch list of SC payment transactions.
	transactions map[types.TransactionID]types.Address

	// Name of the satellite node.
	name string

	// Utilities.
	listener  net.Listener
	log       *zap.Logger
	mu        sync.Mutex
	tg        siasync.ThreadGroup
	closeChan chan int
	ms        mail.MailSender
}

// New returns an initialized portal server.
func New(config *persist.SatdConfig, db *sql.DB, ms mail.MailSender, cm *chain.Manager, w modules.Wallet, m modules.Manager, p modules.Provider, dir string) (*Portal, error) {
	var err error

	// Create the portal object.
	pt := &Portal{
		db:       db,
		ms:       ms,
		cm:       cm,
		w:        w,
		manager:  m,
		provider: p,

		apiPort: config.PortalPort,
		satAddr: config.SatelliteAddr,
		muxAddr: config.MuxAddr,

		authStats:    make(map[string]authenticationStats),
		transactions: make(map[types.TransactionID]types.Address),
		name:         config.Name,

		closeChan: make(chan int, 1),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err != nil {
			close(pt.closeChan)
			err = modules.ComposeErrors(pt.tg.Stop(), err)
		}
	}()

	// Create the logger.
	logger, closeFn, err := persist.NewFileLogger(filepath.Join(dir, "portal.log"))
	if err != nil {
		return nil, err
	}
	// Establish the closing of the logger.
	pt.tg.AfterStop(func() {
		closeFn()
	})
	pt.log = logger
	pt.log.Info("portal created, started logging")

	// Load the portal persistence.
	if err = pt.load(); err != nil {
		return nil, modules.AddContext(err, "unable to load portal")
	}

	// Spawn the thread to periodically save the portal.
	go pt.threadedSaveLoop()

	// Spawn the thread to periodically prune the authentication stats.
	go pt.threadedPruneAuthStats()

	// Spawn the thread to periodically prune the unverified accounts.
	go pt.threadedPruneUnverifiedAccounts()

	// Spawn the thread to periodically check the wallet.
	go pt.threadedWatchForNewTxns()

	// Spawn the thread to periodically check the accounts that are on hold.
	go pt.threadedCheckOnHoldAccounts()

	// Spawn the thread to periodically check the portal announcement.
	go pt.threadedCheckAnnouncement()

	// Make sure that the portal saves after shutdown.
	pt.tg.AfterStop(func() {
		pt.mu.Lock()
		defer pt.mu.Unlock()
		if err := pt.save(); err != nil {
			pt.log.Error("unable to save portal", zap.Error(err))
		}
	})

	// Start listening to API requests.
	if err = pt.initNetworking("127.0.0.1" + pt.apiPort); err != nil {
		pt.log.Error("unable to start the portal server", zap.Error(err))
		return nil, err
	}

	// Subscribe to the consensus set using the most recent consensus change.
	reorgChan := make(chan struct{}, 1)
	reorgChan <- struct{}{}
	unsubscribe := cm.OnReorg(func(_ types.ChainIndex) {
		select {
		case reorgChan <- struct{}{}:
		default:
		}
	})

	go func() {
		defer unsubscribe()

		if err := pt.tg.Add(); err != nil {
			pt.log.Error("couldn't start a thread", zap.Error(err))
			return
		}
		defer pt.tg.Done()

		err := pt.sync(pt.tip)
		if err != nil {
			pt.log.Error("couldn't sync portal", zap.Error(err))
			return
		}

		for {
			select {
			case <-pt.tg.StopChan():
				return
			case <-reorgChan:
			}

			err := pt.sync(pt.tip)
			if err != nil {
				pt.log.Error("couldn't sync portal", zap.Error(err))
				return
			}
		}
	}()

	pt.tg.OnStop(func() {
		// We don't want any recently made payments to go unnoticed.
		pt.managedCheckWallet()
	})

	return pt, nil
}

func (p *Portal) sync(index types.ChainIndex) error {
	addrs, err := p.getSiacoinAddresses()
	if err != nil {
		p.log.Error("couldn't get account addresses", zap.Error(err))
		return err
	}

	for index != p.cm.Tip() {
		select {
		case <-p.tg.StopChan():
			return nil
		default:
		}
		crus, caus, err := p.cm.UpdatesSince(index, 100)
		if err != nil {
			p.log.Error("failed to subscribe to chain manager", zap.Error(err))
			return err
		} else if err := p.UpdateChainState(crus, caus, addrs); err != nil {
			p.log.Error("failed to update chain state", zap.Error(err))
			return err
		}
		if len(caus) > 0 {
			index = caus[len(caus)-1].State.Index
			p.tip = index
			if err := p.saveTip(); err != nil {
				p.log.Error("failed to save tip", zap.Error(err))
				return err
			}
		}
	}
	return nil
}

// Close shuts down the portal.
func (p *Portal) Close() error {
	// Shut down the listener.
	p.closeChan <- 1

	if err := p.tg.Stop(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.save()
}

// GetCredits retrieves the credit data.
func (p *Portal) GetCredits() modules.CreditData {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.credits
}

// SetCredits updates the credit data.
func (p *Portal) SetCredits(c modules.CreditData) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.credits = c
}

// enforce that Portal satisfies the modules.Portal interface.
var _ modules.Portal = (*Portal)(nil)

// init performs the initialization.
func init() {
	initStripe()
	initGoogle()
}
