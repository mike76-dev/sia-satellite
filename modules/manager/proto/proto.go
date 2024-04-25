package proto

import (
	"context"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/consensus"
	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

// transactionSigner is the minimal interface for modules.Wallet.
type transactionSigner interface {
	Sign(cs consensus.State, txn *types.Transaction, toSign []types.Hash256) error
}

// HostSettings uses the Settings RPC to retrieve the host's settings.
func HostSettings(address string, hpk types.PublicKey) (settings rhpv2.HostSettings, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), settingsTimeout)
	defer cancel()

	err = WithTransportV2(ctx, address, hpk, func(t *rhpv2.Transport) (err error) {
		settings, err = RPCSettings(ctx, t)
		return
	})

	return
}

// HostPriceTable retrieves the price table from the host.
func FetchPriceTable(host modules.HostDBEntry) (rhpv3.HostPriceTable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), priceTableTimeout)
	defer cancel()

	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return rhpv3.HostPriceTable{}, err
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	var pt rhpv3.HostPriceTable
	err = WithTransportV3(ctx, siamuxAddr, host.PublicKey, func(t *rhpv3.Transport) (err error) {
		pt, err = RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
			return nil, nil
		})
		return
	})

	return pt, err
}
