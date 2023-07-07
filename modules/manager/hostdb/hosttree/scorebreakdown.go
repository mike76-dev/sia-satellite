package hosttree

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// ScoreBreakdown is an interface that allows us to mock the hostAdjustments
// during testing.
type ScoreBreakdown interface {
	HostScoreBreakdown() modules.HostScoreBreakdown
	Score() types.Currency
}

// HostAdjustments contains all the adjustments relevant to a host's score and
// implements the scoreBreakdown interface.
type HostAdjustments struct {
	AgeAdjustment              float64
	CollateralAdjustment       float64
	InteractionAdjustment      float64
	PriceAdjustment            float64
	StorageRemainingAdjustment float64
	UptimeAdjustment           float64
	VersionAdjustment          float64
}

// HostScoreBreakdown converts a HostAdjustments object into a
// modules.HostScoreBreakdown.
func (h HostAdjustments) HostScoreBreakdown() modules.HostScoreBreakdown {
	return modules.HostScoreBreakdown{
		Age:              h.AgeAdjustment,
		Collateral:       h.CollateralAdjustment,
		Interactions:     h.InteractionAdjustment,
		Prices:           h.PriceAdjustment,
		StorageRemaining: h.StorageRemainingAdjustment,
		Uptime:           h.UptimeAdjustment,
		Version:          h.VersionAdjustment,
	}
}

// Score combines the individual adjustments of the breakdown into a single
// score.
func (h HostAdjustments) Score() types.Currency {
	// Combine the adjustments.
	penalty := h.AgeAdjustment *
		h.CollateralAdjustment *
		h.InteractionAdjustment *
		h.PriceAdjustment *
		h.StorageRemainingAdjustment *
		h.UptimeAdjustment *
		h.VersionAdjustment

	score := modules.FromFloat(penalty).Div(types.HastingsPerSiacoin)
	if score.IsZero() {
		// A score of zero is problematic for for the host tree.
		return types.NewCurrency64(1)
	}
	return score
}
