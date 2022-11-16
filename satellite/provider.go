package satellite

import (
	"go.sia.tech/siad/modules"
)

// Provider implements the methods necessary to communicate with the
// renters.
type Provider interface {
	modules.Alerter
	// Close safely shuts down the provider.
	Close() error
}
