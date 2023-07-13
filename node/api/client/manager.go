package client

import (
	"github.com/mike76-dev/sia-satellite/modules"
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

// ManagerContractsGet requests the /manager/contracts resource.
func (c *Client) ManagerContractsGet(key string) (rc api.RenterContracts, err error) {
	url := "/manager/contracts"
	if key != "" {
		url = url + "/" + key
	}
	err = c.get(url, &rc)
	return
}

// ManagerRenterGet requests the /manager/renter resource.
func (c *Client) ManagerRenterGet(key string) (r modules.Renter, err error) {
	url := "/manager/renter/" + key
	err = c.get(url, &r)
	return
}

// ManagerBalanceGet requests the /manager/balance resource.
func (c *Client) ManagerBalanceGet(key string) (ub modules.UserBalance, err error) {
	url := "/manager/balance/" + key
	err = c.get(url, &ub)
	return
}

// ManagerRentersGet requests the /manager/renters resource.
func (c *Client) ManagerRentersGet() (rg api.RentersGET, err error) {
	err = c.get("/manager/renters", &rg)
	return
}
