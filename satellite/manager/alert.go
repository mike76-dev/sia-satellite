package manager

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the manager.
func (m *Manager) Alerts() (crit, err, warn, info []modules.Alert) {
	return m.staticAlerter.Alerts()
}
