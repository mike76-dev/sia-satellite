package client

import (
	"encoding/json"

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

// ManagerPreferencesGet requests the /manager/preferences resource.
func (c *Client) ManagerPreferencesGet() (ep api.EmailPreferences, err error) {
	err = c.get("/manager/preferences", &ep)
	return
}

// ManagerPreferencesPost uses the /manager/preferences resource to change
// the email preferences.
func (c *Client) ManagerPreferencesPost(ep api.EmailPreferences) error {
	json, err := json.Marshal(ep)
	if err != nil {
		return err
	}
	err = c.post("/manager/preferences", string(json), nil)
	return err
}

// ManagerPricesGet requests the /manager/prices resource.
func (c *Client) ManagerPricesGet() (prices modules.Pricing, err error) {
	err = c.get("/manager/prices", &prices)
	return
}

// ManagerPricesPost uses the /manager/prices resource to change
// the current prices.
func (c *Client) ManagerPricesPost(prices modules.Pricing) error {
	json, err := json.Marshal(prices)
	if err != nil {
		return err
	}
	err = c.post("/manager/prices", string(json), nil)
	return err
}
