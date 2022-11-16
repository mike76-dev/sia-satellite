package satellite

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the satellite.
// It returns all alerts of the satellite and its submodules.
func (s *Satellite) Alerts() (crit, err, warn, info []modules.Alert) {
	satelliteCrit, satelliteErr, satelliteWarn, satelliteInfo := s.staticAlerter.Alerts()
	providerCrit, providerErr, providerWarn, providerInfo := s.p.Alerts()
	managerCrit, managerErr, managerWarn, managerInfo := s.m.Alerts()
	crit = append(append(satelliteCrit, providerCrit...), managerCrit...)
	err = append(append(satelliteErr, providerErr...), managerErr...)
	warn = append(append(satelliteWarn, providerWarn...), managerWarn...)
	info = append(append(satelliteInfo, providerInfo...), managerInfo...)
	return
}
