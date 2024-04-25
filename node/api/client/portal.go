package client

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
)

// PortalCredits requests the /portal/credits resource.
func (c *Client) PortalCredits() (credits modules.CreditData, err error) {
	err = c.c.GET("/portal/credits", &credits)
	return
}

// PortalSetCredits requests the /portal/credits resource.
func (c *Client) PortalSetCredits(credits modules.CreditData) (err error) {
	return c.c.POST("/portal/credits", &credits, nil)
}

// PortalAnnouncement requests the /portal/announcement resource.
func (c *Client) PortalAnnouncement() (string, uint64, error) {
	var req api.Announcement
	err := c.c.GET("/portal/announcement", &req)
	if err != nil {
		return "", 0, err
	}
	return req.Text, req.Expires, nil
}

// PortalSetAnnouncement requests the /portal/announcement resource.
func (c *Client) PortalSetAnnouncement(text string, expires uint64) (err error) {
	req := api.Announcement{
		Text:    text,
		Expires: expires,
	}
	return c.c.POST("/portal/announcement", &req, nil)
}
