package modules

import (
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	smodules.Alerter
	// Close safely shuts down the satellite.
	Close() error
	// PublicKey returns the satellite's public key
	PublicKey() types.SiaPublicKey
	// SecretKey returns the satellite's secret key
	SecretKey() crypto.SecretKey
}

// Portal implements the portal server.
type Portal interface {
	smodules.Alerter
	// Close safely shuts down the portal.
	Close() error
}
