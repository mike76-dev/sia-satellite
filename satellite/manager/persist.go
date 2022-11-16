package manager

import (
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/persist"
)

const (
	// logFile is the name of the log file.
	logFile = "manager.log"

	// persistFilename is the filename to be used when persisting manager
	// information to a JSON file.
	persistFilename = "manager.json"
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

	// TODO Copy data fields.

	return nil
}

// saveSync stores the Provider's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (m *Manager) saveSync() error {
	p := persistence{
		// TODO Copy data fields.
	}
	return persist.SaveJSON(persistMetadata, p, filepath.Join(m.persistDir, persistFilename))
}
