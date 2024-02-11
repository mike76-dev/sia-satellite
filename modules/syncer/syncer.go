package syncer

import (
	"net"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/core/gateway"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/syncer"
	"go.uber.org/zap"
)

// Network bootstrap.
var (
	bootstrapPeers = []string{
		"108.227.62.195:9981",
		"139.162.81.190:9991",
		"144.217.7.188:9981",
		"147.182.196.252:9981",
		"15.235.85.30:9981",
		"167.235.234.84:9981",
		"173.235.144.230:9981",
		"198.98.53.144:7791",
		"199.27.255.169:9981",
		"2.136.192.200:9981",
		"213.159.50.43:9981",
		"24.253.116.61:9981",
		"46.249.226.103:9981",
		"5.165.236.113:9981",
		"5.252.226.131:9981",
		"54.38.120.222:9981",
		"62.210.136.25:9981",
		"63.135.62.123:9981",
		"65.21.93.245:9981",
		"75.165.149.114:9981",
		"77.51.200.125:9981",
		"81.6.58.121:9981",
		"83.194.193.156:9981",
		"84.39.246.63:9981",
		"87.99.166.34:9981",
		"91.214.242.11:9981",
		"93.105.88.181:9981",
		"93.180.191.86:9981",
		"94.130.220.162:9981",
	}
)

// We consider ourselves synced if minSyncedPeers say that we are.
const minSyncedPeers = 5

// A Syncer synchronizes blockchain data with peers.
type Syncer struct {
	s       *syncer.Syncer
	ps      syncer.PeerStore
	l       net.Listener
	log     *zap.Logger
	closeFn func()
}

// Synced returns if the syncer is synced to the blockchain.
func (s *Syncer) Synced() bool {
	var count int
	for _, peer := range s.Peers() {
		if peer.Synced() {
			count++
		}
	}

	return count >= minSyncedPeers
}

// Run spawns goroutines for accepting inbound connections, forming outbound
// connections, and syncing the blockchain from active peers. It blocks until an
// error occurs, upon which all connections are closed and goroutines are
// terminated.
func (s *Syncer) Run() error {
	return s.s.Run()
}

// Connect forms an outbound connection to a peer.
func (s *Syncer) Connect(addr string) (*syncer.Peer, error) {
	return s.s.Connect(addr)
}

// BroadcastHeader broadcasts a header to all peers.
func (s *Syncer) BroadcastHeader(h gateway.BlockHeader) { s.s.BroadcastHeader(h) }

// BroadcastV2Header broadcasts a v2 header to all peers.
func (s *Syncer) BroadcastV2Header(h gateway.V2BlockHeader) { s.s.BroadcastV2Header(h) }

// BroadcastV2BlockOutline broadcasts a v2 block outline to all peers.
func (s *Syncer) BroadcastV2BlockOutline(b gateway.V2BlockOutline) { s.s.BroadcastV2BlockOutline(b) }

// BroadcastTransactionSet broadcasts a transaction set to all peers.
func (s *Syncer) BroadcastTransactionSet(txns []types.Transaction) { s.s.BroadcastTransactionSet(txns) }

// BroadcastV2TransactionSet broadcasts a v2 transaction set to all peers.
func (s *Syncer) BroadcastV2TransactionSet(index types.ChainIndex, txns []types.V2Transaction) {
	s.s.BroadcastV2TransactionSet(index, txns)
}

// Peers returns the set of currently-connected peers.
func (s *Syncer) Peers() []*syncer.Peer {
	return s.s.Peers()
}

// PeerInfo returns the information about the current peers.
func (s *Syncer) PeerInfo() []syncer.PeerInfo {
	info, _ := s.ps.Peers()
	return info
}

// Addr returns the address of the Syncer.
func (s *Syncer) Addr() string {
	return s.s.Addr()
}

// Close shuts down the Syncer.
func (s *Syncer) Close() error {
	err := s.l.Close()
	if err != nil {
		s.log.Sugar().Error("unable to close listener", err)
	}
	s.closeFn()
	return err
}

// New returns a new Syncer.
func New(cm *chain.Manager, addr, dir string) (*Syncer, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, modules.AddContext(err, "unable to start listener")
	}

	syncerAddr := l.Addr().String()
	host, port, _ := net.SplitHostPort(syncerAddr)
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		syncerAddr = net.JoinHostPort("127.0.0.1", port)
	}

	ps, err := NewJSONPeerStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		return nil, modules.AddContext(err, "unable to create store")
	}
	for _, peer := range bootstrapPeers {
		ps.AddPeer(peer)
	}

	_, genesisBlock := chain.Mainnet()
	header := gateway.Header{
		GenesisID:  genesisBlock.ID(),
		UniqueID:   gateway.GenerateUniqueID(),
		NetAddress: syncerAddr,
	}

	logger, closeFn, err := persist.NewFileLogger(filepath.Join(dir, "syncer.log"))
	if err != nil {
		return nil, modules.AddContext(err, "unable to create logger")
	}

	s := syncer.New(l, cm, ps, header, syncer.WithLogger(logger))

	return &Syncer{
		s:       s,
		ps:      ps,
		l:       l,
		log:     logger,
		closeFn: closeFn,
	}, nil
}
