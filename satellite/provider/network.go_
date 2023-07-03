package provider

import (
	"io"
	"net"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"

	"golang.org/x/crypto/chacha20poly1305"

	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// defaultConnectionDeadline is the default read and write deadline which is set
// on a connection. This ensures it times out if I/O exceeds this deadline.
const defaultConnectionDeadline = 5 * time.Minute

// rpcRatelimit prevents someone from spamming the provider connections,
// causing it to spin up enough goroutines to crash.
const rpcRatelimit = time.Millisecond * 50

// requestContractsSpecifier is used when a renter requests the list of their
// active contracts.
var requestContractsSpecifier = types.NewSpecifier("RequestContracts")

// formContractsSpecifier is used when a renter requests to form a number of
// contracts on their behalf.
var formContractsSpecifier = types.NewSpecifier("FormContracts")

// renewContractsSpecifier is used when a renter requests to renew a set of
// contracts.
var renewContractsSpecifier = types.NewSpecifier("RenewContracts")

// updateRevisionSpecifier is used when a renter submits a new revision.
var updateRevisionSpecifier = types.NewSpecifier("UpdateRevision")

// formContractSpecifier is used to form a single contract using the new
// Renter-Satellite protocol.
var formContractSpecifier = types.NewSpecifier("FormContract")

// renewContractSpecifier is used to renew a contract using the new
// Renter-Satellite protocol.
var renewContractSpecifier = types.NewSpecifier("RenewContract")

// getSettingsSpecifier is used to request the renter's opt-in settings.
var getSettingsSpecifier = types.NewSpecifier("GetSettings")

// updateSettingsSpecifier is used to update the renter's opt-in settings.
var updateSettingsSpecifier = types.NewSpecifier("UpdateSettings")

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

	autoAddress := smodules.NetAddress(net.JoinHostPort(hostname.String(), satPort))
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

	e := core.NewEncoder(conn)
	d := core.NewDecoder(io.LimitedReader{R: conn, N: 1024})

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
	xsk, xpk := crypto.GenerateX25519KeyPair()
	pubkeySig := crypto.SignHash(crypto.HashAll(req.PublicKey, xpk), p.satellite.SecretKey())
	cipherKey := crypto.DeriveSharedSecret(xsk, req.PublicKey)

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
	fastrand.Read(s.Challenge[:])

	// Send encrypted challenge.
	challengeReq := smodules.LoopChallengeRequest{
		Challenge: s.Challenge,
	}
	if err := smodules.WriteRPCMessage(conn, aead, challengeReq); err != nil {
		p.log.Println("ERROR: could not send challenge:", err)
		return
	}

	// Read the request specifier.
	id, err := smodules.ReadRPCID(conn, aead)
	if err != nil {
		p.log.Println("ERROR: could not read request specifier:", err)
		return
	}

	switch id {
	case requestContractsSpecifier:
		err = p.managedRequestContracts(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCRequestContracts failed: "), err)
		}
	case formContractsSpecifier:
		err = p.managedFormContracts(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCFormContracts failed: "), err)
		}
	case renewContractsSpecifier:
		err = p.managedRenewContracts(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCRenewContracts failed: "), err)
		}
	case updateRevisionSpecifier:
		err = p.managedUpdateRevision(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCUpdateRevision failed: "), err)
		}
	case formContractSpecifier:
		err = p.managedFormContract(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCFormContract failed: "), err)
		}
	case renewContractSpecifier:
		err = p.managedRenewContract(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCRenewContract failed: "), err)
		}
	case getSettingsSpecifier:
		err = p.managedGetSettings(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCGetSettings failed: "), err)
		}
	case updateSettingsSpecifier:
		err = p.managedUpdateSettings(s)
		if err != nil {
			err = errors.Extend(errors.New("incoming RPCUpdateSettings failed: "), err)
		}
	default:
		p.log.Println("INFO: inbound connection from:", conn.RemoteAddr()) //TODO
	}
	if err != nil {
		p.log.Printf("ERROR: error with %v: %v\n", conn.RemoteAddr(), err)
	}
}
