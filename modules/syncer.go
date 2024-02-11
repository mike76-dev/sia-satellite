package modules

import (
	"go.sia.tech/core/gateway"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/syncer"
)

// A Syncer synchronizes blockchain data with peers.
type Syncer interface {
	// Addr returns the address of the Syncer.
	Addr() string

	// BroadcastHeader broadcasts a header to all peers.
	BroadcastHeader(h gateway.BlockHeader)

	// BroadcastTransactionSet broadcasts a transaction set to all peers.
	BroadcastTransactionSet(txns []types.Transaction)

	// BroadcastV2BlockOutline broadcasts a v2 block outline to all peers.
	BroadcastV2BlockOutline(b gateway.V2BlockOutline)

	// BroadcastV2TransactionSet broadcasts a v2 transaction set to all peers.
	BroadcastV2TransactionSet(index types.ChainIndex, txns []types.V2Transaction)

	// Close shuts down the Syncer.
	Close() error

	// Connect forms an outbound connection to a peer.
	Connect(addr string) (*syncer.Peer, error)

	// PeerInfo returns the information about the current peers.
	PeerInfo() []syncer.PeerInfo

	// Peers returns the set of currently-connected peers.
	Peers() []*syncer.Peer

	// Synced returns if the syncer is synced to the blockchain.
	Synced() bool
}
