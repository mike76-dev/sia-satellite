// satellite package declares the satellite module, which listens for
// renter connections and contacts hosts on the renter's behalf.
package satellite

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mike76-dev/sia-satellite/satellite/manager"
	"github.com/mike76-dev/sia-satellite/satellite/provider"
	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

var (
	// Nil dependency errors.
	errNilSMux    = errors.New("satellite cannot use a nil siamux")
	errNilCS      = errors.New("satellite cannot use a nil state")
	errNilTpool   = errors.New("satellite cannot use a nil transaction pool")
	errNilWallet  = errors.New("satellite cannot use a nil wallet")
	errNilGateway = errors.New("satellite cannot use nil gateway")
)

// A Satellite contains the information necessary to communicate both with
// the renters and with the hosts.
type Satellite struct {
	// Dependencies.
	mux    *siamux.SiaMux
	cs     smodules.ConsensusSet
	g      smodules.Gateway
	tpool  smodules.TransactionPool
	wallet smodules.Wallet

	// ACID fields - these fields need to be updated in serial, ACID transactions.
	publicKey types.SiaPublicKey
	secretKey crypto.SecretKey

	// Submodules.
	m *manager.Manager
	p *provider.Provider

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *smodules.GenericAlerter
}

// PublicKey returns the satellite's public key
func (s *Satellite) PublicKey() types.SiaPublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicKey
}

// SecretKey returns the satellite's secret key
func (s *Satellite) SecretKey() crypto.SecretKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secretKey
}

// New returns an initialized Satellite.
func New(cs smodules.ConsensusSet, g smodules.Gateway, tpool smodules.TransactionPool, wallet smodules.Wallet, mux *siamux.SiaMux, satelliteAddr string, persistDir string) (*Satellite, error) {
	// Check that all the dependencies were provided.
	if mux == nil {
		return nil, errNilSMux
	}
	if cs == nil {
		return nil, errNilCS
	}
	if g == nil {
		return nil, errNilGateway
	}
	if tpool == nil {
		return nil, errNilTpool
	}
	if wallet == nil {
		return nil, errNilWallet
	}

	// Create the perist directory if it does not yet exist.
	err := os.MkdirAll(persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Create the manager.
	m, errChanM := manager.New(cs, g, tpool, wallet, mux, persistDir)
	if err = smodules.PeekErr(errChanM); err != nil {
		return nil, errors.AddContext(err, "unable to create manager")
	}

	// Create the provider.
	p, errChanP := provider.New(g, satelliteAddr, persistDir)
	if err = smodules.PeekErr(errChanP); err != nil {
		return nil, errors.AddContext(err, "unable to create provider")
	}

	// Create the satellite object.
	s := &Satellite{
		cs:            cs,
		g:             g,
		tpool:         tpool,
		wallet:        wallet,

		m:             m,
		p:             p,

		persistDir:    persistDir,
		staticAlerter: smodules.NewAlerter("satellite"),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err != nil {
			err = errors.Compose(s.threads.Stop(), err)
		}
	}()

	// Create the logger.
	s.log, err = persist.NewFileLogger(filepath.Join(s.persistDir, logFile))
	if err != nil {
		return nil, err
	}
	// Establish the closing of the logger.
	s.threads.AfterStop(func() {
		err := s.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the satellite logger:", err)
		}
	})
	s.log.Println("INFO: satellite created, started logging")

	// Load the satellite persistence.
	if loadErr := s.load(); loadErr != nil && !os.IsNotExist(loadErr) {
		err = errors.AddContext(loadErr, "unable to load satellite")
		return nil, err
	}

	// Make sure that the satellite saves on shutdown.
	s.threads.AfterStop(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		err := s.saveSync()
		if err != nil {
			s.log.Println("ERROR: Unable to save satellite:", err)
		}
	})

	return s, nil
}

// ActiveHosts calls Manager.ActiveHosts.
func (s *Satellite) ActiveHosts() ([]smodules.HostDBEntry, error) { return s.m.ActiveHosts() }

// AllHosts calls Manager.AllHosts.
func (s *Satellite) AllHosts() ([]smodules.HostDBEntry, error) { return s.m.AllHosts() }

// Close shuts down the satellite.
func (s *Satellite) Close() error {
	var errP, errM, err error

	// Close the provider.
	errP = s.p.Close()

	// Close the manager.
	errM = s.m.Close()

	if err = s.threads.Stop(); err != nil {
		return errors.Compose(errP, errM, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSync()
}

// Filter calls Manager.Filter.
func (s *Satellite) Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error) { return s.m.Filter() }

// SetFilterMode calls Manager.SetFilterMode.
func (s *Satellite) SetFilterMode(lm smodules.FilterMode, hosts []types.SiaPublicKey, netAddresses []string) error { return s.m.SetFilterMode(lm, hosts, netAddresses) }

// Host calls Manager.Host.
func (s *Satellite) Host(spk types.SiaPublicKey) (smodules.HostDBEntry, bool, error) { return s.m.Host(spk) }

// InitialScanComplete calls Manager.InitialScanComplete.
func (s *Satellite) InitialScanComplete() (bool, error) { return s.m.InitialScanComplete() }

// ScoreBreakdown calls Manager.ScoreBreakdown.
func (s *Satellite) ScoreBreakdown(e smodules.HostDBEntry) (smodules.HostScoreBreakdown, error) { return s.m.ScoreBreakdown(e) }

// EstimateHostScore calls Manager.EstimateHostScore.
func (s *Satellite) EstimateHostScore(e smodules.HostDBEntry, a smodules.Allowance) (smodules.HostScoreBreakdown, error) { return s.m.EstimateHostScore(e, a) }

// RandomHosts calls Manager.RandomHosts.
func (s *Satellite) RandomHosts(n uint64, a smodules.Allowance) ([]smodules.HostDBEntry, error) { return s.m.RandomHosts(n, a) }

// GetAverages calls Manager.GetAverages.
func (s *Satellite) GetAverages() modules.HostAverages { return s.m.GetAverages() }

// FeeEstimation returns the minimum and the maximum estimated fees for
// a transaction.
func (s *Satellite) FeeEstimation() (min, max types.Currency) { return s.tpool.FeeEstimation() }

// enforce that Satellite satisfies the modules.Satellite interface
var _ modules.Satellite = (*Satellite)(nil)
