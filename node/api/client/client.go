package client

import (
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/core/consensus"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/syncer"
	"go.sia.tech/jape"
)

// A Client provides methods for interacting with the API server.
type Client struct {
	c jape.Client
}

// DaemonVersion returns the current version of satd.
func (c *Client) DaemonVersion() (resp api.DaemonVersion, err error) {
	err = c.c.GET("/daemon/version", &resp)
	return
}

// SyncerPeers returns the current peers of the syncer.
func (c *Client) SyncerPeers() (resp []syncer.Peer, err error) {
	err = c.c.GET("/syncer/peers", &resp)
	return
}

// SyncerConnect adds the address as a peer of the syncer.
func (c *Client) SyncerConnect(addr string) (err error) {
	err = c.c.POST("/syncer/connect", addr, nil)
	return
}

// SyncerBroadcastBlock broadcasts a block to all peers.
func (c *Client) SyncerBroadcastBlock(b types.Block) (err error) {
	err = c.c.POST("/syncer/broadcast/block", b, nil)
	return
}

// ConsensusNetwork returns the node's network metadata.
func (c *Client) ConsensusNetwork() (resp *consensus.Network, err error) {
	resp = new(consensus.Network)
	err = c.c.GET("/consensus/network", resp)
	return
}

// ConsensusTip returns the current tip index.
func (c *Client) ConsensusTip() (resp api.ConsensusTipResponse, err error) {
	err = c.c.GET("/consensus/tip", &resp)
	return
}

// ConsensusTipState returns the current tip state.
func (c *Client) ConsensusTipState() (resp consensus.State, err error) {
	err = c.c.GET("/consensus/tipstate", &resp)
	return
}

// TxpoolTransactions returns all transactions in the transaction pool.
func (c *Client) TxpoolTransactions() (txns []types.Transaction, v2txns []types.V2Transaction, err error) {
	var resp api.TxpoolTransactionsResponse
	err = c.c.GET("/txpool/transactions", &resp)
	return resp.Transactions, resp.V2Transactions, err
}

// TxpoolFee returns the recommended fee (per weight unit) to ensure a high
// probability of inclusion in the next block.
func (c *Client) TxpoolFee() (resp types.Currency, err error) {
	err = c.c.GET("/txpool/fee", &resp)
	return
}

// NewClient returns a client that communicates with the API server listening
// on the specified address.
func NewClient() *Client {
	return &Client{}
}

// Client returns the underlying jape.Client.
func (c *Client) Client() *jape.Client {
	return &c.c
}
