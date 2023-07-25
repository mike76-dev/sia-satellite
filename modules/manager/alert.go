package manager

import "github.com/mike76-dev/sia-satellite/modules"

// Alerts implements the modules.Alerter interface for the manager.
func (m *Manager) Alerts() (crit, err, warn, info []modules.Alert) {
	crit, err, warn, info = m.staticAlerter.Alerts()
	contractorCrit, contractorErr, contractorWarn, contractorInfo := m.hostContractor.Alerts()
	hostdbCrit, hostdbErr, hostdbWarn, hostdbInfo := m.hostDB.Alerts()
	crit = append(append(crit, contractorCrit...), hostdbCrit...)
	err = append(append(err, contractorErr...), hostdbErr...)
	warn = append(append(warn, contractorWarn...), hostdbWarn...)
	info = append(append(info, contractorInfo...), hostdbInfo...)
	return
}
