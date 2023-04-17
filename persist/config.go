package persist

import (
	"bytes"
	"io/ioutil"
	"encoding/json"
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"
)

// configFilename is the name of the configuration file.
const configFilename = "satdconfig.json"

// SatdConfig contains the fields that are passed on to the new node.
type SatdConfig struct {
	UserAgent     string `json:"agent"`
	GatewayAddr   string `json:"gateway"`
	APIAddr       string `json:"api"`
	SatelliteAddr string `json:"satellite"`
	SiamuxAddr    string `json:"siamux"`
	SiamuxWSAddr  string `json:"siamuxws"`
	Dir           string `json:"dir"`
	Bootstrap     bool   `json:"bootstrap"`
	DBUser        string `json:"dbuser"`
	DBName        string `json:"dbname"`
	PortalPort    string `json:"portalport"`
}

// satdMetadata contains the header and version strings that identify the
// config file.
type satdMetadata = struct {
	Header  string
	Version string
}

// metadata contains the actual values.
var metadata = satdMetadata{
	Header:  "Satd Configuration",
	Version: "0.1.0",
}

// Load loads the configuration from disk.
func (sc *SatdConfig) Load(dir string) (ok bool, err error) {
	ok = false
	err = loadJSON(metadata, &sc, filepath.Join(dir, configFilename))
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

// Save stores the configuration on disk.
func (sc *SatdConfig) Save(dir string) error {
	return saveJSON(metadata, sc, filepath.Join(dir, configFilename))
}

// loadJSON will try to read a persisted json object from a file.
func loadJSON(meta satdMetadata, object interface{}, filename string) error {
	// Open the file.
	file, err := os.Open(filename)
	if os.IsNotExist(err) {
		return err
	}
	if err != nil {
		return errors.AddContext(err, "unable to open persisted json object file")
	}
	defer func() {
		err = errors.Compose(err, file.Close())
	}()

	// Read the metadata from the file.
	var header, version string
	dec := json.NewDecoder(file)
	if err := dec.Decode(&header); err != nil {
		return errors.AddContext(err, "unable to read header from persisted json object file")
	}
	if header != meta.Header {
		return errors.New("Wrong config file header")
	}
	if err := dec.Decode(&version); err != nil {
		return errors.AddContext(err, "unable to read version from persisted json object file")
	}
	if version != meta.Version {
		return errors.New("Wrong config file version")
	}

	// Read everything else.
	remainingBytes, err := ioutil.ReadAll(dec.Buffered())
	if err != nil {
		return errors.AddContext(err, "unable to read persisted json object data")
	}
	// The buffer may or may not have read the rest of the file, read the rest
	// of the file to be certain.
	remainingBytesExtra, err := ioutil.ReadAll(file)
	if err != nil {
		return errors.AddContext(err, "unable to read persisted json object data")
	}
	remainingBytes = append(remainingBytes, remainingBytesExtra...)

	// Parse the json object.
	return json.Unmarshal(remainingBytes, &object)
}

// saveJSON will save a json object to disk.
func saveJSON(meta satdMetadata, object interface{}, filename string) error {
	// Write the metadata to the buffer.
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	if err := enc.Encode(meta.Header); err != nil {
		return errors.AddContext(err, "unable to encode metadata header")
	}
	if err := enc.Encode(meta.Version); err != nil {
		return errors.AddContext(err, "unable to encode metadata version")
	}

	// Marshal the object into json and write the result to the buffer.
	objBytes, err := json.MarshalIndent(object, "", "\t")
	if err != nil {
		return errors.AddContext(err, "unable to marshal the provided object")
	}
	buf.Write(objBytes)
	data := buf.Bytes()

	// Write out the data to the file, with a sync.
	err = func() (err error) {
		file, err := os.OpenFile(filename, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
		if err != nil {
			return errors.AddContext(err, "unable to open file")
		}
		defer func() {
			err = errors.Compose(err, file.Close())
		}()
		_, err = file.Write(data)
		if err != nil {
			return errors.AddContext(err, "unable to write file")
		}
		err = file.Sync()
		if err != nil {
			return errors.AddContext(err, "unable to sync file")
		}
		return nil
	}()

	return err
}
