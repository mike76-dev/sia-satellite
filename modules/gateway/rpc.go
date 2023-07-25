package gateway

import (
	"encoding/binary"
	"bytes"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// rpcID is an 8-byte signature that is added to all RPCs to tell the gatway
// what to do with the RPC.
type rpcID [8]byte

// String returns a string representation of an rpcID. Empty elements of rpcID
// will be encoded as spaces.
func (id rpcID) String() string {
	for i := range id {
		if id[i] == 0 {
			id[i] = ' '
		}
	}
	return string(id[:])
}

// handlerName truncates a string to 8 bytes. If len(name) < 8, the remaining
// bytes are 0. A handlerName is specified at the beginning of each network
// call, indicating which function should handle the connection.
func handlerName(name string) (id rpcID) {
	copy(id[:], name)
	return
}

// managedRPC calls an RPC on the given address. managedRPC cannot be called on
// an address that the Gateway is not connected to.
func (g *Gateway) managedRPC(addr modules.NetAddress, name string, fn modules.RPCFunc) (err error) {
	g.mu.RLock()
	peer, ok := g.peers[addr]
	g.mu.RUnlock()
	if !ok {
		return errors.New("can't call RPC on unconnected peer " + string(addr))
	}

	conn, err := peer.open()
	if err != nil {
		// Peer probably disconnected without sending a shutdown signal;
		// disconnect from them.
		peer.sess.Close()
		g.mu.Lock()
		delete(g.peers, addr)
		g.mu.Unlock()
		return err
	}
	defer func() {
		err = modules.ComposeErrors(err, conn.Close())
	}()

	// Write header.
	conn.SetDeadline(time.Now().Add(rpcStdDeadline))
	e := types.NewEncoder(conn)
	id := handlerName(name)
	e.WriteUint64(8)
	e.Write(id[:])
	e.Flush()
	conn.SetDeadline(time.Time{})

	// Call fn.
	err = fn(conn)
	return err
}

// RPC calls an RPC on the given address. RPC cannot be called on an address
// that the Gateway is not connected to.
func (g *Gateway) RPC(addr modules.NetAddress, name string, fn modules.RPCFunc) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	return g.managedRPC(addr, name, fn)
}

// RegisterRPC registers an RPCFunc as a handler for a given identifier. To
// call an RPC, use gateway.RPC, supplying the same identifier given to
// RegisterRPC. Identifiers should always use PascalCase. The first 8
// characters of an identifier should be unique, as the identifier used
// internally is truncated to 8 bytes.
func (g *Gateway) RegisterRPC(name string, fn modules.RPCFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.handlers[handlerName(name)]; ok {
		g.log.Println("CRITICAL: RPC already registered: " + name)
	}
	g.handlers[handlerName(name)] = fn
}

// UnregisterRPC unregisters an RPC and removes the corresponding RPCFunc from
// g.handlers. Future calls to the RPC by peers will fail.
func (g *Gateway) UnregisterRPC(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.handlers[handlerName(name)]; !ok {
		g.log.Println("CRITICAL: RPC not registered: " + name)
	}
	delete(g.handlers, handlerName(name))
}

// RegisterConnectCall registers a name and RPCFunc to be called on a peer
// upon connecting.
func (g *Gateway) RegisterConnectCall(name string, fn modules.RPCFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.initRPCs[name]; ok {
		g.log.Println("CRITICAL: ConnectCall already registered: " + name)
	}
	g.initRPCs[name] = fn
}

// UnregisterConnectCall unregisters an on-connect call and removes the
// corresponding RPCFunc from g.initRPCs. Future connections to peers will not
// trigger the RPC to be called on them.
func (g *Gateway) UnregisterConnectCall(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.initRPCs[name]; !ok {
		g.log.Println("CRITICAL: ConnectCall not registered: " + name)
	}
	delete(g.initRPCs, name)
}

// threadedListenPeer listens for new streams on a peer connection and serves them via
// threadedHandleConn.
func (g *Gateway) threadedListenPeer(p *peer) {
	// threadedListenPeer registers to the peerTG instead of the primary thread
	// group because peer connections can be lifetime in length, but can also
	// be short-lived. The fact that they can be lifetime means that they can't
	// call threads.Add as they will block calls to threads.Flush. The fact
	// that they can be short-lived means that threads.OnStop is not a good
	// tool for closing out the threads. Instead, they register to peerTG,
	// which is cleanly closed upon gateway shutdown but will not block any
	// calls to threads.Flush().
	if g.peerTG.Add() != nil {
		return
	}
	defer g.peerTG.Done()

	// Spin up a goroutine to listen for a shutdown signal from both the peer
	// and from the gateway. In the event of either, close the session.
	connClosedChan := make(chan struct{})
	peerCloseChan := make(chan struct{})
	go func() {
		// Signal that the session has been successfully closed, and that this
		// goroutine has terminated.
		defer close(connClosedChan)

		// Listen for a stop signal.
		select {
		case <-g.threads.StopChan():
		case <-peerCloseChan:
		}

		// Close the session and remove p from the peer list.
		p.sess.Close()
		g.mu.Lock()
		delete(g.peers, p.NetAddress)
		g.mu.Unlock()
	}()

	for {
		conn, err := p.accept()
		if err != nil {
			break
		}
		// Set the default deadline on the conn.
		err = conn.SetDeadline(time.Now().Add(rpcStdDeadline))
		if err != nil {
			g.log.Printf("Peer connection (%v) deadline could not be set: %v\n", p.NetAddress, err)
			continue
		}

		// The handler is responsible for closing the connection, though a
		// default deadline has been set.
		go g.threadedHandleConn(conn)
		if !g.managedSleep(peerRPCDelay) {
			break
		}
	}
	// Signal that the goroutine can shutdown.
	close(peerCloseChan)
	// Wait for confirmation that the goroutine has shut down before returning
	// and releasing the threadgroup registration.
	<-connClosedChan
}

// threadedHandleConn reads header data from a connection, then routes it to the
// appropriate handler for further processing.
func (g *Gateway) threadedHandleConn(conn modules.PeerConn) {
	defer func() {
		_ = conn.Close()
	}()
	if g.threads.Add() != nil {
		return
	}
	defer g.threads.Done()

	var id rpcID
	err := conn.SetDeadline(time.Now().Add(rpcStdDeadline))
	if err != nil {
		return
	}
	d := types.NewDecoder(io.LimitedReader{R: conn, N: 16})
	_ = d.ReadUint64()
	d.Read(id[:])
	if err := d.Err(); err != nil {
		return
	}
	// Call registered handler for this ID.
	g.mu.RLock()
	fn, ok := g.handlers[id]
	g.mu.RUnlock()
	if !ok {
		return
	}

	fn(conn)
}

// Broadcast calls an RPC on all of the specified peers. The calls are run in
// parallel. Broadcasts are restricted to "one-way" RPCs, which simply write an
// object and disconnect. This is why Broadcast takes an interface{} instead of
// an RPCFunc.
func (g *Gateway) Broadcast(name string, obj types.EncoderTo, peers []modules.Peer) {
	if g.threads.Add() != nil {
		return
	}
	defer g.threads.Done()

	// Encode once.
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	e.WriteUint64(0) // Placeholder.
	obj.EncodeTo(e)
	e.Flush()
	b := buf.Bytes()
	binary.LittleEndian.PutUint64(b[:8], uint64(len(b) - 8))	

	fn := func(conn modules.PeerConn) error {
		_, err := conn.Write(b)
		return err
	}

	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(addr modules.NetAddress) {
			defer wg.Done()
			err := g.managedRPC(addr, name, fn)
			if err != nil {
				// Try one more time before giving up.
				select {
				case <-time.After(10 * time.Second):
				case <-g.threads.StopChan():
					return
				}
				g.managedRPC(addr, name, fn)
			}
		}(p.NetAddress)
	}
	wg.Wait()
}
