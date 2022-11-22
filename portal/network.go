package portal

import (
	"context"
	"net"
	"net/http"
	"sync"

	"github.com/julienschmidt/httprouter"
)

// portalAPI implements the http.Handler interface.
type portalAPI struct {
	router   http.Handler
	routerMu sync.RWMutex
}

// ServeHTTP implements the http.Handler interface.
func (api *portalAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.routerMu.RLock()
	api.router.ServeHTTP(w, r)
	api.routerMu.RUnlock()
}

// buildHttpRoutes sets up and returns an * httprouter.Router connected
// to the given api.
func (api *portalAPI) buildHTTPRoutes() {
	router := httprouter.New()

	// TODO implement handlers.

	api.routerMu.Lock()
	api.router = router
	api.routerMu.Unlock()
	return
}

// initNetworking starts the portal server listening at the
// specified address.
func (p *Portal) initNetworking(address string) error {
	// Create the listener.
	l, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	p.listener = l

	// Initialize the portal API.
	api := &portalAPI{}
	api.buildHTTPRoutes()

	// Start the portal API server.
	srv := &http.Server{Handler: api}
	go srv.Serve(l)
	p.log.Println("INFO: listening on", l.Addr())

	// Spin up a goroutine to stop the server on shutdown.
	go func() {
		<-p.closeChan
		srv.Shutdown(context.Background())
	}()

	return nil
}
