// satellite package declares the satellite module, which listens for
// renter connections and contacts hosts on the renter's behalf.
package satellite

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/threadgroup"

	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
)

var (
	// Nil dependency errors.
	errNilCS      = errors.New("satellite cannot use a nil state")
	errNilTpool   = errors.New("satellite cannot use a nil transaction pool")
	errNilWallet  = errors.New("satellite cannot use a nil wallet")
	errNilGateway = errors.New("satellite cannot use nil gateway")
)

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	// Close safely shuts down the satellite.
	Close() error
}

// A Satellite contains the information necessary to communicate both with
// the renters and with the hosts.
type SatelliteModule struct {
	// Dependencies.
	cs         modules.ConsensusSet
	g          modules.Gateway
	tpool      modules.TransactionPool
	wallet     modules.Wallet

	// Utilities.
	log        *persist.Logger
	mu         sync.RWMutex
	persist    persistence
	persistDir string
	port       string
	threads    threadgroup.ThreadGroup
}

// New returns an initialized Satellite.
func New(cs modules.ConsensusSet, g modules.Gateway, tpool modules.TransactionPool, wallet modules.Wallet, satelliteAddr string, persistDir string) (*SatelliteModule, error) {
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

	// Create the satellite object.
	s := &SatelliteModule{
		cs:         cs,
		g:          g,
		tpool:      tpool,
		wallet:     wallet,

		persistDir: persistDir,
		port:       satelliteAddr,
	}

	// Create the perist directory if it does not yet exist.
	err := os.MkdirAll(s.persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Create the logger.
	s.log, err = persist.NewFileLogger(filepath.Join(s.persistDir, logFile))
	if err != nil {
		return nil, err
	}
	// Establish the closing of the logger.
	s.threads.AfterStop(func() error {
		if err := s.log.Close(); err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the satellite logger:", err)
			return err
		}
		return nil
	})
	s.log.Println("INFO: satellite created, started logging")

	// Load the satellite persistence.
	if loadErr := s.load(); loadErr != nil && !os.IsNotExist(loadErr) {
		return nil, errors.AddContext(loadErr, "unable to load satellite")
	}

	// Make sure that the satellite saves on shutdown.
	s.threads.AfterStop(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.saveSync(); err != nil {
			s.log.Println("ERROR: Unable to save satellite:", err)
			return err
		}
		return nil
	})

	return s, nil
}

// Close shuts down the satellite.
func (s *SatelliteModule) Close() error {
	if err := s.threads.Stop(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSync()
}
