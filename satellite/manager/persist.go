package manager

import (
	"os"
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/persist"
)

const (
	// logFile is the name of the log file.
	logFile = "manager.log"

	// persistFilename is the filename to be used when persisting manager
	// information to a JSON file.
	persistFilename = "manager.json"

	// saveFrequency defines how often the manager should be saved to disk.
	saveFrequency = time.Minute * 2
)

// persistMetadata contains the header and version strings that identify the
// manager persist file.
var persistMetadata = persist.Metadata{
	Header:  "Manager Persistence",
	Version: "0.1.0",
}

type (
	// persist contains all of the persistent manager data.
	persistence struct {
		HostAverages modules.HostAverages `json:"averages"`
	}
)

// load loads the Manager's persistent data from disk.
func (m *Manager) load() error {
	err := persist.LoadJSON(persistMetadata, &m.persist, filepath.Join(m.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no persist.json, nothing to load.
		return nil
	}
	if err != nil {
		return errors.AddContext(err, "failed to load manager persistence")
	}

	m.mu.Lock()
	m.hostAverages = m.persist.HostAverages
	m.mu.Unlock()

	return nil
}

// saveSync stores the Manager's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (m *Manager) saveSync() error {
	m.persist = persistence{
		HostAverages: m.hostAverages,
	}
	return persist.SaveJSON(persistMetadata, m.persist, filepath.Join(m.persistDir, persistFilename))
}

// threadedSaveLoop periodically saves the Portal's persistent data.
func (m *Manager) threadedSaveLoop() {
	for {
		select {
		case <-m.threads.StopChan():
			return
		case <-time.After(saveFrequency):
		}

		func() {
			err := m.threads.Add()
			if err != nil {
				return
			}
			defer m.threads.Done()

			m.mu.Lock()
			defer m.mu.Unlock()
			err = m.saveSync()
			if err != nil {
				m.log.Println("ERROR: Unable to save manager persistence:", err)
			}
		}()
	}
}
