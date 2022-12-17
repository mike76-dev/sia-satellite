// manager package is the host-facing part of the satellite module.
package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mike76-dev/sia-satellite/satellite/manager/contractor"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/modules/renter/hostdb"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {
	modules.Alerter

	// Allowance returns the current allowance.
	Allowance() modules.Allowance

	// Close closes the hostContractor.
	Close() error

	// ContractPublicKey returns the public key capable of verifying the renter's
	// signature on a contract.
	ContractPublicKey(pk types.SiaPublicKey) (crypto.PublicKey, bool)

	// CurrentPeriod returns the height at which the current allowance period
	// began.
	CurrentPeriod() types.BlockHeight

	// PeriodSpending returns the amount spent on contracts during the current
	// billing period.
	PeriodSpending() (modules.ContractorSpending, error)

	// RefreshedContract checks if the contract was previously refreshed.
	RefreshedContract(fcid types.FileContractID) bool

	// Synced returns a channel that is closed when the contractor is fully
	// synced with the peer-to-peer network.
	Synced() <-chan struct{}
}

// A Manager contains the information necessary to communicate with the
// hosts.
type Manager struct {
	// Dependencies.
	hostContractor hostContractor
	hostDB         modules.HostDB

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter
	staticMux     *siamux.SiaMux
}

// New returns an initialized Manager.
func New(cs modules.ConsensusSet, g modules.Gateway, tpool modules.TransactionPool, wallet modules.Wallet, mux *siamux.SiaMux, persistDir string) (*Manager, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the HostDB object.
	hdb, errChanHDB := hostdb.New(g, cs, tpool, mux, persistDir)
	if err := modules.PeekErr(errChanHDB); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the hostContractor object.
	hc, errChanContractor := contractor.New(cs, wallet, tpool, hdb, persistDir)
	if err := modules.PeekErr(errChanContractor); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Manager object.
	m := &Manager{
		hostContractor: hc,
		hostDB:         hdb,
		persistDir:     persistDir,
		staticAlerter:  modules.NewAlerter("manager"),
		staticMux:      mux,
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err = modules.PeekErr(errChan); err != nil {
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

	// Make sure that the manager saves on shutdown.
	m.threads.AfterStop(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		err := m.saveSync()
		if err != nil {
			m.log.Println("ERROR: Unable to save manager:", err)
		}
	})

	return m, errChan
}

// ActiveHosts returns an array of hostDB's active hosts.
func (m *Manager) ActiveHosts() ([]modules.HostDBEntry, error) { return m.hostDB.ActiveHosts() }

// AllHosts returns an array of all hosts.
func (m *Manager) AllHosts() ([]modules.HostDBEntry, error) { return m.hostDB.AllHosts() }

// Close shuts down the manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return errors.Compose(m.threads.Stop(), m.hostContractor.Close(), m.hostDB.Close(), m.saveSync())
}

// Filter returns the hostdb's filterMode and filteredHosts.
func (m *Manager) Filter() (modules.FilterMode, map[string]types.SiaPublicKey, []string, error) {
	var fm modules.FilterMode
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
func (m *Manager) SetFilterMode(lm modules.FilterMode, hosts []types.SiaPublicKey, netAddresses []string) error {
	if err := m.threads.Add(); err != nil {
		return err
	}
	defer m.threads.Done()
	// Check to see how many hosts are needed for the allowance.
	/*settings, err := s.m.Settings()
	if err != nil {
		return errors.AddContext(err, "error getting manager settings:")
	}
	minHosts := settings.Allowance.Hosts
	if len(hosts) < int(minHosts) && lm == smodules.HostDBActiveWhitelist {
		s.m.log.Printf("WARN: There are fewer whitelisted hosts than the allowance requires.  Have %v whitelisted hosts, need %v to support allowance\n", len(hosts), minHosts)
	}*/ //TODO

	// Set list mode filter for the hostdb.
	if err := m.hostDB.SetFilterMode(lm, hosts, netAddresses); err != nil {
		return err
	}

	return nil
}

// Host returns the host associated with the given public key.
func (m *Manager) Host(spk types.SiaPublicKey) (modules.HostDBEntry, bool, error) {
	return m.hostDB.Host(spk)
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (m *Manager) InitialScanComplete() (bool, error) { return m.hostDB.InitialScanComplete() }

// ScoreBreakdown returns the score breakdown of the specific host.
func (m *Manager) ScoreBreakdown(e modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	return m.hostDB.ScoreBreakdown(e)
}

// EstimateHostScore returns the estimated host score.
func (m *Manager) EstimateHostScore(e modules.HostDBEntry, a modules.Allowance) (modules.HostScoreBreakdown, error) {
	/*if reflect.DeepEqual(a, smodules.Allowance{}) {
		settings, err := s.m.Settings()
		if err != nil {
			return smodules.HostScoreBreakdown{}, errors.AddContext(err, "error getting renter settings:")
		}
		a = settings.Allowance
	}
	if reflect.DeepEqual(a, smodules.Allowance{}) {
		a = smodules.DefaultAllowance
	}*/ //TODO
	return m.hostDB.EstimateHostScore(e, a)
}
