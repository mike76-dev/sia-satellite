package client

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"

	"go.sia.tech/core/types"
)

// HostDb requests the /hostdb endpoint's resources.
func (c *Client) HostDb() (hdg api.HostdbGET, err error) {
	err = c.c.GET("/hostdb", &hdg)
	return
}

// HostDbActiveHosts requests the /hostdb/active endpoint's resources.
func (c *Client) HostDbActiveHosts() (hdag api.HostdbHostsGET, err error) {
	err = c.c.GET("/hostdb/active", &hdag)
	return
}

// HostDbAllHosts requests the /hostdb/all endpoint's resources.
func (c *Client) HostDbAllHosts() (hdag api.HostdbHostsGET, err error) {
	err = c.c.GET("/hostdb/all", &hdag)
	return
}

// HostDbFilterMode requests the /hostdb/filtermode GET endpoint.
func (c *Client) HostDbFilterMode() (hdfmg api.HostdbFilterModeGET, err error) {
	err = c.c.GET("/hostdb/filtermode", &hdfmg)
	return
}

// HostDbSetFilterMode requests the /hostdb/filtermode POST endpoint.
func (c *Client) HostDbSetFilterMode(fm modules.FilterMode, hosts []types.PublicKey, netAddresses []string) (err error) {
	filterMode := fm.String()
	hdblp := api.HostdbFilterModePOST{
		FilterMode:   filterMode,
		Hosts:        hosts,
		NetAddresses: netAddresses,
	}

	return c.c.POST("/hostdb/filtermode", &hdblp, nil)
}

// HostDbHost request the /hostdb/host/:publickey endpoint's resources.
func (c *Client) HostDbHost(pk types.PublicKey) (hhg api.HostdbHostGET, err error) {
	err = c.c.GET("/hostdb/host/"+pk.String(), &hhg)
	return
}
