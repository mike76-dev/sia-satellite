package provider

import "github.com/mike76-dev/sia-satellite/modules"

// Alerts implements the modules.Alerter interface for the provider.
func (p *Provider) Alerts() (crit, err, warn, info []modules.Alert) {
	return p.staticAlerter.Alerts()
}
