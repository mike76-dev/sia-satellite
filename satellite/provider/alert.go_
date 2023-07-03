package provider

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the provider.
func (p *Provider) Alerts() (crit, err, warn, info []modules.Alert) {
	return p.staticAlerter.Alerts()
}
