package manager

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the manager.
func (m *Manager) Alerts() (crit, err, warn, info []modules.Alert) {
	managerCrit, managerErr, managerWarn, managerInfo := m.staticAlerter.Alerts()
	contractorCrit, contractorErr, contractorWarn, contractorInfo := m.hostContractor.Alerts()
	hostdbCrit, hostdbErr, hostdbWarn, hostdbInfo := m.hostDB.Alerts()
	crit = append(append(managerCrit, contractorCrit...), hostdbCrit...)
	err = append(append(managerErr, contractorErr...), hostdbErr...)
	warn = append(append(managerWarn, contractorWarn...), hostdbWarn...)
	info = append(append(managerInfo, contractorInfo...), hostdbInfo...)
	return
}
