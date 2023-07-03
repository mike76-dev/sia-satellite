package portal

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the portal.
func (p *Portal) Alerts() (crit, err, warn, info []modules.Alert) {
	return p.staticAlerter.Alerts()
}
