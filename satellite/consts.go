package satellite

import (
	"time"
)

const (
	// rpcRatelimit prevents someone from spamming the satellite connections,
	// causing it to spin up enough goroutines to crash.
	rpcRatelimit = time.Millisecond * 50
)
