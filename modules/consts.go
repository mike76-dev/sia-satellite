package modules

import (
	"go.sia.tech/siad/types"
)

var (
	// MaxRPCPrice is how much the Satellite is willing to pay
	// for a single RPC call.
	MaxRPCPrice = types.SiacoinPrecision.MulFloat(1e-7)

	// MaxSectorAccessPrice is how much the Satellite is willing
	// to pay to download a single sector.
	MaxSectorAccessPrice = types.SiacoinPrecision.MulFloat(1e-6)
)
