package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

var (
	errPeerExists       = errors.New("already connected to this peer")
	errPeerRejectedConn = errors.New("peer rejected connection")

	// ErrPeerNotConnected is returned when trying to disconnect from a peer
	// that the gateway is not connected to.
	ErrPeerNotConnected = errors.New("not connected to that node")
)

// insufficientVersionError indicates a peer's version is insufficient.
type insufficientVersionError string

// Error implements the error interface for insufficientVersionError.
func (s insufficientVersionError) Error() string {
	return "unacceptable version: " + string(s)
}

// invalidVersionError indicates a peer's version is not a valid version number.
type invalidVersionError string

// Error implements the error interface for invalidVersionError.
func (s invalidVersionError) Error() string {
	return "invalid version: " + string(s)
}

type peer struct {
	modules.Peer
	sess streamSession
}

// sessionHeader is sent after the initial version exchange. It prevents peers
// on different blockchains from connecting to each other, and prevents the
// gateway from connecting to itself.
type sessionHeader struct {
	GenesisID  types.BlockID
	UniqueID   gatewayID
	NetAddress modules.NetAddress
}

// EncodeTo implements the types.EncoderTo interface.
func (sh *sessionHeader) EncodeTo(e *types.Encoder) {
	var buf bytes.Buffer
	enc := types.NewEncoder(&buf)
	enc.Write(sh.GenesisID[:])
	enc.Write(sh.UniqueID[:])
	enc.WriteString(string(sh.NetAddress))
	enc.Flush()
	b := buf.Bytes()
	e.WriteBytes(b)
}

// DecodeFrom implements the types.DecoderFrom interface.
func (sh *sessionHeader) DecodeFrom(d *types.Decoder) {
	_ = d.ReadUint64() // Length prefix.
	d.Read(sh.GenesisID[:])
	d.Read(sh.UniqueID[:])
	sh.NetAddress = modules.NetAddress(d.ReadString())
}

func (p *peer) open() (modules.PeerConn, error) {
	conn, err := p.sess.Open()
	if err != nil {
		return nil, err
	}
	return &peerConn{conn, p.NetAddress}, nil
}

func (p *peer) accept() (modules.PeerConn, error) {
	conn, err := p.sess.Accept()
	if err != nil {
		return nil, err
	}
	return &peerConn{conn, p.NetAddress}, nil
}

// addPeer adds a peer to the Gateway's peer list, spawns a listener thread to
// handle its requests and increments the remotePeers accordingly.
func (g *Gateway) addPeer(p *peer) {
	g.peers[p.NetAddress] = p
	go g.threadedListenPeer(p)
}

// callInitRPCs calls the rpcs that are registered to be called upon connecting
// to a peer.
func (g *Gateway) callInitRPCs(addr modules.NetAddress) {
	for name, fn := range g.initRPCs {
		go func(name string, fn modules.RPCFunc) {
			if g.threads.Add() != nil {
				return
			}
			defer g.threads.Done()

			g.managedRPC(addr, name, fn)
		}(name, fn)
	}
}

// randomOutboundPeer returns a random outbound peer.
func (g *Gateway) randomOutboundPeer() (modules.NetAddress, error) {
	// Get the list of outbound peers.
	var addrs []modules.NetAddress
	for addr, peer := range g.peers {
		if peer.Inbound {
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return "", errNoPeers
	}

	// Of the remaining options, select one at random.
	return addrs[frand.Intn(len(addrs))], nil
}

// permanentListen handles incoming connection requests. If the connection is
// accepted, the peer will be added to the Gateway's peer list.
func (g *Gateway) permanentListen(closeChan chan struct{}) {
	// Signal that the permanentListen thread has completed upon returning.
	defer close(closeChan)

	for {
		conn, err := g.listener.Accept()
		if err != nil {
			return
		}

		go g.threadedAcceptConn(conn)

		// Sleep after each accept. This limits the rate at which the Gateway
		// will accept new connections. The intent here is to prevent new
		// incoming connections from kicking out old ones before they have a
		// chance to request additional nodes.
		select {
		case <-time.After(acceptInterval):
		case <-g.threads.StopChan():
			return
		}
	}
}

// threadedAcceptConn adds a connecting node as a peer.
func (g *Gateway) threadedAcceptConn(conn net.Conn) {
	if g.threads.Add() != nil {
		conn.Close()
		return
	}
	defer g.threads.Done()
	conn.SetDeadline(time.Now().Add(connStdDeadline))

	addr := modules.NetAddress(conn.RemoteAddr().String())

	g.mu.RLock()
	_, exists := g.blocklist[addr.Host()]
	g.mu.RUnlock()
	if exists {
		conn.Close()
		return
	}
	remoteVersion, err := acceptVersionHandshake(conn, ProtocolVersion)
	if err != nil {
		conn.Close()
		return
	}

	if err = acceptableVersion(remoteVersion); err == nil {
		err = g.managedAcceptConnPeer(conn, remoteVersion)
	}
	if err != nil {
		conn.Close()
		return
	}

	// Handshake successful, remove the deadline.
	conn.SetDeadline(time.Time{})
}

// acceptableSessionHeader returns an error if remoteHeader indicates a peer
// that should not be connected to.
func acceptableSessionHeader(ourHeader, remoteHeader sessionHeader, remoteAddr string) error {
	if remoteHeader.GenesisID != ourHeader.GenesisID {
		return errPeerGenesisID
	} else if remoteHeader.UniqueID == ourHeader.UniqueID {
		return errOurAddress
	} else if err := remoteHeader.NetAddress.IsStdValid(); err != nil {
		return modules.AddContext(err, "invalid remote address")
	}
	return nil
}

// managedAcceptConnPeer accepts connection requests from peers >= v1.3.1.
// The requesting peer is added as a node and a peer. The peer is only added if
// a nil error is returned.
func (g *Gateway) managedAcceptConnPeer(conn net.Conn, remoteVersion string) error {
	// Perform header handshake.
	g.mu.RLock()
	ourHeader := sessionHeader{
		GenesisID:  modules.GenesisID,
		UniqueID:   g.staticID,
		NetAddress: g.myAddr,
	}
	g.mu.RUnlock()

	remoteHeader, err := exchangeRemoteHeader(conn, ourHeader)
	if err != nil {
		return err
	}
	if err := exchangeOurHeader(conn, ourHeader); err != nil {
		return err
	}

	// Get the remote address on which the connecting peer is listening on.
	// This means we need to combine the incoming connections ip address with
	// the announced open port of the peer.
	remoteIP := modules.NetAddress(conn.RemoteAddr().String()).Host()
	remotePort := remoteHeader.NetAddress.Port()
	remoteAddr := modules.NetAddress(net.JoinHostPort(remoteIP, remotePort))

	// Accept the peer.
	peer := &peer{
		Peer: modules.Peer{
			Inbound: true,
			// NOTE: local may be true even if the supplied NetAddress is not
			// actually reachable.
			Local: remoteAddr.IsLocal(),
			// Ignoring claimed IP address (which should be == to the socket address)
			// by the host but keeping note of the port number so we can call back.
			NetAddress: remoteAddr,
			Version:    remoteVersion,
		},
		sess: newServerStream(conn, remoteVersion),
	}
	g.mu.Lock()
	g.acceptPeer(peer)
	g.mu.Unlock()

	// Attempt to ping the supplied address. If successful, we will add
	// remoteHeader.NetAddress to our node list after accepting the peer. We
	// do this in a goroutine so that we can begin communicating with the peer
	// immediately.
	go func() {
		err := g.staticPingNode(remoteAddr)
		if err == nil {
			g.mu.Lock()
			g.addNode(remoteAddr)
			g.mu.Unlock()
		}
	}()

	return nil
}

// acceptPeer makes room for the peer if necessary by kicking out existing
// peers, then adds the peer to the peer list.
func (g *Gateway) acceptPeer(p *peer) {
	// If we are not fully connected, add the peer without kicking any out.
	if len(g.peers) < fullyConnectedThreshold {
		g.addPeer(p)
		return
	}

	// Select a peer to kick. Outbound peers and local peers are not
	// available to be kicked.
	var addrs, preferredAddrs []modules.NetAddress
	for addr, peer := range g.peers {
		// Do not kick outbound peers or local peers.
		if !peer.Inbound || peer.Local {
			continue
		}

		// Prefer kicking a peer with the same hostname.
		if addr.Host() == p.NetAddress.Host() {
			preferredAddrs = append(preferredAddrs, addr)
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(preferredAddrs) > 0 {
		// If there are preferredAddrs we choose randomly from them.
		addrs = preferredAddrs
	}
	if len(addrs) == 0 {
		// There is nobody suitable to kick, therefore do not kick anyone.
		g.addPeer(p)
		return
	}

	// Of the remaining options, select one at random.
	kick := addrs[frand.Intn(len(addrs))]

	g.peers[kick].sess.Close()
	delete(g.peers, kick)
	g.log.Printf("INFO: disconnected from %v to make room for %v\n", kick, p.NetAddress)
	g.addPeer(p)
}

// acceptableVersion returns an error if the version is unacceptable.
func acceptableVersion(version string) error {
	if !build.IsVersion(version) {
		return invalidVersionError(version)
	}
	if build.VersionCmp(version, minimumAcceptablePeerVersion) < 0 {
		return insufficientVersionError(version)
	}
	return nil
}

// connectVersionHandshake performs the version handshake and should be called
// on the side making the connection request. The remote version is only
// returned if err == nil.
func connectVersionHandshake(conn net.Conn, version string) (remoteVersion string, err error) {
	// Send our version.
	e := types.NewEncoder(conn)
	modules.WriteString(version, e)

	// Read remote version.
	d := types.NewDecoder(io.LimitedReader{R: conn, N: build.MaxEncodedVersionLength})
	remoteVersion = modules.ReadString(d)
	if err := d.Err(); err != nil {
		return "", modules.AddContext(err, "failed to read remote version")
	}

	// Check that their version is acceptable.
	if remoteVersion == "reject" {
		return "", errPeerRejectedConn
	}
	if err := acceptableVersion(remoteVersion); err != nil {
		return "", err
	}

	return remoteVersion, nil
}

// acceptVersionHandshake performs the version handshake and should be
// called on the side accepting a connection request. The remote version is
// only returned if err == nil.
func acceptVersionHandshake(conn net.Conn, version string) (remoteVersion string, err error) {
	// Read remote version.
	d := types.NewDecoder(io.LimitedReader{R: conn, N: build.MaxEncodedVersionLength})
	remoteVersion = modules.ReadString(d)
	if err := d.Err(); err != nil {
		return "", modules.AddContext(err, "failed to read remote version")
	}

	// Check that their version is acceptable.
	e := types.NewEncoder(conn)
	if err := acceptableVersion(remoteVersion); err != nil {
		modules.WriteString("reject", e)
		return "", err
	}

	// Send our version.
	modules.WriteString(version, e)

	return remoteVersion, nil
}

// exchangeOurHeader writes ourHeader and reads the remote's error response.
func exchangeOurHeader(conn net.Conn, ourHeader sessionHeader) error {
	// Send our header.
	e := types.NewEncoder(conn)
	ourHeader.EncodeTo(e)
	e.Flush()

	// Read remote response.
	d := types.NewDecoder(io.LimitedReader{R: conn, N: 100})
	response := modules.ReadString(d)
	if err := d.Err(); err != nil {
		return modules.AddContext(err, "failed to read header acceptance")
	} else if response == modules.StopResponse {
		return errors.New("peer did not want a connection")
	} else if response != modules.AcceptResponse {
		return fmt.Errorf("peer rejected our header: %v", response)
	}

	return nil
}

// exchangeRemoteHeader reads the remote header and writes an error response.
func exchangeRemoteHeader(conn net.Conn, ourHeader sessionHeader) (sessionHeader, error) {
	// Read remote header.
	var remoteHeader sessionHeader
	d := types.NewDecoder(io.LimitedReader{R: conn, N: maxEncodedSessionHeaderSize})
	remoteHeader.DecodeFrom(d)
	if err := d.Err(); err != nil {
		return sessionHeader{}, modules.AddContext(err, "failed to read remote header")
	}

	// Validate remote header and write acceptance or rejection.
	e := types.NewEncoder(conn)
	err := acceptableSessionHeader(ourHeader, remoteHeader, conn.RemoteAddr().String())
	if err != nil {
		modules.WriteString(err.Error(), e)
		return sessionHeader{}, modules.AddContext(err, "peer's header was not acceptable")
	}
	modules.WriteString(modules.AcceptResponse, e)

	return remoteHeader, nil
}

// managedConnectPeer connects to peers >= v1.3.1. The peer is added as a
// node and a peer. The peer is only added if a nil error is returned.
func (g *Gateway) managedConnectPeer(conn net.Conn, remoteVersion string, remoteAddr modules.NetAddress) error {
	// Perform header handshake.
	g.mu.RLock()
	ourHeader := sessionHeader{
		GenesisID:  modules.GenesisID,
		UniqueID:   g.staticID,
		NetAddress: g.myAddr,
	}
	g.mu.RUnlock()

	if err := exchangeOurHeader(conn, ourHeader); err != nil {
		return err
	} else if _, err := exchangeRemoteHeader(conn, ourHeader); err != nil {
		return err
	}
	return nil
}

// managedConnect establishes a persistent connection to a peer, and adds it to
// the Gateway's peer list.
func (g *Gateway) managedConnect(addr modules.NetAddress) error {
	// Perform verification on the input address.
	g.mu.RLock()
	gaddr := g.myAddr
	g.mu.RUnlock()
	if addr == gaddr {
		err := errors.New("can't connect to our own address")
		return err
	}
	if err := addr.IsStdValid(); err != nil {
		err := errors.New("can't connect to invalid address")
		return err
	}
	if net.ParseIP(addr.Host()) == nil {
		err := errors.New("address must be an IP address")
		return err
	}
	if _, exists := g.blocklist[addr.Host()]; exists {
		err := errors.New("can't connect to blocklisted address")
		return err
	}
	g.mu.RLock()
	_, exists := g.peers[addr]
	g.mu.RUnlock()
	if exists {
		return errPeerExists
	}

	// Dial the peer and perform peer initialization.
	conn, err := g.staticDial(addr)
	if err != nil {
		return err
	}

	// Perform peer initialization.
	remoteVersion, err := connectVersionHandshake(conn, ProtocolVersion)
	if err != nil {
		conn.Close()
		return err
	}

	if err = acceptableVersion(remoteVersion); err == nil {
		err = g.managedConnectPeer(conn, remoteVersion, addr)
	}
	if err != nil {
		conn.Close()
		return err
	}

	// Connection successful, clear the timeout as to maintain a persistent
	// connection to this peer.
	conn.SetDeadline(time.Time{})

	// Add the peer.
	g.mu.Lock()
	defer g.mu.Unlock()

	g.addPeer(&peer{
		Peer: modules.Peer{
			Inbound:    false,
			Local:      addr.IsLocal(),
			NetAddress: addr,
			Version:    remoteVersion,
		},
		sess: newClientStream(conn, remoteVersion),
	})
	g.addNode(addr)
	g.nodes[addr].WasOutboundPeer = true

	if err := g.save(); err != nil {
		g.log.Println("ERROR: unable to save new outbound peer to gateway:", err)
	}

	// call initRPCs
	g.callInitRPCs(addr)

	return nil
}

// Connect establishes a persistent connection to a peer, and adds it to the
// Gateway's peer list.
func (g *Gateway) Connect(addr modules.NetAddress) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	return g.managedConnect(addr)
}

// Disconnect terminates a connection to a peer and removes it from the
// Gateway's peer list.
func (g *Gateway) Disconnect(addr modules.NetAddress) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()

	g.mu.RLock()
	p, exists := g.peers[addr]
	g.mu.RUnlock()
	if !exists {
		err := ErrPeerNotConnected
		return err
	}

	p.sess.Close()
	g.mu.Lock()
	// Peer is removed from the peer list as well as the node list, to prevent
	// the node from being re-connected while looking for a replacement peer.
	delete(g.peers, addr)
	delete(g.nodes, addr)
	g.mu.Unlock()

	g.log.Println("INFO: disconnected from peer", addr)
	return nil
}

// ConnectManual is a wrapper for the Connect function. It is specifically used
// if a user wants to connect to a node manually. This also removes the node
// from the blocklist.
func (g *Gateway) ConnectManual(addr modules.NetAddress) error {
	g.mu.Lock()
	var err error
	if _, exists := g.blocklist[addr.Host()]; exists {
		delete(g.blocklist, addr.Host())
		err = g.save()
	}
	g.mu.Unlock()
	return modules.ComposeErrors(err, g.Connect(addr))
}

// DisconnectManual is a wrapper for the Disconnect function. It is
// specifically used if a user wants to connect to a node manually. This also
// adds the node to the blocklist.
func (g *Gateway) DisconnectManual(addr modules.NetAddress) error {
	err := g.Disconnect(addr)
	if err == nil {
		g.mu.Lock()
		g.blocklist[addr.Host()] = struct{}{}
		err = g.save()
		g.mu.Unlock()
	}
	return err
}

// Online returns true if the node is connected to the internet.
func (g *Gateway) Online() (online bool) {
	defer func() {
		if online {
			g.staticAlerter.UnregisterAlert(modules.AlertIDGatewayOffline)
		} else {
			g.staticAlerter.RegisterAlert(modules.AlertIDGatewayOffline, AlertMSGGatewayOffline, "", modules.SeverityWarning)
		}
	}()

	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, p := range g.peers {
		if !p.Local {
			return true
		}
	}
	return false
}

// Peers returns the addresses currently connected to the Gateway.
func (g *Gateway) Peers() []modules.Peer {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var peers []modules.Peer
	for _, p := range g.peers {
		peers = append(peers, p.Peer)
	}
	return peers
}
