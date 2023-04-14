package satellite

import (
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/persist"
	"go.sia.tech/siad/types"

	"golang.org/x/crypto/ed25519"
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
		// Satellite identity.
		PublicKey   types.SiaPublicKey `json:"publickey"`
		SecretKey   crypto.SecretKey   `json:"secretkey"`

		// Block height timestamps.
		CurrentMonth blockHeightTimestamp `json:"currentmonth"`
		PrevMonth    blockHeightTimestamp `json:"prevmonth"`
	}
)

// establishDefaults configures the default settings for the satellite,
// overwriting any existing settings.
func (s *Satellite) establishDefaults() {
	// Generate the satellite's key pair.
	epk, esk, _ := ed25519.GenerateKey(fastrand.Reader)

	var sk crypto.SecretKey
	var pk crypto.PublicKey
	copy(sk[:], esk[:])
	copy(pk[:], epk[:])

	s.publicKey = types.Ed25519PublicKey(pk)
	s.secretKey = sk

	// The generated keys are important, save them.
	err := s.saveSync()
	if err != nil {
		s.log.Println("failed to save satellite persistence:", err)
	}
}

// load loads the Satellite's persistent data from disk.
func (s *Satellite) load() error {
	err := persist.LoadJSON(persistMetadata, &s.persist, filepath.Join(s.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no satellite.json, nothing to load.
		s.establishDefaults()
		return nil
	}
	if err != nil {
		s.establishDefaults()
		return errors.AddContext(err, "failed to load satellite persistence")
	}
	// Copy over the data.
	s.publicKey = s.persist.PublicKey
	s.secretKey = s.persist.SecretKey
	s.currentMonth = s.persist.CurrentMonth
	s.prevMonth = s.persist.PrevMonth

	return nil
}

// saveSync stores the Satellite's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (s *Satellite) saveSync() error {
	p := persistence{
		PublicKey:    s.publicKey,
		SecretKey:    s.secretKey,
		CurrentMonth: s.currentMonth,
		PrevMonth:    s.prevMonth,
	}
	return persist.SaveJSON(persistMetadata, p, filepath.Join(s.persistDir, persistFilename))
}
