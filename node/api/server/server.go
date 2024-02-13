package server

import (
	"net"
	"net/http"
	"strings"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node"
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/jape"
)

type server struct {
	cm *chain.Manager
	s  modules.Syncer
	m  modules.Manager
	w  modules.Wallet
}

// newServer returns an HTTP handler that serves the hsd API.
func newServer(cm *chain.Manager, s modules.Syncer, m modules.Manager, w modules.Wallet) http.Handler {
	srv := server{
		cm: cm,
		s:  s,
		m:  m,
		w:  w,
	}
	return jape.Mux(map[string]jape.Handler{
		"GET /daemon/version": srv.versionHandler,

		"GET /consensus/network":  srv.consensusNetworkHandler,
		"GET /consensus/tip":      srv.consensusTipHandler,
		"GET /consensus/tipstate": srv.consensusTipStateHandler,

		"GET  /syncer/peers":           srv.syncerPeersHandler,
		"POST /syncer/connect":         srv.syncerConnectHandler,
		"POST /syncer/broadcast/block": srv.syncerBroadcastBlockHandler,

		"GET  /txpool/transactions": srv.txpoolTransactionsHandler,
		"GET  /txpool/fee":          srv.txpoolFeeHandler,
		"POST /txpool/broadcast":    srv.txpoolBroadcastHandler,

		"GET    /wallet/address":     srv.walletAddressHandler,
		"GET    /wallet/addresses":   srv.walletAddressesHandler,
		"GET    /wallet/balance":     srv.walletBalanceHandler,
		"GET    /wallet/txpool":      srv.walletTxpoolHandler,
		"GET    /wallet/outputs":     srv.walletOutputsHandler,
		"GET    /wallet/watch":       srv.walletWatchHandler,
		"PUT    /wallet/watch/:addr": srv.walletAddWatchHandler,
		"DELETE /wallet/watch/:addr": srv.walletRemoveWatchHandler,
		"POST   /wallet/send":        srv.walletSendHandler,

		"GET  /manager/averages/:currency":   srv.managerAveragesHandler,
		"GET  /manager/renters":              srv.managerRentersHandler,
		"GET  /manager/renter/:publickey":    srv.managerRenterHandler,
		"GET  /manager/balance/:publickey":   srv.managerBalanceHandler,
		"GET  /manager/contracts/:publickey": srv.managerContractsHandler,
		"GET  /manager/preferences":          srv.managerPreferencesHandler,
		"POST /manager/preferences":          srv.managerUpdatePreferencesHandler,
		"GET  /manager/prices":               srv.managerPricesHandler,
		"POST /manager/prices":               srv.managerUpdatePricesHandler,
		"GET  /manager/maintenance":          srv.managerMaintenanceHandler,
		"POST /manager/maintenance":          srv.managerSetMaintenanceHandler,

		"GET  /hostdb":                 srv.hostdbHandler,
		"GET  /hostdb/active":          srv.hostdbActiveHandler,
		"GET  /hostdb/all":             srv.hostdbAllHandler,
		"GET  /hostdb/host/:publickey": srv.hostdbHostHandler,
		"GET  /hostdb/filtermode":      srv.hostdbFilterModeHandler,
		"POST /hostdb/filtermode":      srv.hostdbSetFilterModeHandler,
	})
}

func StartWeb(l net.Listener, node *node.Node, password string) error {
	server := newServer(node.ChainManager, node.Syncer, node.Manager, node.Wallet)
	api := jape.BasicAuth(password)(server)
	return http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			api.ServeHTTP(w, r)
			return
		}
	}))
}

// versionHandler handles the API call that requests the daemon's version.
func (s *server) versionHandler(jc jape.Context) {
	jc.Encode(api.DaemonVersion{Version: build.NodeVersion, GitRevision: build.GitRevision, BuildTime: build.BuildTime})
}
