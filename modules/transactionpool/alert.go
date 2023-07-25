package transactionpool

import "github.com/mike76-dev/sia-satellite/modules"

// Alerts implements the modules.Alerter interface for the transactionpool.
func (tpool *TransactionPool) Alerts() (crit, err, warn, info []modules.Alert) {
	return
}
