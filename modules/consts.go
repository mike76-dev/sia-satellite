package modules

import (
	"go.sia.tech/siad/types"
)

// SatelliteOverhead determines how much extra a renter is charged for
// using the service. This should include any payment processing fees.
const SatelliteOverhead = 1.1

var (
	// MaxRPCPrice is how much the Satellite is willing to pay
	// for a single RPC call.
	MaxRPCPrice = types.SiacoinPrecision.MulFloat(1e-7)

	// MaxSectorAccessPrice is how much the Satellite is willing
	// to pay to download a single sector.
	MaxSectorAccessPrice = types.SiacoinPrecision.MulFloat(1e-6)
)
