package gateway

import "github.com/mike76-dev/sia-satellite/modules"

// Alerts implements the modules.Alerter interface for the gateway.
func (g *Gateway) Alerts() (crit, err, warn, info []modules.Alert) {
	return g.staticAlerter.Alerts()
}
