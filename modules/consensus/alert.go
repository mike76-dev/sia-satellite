package consensus

import (
	"github.com/mike76-dev/sia-satellite/modules"
)

// Alerts implements the Alerter interface for the consensusset.
func (c *ConsensusSet) Alerts() (crit, err, warn, info []modules.Alert) {
	return
}
