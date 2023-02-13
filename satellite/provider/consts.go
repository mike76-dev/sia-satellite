package provider

import (
	"time"
)

// requestKeyTime defines the amount of time that the renter and
// the satellite have to exchange their public keys.
const requestKeyTime = 60 * time.Second
