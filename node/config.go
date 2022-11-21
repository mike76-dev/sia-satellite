package node

import (
	"os"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/persist"
)

// configFilename is the name of the configuration file.
const configFilename = "config.json"

// SatdConfig contains the fields that are passed on to the new node.
type SatdConfig struct {
	UserAgent     string `json: "agent"`
	GatewayAddr   string `json: "gateway"`
	APIAddr       string `json: "api"`
	SatelliteAddr string `json: "satellite"`
	Dir           string `json: "dir"`
	Bootstrap     bool   `json: "bootstrap"`
}

// metadata contains the header and version strings that identify the
// config file.
var metadata = persist.Metadata{
	Header:  "Satd Configuration",
	Version: "0.1.0",
}

// load loads the configuration from disk.
func (sc *SatdConfig) Load() (ok bool, err error) {
	ok = false
	err = persist.LoadJSON(metadata, &sc, configFilename)
	if os.IsNotExist(err) {
		// There is no config.json, nothing to load.
		err = nil
		return
	}
	if err != nil {
		err = errors.AddContext(err, "failed to load the configuration")
		return
	}
	ok = true
	return
}

// save stores the configuration on disk.
func (sc *SatdConfig) Save() error {
	return persist.SaveJSON(metadata, sc, configFilename)
}
