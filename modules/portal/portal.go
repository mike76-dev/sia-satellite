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
)

var (
	// Nil dependency errors.
	errNilDB       = errors.New("portal cannot use a nil database")
	errNilManager  = errors.New("portal cannot use a nil manager")
	errNilProvider = errors.New("portal cannot use a nil provider")
)

// Portal contains the information related to the server.
type Portal struct {
	// Dependencies.
	db       *sql.DB
	manager  modules.Manager
	provider modules.Provider

	// Server-related fields.
	apiPort string

	// Atomic stats.
	authStats map[string]authenticationStats
	credits   modules.CreditData

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
func New(config *persist.SatdConfig, db *sql.DB, m modules.Manager, p modules.Provider, dir string) (*Portal, error) {
	var err error

	// Check that all the dependencies were provided.
	if db == nil {
		return nil, errNilDB
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
		manager:  m,
		provider: p,

		apiPort: config.PortalPort,

		authStats: make(map[string]authenticationStats),

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

	// Create the mail client.
	pt.ms, err = mail.New(config.Dir)
	if err != nil {
		pt.log.Println("ERROR: could not create mail client", err)
	}

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
