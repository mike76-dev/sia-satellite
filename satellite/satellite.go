// satellite package declares the satellite module, which listens for
// renter connections and contacts hosts on the renter's behalf.
package satellite

import (
	"os"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/modules"
)

var (
	// Nil dependency errors.
	errNilCS      = errors.New("satellite cannot use a nil state")
	errNilTpool   = errors.New("satellite cannot use a nil transaction pool")
	errNilWallet  = errors.New("satellite cannot use a nil wallet")
	errNilGateway = errors.New("satellite cannot use nil gateway")
)

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	// Close safely shuts down the satellite.
	Close() error
}

// A Satellite contains the information necessary to communicate both with
// the renters and with the hosts.
type SatelliteModule struct {
	// Dependencies.
	cs						modules.ConsensusSet
	g							modules.Gateway
	tpool					modules.TransactionPool
	wallet				modules.Wallet

	// Utilities.
	persistDir    string
	port          string
}

// New returns an initialized Satellite.
func New(cs modules.ConsensusSet, g modules.Gateway, tpool modules.TransactionPool, wallet modules.Wallet, satelliteAddr string, persistDir string) (*SatelliteModule, error) {
	// Check that all the dependencies were provided.
	if cs == nil {
		return nil, errNilCS
	}
	if g == nil {
		return nil, errNilGateway
	}
	if tpool == nil {
		return nil, errNilTpool
	}
	if wallet == nil {
		return nil, errNilWallet
	}

	// Create the satellite object.
	s := &SatelliteModule{
		cs:						cs,
		g:						g,
		tpool:				tpool,
		wallet:				wallet,

		persistDir:		persistDir,
		port:					satelliteAddr,
	}

	// Create the perist directory if it does not yet exist.
	err := os.MkdirAll(s.persistDir, 0700)
	if err != nil {
		return nil, err
	}

	return s, nil
}

// Close shuts down the satellite.
func (s *SatelliteModule) Close() error {
	return nil
}
