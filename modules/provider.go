package modules

import (
	"go.sia.tech/core/types"
)

// Provider implements the methods necessary to communicate with the
// renters.
type Provider interface {
	Alerter

	// Close safely shuts down the provider.
	Close() error

	// PublicKey returns the provider's public key.
	PublicKey() types.PublicKey

	// SecretKey returns the provider's secret key.
	SecretKey() types.PrivateKey
}
