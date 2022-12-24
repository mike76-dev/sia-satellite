package modules

import (
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// HostAverages contains the host network averages from HostDB.
type HostAverages struct {
	NumHosts               uint64
	Duration               types.BlockHeight
	StoragePrice           types.Currency
	Collateral             types.Currency
	DownloadBandwidthPrice types.Currency
	UploadBandwidthPrice   types.Currency
	ContractPrice          types.Currency
	BaseRPCPrice           types.Currency
	SectorAccessPrice      types.Currency
}

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	smodules.Alerter

	// ActiveHosts provides the list of hosts that the manager is selecting,
	// sorted by preference.
	ActiveHosts() ([]smodules.HostDBEntry, error)

	// AllHosts returns the full list of hosts known to the manager.
	AllHosts() ([]smodules.HostDBEntry, error)

	// Close safely shuts down the satellite.
	Close() error

	// EstimateHostScore will return the score for a host with the provided
	// settings, assuming perfect age and uptime adjustments.
	EstimateHostScore(smodules.HostDBEntry, smodules.Allowance) (smodules.HostScoreBreakdown, error)

	// Filter returns the hostdb's filterMode and filteredHosts.
	Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error)

	// SetFilterMode sets the hostdb's filter mode.
	SetFilterMode(smodules.FilterMode, []types.SiaPublicKey, []string) error

	// Host provides the DB entry and score breakdown for the requested host.
	Host(types.SiaPublicKey) (smodules.HostDBEntry, bool, error)

	// InitialScanComplete returns a boolean indicating if the initial scan of
	// the hostdb is completed.
	InitialScanComplete() (bool, error)

	// ScoreBreakdown will return the score for a host db entry using the
	// hostdb's weighting algorithm.
	ScoreBreakdown(smodules.HostDBEntry) (smodules.HostScoreBreakdown, error)

	// RandomHosts picks up to the specified number of random hosts from the
	// hostdb sorted by weight.
	RandomHosts(uint64, smodules.Allowance) ([]smodules.HostDBEntry, error)

	// PublicKey returns the satellite's public key.
	PublicKey() types.SiaPublicKey

	// SecretKey returns the satellite's secret key.
	SecretKey() crypto.SecretKey

	// GetAverages retrieves the host network averages.
	GetAverages() HostAverages

	// FeeEstimation returns the minimum and the maximum estimated fees for
	// a transaction.
	FeeEstimation() (min, max types.Currency)
}

// Manager implements the methods necessary to communicate with the
// hosts.
type Manager interface {
	smodules.Alerter

	// ActiveHosts provides the list of hosts that the manager is selecting,
	// sorted by preference.
	ActiveHosts() ([]smodules.HostDBEntry, error)

	// AllHosts returns the full list of hosts known to the manager.
	AllHosts() ([]smodules.HostDBEntry, error)

	// Close safely shuts down the manager.
	Close() error

	// EstimateHostScore will return the score for a host with the provided
	// settings, assuming perfect age and uptime adjustments.
	EstimateHostScore(entry smodules.HostDBEntry, allowance smodules.Allowance) (smodules.HostScoreBreakdown, error)

	// Filter returns the hostdb's filterMode and filteredHosts.
	Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error)

	// SetFilterMode sets the hostdb's filter mode.
	SetFilterMode(smodules.FilterMode, []types.SiaPublicKey, []string) error

	// Host provides the DB entry and score breakdown for the requested host.
	Host(pk types.SiaPublicKey) (smodules.HostDBEntry, bool, error)

	// InitialScanComplete returns a boolean indicating if the initial scan of
	// the hostdb is completed.
	InitialScanComplete() (bool, error)

	// ScoreBreakdown will return the score for a host db entry using the
	// hostdb's weighting algorithm.
	ScoreBreakdown(entry smodules.HostDBEntry) (smodules.HostScoreBreakdown, error)

	// RandomHosts picks up to the specified number of random hosts from the
	// hostdb sorted by weight.
	RandomHosts(uint64, smodules.Allowance) ([]smodules.HostDBEntry, error)

	// GetAverages retrieves the host network averages.
	GetAverages() HostAverages
}

// Provider implements the methods necessary to communicate with the
// renters.
type Provider interface {
	smodules.Alerter

	// Close safely shuts down the provider.
	Close() error
}

// Portal implements the portal server.
type Portal interface {
	smodules.Alerter

	// Close safely shuts down the portal.
	Close() error
}
