// portal package declares the server for the web portal.
package portal

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/mike76-dev/sia-satellite/mail"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"github.com/mike76-dev/sia-satellite/satellite"

	"gitlab.com/NebulousLabs/errors"

	smodules "go.sia.tech/siad/modules"
	spersist "go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
)

// Portal contains the information related to the server.
type Portal struct {
	// Dependencies.
	db         *sql.DB
	satellite  *satellite.Satellite

	// Server-related fields.
	apiPort    string

	// Atomic stats.
	authStats     map[string]authenticationStats
	credits       modules.CreditData

	// Utilities.
	listener      net.Listener
	log           *spersist.Logger
	mu            sync.Mutex
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *smodules.GenericAlerter
	closeChan     chan int
	ms            mail.MailSender
}

// New returns an initialized portal server.
func New(config *persist.SatdConfig, s *satellite.Satellite, db *sql.DB, persistDir string) (*Portal, error) {
	// Create the perist directory if it does not yet exist.
	err := os.MkdirAll(persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Create the portal object.
	p := &Portal{
		db:        db,
		satellite: s,

		apiPort: config.PortalPort,

		authStats: make(map[string]authenticationStats),

		persistDir:    persistDir,
		staticAlerter: smodules.NewAlerter("portal"),
		closeChan:     make(chan int, 1),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err != nil {
			close(p.closeChan)
			err = errors.Compose(p.threads.Stop(), err)
		}
	}()

	// Create the logger.
	p.log, err = spersist.NewFileLogger(filepath.Join(p.persistDir, logFile))
	if err != nil {
		return nil, err
	}
	// Establish the closing of the logger.
	p.threads.AfterStop(func() {
		err := p.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the portal logger:", err)
		}
	})
	p.log.Println("INFO: portal created, started logging")

	// Create the mail client.
	ms, err := mail.New(config.Dir)
	if err != nil {
		p.log.Println("ERROR: could not create mail client", err)
	}
	p.ms = ms

	// Load the portal persistence.
	if err = p.load(); err != nil {
		return nil, errors.AddContext(err, "unable to load portal")
	}

	// Spawn the thread to periodically save the portal.
	go p.threadedSaveLoop()

	// Spawn the thread to periodically prune the authentication stats.
	go p.threadedPruneAuthStats()

	// Spawn the thread to periodically prune the unverified accounts.
	go p.threadedPruneUnverifiedAccounts()

	// Make sure that the portal saves after shutdown.
	p.threads.AfterStop(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if err := p.saveSync(); err != nil {
			p.log.Println("ERROR: unable to save portal:", err)
		}
	})

	// Start listening to API requests.
	if err = p.initNetworking("127.0.0.1" + p.apiPort); err != nil {
		p.log.Println("Unable to start the portal server:", err)
		return nil, err
	}

	return p, nil
}

// Close shuts down the portal.
func (p *Portal) Close() error {
	// Shut down the listener.
	p.closeChan <- 1
	
	if err := p.threads.Stop(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveSync()
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

// enforce that Portal satisfies the modules.Portal interface
var _ modules.Portal = (*Portal)(nil)
