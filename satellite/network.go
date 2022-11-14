package satellite

import (
	"net"
	"time"

	"go.sia.tech/siad/modules"
)

// defaultConnectionDeadline is the default read and write deadline which is set
// on a connection. This ensures it times out if I/O exceeds this deadline.
const defaultConnectionDeadline = 5 * time.Minute

// threadedUpdateHostname periodically runs 'managedLearnHostname', which
// checks if the satellite's hostname has changed.
func (s *Satellite) threadedUpdateHostname(closeChan chan struct{}) {
	defer close(closeChan)
	for {
		s.managedLearnHostname()
		// Wait 30 minutes to check again. We want the satellite to be always
		// accessible by the renters.
		select {
		case <-s.threads.StopChan():
			return
		case <-time.After(time.Minute * 30):
			continue
		}
	}
}

// managedLearnHostname discovers the external IP of the Satellite.
func (s *Satellite) managedLearnHostname() {
	// Fetch the necessary variables.
	s.mu.RLock()
	satPort := s.port
	satAutoAddress := s.autoAddress
	s.mu.RUnlock()

	// Use the gateway to get the external ip.
	hostname, err := s.g.DiscoverAddress(s.threads.StopChan())
	if err != nil {
		s.log.Println("WARN: failed to discover external IP")
		return
	}

	autoAddress := modules.NetAddress(net.JoinHostPort(hostname.String(), satPort))
	if err := autoAddress.IsValid(); err != nil {
		s.log.Printf("WARN: discovered hostname %q is invalid: %v", autoAddress, err)
		return
	}
	if autoAddress == satAutoAddress {
		// Nothing to do - the auto address has not changed.
		return
	}

	s.mu.Lock()
	s.autoAddress = autoAddress
	err = s.saveSync()
	s.mu.Unlock()
	if err != nil {
		s.log.Println(err)
	}

	// TODO inform the renters that the Satellite address has changed.
}

// initNetworking performs actions like port forwarding, and gets the
// satellite established on the network.
func (s *Satellite) initNetworking(address string) (err error) {
	// Create the listener and setup the close procedures.
	s.listener, err = net.Listen("tcp", address)
	if err != nil {
		return err
	}
	// Automatically close the listener when s.threads.Stop() is called.
	threadedListenerClosedChan := make(chan struct{})
	s.threads.OnStop(func() {
		err := s.listener.Close()
		if err != nil {
			s.log.Println("WARN: closing the listener failed:", err)
		}

		// Wait until the threadedListener has returned to continue shutdown.
		<-threadedListenerClosedChan
	})

	// Set the port.
	_, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return err
	}
	s.port = port

	// Non-blocking, perform port forwarding and create the hostname discovery
	// thread.
	go func() {
		// Add this function to the threadgroup, so that the logger will not
		// disappear before port closing can be registered to the threadgroup
		// OnStop functions.
		err := s.threads.Add()
		if err != nil {
			// If this goroutine is not run before shutdown starts, this
			// codeblock is reachable.
			return
		}
		defer s.threads.Done()

		err = s.g.ForwardPort(port)
		if err != nil {
			s.log.Println("ERROR: failed to forward port:", err)
		}

		threadedUpdateHostnameClosedChan := make(chan struct{})
		go s.threadedUpdateHostname(threadedUpdateHostnameClosedChan)
		s.threads.OnStop(func() {
			<-threadedUpdateHostnameClosedChan
		})
	}()

	// Launch the listener.
	go s.threadedListen(threadedListenerClosedChan)

	return nil
}

// threadedListen listens for incoming RPCs and spawns an appropriate handler for each.
func (s *Satellite) threadedListen(closeChan chan struct{}) {
	defer close(closeChan)

	// Receive connections until an error is returned by the listener. When an
	// error is returned, there will be no more calls to receive.
	for {
		// Block until there is a connection to handle.
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		go s.threadedHandleConn(conn)

		// Soft-sleep to ratelimit the number of incoming connections.
		select {
		case <-s.threads.StopChan():
		case <-time.After(rpcRatelimit):
		}
	}
}

// threadedHandleConn handles an incoming connection to the satellite,
// typically an RPC.
func (s *Satellite) threadedHandleConn(conn net.Conn) {
	err := s.threads.Add()
	if err != nil {
		return
	}
	defer s.threads.Done()

	// Close the conn on satellite.Close or when the method terminates, whichever
	// comes first.
	connCloseChan := make(chan struct{})
	defer close(connCloseChan)
	go func() {
		select {
		case <-s.threads.StopChan():
		case <-connCloseChan:
		}
		conn.Close()
	}()

	// Set an initial duration that is generous, but finite. RPCs can extend
	// this if desired.
	err = conn.SetDeadline(time.Now().Add(defaultConnectionDeadline))
	if err != nil {
		s.log.Println("WARN: could not set deadline on connection:", err)
		return
	}

	s.log.Println("INFO: inbound connection from:", conn.RemoteAddr())
}

