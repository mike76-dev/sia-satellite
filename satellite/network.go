package satellite

import (
	"net"
	"time"
)

// defaultConnectionDeadline is the default read and write deadline which is set
// on a connection. This ensures it times out if I/O exceeds this deadline.
const defaultConnectionDeadline = 5 * time.Minute

// initNetworking performs actions like port forwarding, and gets the
// satellite established on the network.
func (s *SatelliteModule) initNetworking(address string) (err error) {
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

	// Non-blocking, perform port forwarding.
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
	}()

	// Launch the listener.
	go s.threadedListen(threadedListenerClosedChan)

	return nil
}

// threadedListen listens for incoming RPCs and spawns an appropriate handler for each.
func (s *SatelliteModule) threadedListen(closeChan chan struct{}) {
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
func (s *SatelliteModule) threadedHandleConn(conn net.Conn) {
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

