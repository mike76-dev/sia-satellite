package client

import (
	"fmt"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/core/types"
)

// WalletAddress returns a newly-generated address.
func (c *Client) WalletAddress() (addr types.Address, err error) {
	err = c.c.GET("/wallet/address", &addr)
	return
}

// WalletBalance returns the current wallet balance.
func (c *Client) WalletBalance() (resp api.WalletBalanceResponse, err error) {
	err = c.c.GET("/wallet/balance", &resp)
	return
}

// WalletPoolTransactions returns all txpool transactions relevant to the wallet.
func (c *Client) WalletPoolTransactions() (resp []modules.PoolTransaction, err error) {
	err = c.c.GET("/wallet/txpool", &resp)
	return
}

// WalletOutputs returns the set of unspent outputs controlled by the wallet.
func (c *Client) WalletOutputs() (sc []types.SiacoinElement, sf []types.SiafundElement, err error) {
	var resp api.WalletOutputsResponse
	err = c.c.GET("/wallet/outputs", &resp)
	return resp.SiacoinOutputs, resp.SiafundOutputs, err
}

// WalletAddresses returns the addresses controlled by the wallet.
func (c *Client) WalletAddresses() (addrs []types.Address, err error) {
	err = c.c.GET("/wallet/addresses", &addrs)
	return
}

// WalletAddWatch adds the specified watch address.
func (c *Client) WalletAddWatch(addr types.Address) (err error) {
	err = c.c.PUT(fmt.Sprintf("/wallet/watch/%v", addr), nil)
	return
}

// WalletRemoveWatch removes the specified watch address.
func (c *Client) WalletRemoveWatch(addr types.Address) (err error) {
	err = c.c.DELETE(fmt.Sprintf("/wallet/watch/%v", addr))
	return
}

// WalletWatchedAddresses returns a list of the watched addresses.
func (c *Client) WalletWatchedAddresses() (addrs []types.Address, err error) {
	err = c.c.GET("/wallet/watch", &addrs)
	return
}

// WalletSendSiacoins sends a specified amount of SC to the specified address.
func (c *Client) WalletSendSiacoins(amount types.Currency, dest types.Address) (err error) {
	err = c.c.POST("/wallet/send", api.WalletSendRequest{
		Amount:      amount,
		Destination: dest,
	}, nil)
	return
}
