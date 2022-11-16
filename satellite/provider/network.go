package provider

import (
	"net"
	"time"

	"go.sia.tech/siad/modules"
)

// defaultConnectionDeadline is the default read and write deadline which is set
// on a connection. This ensures it times out if I/O exceeds this deadline.
const defaultConnectionDeadline = 5 * time.Minute

// rpcRatelimit prevents someone from spamming the provider connections,
// causing it to spin up enough goroutines to crash.
const rpcRatelimit = time.Millisecond * 50

// threadedUpdateHostname periodically runs 'managedLearnHostname', which
// checks if the Satellite's hostname has changed.
func (p *Provider) threadedUpdateHostname(closeChan chan struct{}) {
	defer close(closeChan)
	for {
		p.managedLearnHostname()
		// Wait 30 minutes to check again. We want the Satellite to be always
		// accessible by the renters.
		select {
		case <-p.threads.StopChan():
			return
		case <-time.After(time.Minute * 30):
			continue
		}
	}
}

// managedLearnHostname discovers the external IP of the Satellite.
func (p *Provider) managedLearnHostname() {
	// Fetch the necessary variables.
	p.mu.RLock()
	satPort := p.port
	satAutoAddress := p.autoAddress
	p.mu.RUnlock()

	// Use the gateway to get the external ip.
	hostname, err := p.g.DiscoverAddress(p.threads.StopChan())
	if err != nil {
		p.log.Println("WARN: failed to discover external IP")
		return
	}

	autoAddress := modules.NetAddress(net.JoinHostPort(hostname.String(), satPort))
	if err := autoAddress.IsValid(); err != nil {
		p.log.Printf("WARN: discovered hostname %q is invalid: %v", autoAddress, err)
		return
	}
	if autoAddress == satAutoAddress {
		// Nothing to do - the auto address has not changed.
		return
	}

	p.mu.Lock()
	p.autoAddress = autoAddress
	err = p.saveSync()
	p.mu.Unlock()
	if err != nil {
		p.log.Println(err)
	}

	// TODO inform the renters that the Satellite address has changed.
}

// initNetworking performs actions like port forwarding, and gets the
// Satellite established on the network.
func (p *Provider) initNetworking(address string) (err error) {
	// Create the listener and setup the close procedures.
	p.listener, err = net.Listen("tcp", address)
	if err != nil {
		return err
	}
	// Automatically close the listener when p.threads.Stop() is called.
	threadedListenerClosedChan := make(chan struct{})
	p.threads.OnStop(func() {
		err := p.listener.Close()
		if err != nil {
			p.log.Println("WARN: closing the listener failed:", err)
		}

		// Wait until the threadedListener has returned to continue shutdown.
		<-threadedListenerClosedChan
	})

	// Set the port.
	_, port, err := net.SplitHostPort(p.listener.Addr().String())
	if err != nil {
		return err
	}
	p.port = port

	// Non-blocking, perform port forwarding and create the hostname discovery
	// thread.
	go func() {
		// Add this function to the threadgroup, so that the logger will not
		// disappear before port closing can be registered to the threadgroup
		// OnStop functions.
		err := p.threads.Add()
		if err != nil {
			// If this goroutine is not run before shutdown starts, this
			// codeblock is reachable.
			return
		}
		defer p.threads.Done()

		err = p.g.ForwardPort(port)
		if err != nil {
			p.log.Println("ERROR: failed to forward port:", err)
		}

		threadedUpdateHostnameClosedChan := make(chan struct{})
		go p.threadedUpdateHostname(threadedUpdateHostnameClosedChan)
		p.threads.OnStop(func() {
			<-threadedUpdateHostnameClosedChan
		})
	}()

	// Launch the listener.
	go p.threadedListen(threadedListenerClosedChan)

	return nil
}

// threadedListen listens for incoming RPCs and spawns an appropriate handler for each.
func (p *Provider) threadedListen(closeChan chan struct{}) {
	defer close(closeChan)

	// Receive connections until an error is returned by the listener. When an
	// error is returned, there will be no more calls to receive.
	for {
		// Block until there is a connection to handle.
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}

		go p.threadedHandleConn(conn)

		// Soft-sleep to ratelimit the number of incoming connections.
		select {
		case <-p.threads.StopChan():
		case <-time.After(rpcRatelimit):
		}
	}
}

// threadedHandleConn handles an incoming connection to the provider,
// typically an RPC.
func (p *Provider) threadedHandleConn(conn net.Conn) {
	err := p.threads.Add()
	if err != nil {
		return
	}
	defer p.threads.Done()

	// Close the conn on provider.Close or when the method terminates, whichever
	// comes first.
	connCloseChan := make(chan struct{})
	defer close(connCloseChan)
	go func() {
		select {
		case <-p.threads.StopChan():
		case <-connCloseChan:
		}
		conn.Close()
	}()

	// Set an initial duration that is generous, but finite. RPCs can extend
	// this if desired.
	err = conn.SetDeadline(time.Now().Add(defaultConnectionDeadline))
	if err != nil {
		p.log.Println("WARN: could not set deadline on connection:", err)
		return
	}

	p.log.Println("INFO: inbound connection from:", conn.RemoteAddr())
}

