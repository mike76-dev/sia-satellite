package main

import (
	"net"
	"net/http"
	"strings"

	"github.com/mike76-dev/sia-satellite/api"
	"go.sia.tech/jape"
)

func startWeb(l net.Listener, n *node, password string) error {
	server := api.NewServer(n.chain, n.syncer, n.wallet, n.hostDB)
	api := jape.BasicAuth(password)(server)
	return http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			api.ServeHTTP(w, r)
			return
		}
	}))
}
