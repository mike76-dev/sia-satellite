package gateway

import (
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
)

// Constants related to the gateway's alerts.
const (
	// AlertMSGGatewayOffline indicates that the last time the gateway checked
	// the network status it was offline.
	AlertMSGGatewayOffline = "not connected to the internet"
)

const (
	// handshakeUpgradeVersion is the version where the gateway handshake RPC
	// was altered to include additional information transfer.
	handshakeUpgradeVersion = "1.0.0"

	// maxEncodedSessionHeaderSize is the maximum allowed size of an encoded
	// sessionHeader object.
	maxEncodedSessionHeaderSize = 40 + modules.MaxEncodedNetAddressLength

	// maxLocalOutbound is currently set to 3, meaning the gateway will not
	// consider a local node to be an outbound peer if the gateway already has
	// 3 outbound peers. Three is currently needed to handle situations where
	// the gateway is at high risk of connecting to itself (such as a low
	// number of total peers, especially such as in a testing environment).
	// Once the gateway has a proper way to figure out that it's trying to
	// connect to itself, this number can be reduced.
	maxLocalOutboundPeers = 3

	// saveFrequency defines how often the gateway saves its persistence.
	saveFrequency = time.Minute * 2

	// minimumAcceptablePeerVersion is the oldest version for which we accept
	// incoming connections. This version is usually raised if changes to the
	// codebase were made that weren't backwards compatible. This might include
	// changes to the protocol or hardforks.
	minimumAcceptablePeerVersion = "1.5.4"
)

const (
	// fastNodePurgeDelay defines the amount of time that is waited between each
	// iteration of the purge loop when the gateway has enough nodes to be
	// needing to purge quickly.
	fastNodePurgeDelay = 1 * time.Minute

	// healthyNodeListLen defines the number of nodes that the gateway must
	// have in the node list before it will stop asking peers for more nodes.
	healthyNodeListLen = 200

	// maxSharedNodes defines the number of nodes that will be shared between
	// peers when they are expanding their node lists.
	maxSharedNodes = uint64(10)

	// nodeListDelay defines the amount of time that is waited between each
	// iteration of the node list loop.
	nodeListDelay = 5 * time.Second

	// nodePurgeDelay defines the amount of time that is waited between each
	// iteration of the node purge loop.
	nodePurgeDelay = 10 * time.Minute

	// onlineCheckFrequency defines how often the gateway calls 'Online' in
	// threadedOnlineCheck.
	onlineCheckFrequency = 30 * time.Second

	// peerRPCDelay defines the amount of time waited between each RPC accepted
	// from a peer. Without this delay, a peer can force us to spin up thousands
	// of goroutines per second.
	peerRPCDelay = 3 * time.Second

	// pruneNodeListLen defines the number of nodes that the gateway must have
	// to be pruning nodes from the node list.
	pruneNodeListLen = 50

	// quickPruneListLen defines the number of nodes that the gateway must have
	// to be pruning nodes quickly from the node list.
	quickPruneListLen = 250
)

const (
	// The gateway will sleep this long between incoming connections. For
	// attack reasons, the acceptInterval should be longer than the
	// nodeListDelay. Right at startup, a node is vulnerable to being flooded
	// by Sybil attackers. The node's best defense is to wait until it has
	// filled out its nodelist somewhat from the bootstrap nodes. An attacker
	// needs to completely dominate the nodelist and the peerlist to be
	// successful, so just a few honest nodes from requests to the bootstraps
	// should be enough to fend from most attacks.
	acceptInterval = 6 * time.Second

	// acquiringPeersDelay defines the amount of time that is waited between
	// iterations of the peer acquisition loop if the gateway is actively
	// forming new connections with peers.
	acquiringPeersDelay = 5 * time.Second

	// fullyConnectedThreshold defines the number of peers that the gateway can
	// have before it stops accepting inbound connections.
	fullyConnectedThreshold = 128

	// maxConcurrentOutboundPeerRequests defines the maximum number of peer
	// connections that the gateway will try to form concurrently.
	maxConcurrentOutboundPeerRequests = 3

	// noNodesDelay defines the amount of time that is waited between
	// iterations of the peer acquisition loop if the gateway does not have any
	// nodes in the nodelist.
	noNodesDelay = 20 * time.Second

	// unwawntedLocalPeerDelay defines the amount of time that is waited
	// between iterations of the permanentPeerManager if the gateway has at
	// least a few outbound peers, but is not well connected, and the recently
	// selected peer was a local peer. The wait is mostly to prevent the
	// gateway from hogging the CPU in the event that all peers are local
	// peers.
	unwantedLocalPeerDelay = 2 * time.Second

	// wellConnectedDelay defines the amount of time that is waited between
	// iterations of the peer acquisition loop if the gateway is well
	// connected.
	wellConnectedDelay = 5 * time.Minute

	// wellConnectedThreshold is the number of outbound connections at which
	// the gateway will not attempt to make new outbound connections.
	wellConnectedThreshold = 8
)

const (
	// connStdDeadline defines the standard deadline that should be used for
	// all temporary connections to the gateway.
	connStdDeadline = 5 * time.Minute

	// the gateway will abort a connection attempt after this long.
	dialTimeout = 3 * time.Minute

	// rpcStdDeadline defines the standard deadline that should be used for all
	// incoming RPC calls.
	rpcStdDeadline = 5 * time.Minute
)

const (
	// minPeersForIPDiscovery is the minimum number of peer connections we wait
	// for before we try to discover our public ip from them. It is also the
	// minimum number of successful replies we expect from our peers before we
	// accept a result.
	minPeersForIPDiscovery = 5

	// timeoutIPDiscovery is the time after which managedIPFromPeers will fail
	// if the ip couldn't be discovered successfully.
	timeoutIPDiscovery = 5 * time.Minute

	// rediscoverIPIntervalSuccess is the time that has to pass after a
	// successful IP discovery before we rediscover the IP.
	rediscoverIPIntervalSuccess = 3 * time.Hour

	// rediscoverIPIntervalFailure is the time that has to pass after a failed
	// IP discovery before we try again.
	rediscoverIPIntervalFailure = 15 * time.Minute

	// peerDiscoveryRetryInterval is the time we wait when there were not
	// enough peers to determine our public ip address before trying again.
	peerDiscoveryRetryInterval = 10 * time.Second
)
