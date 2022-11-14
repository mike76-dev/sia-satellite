// interfaces Package defines various interfaces used by the Satellite.
package interfaces

import (
	"go.sia.tech/siad/modules"
)

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	modules.Alerter
	// Close safely shuts down the satellite.
	Close() error
}
