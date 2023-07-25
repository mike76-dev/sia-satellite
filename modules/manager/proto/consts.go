package proto

import (
	"time"
)

const (
	// settingsTimeout is the amount of time we wait to retrieve the settings
	// from the host.
	settingsTimeout = 30 * time.Second

	// priceTableTimeout is the amount of time we wait to receive a price table
	// from the host.
	priceTableTimeout = 30 * time.Second
)
