package main

import (
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/internal/syncerutil"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/core/consensus"
	"go.sia.tech/core/gateway"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/syncer"
	"go.uber.org/zap/zapcore"
)

type node struct {
	chain  *chain.Manager
	syncer *syncer.Syncer

	Start func() (stop func())
}

// newNode created a new satd node.
func newNode(config *persist.SatdConfig) *node {
	// Make sure the path is an absolute one.
	dir, err := filepath.Abs(config.Dir)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", config.Dir)
	}

	// Create the state directory if it does not yet exist.
	// This also checks if the provided directory parameter is valid.
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", dir)
	}

	// Extract network parameters.
	var network *consensus.Network
	var genesisBlock types.Block
	var bootstrap []string
	if config.Test {
		network, genesisBlock = chain.TestnetZen()
		bootstrap = syncer.ZenBootstrapPeers
	} else {
		network, genesisBlock = chain.Mainnet()
		bootstrap = syncer.MainnetBootstrapPeers
	}

	// Initialize consensus.
	bdb, err := coreutils.OpenBoltChainDB(filepath.Join(dir, "consensus.db"))
	if err != nil {
		log.Fatalf("Could not initialize consensus: %v\n", err)
	}

	dbstore, tipState, err := chain.NewDBStore(bdb, network, genesisBlock)
	if err != nil {
		log.Fatalf("Could not initialize consensus store: %v\n", err)
	}

	cmLogger, cmCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "cm.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Could not initialize consensus logger: %v\n", err)
	}

	cm := chain.NewManager(dbstore, tipState)
	chain.WithLog(cmLogger)(cm)

	// Initialize syncer.
	l, err := net.Listen("tcp", config.GatewayAddr)
	if err != nil {
		log.Fatalf("Could not start listener: %v\n", err)
	}

	// Peers will reject us if our hostname is empty or unspecified, so use loopback.
	syncerAddr := l.Addr().String()
	host, port, _ := net.SplitHostPort(syncerAddr)
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		syncerAddr = net.JoinHostPort("127.0.0.1", port)
	}

	ps, err := syncerutil.NewJSONPeerStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		log.Fatalf("Could not initialize peer store: %v\n", err)
	}

	for _, peer := range bootstrap {
		if err := ps.AddPeer(peer); err != nil {
			log.Fatalf("Could not add %s to peers: %v\n", peer, err)
		}
	}

	header := gateway.Header{
		GenesisID:  genesisBlock.ID(),
		UniqueID:   gateway.GenerateUniqueID(),
		NetAddress: syncerAddr,
	}

	syncerLogger, syncerCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "syncer.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Could not initialize syncer logger: %v\n", err)
	}

	s := syncer.New(l, cm, ps, header, syncer.WithLogger(syncerLogger))

	return &node{
		chain:  cm,
		syncer: s,
		Start: func() func() {
			ch := make(chan struct{})
			go func() {
				s.Run()
				close(ch)
			}()
			return func() {
				l.Close()
				<-ch
				bdb.Close()
				syncerCloseFn()
				cmCloseFn()
			}
		},
	}
}
