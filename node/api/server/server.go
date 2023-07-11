package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"github.com/mike76-dev/sia-satellite/node"
	"github.com/mike76-dev/sia-satellite/node/api"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"

	"golang.org/x/crypto/blake2b"

	"lukechampine.com/frand"
)

// A Server is a collection of modules that can be communicated with over an http API.
type Server struct {
	api               *api.API
	apiServer         *http.Server
	listener          net.Listener

	node              *node.Node
	requiredUserAgent string

	serveChan         chan struct{}
	serveErr          error

	closeChan         chan struct{}

	closeMu           sync.Mutex
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
	if !modules.ContainsError(srv.serveErr, http.ErrServerClosed) {
		err = modules.ComposeErrors(err, srv.serveErr)
	}
	// Shutdown modules.
	if srv.node != nil {
		err = modules.ComposeErrors(err, srv.node.Close())
	}
	return fmt.Errorf("error while closing server: %s", err)
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
	var validKeys []modules.WalletKey
	key, err := modules.KeyFromPhrase(password)
	if err == nil {
		h := blake2b.Sum256(key[:])
		wk := make([]byte, len(h))
		copy(wk, h[:])
		validKeys = append(validKeys, modules.WalletKey(wk))
		frand.Read(h[:])
	}
	h := blake2b.Sum256([]byte(password))
	buf := make([]byte, 32 + 8)
	copy(buf[:32], h[:])
	binary.LittleEndian.PutUint64(buf[32:], 0)
	h = blake2b.Sum256(buf)
	key = types.NewPrivateKeyFromSeed(h[:])
	h = blake2b.Sum256(key[:])
	wk := make([]byte, len(h))
	copy(wk, h[:])
	validKeys = append(validKeys, modules.WalletKey(wk))
	frand.Read(h[:])
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
func NewAsync(config *persist.SatdConfig, apiPassword string, dbPassword string, loadStartTime time.Time) (*Server, <-chan error) {
	c := make(chan error, 1)
	defer close(c)

	var errChan <-chan error
	var n *node.Node
	s, err := func() (*Server, error) {
		// Create the server listener.
		listener, err := net.Listen("tcp", config.APIAddr)
		if err != nil {
			return nil, err
		}

		// Create the api for the server.
		api := api.New(config.UserAgent, apiPassword, nil, nil, nil, nil, nil, nil)
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
			requiredUserAgent: config.UserAgent,
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
		n, errChan = node.New(config, dbPassword, loadStartTime)
		if err := modules.PeekErr(errChan); err != nil {
			if isAddrInUseErr(err) {
				return nil, fmt.Errorf("%v; are you running another instance of siad?", err.Error())
			}
			return nil, fmt.Errorf("server is unable to create the Sia node: %s", err)
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
		api.SetModules(n.Gateway, n.ConsensusSet, n.Manager, n.Provider, n.TransactionPool, n.Wallet)
		return srv, nil
	}()
	if err != nil {
		if n != nil {
			err = modules.ComposeErrors(err, n.Close())
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
func New(config *persist.SatdConfig, apiPassword string, dbPassword string, loadStartTime time.Time) (*Server, error) {
	// Wait for the node to be done loading.
	srv, errChan := NewAsync(config, apiPassword, dbPassword, loadStartTime)
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
