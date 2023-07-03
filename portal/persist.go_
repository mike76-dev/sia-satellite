package portal

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

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

// persistData contains the portal persistence.
type persistData struct {
	Stats   []authenticationStats `json:"stats"`
	Credits modules.CreditData    `json:"credits"`
}

// persistence returns the portal persistence that will be saved to disk.
func (p *Portal) toPersistence() (persistence persistData) {
	for _, entry := range p.authStats {
		persistence.Stats = append(persistence.Stats, entry)
	}
	persistence.Credits = p.credits
	return
}

// load loads the Portal's persistent data from disk.
func (p *Portal) load() error {
	var persistence persistData
	
	err := persist.LoadJSON(persistMetadata, &persistence, filepath.Join(p.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no persist.json, nothing to load.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to load portal persistence: %s", err)
	}

	// Copy over the data.
	for _, s := range persistence.Stats {
		p.authStats[s.RemoteHost] = s
	}
	p.credits = persistence.Credits

	return nil
}

// saveSync stores the Portal's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (p *Portal) saveSync() error {
	return persist.SaveJSON(persistMetadata, p.toPersistence(), filepath.Join(p.persistDir, persistFilename))
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
