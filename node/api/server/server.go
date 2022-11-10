package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	mnemonics "gitlab.com/NebulousLabs/entropy-mnemonics"
	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"

	"github.com/mike76-dev/sia-satellite/node"
	"github.com/mike76-dev/sia-satellite/node/api"
)

// A Server is a collection of modules that can be communicated with over an http API.
type Server struct {
	api								*api.API
	apiServer					*http.Server
	listener					net.Listener

	node							*node.Node
	requiredUserAgent	string

	serveChan					chan struct{}
	serveErr					error

	closeChan					chan struct{}

	closeMu						sync.Mutex
}

// serve listens for and handles API calls. It is a blocking function.
func (srv *Server) serve() error {
	// The server will run until an error is encountered or the listener is
	// closed, via either the Close method or by signal handling.  Closing the
	// listener will result in the benign error handled below.
	err := srv.apiServer.Serve(srv.listener)
	if err != nil && !strings.HasSuffix(err.Error(), "use of closed network connection") {
		return err
	}
	return nil
}

// Close closes the Server's listener, causing the HTTP server to shut down.
func (srv *Server) Close() error {
	defer close(srv.closeChan)
	srv.closeMu.Lock()
	defer srv.closeMu.Unlock()
	// Stop accepting API requests.
	err := srv.apiServer.Shutdown(context.Background())
	// Wait for serve() to return and capture its error.
	<-srv.serveChan
	if !errors.Contains(srv.serveErr, http.ErrServerClosed) {
		err = errors.Compose(err, srv.serveErr)
	}
	// Shutdown modules.
	if srv.node != nil {
		err = errors.Compose(err, srv.node.Close())
	}
	return errors.AddContext(err, "error while closing server")
}

// WaitClose blocks until the server is done shutting down.
func (srv *Server) WaitClose() {
	<-srv.closeChan
}

// APIAddress returns the underlying node's api address.
func (srv *Server) APIAddress() string {
	return srv.listener.Addr().String()
}

// GatewayAddress returns the underlying node's gateway address.
func (srv *Server) GatewayAddress() modules.NetAddress {
	return srv.node.Gateway.Address()
}

// ServeErr is a blocking call that will return the result of srv.serve after
// the server stopped.
func (srv *Server) ServeErr() <-chan error {
	c := make(chan error)
	go func() {
		<-srv.serveChan
		close(c)
	}()
	return c
}

// Unlock unlocks the wallet using the provided password.
func (srv *Server) Unlock(password string) error {
	if srv.node.Wallet == nil {
		return errors.New("server doesn't have a wallet")
	}
	var validKeys []crypto.CipherKey
	dicts := []mnemonics.DictionaryID{"english", "german", "japanese"}
	for _, dict := range dicts {
		seed, err := modules.StringToSeed(password, dict)
		if err != nil {
			continue
		}
		validKeys = append(validKeys, crypto.NewWalletKey(crypto.HashObject(seed)))
	}
	validKeys = append(validKeys, crypto.NewWalletKey(crypto.HashObject(password)))
	for _, key := range validKeys {
		if err := srv.node.Wallet.Unlock(key); err == nil {
			return nil
		}
	}
	return modules.ErrBadEncryptionKey
}

// NewAsync creates a new API server. The API will require authentication using
// HTTP basic auth if the supplied password is not the empty string. Usernames
// are ignored for authentication. This type of authentication sends passwords
// in plaintext and should therefore only be used if the apiAddr is localhost.
func NewAsync(requiredUserAgent string, gatewayAddr string, apiAddr string, requiredPassword string, satelliteAddr string, dir string, bootstrap bool, loadStartTime time.Time) (*Server, <-chan error) {
	c := make(chan error, 1)
	defer close(c)

	var errChan <-chan error
	var n *node.Node
	s, err := func() (*Server, error) {
		// Create the server listener.
		listener, err := net.Listen("tcp", apiAddr)
		if err != nil {
			return nil, err
		}

		// Create the api for the server.
		api := api.New(requiredUserAgent, requiredPassword, nil, nil, nil, nil, nil)
		srv := &Server{
			api: api,
			apiServer: &http.Server{
				Handler: api,

				// ReadTimeout defines the maximum amount of time allowed to fully read
				// the request body. This timeout is applied to every handler in the
				// server.
				ReadTimeout: time.Minute * 360,

				// ReadHeaderTimeout defines the amount of time allowed to fully read the
				// request headers.
				ReadHeaderTimeout: time.Minute * 2,

				// IdleTimeout defines the maximum duration a HTTP Keep-Alive connection
				// the API is kept open with no activity before closing.
				IdleTimeout: time.Minute * 5,
			},
			closeChan:         make(chan struct{}),
			serveChan:         make(chan struct{}),
			listener:          listener,
			requiredUserAgent: requiredUserAgent,
		}

		// Set the shutdown method to allow the api to shutdown the server.
		api.Shutdown = srv.Close

		// Spin up a goroutine that serves the API and closes srv.done when
		// finished.
		go func() {
			srv.serveErr = srv.serve()
			close(srv.serveChan)
		}()

		// Create the node for the server after the server was started.
		n, errChan = node.New(gatewayAddr, satelliteAddr, dir, bootstrap, loadStartTime)
		if err := modules.PeekErr(errChan); err != nil {
			if isAddrInUseErr(err) {
				return nil, fmt.Errorf("%v; are you running another instance of siad?", err.Error())
			}
			return nil, errors.AddContext(err, "server is unable to create the Sia node")
		}

		// Make sure that the server wasn't shut down while loading the modules.
		srv.closeMu.Lock()
		defer srv.closeMu.Unlock()
		select {
		case <-srv.serveChan:
			// Server was shut down. Close node and exit.
			return srv, n.Close()
		default:
		}

		// Server wasn't shut down. Replace modules.
		srv.node = n
		api.SetModules(n.ConsensusSet, n.Gateway, n.Satellite, n.TransactionPool, n.Wallet)
		return srv, nil
	}()
	if err != nil {
		if n != nil {
			err = errors.Compose(err, n.Close())
		}
		c <- err
		return nil, c
	}
	return s, errChan
}

// New creates a new API server. The API will require authentication using
// HTTP basic auth if the supplied password is not the empty string.
// Usernames are ignored for authentication. This type of authentication
// sends passwords in plaintext and should therefore only be used if the
// apiAddr is localhost.
func New(apiAddr string, requiredUserAgent string, requiredPassword string, gatewayAddr string, satelliteAddr string, dir string, bootstrap bool, loadStartTime time.Time) (*Server, error) {
	// Wait for the node to be done loading.
	srv, errChan := NewAsync(requiredUserAgent, gatewayAddr, apiAddr, requiredPassword, satelliteAddr, dir, bootstrap, loadStartTime)
	if err := <-errChan; err != nil {
		// Error occurred during async load. Close all modules.
		fmt.Println("ERROR:", err)
		return nil, err
	}
	return srv, nil
}


// isAddrInUseErr checks if the error corresponds to syscall.EADDRINUSE.
func isAddrInUseErr(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		if syscallErr, ok := opErr.Err.(*os.SyscallError); ok {
			return syscallErr.Err == syscall.EADDRINUSE
		}
	}
	return false
}
