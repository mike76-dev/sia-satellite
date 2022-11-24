package portal

import (
	"os"
	"path/filepath"
	"time"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/persist"
)

const (
	// logFile is the name of the log file.
	logFile = "portal.log"

	// persistFilename is the filename to be used when persisting portal
	// information to a JSON file.
	persistFilename = "portal.json"

	// saveFrequency defines how often the portal should be saved to disk.
	saveFrequency = time.Minute * 2
)

// persistMetadata contains the header and version strings that identify the
// portal persist file.
var persistMetadata = persist.Metadata{
	Header:  "Portal Persistence",
	Version: "0.1.0",
}

// authStatsPersistData returns the authentication statistics that will be
// saved to disk. The portal must be locked.
func (p *Portal) authStatsPersistData() (stats []authenticationStats) {
	for _, entry := range p.authStats {
		stats = append(stats, entry)
	}
	return
}

// load loads the Portal's persistent data from disk.
func (p *Portal) load() error {
	// Load stats.
	var stats []*authenticationStats
	
	err := persist.LoadJSON(persistMetadata, &stats, filepath.Join(p.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no persist.json, nothing to load.
		return nil
	}
	if err != nil {
		return errors.AddContext(err, "failed to load portal persistence")
	}

	// Copy over the stats.
	for i := range stats {
		p.authStats[stats[i].RemoteHost] = *stats[i]
	}

	return nil
}

// saveSync stores the Portal's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (p *Portal) saveSync() error {
	return persist.SaveJSON(persistMetadata, p.authStatsPersistData(), filepath.Join(p.persistDir, persistFilename))
}

// threadedSaveLoop periodically saves the Portal's persistent data.
func (p *Portal) threadedSaveLoop() {
	for {
		select {
		case <-p.threads.StopChan():
			return
		case <-time.After(saveFrequency):
		}

		func() {
			err := p.threads.Add()
			if err != nil {
				return
			}
			defer p.threads.Done()

			p.mu.Lock()
			defer p.mu.Unlock()
			err = p.saveSync()
			if err != nil {
				p.log.Println("ERROR: Unable to save portal persistence:", err)
			}
		}()
	}
}
