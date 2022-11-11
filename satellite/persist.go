package satellite

import (
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/persist"
)

const (
	// logFile is the name of the log file.
	logFile = "satellite.log"

	// persistFilename is the filename to be used when persisting satellite
	// information to a JSON file.
	persistFilename = "satellite.json"
)

// persistMetadata contains the header and version strings that identify the
// satellite persist file.
var persistMetadata = persist.Metadata{
	Header:  "Satellite Persistence",
	Version: "0.1.0",
}

type (
	// persist contains all of the persistent satellite data.
	persistence struct {
	}
)

// load loads the Satellite's persistent data from disk.
func (s *SatelliteModule) load() error {
	err := persist.LoadJSON(persistMetadata, &s.persist, filepath.Join(s.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no satellite.json, nothing to load.
		return nil
	}
	if err != nil {
		return errors.AddContext(err, "failed to load satellite persistence")
	}
	return nil
}

// saveSync stores the Satellite's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (s *SatelliteModule) saveSync() error {
	return persist.SaveJSON(persistMetadata, s.persist, filepath.Join(s.persistDir, persistFilename))
}
