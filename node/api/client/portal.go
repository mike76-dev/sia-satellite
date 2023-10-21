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

// PortalAnnouncementGet requests the /portal/announcement resource.
func (c *Client) PortalAnnouncementGet() (string, error) {
	url := "/portal/announcement"
	var req struct {
		Text string `json:"text"`
	}
	err := c.get(url, &req)
	if err != nil {
		return "", err
	}
	return req.Text, nil
}

// PortalAnnouncementPost requests the /portal/announcement resource.
func (c *Client) PortalAnnouncementPost(text string) (err error) {
	var req struct {
		Text string `json:"text"`
	}
	req.Text = text
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	err = c.post("/portal/announcement", string(data), nil)
	return
}
