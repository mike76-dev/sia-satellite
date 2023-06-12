package client

import (
	"encoding/json"

	"github.com/mike76-dev/sia-satellite/modules"
)

// PortalCreditsGet requests the /portal/credits resource.
func (c *Client) PortalCreditsGet() (credits modules.CreditData, err error) {
	url := "/portal/credits"
	err = c.get(url, &credits)
	return
}

// PortalCreditsPost requests the /portal/credits resource.
func (c *Client) PortalCreditsPost(credits modules.CreditData) (err error) {
	data, err := json.Marshal(credits)
	if err != nil {
		return err
	}
	err = c.post("/portal/credits", string(data), nil)
	return
}
