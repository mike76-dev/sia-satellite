package satellite

import (
	"go.sia.tech/siad/modules"
)

// Manager implements the methods necessary to communicate with the
// hosts.
type Manager interface {
	modules.Alerter
	// Close safely shuts down the manager.
	Close() error
}
