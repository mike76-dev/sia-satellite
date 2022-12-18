package manager

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the manager.
func (m *Manager) Alerts() (crit, err, warn, info []modules.Alert) {
	managerCrit, managerErr, managerWarn, managerInfo := m.staticAlerter.Alerts()
	hostdbCrit, hostdbErr, hostdbWarn, hostdbInfo := m.hostDB.Alerts()
	crit = append(managerCrit, hostdbCrit...)
	err = append(managerErr, hostdbErr...)
	warn = append(managerWarn, hostdbWarn...)
	info = append(managerInfo, hostdbInfo...)
	return
}
