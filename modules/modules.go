package modules

import (
	smodules "go.sia.tech/siad/modules"
)

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	smodules.Alerter
	// Close safely shuts down the satellite.
	Close() error
}

// Portal implements the portal server.
type Portal interface {
	smodules.Alerter
	// Close safely shuts down the portal.
	Close() error
}
