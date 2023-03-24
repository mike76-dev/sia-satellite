package proto

import (
	"context"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/siad/types"
)

// dial is a helper function, which connects to the specified address.
func dial(ctx context.Context, hostIP string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", hostIP)
	return conn, err
}

//WithTransportV2 creates a transport and calls an RHP2 RPC.
func WithTransportV2(ctx context.Context, hostIP string, hostKey types.SiaPublicKey, fn func(*rhpv2.Transport) error) (err error) {
	conn, err := dial(ctx, hostIP)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			conn.Close()
		}
	}()
	defer func() {
		close(done)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	t, err := rhpv2.NewRenterTransport(conn, modules.ConvertPublicKey(hostKey))
	if err != nil {
		return err
	}
	defer t.Close()
	return fn(t)
}

//WithTransportV3 creates a transport and calls an RHP3 RPC.
func WithTransportV3(ctx context.Context, siamuxAddr string, hostKey types.SiaPublicKey, fn func(*rhpv3.Transport) error) (err error) {
	conn, err := dial(ctx, siamuxAddr)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			conn.Close()
		}
	}()
	defer func() {
		close(done)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	t, err := rhpv3.NewRenterTransport(conn, modules.ConvertPublicKey(hostKey))
	if err != nil {
		return err
	}
	defer t.Close()
	return fn(t)
}
