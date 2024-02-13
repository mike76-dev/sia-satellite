package client

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
)

// ManagerAverages requests the /manager/averages resource.
func (c *Client) ManagerAverages(currency string) (ha api.HostAverages, err error) {
	err = c.c.GET("/manager/averages/"+currency, &ha)
	return
}

// ManagerContracts requests the /manager/contracts resource.
func (c *Client) ManagerContracts(key string) (rc api.RenterContracts, err error) {
	err = c.c.GET("/manager/contracts/"+key, &rc)
	return
}

// ManagerRenter requests the /manager/renter resource.
func (c *Client) ManagerRenter(key string) (r modules.Renter, err error) {
	err = c.c.GET("/manager/renter/"+key, &r)
	return
}

// ManagerBalance requests the /manager/balance resource.
func (c *Client) ManagerBalance(key string) (ub modules.UserBalance, err error) {
	err = c.c.GET("/manager/balance/"+key, &ub)
	return
}

// ManagerRenters requests the /manager/renters resource.
func (c *Client) ManagerRenters() (rg api.RentersGET, err error) {
	err = c.c.GET("/manager/renters", &rg)
	return
}

// ManagerPreferences requests the /manager/preferences resource.
func (c *Client) ManagerPreferences() (ep api.EmailPreferences, err error) {
	err = c.c.GET("/manager/preferences", &ep)
	return
}

// ManagerUpdatePreferences uses the /manager/preferences resource to change
// the email preferences.
func (c *Client) ManagerUpdatePreferences(ep api.EmailPreferences) error {
	return c.c.POST("/manager/preferences", &ep, nil)
}

// ManagerPrices requests the /manager/prices resource.
func (c *Client) ManagerPrices() (prices modules.Pricing, err error) {
	err = c.c.GET("/manager/prices", &prices)
	return
}

// ManagerUpdatePrices uses the /manager/prices resource to change
// the current prices.
func (c *Client) ManagerUpdatePrices(prices modules.Pricing) error {
	return c.c.POST("/manager/prices", &prices, nil)
}

// ManagerMaintenance requests the /manager/maintenance resource.
func (c *Client) ManagerMaintenance() (maintenance bool, err error) {
	var req struct {
		Maintenance bool `json:"maintenance"`
	}
	err = c.c.GET("/manager/maintenance", &req)
	if err != nil {
		return false, err
	}
	return req.Maintenance, nil
}

// ManagerSetMaintenance uses the /manager/maintenance resource to set
// or clear the maintenance flag.
func (c *Client) ManagerSetMaintenance(start bool) error {
	req := struct {
		Start bool `json:"start"`
	}{Start: start}
	return c.c.POST("/manager/maintenance", &req, nil)
}
