package server

import (
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/core/types"
	"go.sia.tech/jape"
)

func (s *server) walletAddressHandler(jc jape.Context) {
	uc, err := s.w.NextAddress()
	if jc.Check("unable to generate address", err) != nil {
		return
	}
	jc.Encode(uc.UnlockHash())
}

func (s *server) walletAddressesHandler(jc jape.Context) {
	addrs := s.w.Addresses()
	jc.Encode(addrs)
}

func (s *server) walletBalanceHandler(jc jape.Context) {
	sc, isc, sf := s.w.ConfirmedBalance()
	outgoing, incoming := s.w.UnconfirmedBalance()
	height := s.w.Tip().Height
	fee := s.cm.RecommendedFee()
	resp := api.WalletBalanceResponse{
		Height:           height,
		Siacoins:         sc,
		ImmatureSiacoins: isc,
		IncomingSiacoins: incoming,
		OutgoingSiacoins: outgoing,
		Siafunds:         sf,
		RecommendedFee:   fee,
	}
	jc.Encode(resp)
}

func (s *server) walletTxpoolHandler(jc jape.Context) {
	pool := s.w.Annotate(s.cm.PoolTransactions())
	jc.Encode(pool)
}

func (s *server) walletOutputsHandler(jc jape.Context) {
	scos := s.w.UnspentSiacoinOutputs()
	sfos := s.w.UnspentSiafundOutputs()
	jc.Encode(api.WalletOutputsResponse{
		SiacoinOutputs: scos,
		SiafundOutputs: sfos,
	})
}

func (s *server) walletAddWatchHandler(jc jape.Context) {
	var addr types.Address
	if jc.DecodeParam("addr", &addr) != nil {
		return
	} else if jc.Check("couldn't add address", s.w.AddWatch(addr)) != nil {
		return
	}
}

func (s *server) walletRemoveWatchHandler(jc jape.Context) {
	var addr types.Address
	if jc.DecodeParam("addr", &addr) != nil {
		return
	} else if jc.Check("couldn't remove address", s.w.RemoveWatch(addr)) != nil {
		return
	}
}

func (s *server) walletWatchHandler(jc jape.Context) {
	addrs := s.w.WatchedAddresses()
	jc.Encode(addrs)
}
