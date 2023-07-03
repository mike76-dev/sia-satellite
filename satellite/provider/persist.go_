package provider

import (
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
)

const (
	// logFile is the name of the log file.
	logFile = "provider.log"

	// persistFilename is the filename to be used when persisting provider
	// information to a JSON file.
	persistFilename = "provider.json"
)

// persistMetadata contains the header and version strings that identify the
// provider persist file.
var persistMetadata = persist.Metadata{
	Header:  "Provider Persistence",
	Version: "0.1.0",
}

type (
	// persist contains all of the persistent provider data.
	persistence struct {
		AutoAddress modules.NetAddress `json:"autoaddress"`
	}
)

// load loads the Provider's persistent data from disk.
func (p *Provider) load() error {
	err := persist.LoadJSON(persistMetadata, &p.persist, filepath.Join(p.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no persist.json, nothing to load.
		return nil
	}
	if err != nil {
		return errors.AddContext(err, "failed to load provider persistence")
	}
	// Copy over the identity.
	p.autoAddress = p.persist.AutoAddress

	return nil
}

// saveSync stores the Provider's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (p *Provider) saveSync() error {
	ps := persistence{
		AutoAddress: p.autoAddress,
	}
	return persist.SaveJSON(persistMetadata, ps, filepath.Join(p.persistDir, persistFilename))
}
