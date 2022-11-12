package satellite

import "go.sia.tech/siad/modules"

// Alerts implements the modules.Alerter interface for the satellite.
func (s *SatelliteModule) Alerts() (crit, err, warn, info []modules.Alert) {
	return s.staticAlerter.Alerts()
}
