package provider

import (
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"

	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

// threadedUpdateHostname periodically runs 'managedLearnHostname', which
// checks if the Satellite's hostname has changed.
func (p *Provider) threadedUpdateHostname(closeChan chan struct{}) {
	defer close(closeChan)
	for {
		p.managedLearnHostname()
		// Wait 30 minutes to check again. We want the Satellite to be always
		// accessible by the renters.
		select {
		case <-p.tg.StopChan():
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
	hostname, err := p.g.DiscoverAddress(p.tg.StopChan())
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
	err = p.save()
	p.mu.Unlock()
	if err != nil {
		p.log.Println("ERROR: couldn't save provider:", err)
	}

	// TODO inform the renters that the Satellite address has changed.
}

// initNetworking performs actions like port forwarding, and gets the
// Satellite established on the network.
func (p *Provider) initNetworking(address, muxAddr string) (err error) {
	// Create the listener and setup the close procedures.
	p.listener, err = net.Listen("tcp", address)
	if err != nil {
		return err
	}

	// Automatically close the listener when p.tg.Stop() is called.
	threadedListenerClosedChan := make(chan struct{})
	p.tg.OnStop(func() {
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
		err := p.tg.Add()
		if err != nil {
			// If this goroutine is not run before shutdown starts, this
			// codeblock is reachable.
			return
		}
		defer p.tg.Done()

		err = p.g.ForwardPort(port)
		if err != nil {
			p.log.Println(err)
		}

		threadedUpdateHostnameClosedChan := make(chan struct{})
		go p.threadedUpdateHostname(threadedUpdateHostnameClosedChan)
		p.tg.OnStop(func() {
			<-threadedUpdateHostnameClosedChan
		})
	}()

	// Launch the listener.
	go p.threadedListen(threadedListenerClosedChan)
	p.log.Println("INFO: listening on port", port)

	// Create the mux and setup the close procedures.
	p.mux, err = net.Listen("tcp", muxAddr)
	if err != nil {
		return err
	}
	_, muxPort, err := net.SplitHostPort(p.mux.Addr().String())
	if err != nil {
		return err
	}

	// Automatically close the mux when p.tg.Stop() is called.
	threadedMuxClosedChan := make(chan struct{})
	p.tg.OnStop(func() {
		err := p.mux.Close()
		if err != nil {
			p.log.Println("WARN: closing the mux failed:", err)
		}

		// Wait until the threadedListener has returned to continue shutdown.
		<-threadedMuxClosedChan
	})

	// Launch the mux.
	go p.threadedListenMux(threadedMuxClosedChan)
	p.log.Println("INFO: mux listening on port", muxPort)

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
		case <-p.tg.StopChan():
		case <-time.After(rpcRatelimit):
		}
	}
}

// threadedListenMux listens for incoming RHP3 requests.
func (p *Provider) threadedListenMux(closeChan chan struct{}) {
	defer close(closeChan)

	// Receive connections until an error is returned by the listener. When an
	// error is returned, there will be no more calls to receive.
	for {
		// Block until there is a connection to handle.
		conn, err := p.mux.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				p.log.Println("WARN: falied to accept connection:", err)
			}
			return
		}

		go func() {
			defer conn.Close()

			// Upgrade the connection to RHP3.
			t, err := rhpv3.NewHostTransport(conn, p.secretKey)
			if err != nil {
				p.log.Println("ERROR: falied to upgrade connection:", err)
				return
			}
			defer t.Close()

			for {
				stream, err := t.AcceptStream()
				if err != nil {
					if !strings.Contains(err.Error(), "peer closed stream gracefully") && !strings.Contains(err.Error(), "peer closed underlying connection") {
						p.log.Println("ERROR: falied to accept stream:", err)
					}
					return
				}

				go p.threadedHandleStream(stream, conn.RemoteAddr().String())
			}
		}()

		// Soft-sleep to ratelimit the number of incoming connections.
		select {
		case <-p.tg.StopChan():
		case <-time.After(rpcRatelimit):
		}
	}
}

// threadedHandleConn handles an incoming connection to the provider,
// typically an RPC.
func (p *Provider) threadedHandleConn(conn net.Conn) {
	err := p.tg.Add()
	if err != nil {
		return
	}
	defer p.tg.Done()

	// Close the conn on provider.Close or when the method terminates, whichever
	// comes first.
	connCloseChan := make(chan struct{})
	defer close(connCloseChan)
	go func() {
		select {
		case <-p.tg.StopChan():
		case <-connCloseChan:
		}
		conn.Close()
	}()

	// Skip if a satellite maintenance is running.
	if p.m.Maintenance() {
		p.log.Println("INFO: closing inbound connection because satellite maintenance is running")
		return
	}

	// Set an initial duration that is generous, but finite. RPCs can extend
	// this if desired.
	err = conn.SetDeadline(time.Now().Add(defaultConnectionDeadline))
	if err != nil {
		p.log.Println("WARN: could not set deadline on connection:", err)
		return
	}

	e := types.NewEncoder(conn)
	d := types.NewDecoder(io.LimitedReader{R: conn, N: 1024})

	// Read renter's half of key exchange.
	var req loopKeyExchangeRequest
	req.DecodeFrom(d)
	if err = d.Err(); err != nil {
		p.log.Println("ERROR: could not read handshake request:", err)
		return
	}
	if req.Specifier != loopEnterSpecifier {
		p.log.Println("ERROR: wrong handshake request specifier")
		return
	}

	// Check for a supported cipher.
	var supportsChaCha bool
	for _, c := range req.Ciphers {
		if c == cipherChaCha20Poly1305 {
			supportsChaCha = true
		}
	}
	if !supportsChaCha {
		(&loopKeyExchangeResponse{Cipher: cipherNoOverlap}).EncodeTo(e)
		p.log.Println("ERROR: no supported ciphers")
		return
	}

	// Generate a session key, sign it, and derive the shared secret.
	xsk, xpk := generateX25519KeyPair()
	h := types.NewHasher()
	h.E.Write(req.PublicKey[:])
	h.E.Write(xpk[:])
	pubkeySig := p.secretKey.SignHash(h.Sum())
	cipherKey := deriveSharedSecret(xsk, req.PublicKey)

	// Send our half of the key exchange.
	resp := loopKeyExchangeResponse{
		Cipher:    cipherChaCha20Poly1305,
		PublicKey: xpk,
	}
	copy(resp.Signature[:], pubkeySig[:])
	resp.EncodeTo(e)
	if err = e.Flush(); err != nil {
		p.log.Println("ERROR: could not send handshake response:", err)
		return
	}

	// Use cipherKey to initialize an AEAD cipher.
	aead, err := chacha20poly1305.New(cipherKey[:])
	if err != nil {
		p.log.Println("ERROR: could not create cipher:", err)
		return
	}

	// Create the session object.
	s := &modules.RPCSession{
		Conn: conn,
		Aead: aead,
	}
	frand.Read(s.Challenge[:])

	// Send encrypted challenge.
	challengeReq := loopChallengeRequest{
		Challenge: s.Challenge,
	}
	if err := s.WriteMessage(&challengeReq); err != nil {
		p.log.Println("ERROR: could not send challenge:", err)
		return
	}

	// Read the request specifier.
	var id types.Specifier
	err = s.ReadMessage(&id, modules.MinMessageSize)
	if err != nil {
		p.log.Println("ERROR: could not read request specifier:", err)
		return
	}

	switch id {
	case requestContractsSpecifier:
		err = p.managedRequestContracts(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCRequestContracts failed")
		}
	case formContractsSpecifier:
		err = p.managedFormContracts(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCFormContracts failed")
		}
	case renewContractsSpecifier:
		err = p.managedRenewContracts(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCRenewContracts failed")
		}
	case updateRevisionSpecifier:
		err = p.managedUpdateRevision(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCUpdateRevision failed")
		}
	case formContractSpecifier:
		err = p.managedFormContract(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCFormContract failed")
		}
	case renewContractSpecifier:
		err = p.managedRenewContract(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCRenewContract failed")
		}
	case getSettingsSpecifier:
		err = p.managedGetSettings(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCGetSettings failed")
		}
	case updateSettingsSpecifier:
		err = p.managedUpdateSettings(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCUpdateSettings failed")
		}
	case updateSlabSpecifier:
		err = p.managedUpdateSlab(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCUpdateSlab failed")
		}
	case requestSlabsSpecifier:
		err = p.managedRequestSlabs(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCRequestSlabs failed")
		}
	case shareContractsSpecifier:
		err = p.managedAcceptContracts(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCShareContracts failed")
		}
	case registerMultipartSpecifier:
		err = p.managedRegisterMultipart(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCCreateMultipart failed")
		}
	case deleteMultipartSpecifier:
		err = p.managedDeleteMultipart(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCAbortMultipart failed")
		}
	default:
		p.log.Println("INFO: inbound connection from:", conn.RemoteAddr()) //TODO
	}
	if err != nil {
		p.log.Printf("ERROR: error with %v: %v\n", conn.RemoteAddr(), err)
	}
}

// threadedHandleStream handles an incoming RHP3 stream.
func (p *Provider) threadedHandleStream(s *rhpv3.Stream, addr string) {
	err := p.tg.Add()
	if err != nil {
		return
	}
	defer p.tg.Done()

	// Close the stream on provider.Close or when the method terminates, whichever
	// comes first.
	streamCloseChan := make(chan struct{})
	defer close(streamCloseChan)
	go func() {
		select {
		case <-p.tg.StopChan():
		case <-streamCloseChan:
		}
		s.Close()
	}()

	// Skip if a satellite maintenance is running.
	if p.m.Maintenance() {
		p.log.Println("INFO: closing inbound stream because satellite maintenance is running")
		return
	}

	// Set an initial duration that is generous, but finite. RPCs can extend
	// this if desired.
	err = s.SetDeadline(time.Now().Add(defaultStreamDeadline))
	if err != nil {
		p.log.Println("ERROR: could not set deadline on stream:", err)
		return
	}

	id, err := s.ReadID()
	if err != nil {
		p.log.Println("ERROR: failed to read RPC ID:", err)
		return
	}

	switch id {
	case saveMetadataSpecifier:
		err = p.managedSaveMetadata(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCSaveMetadata failed")
		}
	case requestMetadataSpecifier:
		err = p.managedRequestMetadata(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCRequestMetadata failed")
		}
	case uploadFileSpecifier:
		err = p.managedReceiveFile(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCUploadFile failed")
		}
	case uploadPartSpecifier:
		err = p.managedReceivePart(s)
		if err != nil {
			err = modules.AddContext(err, "incoming RPCUploadPart failed")
		}
	default:
		p.log.Println("INFO: unknown inbound stream from", addr) //TODO
	}
	if err != nil {
		p.log.Printf("ERROR: error with %v: %v\n", addr, err)
	}
}

// generateX25519KeyPair generates an ephemeral key pair for use in ECDH.
func generateX25519KeyPair() (xsk [32]byte, xpk [32]byte) {
	frand.Read(xsk[:])
	curve25519.ScalarBaseMult(&xpk, &xsk)
	return
}

// deriveSharedSecret derives 32 bytes of entropy from a secret key and public
// key. Derivation is via ScalarMult of the private and public keys, followed
// by a 256-bit unkeyed blake2b hash.
func deriveSharedSecret(xsk [32]byte, xpk [32]byte) (secret [32]byte) {
	dst, _ := curve25519.X25519(xsk[:], xpk[:])
	return blake2b.Sum256(dst)
}
