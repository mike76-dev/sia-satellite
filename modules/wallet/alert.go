package wallet

import (
	"github.com/mike76-dev/sia-satellite/modules"
)

// Alerts implements the Alerter interface for the wallet.
func (w *Wallet) Alerts() (crit, err, warn, info []modules.Alert) {
	return
}
