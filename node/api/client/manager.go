package client

import (
	//"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
)

// ManagerRateGet requests the /manager/rate resource.
func (c *Client) ManagerRateGet(currency string) (er api.ExchangeRate, err error) {
	url := "/manager/rate/" + currency
	err = c.get(url, &er)
	return
}
