package contractor

import "github.com/mike76-dev/sia-satellite/modules"

// Alerts implements the modules.Alerter interface for the contractor. It returns
// all alerts of the contractor.
func (c *Contractor) Alerts() (crit, err, warn, info []modules.Alert) {
	return c.staticAlerter.Alerts()
}
