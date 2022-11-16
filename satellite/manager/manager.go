// manager package is the host-facing part of the satellite module.
package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
)

// A Manager contains the information necessary to communicate with the
// hosts.
type Manager struct {
	// Utilities
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter
}

// New returns an initialized Manager.
func New(persistDir string) (*Manager, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the Manager object.
	m := &Manager{
		persistDir:    persistDir,
		staticAlerter: modules.NewAlerter("manager"),
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

// Close shuts down the manager.
func (m *Manager) Close() error {
	if err := m.threads.Stop(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveSync()
}
