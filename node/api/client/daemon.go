package client

import (
	"github.com/mike76-dev/sia-satellite/node/api"
)

// DaemonAlertsGet requests the /daemon/alerts resource.
func (c *Client) DaemonAlertsGet() (dag api.DaemonAlertsGet, err error) {
	err = c.get("/daemon/alerts", &dag)
	return
}

// DaemonVersionGet requests the /daemon/version resource.
func (c *Client) DaemonVersionGet() (dvg api.DaemonVersionGet, err error) {
	err = c.get("/daemon/version", &dvg)
	return
}

// DaemonStopGet stops the daemon using the /daemon/stop endpoint.
func (c *Client) DaemonStopGet() (err error) {
	err = c.get("/daemon/stop", nil)
	return
}
