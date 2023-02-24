package client

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
)

// SatelliteContractsGet requests the /satellite/contracts resource.
func (c *Client) SatelliteContractsGet(key string) (rc api.RenterContracts, err error) {
	url := "/satellite/contracts"
	if key != "" {
		url = url + "/" + key
	}
	err = c.get(url, &rc)
	return
}

// SatelliteRenterGet requests the /satellite/renter resource.
func (c *Client) SatelliteRenterGet(key string) (r modules.Renter, err error) {
	url := "/satellite/renter/" + key
	err = c.get(url, &r)
	return
}

// SatelliteBalanceGet requests the /satellite/balance resource.
func (c *Client) SatelliteBalanceGet(key string) (ub modules.UserBalance, err error) {
	url := "/satellite/balance/" + key
	err = c.get(url, &ub)
	return
}

// SatelliteRentersGet requests the /satellite/renters resource.
func (c *Client) SatelliteRentersGet() (rg api.RentersGET, err error) {
	err = c.get("/satellite/renters", &rg)
	return
}
