package portal

import "github.com/mike76-dev/sia-satellite/modules"

// Alerts implements the modules.Alerter interface for the portal.
func (p *Portal) Alerts() (crit, err, warn, info []modules.Alert) {
	return p.staticAlerter.Alerts()
}
