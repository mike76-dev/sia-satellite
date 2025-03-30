package api

import (
	"net/http"
	"time"

	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/syncer"
	"go.sia.tech/jape"
)

type server struct {
	chain  *chain.Manager
	syncer *syncer.Syncer
}

func isSynced(s *syncer.Syncer) bool {
	var count int
	for _, p := range s.Peers() {
		if p.Synced() {
			count++
		}
	}
	return count >= 5
}

func (s *server) consensusNetworkHandler(jc jape.Context) {
	jc.Encode(s.chain.TipState().Network)
}

func (s *server) consensusTipHandler(jc jape.Context) {
	state := s.chain.TipState()
	synced := isSynced(s.syncer) && time.Since(state.PrevTimestamps[0]) < 24*time.Hour

	jc.Encode(ConsensusTipResponse{
		Height:  state.Index.Height,
		BlockID: state.Index.ID,
		Synced:  synced,
	})
}

func (s *server) consensusTipStateHandler(jc jape.Context) {
	jc.Encode(s.chain.TipState())
}

func (s *server) syncerPeersHandler(jc jape.Context) {
	var peers []GatewayPeer
	ps := s.syncer.Peers()

	for _, p := range ps {
		peers = append(peers, GatewayPeer{
			Addr:    p.Addr(),
			Inbound: p.Inbound,
			Version: p.Version(),
		})
	}

	jc.Encode(peers)
}

func (s *server) syncerConnectHandler(jc jape.Context) {
	var addr string
	if jc.Decode(&addr) != nil {
		return
	}
	_, err := s.syncer.Connect(jc.Request.Context(), addr)
	if jc.Check("couldn't connect to peer", err) != nil {
		return
	}
	jc.EmptyResonse()
}

func (s *server) txpoolTransactionsHandler(jc jape.Context) {
	jc.Encode(TxpoolTransactionsResponse{
		Basis:          s.chain.Tip(),
		Transactions:   s.chain.PoolTransactions(),
		V2Transactions: s.chain.V2PoolTransactions(),
	})
}

func (s *server) txpoolFeeHandler(jc jape.Context) {
	jc.Encode(s.chain.RecommendedFee())
}

// NewServer returns an HTTP handler that serves the satd API.
func NewServer(cm *chain.Manager, s *syncer.Syncer) http.Handler {
	srv := server{cm, s}
	return jape.Mux(map[string]jape.Handler{
		"GET /consensus/network":  srv.consensusNetworkHandler,
		"GET /consensus/tip":      srv.consensusTipHandler,
		"GET /consensus/tipstate": srv.consensusTipStateHandler,

		"GET  /syncer/peers":   srv.syncerPeersHandler,
		"POST /syncer/connect": srv.syncerConnectHandler,

		"GET  /txpool/transactions": srv.txpoolTransactionsHandler,
		"GET  /txpool/fee":          srv.txpoolFeeHandler,
	})
}
