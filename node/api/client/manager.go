package client

import (
	"github.com/mike76-dev/sia-satellite/node/api"
)

// ManagerRateGet requests the /manager/rate resource.
func (c *Client) ManagerRateGet(currency string) (er api.ExchangeRate, err error) {
	url := "/manager/rate/" + currency
	err = c.get(url, &er)
	return
}

// ManagerAveragesGet requests the /manager/averages resource.
func (c *Client) ManagerAveragesGet(currency string) (ha api.HostAverages, err error) {
	url := "/manager/averages/" + currency
	err = c.get(url, &ha)
	return
}
