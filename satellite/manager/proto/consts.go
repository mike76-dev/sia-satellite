package proto

import (
	"time"

	"gitlab.com/NebulousLabs/errors"
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

	// contractHostFormTimeout is the amount of time we wait to form a
	// contract with the host
	contractHostFormTimeout = 5 * time.Minute
)

var (
	// ErrBadHostVersion indicates that the host is using an older, incompatible
	// version of the renter-host protocol.
	ErrBadHostVersion = errors.New("Bad host version; host does not support required protocols")
)
