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
	"github.com/mike76-dev/sia-satellite/sat"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

var (
	// Nil dependency errors.
	errNilCS      = errors.New("satellite cannot use a nil state")
	errNilTpool   = errors.New("satellite cannot use a nil transaction pool")
	errNilWallet  = errors.New("satellite cannot use a nil wallet")
	errNilGateway = errors.New("satellite cannot use nil gateway")
)

// A Satellite contains the information necessary to communicate both with
// the renters and with the hosts.
type Satellite struct {
	// Dependencies.
	cs     modules.ConsensusSet
	g      modules.Gateway
	tpool  modules.TransactionPool
	wallet modules.Wallet

	// ACID fields - these fields need to be updated in serial, ACID transactions.
	publicKey types.SiaPublicKey
	secretKey crypto.SecretKey

	// Submodules.
	m Manager
	p Provider

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter
}

// New returns an initialized Satellite.
func New(cs modules.ConsensusSet, g modules.Gateway, tpool modules.TransactionPool, wallet modules.Wallet, satelliteAddr string, persistDir string) (*Satellite, error) {
	// Check that all the dependencies were provided.
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
	m, errChanM := manager.New(persistDir)
	if err = modules.PeekErr(errChanM); err != nil {
		return nil, errors.AddContext(err, "unable to create manager")
	}

	// Create the provider.
	p, errChanP := provider.New(g, satelliteAddr, persistDir)
	if err = modules.PeekErr(errChanP); err != nil {
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
		staticAlerter: modules.NewAlerter("satellite"),
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

// enforce that Satellite satisfies the interfaces.Satellite interface
var _ sat.Satellite = (*Satellite)(nil)
