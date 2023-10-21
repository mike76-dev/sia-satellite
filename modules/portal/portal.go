package portal

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/mail"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/core/types"
)

var (
	// Nil dependency errors.
	errNilDB       = errors.New("portal cannot use a nil database")
	errNilMail     = errors.New("portal cannot use a nil mail client")
	errNilCS       = errors.New("portal cannot use a nil state")
	errNilWallet   = errors.New("portal cannot use a nil wallet")
	errNilManager  = errors.New("portal cannot use a nil manager")
	errNilProvider = errors.New("portal cannot use a nil provider")
)

// Portal contains the information related to the server.
type Portal struct {
	// Dependencies.
	db       *sql.DB
	cs       modules.ConsensusSet
	w        modules.Wallet
	manager  modules.Manager
	provider modules.Provider

	// Server-related fields.
	apiPort string

	// Atomic stats.
	authStats map[string]authenticationStats
	credits   modules.CreditData

	// Watch list of SC payment transactions.
	transactions map[types.TransactionID]types.Address

	// Name of the satellite node.
	name string

	// Utilities.
	listener      net.Listener
	log           *persist.Logger
	mu            sync.Mutex
	tg            siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter
	closeChan     chan int
	ms            mail.MailSender
}

// New returns an initialized portal server.
func New(config *persist.SatdConfig, db *sql.DB, ms mail.MailSender, cs modules.ConsensusSet, w modules.Wallet, m modules.Manager, p modules.Provider, dir string, name string) (*Portal, error) {
	var err error

	// Check that all the dependencies were provided.
	if db == nil {
		return nil, errNilDB
	}
	if ms == nil {
		return nil, errNilMail
	}
	if cs == nil {
		return nil, errNilCS
	}
	if w == nil {
		return nil, errNilWallet
	}
	if m == nil {
		return nil, errNilManager
	}
	if p == nil {
		return nil, errNilProvider
	}

	// Create the portal object.
	pt := &Portal{
		db:       db,
		ms:       ms,
		cs:       cs,
		w:        w,
		manager:  m,
		provider: p,

		apiPort: config.PortalPort,

		authStats:    make(map[string]authenticationStats),
		transactions: make(map[types.TransactionID]types.Address),
		name:         name,

		staticAlerter: modules.NewAlerter("portal"),
		closeChan:     make(chan int, 1),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err != nil {
			close(pt.closeChan)
			err = modules.ComposeErrors(pt.tg.Stop(), err)
		}
	}()

	// Create the logger.
	pt.log, err = persist.NewFileLogger(filepath.Join(dir, "portal.log"))
	if err != nil {
		return nil, err
	}
	// Establish the closing of the logger.
	pt.tg.AfterStop(func() {
		err := pt.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("ERROR: failed to close the portal logger:", err)
		}
	})
	pt.log.Println("INFO: portal created, started logging")

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
			pt.log.Println("ERROR: unable to save portal:", err)
		}
	})

	// Start listening to API requests.
	if err = pt.initNetworking("127.0.0.1" + pt.apiPort); err != nil {
		pt.log.Println("ERROR: unable to start the portal server:", err)
		return nil, err
	}

	// Subscribe to the consensus set using the most recent consensus change.
	go func() {
		err := pt.cs.ConsensusSetSubscribe(pt, modules.ConsensusChangeRecent, pt.tg.StopChan())
		if modules.ContainsError(err, siasync.ErrStopped) {
			return
		}
		if err != nil {
			pt.log.Critical(err)
			return
		}
	}()
	pt.tg.OnStop(func() {
		pt.cs.Unsubscribe(pt)
		// We don't want any recently made payments to go unnoticed.
		pt.managedCheckWallet()
	})

	return pt, nil
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
