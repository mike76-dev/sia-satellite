// manager package is the host-facing part of the satellite module.
package manager

import (
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/contractor"
	"github.com/mike76-dev/sia-satellite/satellite/manager/hostdb"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {
	smodules.Alerter

	// SetAllowance sets the amount of money the contractor is allowed to
	// spend on contracts over a given time period, divided among the number
	// of hosts specified. Note that contractor can start forming contracts as
	// soon as SetAllowance is called; that is, it may block.
	SetAllowance(types.SiaPublicKey, smodules.Allowance) error

	// Allowance returns the current allowance of the renter.
	Allowance(types.SiaPublicKey) smodules.Allowance

	// Close closes the hostContractor.
	Close() error

	// CancelContract cancels the Renter's contract.
	CancelContract(types.FileContractID) error

	// Contracts returns the staticContracts of the renter's hostContractor.
	Contracts() []modules.RenterContract

	// ContractByPublicKeys returns the contract associated with the renter
	// and the host keys.
	ContractByPublicKeys(types.SiaPublicKey, types.SiaPublicKey) (modules.RenterContract, bool)

	// ContractPublicKey returns the public key capable of verifying the renter's
	// signature on a contract.
	ContractPublicKey(types.SiaPublicKey, types.SiaPublicKey) (crypto.PublicKey, bool)

	// ContractUtility returns the utility field for a given contract, along
	// with a bool indicating if it exists.
	ContractUtility(types.SiaPublicKey, types.SiaPublicKey) (smodules.ContractUtility, bool)

	// ContractStatus returns the status of the given contract within the
	// watchdog.
	ContractStatus(fcID types.FileContractID) (smodules.ContractWatchStatus, bool)

	// CurrentPeriod returns the height at which the current allowance period
	// of the renter began.
	CurrentPeriod(types.SiaPublicKey) types.BlockHeight

	// PeriodSpending returns the amount spent on contracts during the current
	// billing period of the renter.
	PeriodSpending(types.SiaPublicKey) (smodules.ContractorSpending, error)

	// ProvidePayment takes a stream and a set of payment details and handles
	// the payment for an RPC by sending and processing payment request and
	// response objects to the host. It returns an error in case of failure.
	ProvidePayment(stream io.ReadWriter, pt *smodules.RPCPriceTable, details contractor.PaymentDetails) error

	// OldContracts returns the oldContracts of the renter's hostContractor.
	OldContracts() []modules.RenterContract

	// IsOffline reports whether the specified host is considered offline.
	IsOffline(types.SiaPublicKey) bool

	// Session creates a Session from the specified contract ID.
	Session(types.SiaPublicKey, types.SiaPublicKey, <-chan struct{}) (contractor.Session, error)

	// RefreshedContract checks if the contract was previously refreshed.
	RefreshedContract(fcid types.FileContractID) bool

	// RenewContract takes an established connection to a host and renews the
	// given contract with that host.
	RenewContract(conn net.Conn, fcid types.FileContractID, params smodules.ContractParams, txnBuilder smodules.TransactionBuilder, tpool smodules.TransactionPool, hdb modules.HostDB, pt *smodules.RPCPriceTable) (modules.RenterContract, []types.Transaction, error)

	// Synced returns a channel that is closed when the contractor is fully
	// synced with the peer-to-peer network.
	Synced() <-chan struct{}

	// UpdateWorkerPool updates the workerpool currently in use by the contractor.
	UpdateWorkerPool(smodules.WorkerPool)
}

// A Manager contains the information necessary to communicate with the
// hosts.
type Manager struct {
	// Dependencies.
	db             *sql.DB
	hostContractor hostContractor
	hostDB         modules.HostDB

	// Atomic properties.
	hostAverages modules.HostAverages

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *smodules.GenericAlerter
}

// New returns an initialized Manager.
func New(cs smodules.ConsensusSet, g smodules.Gateway, tpool smodules.TransactionPool, wallet smodules.Wallet, db *sql.DB, mux *siamux.SiaMux, persistDir string) (*Manager, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the HostDB object.
	hdb, errChanHDB := hostdb.New(g, cs, tpool, db, mux, persistDir)
	if err := smodules.PeekErr(errChanHDB); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Contractor.
	hc, errChanContractor := contractor.New(cs, wallet, tpool, hdb, db, persistDir)
	if err := smodules.PeekErr(errChanContractor); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Manager object.
	m := &Manager{
		db:             db,
		hostContractor: hc,
		hostDB:         hdb,
		persistDir:     persistDir,
		staticAlerter:  smodules.NewAlerter("manager"),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err = smodules.PeekErr(errChan); err != nil {
			errChan <- errors.Compose(m.threads.Stop(), err)
		}
	}()

	// Create the logger.
	m.log, err = persist.NewFileLogger(filepath.Join(persistDir, logFile))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Establish the closing of the logger.
	m.threads.AfterStop(func() {
		err := m.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the manager logger:", err)
		}
	})
	m.log.Println("INFO: manager created, started logging")

	// Load the manager persistence.
	if loadErr := m.load(); loadErr != nil && !os.IsNotExist(loadErr) {
		errChan <- errors.AddContext(loadErr, "unable to load manager")
		return nil, errChan
	}

	// Spawn the thread to periodically save the manager.
	go m.threadedSaveLoop()

	// Make sure that the manager saves on shutdown.
	m.threads.AfterStop(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		err := m.saveSync()
		if err != nil {
			m.log.Println("ERROR: Unable to save manager:", err)
		}
	})

	// Spawn a thread to calculate the host network averages.
	go m.threadedCalculateAverages()

	return m, errChan
}

// ActiveHosts returns an array of hostDB's active hosts.
func (m *Manager) ActiveHosts() ([]smodules.HostDBEntry, error) { return m.hostDB.ActiveHosts() }

// AllHosts returns an array of all hosts.
func (m *Manager) AllHosts() ([]smodules.HostDBEntry, error) { return m.hostDB.AllHosts() }

// Close shuts down the manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return errors.Compose(m.threads.Stop(), m.hostDB.Close(), m.hostContractor.Close(), m.saveSync())
}

// Filter returns the hostdb's filterMode and filteredHosts.
func (m *Manager) Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error) {
	var fm smodules.FilterMode
	hosts := make(map[string]types.SiaPublicKey)
	if err := m.threads.Add(); err != nil {
		return fm, hosts, nil, err
	}
	defer m.threads.Done()
	fm, hosts, netAddresses, err := m.hostDB.Filter()
	if err != nil {
		return fm, hosts, netAddresses, errors.AddContext(err, "error getting hostdb filter:")
	}
	return fm, hosts, netAddresses, nil
}

// SetFilterMode sets the hostdb filter mode.
func (m *Manager) SetFilterMode(lm smodules.FilterMode, hosts []types.SiaPublicKey, netAddresses []string) error {
	if err := m.threads.Add(); err != nil {
		return err
	}
	defer m.threads.Done()

	// Set list mode filter for the hostdb.
	if err := m.hostDB.SetFilterMode(lm, hosts, netAddresses); err != nil {
		return err
	}

	return nil
}

// Host returns the host associated with the given public key.
func (m *Manager) Host(spk types.SiaPublicKey) (smodules.HostDBEntry, bool, error) {
	return m.hostDB.Host(spk)
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (m *Manager) InitialScanComplete() (bool, types.BlockHeight, error) { return m.hostDB.InitialScanComplete() }

// ScoreBreakdown returns the score breakdown of the specific host.
func (m *Manager) ScoreBreakdown(e smodules.HostDBEntry) (smodules.HostScoreBreakdown, error) {
	return m.hostDB.ScoreBreakdown(e)
}

// EstimateHostScore returns the estimated host score.
func (m *Manager) EstimateHostScore(e smodules.HostDBEntry, a smodules.Allowance) (smodules.HostScoreBreakdown, error) {
	return m.hostDB.EstimateHostScore(e, a)
}

// RandomHosts picks up to the specified number of random hosts from the
// hostdb sorted by weight.
func (m *Manager) RandomHosts(n uint64, a smodules.Allowance) ([]smodules.HostDBEntry, error) {
	return m.hostDB.RandomHostsWithLimits(int(n), a)
}

// GetAverages retrieves the host network averages from HostDB.
func (m *Manager) GetAverages() modules.HostAverages {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hostAverages
}
