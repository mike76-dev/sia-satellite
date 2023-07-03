package proto

import (
	"errors"
	"time"
)

const (
	// connTimeout determines the number of seconds before a dial-up or
	// revision negotiation times out.
	connTimeout = 2 * time.Minute

	// defaultContractLockTimeout is the default amount of the time, in
	// milliseconds, that the manager will try to acquire a contract lock for.
	defaultContractLockTimeout = uint64(5 * 60 * 1000) // 5 minutes

	// hostPriceLeeway is the amount of flexibility we give to hosts when
	// choosing how much to pay for file uploads. If the host does not have the
	// most recent block yet, the host will be expecting a slightly larger
	// payment.
	hostPriceLeeway = 0.003

	// contractFormTimeout is the amount of time we wait to form a contract
	// with the host.
	contractFormTimeout = 5 * time.Minute

	// contractRenewTimeout is the amount of time we wait to renew a contract.
	contractRenewTimeout = 5 * time.Minute

	// settingsTimeout is the amount of time we wait to retrieve the settings
	// from the host.
	settingsTimeout = 30 * time.Second

	// priceTableTimeout is the amount of time we wait to receive a price table
	// from the host.
	priceTableTimeout = 30 * time.Second

	// minimumSupportedRenterHostProtocolVersion is the minimum version of Sia
	// that supports the currently used version of the renter-host protocol.
	minimumSupportedRenterHostProtocolVersion = "1.4.1"
)

var (
	// ErrBadHostVersion indicates that the host is using an older, incompatible
	// version of the renter-host protocol.
	ErrBadHostVersion = errors.New("bad host version; host does not support required protocols")
)
