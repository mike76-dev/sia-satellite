// portal package declares the server for the web portal.
package portal

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-sql-driver/mysql"
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
	// Satellite reference.
	satellite  *satellite.Satellite

	// Database-related fields.
	db         *sql.DB
	dbUser     string
	dbPassword string
	dbName     string

	// Server-related fields.
	apiPort    string

	// Atomic stats.
	authStats     map[string]authenticationStats
	exchRates     map[string]float64
	scusdRate     float64

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
func New(config *persist.SatdConfig, s *satellite.Satellite, dbPassword string, persistDir string) (*Portal, error) {
	// Create the perist directory if it does not yet exist.
	err := os.MkdirAll(persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Create the portal object.
	p := &Portal{
		satellite:     s,

		dbUser:        config.DBUser,
		dbPassword:    dbPassword,
		dbName:        config.DBName,

		apiPort:       config.PortalPort,

		authStats:     make(map[string]authenticationStats),
		exchRates:     make(map[string]float64),

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
	ms, err := mail.New(persistDir)
	if err != nil {
		p.log.Println("ERROR: could not create mail client", err)
	}
	p.ms = ms

	// Connect to the database.
	cfg := mysql.Config {
		User:		p.dbUser,
		Passwd:	p.dbPassword,
		Net:		"tcp",
		Addr:		"127.0.0.1:3306",
		DBName: p.dbName,
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatalf("Could not connect to the database: %v\n", err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatalf("MySQL database not responding: %v\n", err)
	}
	p.db = db
	p.log.Println("INFO: successfully connected to MySQL database")
	// Make sure that the database is closed on shutdown.
	p.threads.AfterStop(func() {
		err := p.db.Close()
		if err != nil {
			p.log.Println("ERROR: unable to close the database:", err)
		}
	})

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

	// Spawn the threads to fetch the exchange rates.
	go p.threadedFetchExchangeRates()
	go p.threadedFetchSCUSDRate()

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
	
	// Close the database.
	errDB := p.db.Close()

	if err := p.threads.Stop(); err != nil {
		return errors.Compose(errDB, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveSync()
}

// enforce that Portal satisfies the modules.Portal interface
var _ modules.Portal = (*Portal)(nil)
