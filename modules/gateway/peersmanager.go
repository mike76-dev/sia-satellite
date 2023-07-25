package gateway

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"lukechampine.com/frand"
)

// managedPeerManagerConnect is a blocking function which tries to connect to
// the input address as a peer.
func (g *Gateway) managedPeerManagerConnect(addr modules.NetAddress) {
	err := g.managedConnect(addr)
	if modules.ContainsError(err, errPeerExists) {
		// This peer is already connected to us. Safety around the
		// outbound peers relates to the fact that we have picked out
		// the outbound peers instead of allow the attacker to pick out
		// the peers for us. Because we have made the selection, it is
		// okay to set the peer as an outbound peer.
		//
		// The nodelist size check ensures that an attacker can't flood
		// a new node with a bunch of inbound requests. Doing so would
		// result in a nodelist that's entirely full of attacker nodes.
		// There's not much we can do about that anyway, but at least
		// we can hold off making attacker nodes 'outbound' peers until
		// our nodelist has had time to fill up naturally.
		g.mu.Lock()
		p, exists := g.peers[addr]
		if exists {
			// Have to check it exists because we released the lock, a
			// race condition could mean that the peer was disconnected
			// before this code block was reached.
			p.Inbound = false
			if n, ok := g.nodes[p.NetAddress]; ok && !n.WasOutboundPeer {
				n.WasOutboundPeer = true
				g.nodes[n.NetAddress] = n
			}
			g.callInitRPCs(p.NetAddress)
		}
		g.mu.Unlock()
	} else if err != nil {
		// Remove the node, but only if there are enough nodes in the node list.
		g.mu.Lock()
		if len(g.nodes) > pruneNodeListLen {
			g.removeNode(addr)
		}
		g.mu.Unlock()
	}
}

// numOutboundPeers returns the number of outbound peers in the gateway.
func (g *Gateway) numOutboundPeers() int {
	n := 0
	for _, p := range g.peers {
		if !p.Inbound {
			n++
		}
	}
	return n
}

// permanentPeerManager tries to keep the Gateway well-connected. As long as
// the Gateway is not well-connected, it tries to connect to random nodes.
func (g *Gateway) permanentPeerManager(closedChan chan struct{}) {
	// Send a signal upon shutdown.
	defer close(closedChan)

	// permanentPeerManager will attempt to connect to peers asynchronously,
	// such that multiple connection attempts can be open at once, but a
	// limited number.
	connectionLimiterChan := make(chan struct{}, maxConcurrentOutboundPeerRequests)

	for {
		// Fetch the set of nodes to try.
		g.mu.RLock()
		nodes := g.buildPeerManagerNodeList()
		g.mu.RUnlock()
		if len(nodes) == 0 {
			if !g.managedSleep(noNodesDelay) {
				return
			}
			continue
		}

		for _, addr := range nodes {
			// Break as soon as we have enough outbound peers.
			g.mu.RLock()
			numOutboundPeers := g.numOutboundPeers()
			isOutboundPeer := g.peers[addr] != nil && !g.peers[addr].Inbound
			g.mu.RUnlock()
			if numOutboundPeers >= wellConnectedThreshold {
				if !g.managedSleep(wellConnectedDelay) {
					return
				}
				break
			}
			if isOutboundPeer {
				// Skip current outbound peers.
				if !g.managedSleep(acquiringPeersDelay) {
					return
				}
				continue
			}

			// We need at least some of our outbound peers to be remote peers. If
			// we already have reached a certain threshold of outbound peers and
			// this peer is a local peer, do not consider it for an outbound peer.
			// Sleep briefly to prevent the gateway from hogging the CPU if all
			// peers are local.
			if numOutboundPeers >= maxLocalOutboundPeers && addr.IsLocal() {
				if !g.managedSleep(unwantedLocalPeerDelay) {
					return
				}
				continue
			}

			// Try connecting to that peer in a goroutine. Do not block unless
			// there are currently 3 or more peer connection attempts open at once.
			// Before spawning the thread, make sure that there is enough room by
			// throwing a struct into the buffered channel.
			connectionLimiterChan <- struct{}{}
			go func(addr modules.NetAddress) {
				// After completion, take the struct out of the channel so that the
				// next thread may proceed.
				defer func() {
					<-connectionLimiterChan
				}()

				if err := g.threads.Add(); err != nil {
					return
				}
				defer g.threads.Done()
				// peerManagerConnect will handle all of its own logging.
				g.managedPeerManagerConnect(addr)
			}(addr)

			// Wait a bit before trying the next peer. The peer connections are
			// non-blocking, so they should be spaced out to avoid spinning up an
			// uncontrolled number of threads and therefore peer connections.
			if !g.managedSleep(acquiringPeersDelay) {
				return
			}
		}
	}
}

// buildPeerManagerNodeList returns the gateway's node list in the order that
// permanentPeerManager should attempt to connect to them.
func (g *Gateway) buildPeerManagerNodeList() []modules.NetAddress {
	// Flatten the node map, inserting in random order.
	nodes := make([]modules.NetAddress, len(g.nodes))
	perm := frand.Perm(len(nodes))
	for _, node := range g.nodes {
		nodes[perm[0]] = node.NetAddress
		perm = perm[1:]
	}

	// swap the outbound nodes to the front of the list
	numOutbound := 0
	for i, node := range nodes {
		if g.nodes[node].WasOutboundPeer {
			nodes[numOutbound], nodes[i] = nodes[i], nodes[numOutbound]
			numOutbound++
		}
	}
	return nodes
}
