package client

import (
	"encoding/json"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
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
func (c *Client) PortalAnnouncementGet() (string, uint64, error) {
	url := "/portal/announcement"
	var req api.Announcement
	err := c.get(url, &req)
	if err != nil {
		return "", 0, err
	}
	return req.Text, req.Expires, nil
}

// PortalAnnouncementPost requests the /portal/announcement resource.
func (c *Client) PortalAnnouncementPost(text string, expires uint64) (err error) {
	req := api.Announcement{
		Text:    text,
		Expires: expires,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	err = c.post("/portal/announcement", string(data), nil)
	return
}
