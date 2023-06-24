package gateway

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"

	"lukechampine.com/frand"
)

// ProtocolVersion is the current version of the gateway p2p protocol.
const ProtocolVersion = "1.5.4"

var (
	errNoPeers = errors.New("no peers")
	errNilDB   = errors.New("cannot have a nil database as a dependency")
)

// Gateway implements the modules.Gateway interface.
type Gateway struct {
	db       *sql.DB
	listener net.Listener
	myAddr   modules.NetAddress
	port     string

	// handlers are the RPCs that the Gateway can handle.
	// initRPCs are the RPCs that the Gateway calls upon connecting to a peer.
	handlers map[rpcID]modules.RPCFunc
	initRPCs map[string]modules.RPCFunc

	// blocklist are peers that the gateway shouldn't connect to.
	// nodes is the set of all known nodes (i.e. potential peers).
	// peers are the nodes that the gateway is currently connected to.
	// peerTG is a special thread group for tracking peer connections, and will
	// block shutdown until all peer connections have been closed out. The peer
	// connections are put in a separate TG because of their unique
	// requirements - they have the potential to live for the lifetime of the
	// program, but also the potential to close early. Calling threads.OnStop
	// for each peer could create a huge backlog of functions that do nothing
	// (because most of the peers disconnected prior to shutdown). And they
	// can't call threads.Add because they are potentially very long running
	// and would block any threads.Flush() calls. So a second threadgroup is
	// added which handles clean-shutdown for the peers, without blocking
	// threads.Flush() calls.
	blocklist map[string]struct{}
	nodes     map[modules.NetAddress]*node
	peers     map[modules.NetAddress]*peer
	peerTG    siasync.ThreadGroup

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	threads       siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter

	// Unique ID
	staticID gatewayID

	staticUseUPNP bool
}

type gatewayID [8]byte

// addToBlocklist adds addresses to the Gateway's blocklist.
func (g *Gateway) addToBlocklist(addresses []string) error {
	// Add addresses to the blocklist and disconnect from them.
	var err error
	for _, addr := range addresses {
		// Check Gateway peer map for address.
		for peerAddr, peer := range g.peers {
			// If the address corresponds with a peer, close the peer session
			// and remove the peer from the peer map.
			if peerAddr.Host() == addr {
				err = modules.ComposeErrors(err, peer.sess.Close())
				delete(g.peers, peerAddr)
			}
		}
		// Check Gateway node map for address.
		for nodeAddr := range g.nodes {
			// If the address corresponds with a node remove the node from the
			// node map to prevent the node from being re-connected while
			// looking for a replacement peer.
			if nodeAddr.Host() == addr {
				delete(g.nodes, nodeAddr)
			}
		}

		// Add address to the blocklist.
		g.blocklist[addr] = struct{}{}
	}
	return modules.ComposeErrors(err, g.save())
}

// managedSleep will sleep for the given period of time. If the full time
// elapses, 'true' is returned. If the sleep is interrupted for shutdown,
// 'false' is returned.
func (g *Gateway) managedSleep(t time.Duration) (completed bool) {
	select {
	case <-time.After(t):
		return true
	case <-g.threads.StopChan():
		return false
	}
}

// Address returns the NetAddress of the Gateway.
func (g *Gateway) Address() modules.NetAddress {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.myAddr
}

// AddToBlocklist adds addresses to the Gateway's blocklist.
func (g *Gateway) AddToBlocklist(addresses []string) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.addToBlocklist(addresses)
}

// Blocklist returns the Gateway's blocklist.
func (g *Gateway) Blocklist() ([]string, error) {
	if err := g.threads.Add(); err != nil {
		return nil, err
	}
	defer g.threads.Done()
	g.mu.RLock()
	defer g.mu.RUnlock()

	var blocklist []string
	for addr := range g.blocklist {
		blocklist = append(blocklist, addr)
	}
	return blocklist, nil
}

// Close saves the state of the Gateway and stops its listener process.
func (g *Gateway) Close() error {
	if err := g.threads.Stop(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.save()
}

// DiscoverAddress discovers and returns the current public IP address of the
// gateway. Contrary to Address, DiscoverAddress is blocking and might take
// multiple minutes to return. A channel to cancel the discovery can be
// supplied optionally. If nil is supplied, a reasonable timeout will be used
// by default.
func (g *Gateway) DiscoverAddress(cancel <-chan struct{}) (net.IP, error) {
	return g.managedLearnHostname(cancel)
}

// ForwardPort adds a port mapping to the router.
func (g *Gateway) ForwardPort(port string) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	return g.managedForwardPort(port)
}

// RemoveFromBlocklist removes addresses from the Gateway's blocklist.
func (g *Gateway) RemoveFromBlocklist(addresses []string) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	g.mu.Lock()
	defer g.mu.Unlock()

	// Remove addresses from the blocklist
	for _, addr := range addresses {
		delete(g.blocklist, addr)
	}
	return g.save()
}

// SetBlocklist sets the blocklist of the gateway.
func (g *Gateway) SetBlocklist(addresses []string) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	g.mu.Lock()
	defer g.mu.Unlock()

	// Reset the gateway blocklist since we are replacing the list with the new
	// list of peers.
	g.blocklist = make(map[string]struct{})

	// If the length of addresses is 0 we are done, save and return.
	if len(addresses) == 0 {
		return g.save()
	}

	// Add addresses to the blocklist and disconnect from them.
	return g.addToBlocklist(addresses)
}

// New returns an initialized Gateway.
func New(db *sql.DB, addr string, bootstrap bool, useUPNP bool, dir string) (*Gateway, error) {
	// Check for the nil dependency.
	if db == nil {
		return nil, errNilDB
	}

	g := &Gateway{
		db: db,

		handlers: make(map[rpcID]modules.RPCFunc),
		initRPCs: make(map[string]modules.RPCFunc),

		blocklist: make(map[string]struct{}),
		nodes:     make(map[modules.NetAddress]*node),
		peers:     make(map[modules.NetAddress]*peer),

		staticAlerter: modules.NewAlerter("gateway"),
		staticUseUPNP: useUPNP,
	}

	// Set Unique GatewayID.
	frand.Read(g.staticID[:])

	// Create the logger.
	var err error
	g.log, err = persist.NewFileLogger(filepath.Join(dir, logFile))
	if err != nil {
		return nil, err
	}
	// Establish the closing of the logger.
	g.threads.AfterStop(func() {
		if err := g.log.Close(); err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the gateway logger:", err)
		}
	})
	g.log.Println("INFO: gateway created, started logging")

	// Establish that the peerTG must complete shutdown before the primary
	// thread group completes shutdown.
	g.threads.OnStop(func() {
		err = g.peerTG.Stop()
		if err != nil {
			g.log.Println("ERROR: peerTG experienced errors while shutting down:", err)
		}
	})

	// Register RPCs.
	g.RegisterRPC("ShareNodes", g.shareNodes)
	g.RegisterRPC("DiscoverIP", g.discoverPeerIP)
	g.RegisterConnectCall("ShareNodes", g.requestNodes)
	// Establish the de-registration of the RPCs.
	g.threads.OnStop(func() {
		g.UnregisterRPC("ShareNodes")
		g.UnregisterRPC("DiscoverIP")
		g.UnregisterConnectCall("ShareNodes")
	})

	// Load the old node list and gateway persistence.
	if err := g.load(); err != nil {
		return nil, modules.AddContext(err, "unable to load gateway")
	}

	// Spawn the thread to periodically save the gateway.
	go g.threadedSaveLoop()

	// Make sure that the gateway saves after shutdown.
	g.threads.AfterStop(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if err := g.save(); err != nil {
			g.log.Println("ERROR: unable to save gateway:", err)
		}
	})

	// Add the bootstrap peers to the node list.
	if bootstrap {
		for _, addr := range modules.BootstrapPeers {
			err := g.addNode(addr)
			if err != nil && !modules.ContainsError(err, errNodeExists) {
				g.log.Printf("WARN: failed to add the bootstrap node '%v': %v", addr, err)
			}
		}
	}

	// Create the listener which will listen for new connections from peers.
	permanentListenClosedChan := make(chan struct{})
	g.listener, err = net.Listen("tcp", addr)
	if err != nil {
		context := fmt.Sprintf("unable to create gateway tcp listener with address %v", addr)
		return nil, modules.AddContext(err, context)
	}

	// Automatically close the listener when g.threads.Stop() is called.
	g.threads.OnStop(func() {
		err := g.listener.Close()
		if err != nil {
			g.log.Println("WARN: closing the listener failed:", err)
		}
		<-permanentListenClosedChan
	})

	// Set the address and port of the gateway.
	host, port, err := net.SplitHostPort(g.listener.Addr().String())
	g.port = port
	if err != nil {
		context := fmt.Sprintf("unable to split host and port from address %v", g.listener.Addr().String())
		return nil, modules.AddContext(err, context)
	}

	if ip := net.ParseIP(host); ip.IsUnspecified() && ip != nil {
		// If host is unspecified, set a dummy one for now.
		host = "localhost"
	}

	// Set myAddr equal to the address returned by the listener. It will be
	// overwritten by threadedLearnHostname later on.
	g.myAddr = modules.NetAddress(net.JoinHostPort(host, port))

	// Spawn the peer connection listener.
	go g.permanentListen(permanentListenClosedChan)

	// Spawn the peer manager and provide tools for ensuring clean shutdown.
	peerManagerClosedChan := make(chan struct{})
	g.threads.OnStop(func() {
		<-peerManagerClosedChan
	})
	go g.permanentPeerManager(peerManagerClosedChan)

	// Spawn the node manager and provide tools for ensuring clean shutdown.
	nodeManagerClosedChan := make(chan struct{})
	g.threads.OnStop(func() {
		<-nodeManagerClosedChan
	})
	go g.permanentNodeManager(nodeManagerClosedChan)

	// Spawn the node purger and provide tools for ensuring clean shutdown.
	nodePurgerClosedChan := make(chan struct{})
	g.threads.OnStop(func() {
		<-nodePurgerClosedChan
	})
	go g.permanentNodePurger(nodePurgerClosedChan)

	// Spawn threads to take care of port forwarding and hostname discovery.
	go g.threadedForwardPort(g.port)
	go g.threadedLearnHostname()

	// Spawn thread to periodically check if the gateway is online.
	go g.threadedOnlineCheck()

	return g, nil
}

// threadedOnlineCheck periodically calls 'Online' to register the
// GatewayOffline alert.
func (g *Gateway) threadedOnlineCheck() {
	if err := g.threads.Add(); err != nil {
		return
	}
	defer g.threads.Done()
	for {
		select {
		case <-g.threads.StopChan():
			return
		case <-time.After(onlineCheckFrequency):
		}
		_ = g.Online()
	}
}

// enforce that Gateway satisfies the modules.Gateway interface.
var _ modules.Gateway = (*Gateway)(nil)
