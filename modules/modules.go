package modules

import (
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// HostAverages contains the host network averages from HostDB.
type HostAverages struct {
	NumHosts               uint64            `json:"numhosts"`
	Duration               types.BlockHeight `json:"height"`
	StoragePrice           types.Currency    `json:"storageprice"`
	Collateral             types.Currency    `json:"collateral"`
	DownloadBandwidthPrice types.Currency    `json:"downloadprice"`
	UploadBandwidthPrice   types.Currency    `json:"uploadprice"`
	ContractPrice          types.Currency    `json:"contractprice"`
	BaseRPCPrice           types.Currency    `json:"baserpcprice"`
	SectorAccessPrice      types.Currency    `json:"sectoraccessprice"`
}

// userBalance holds the current balance as well as
// the data on the chosen payment scheme.
type UserBalance struct {
	IsUser     bool    `json:"isuser"`
	Subscribed bool    `json:"subscribed"`
	Balance    float64 `json:"balance"`
	Locked     float64 `json:"locked"`
	Currency   string  `json:"currency"`
	SCBalance  float64 `json:"scbalance"`
	StripeID   string  `json:"stripeid"`
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
	InitialScanComplete() (bool, types.BlockHeight, error)

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

	// GetWalletSeed returns the wallet seed.
	GetWalletSeed() (smodules.Seed, error)

	// GetRenter returns the renter by the public key.
	GetRenter(types.SiaPublicKey) (Renter, error)

	// Renters retrieves the list of renters.
	Renters() []Renter

	// GetBalance retrieves the balance information on the account.
	GetBalance(string) (*UserBalance, error)

	// Contracts returns storage contracts.
	Contracts() []RenterContract

	// RefreshedContract returns a bool indicating if the contract was refreshed.
	RefreshedContract(types.FileContractID) bool

	// OldContracts returns the contracts that have expired.
	OldContracts() []RenterContract
}

// Manager implements the methods necessary to communicate with the
// hosts.
type Manager interface {
	smodules.Alerter

	// Close safely shuts down the manager.
	Close() error

	// PriceEstimation estimates the cost in siacoins of performing various
	// storage and data operations. The estimation will be done using the provided
	// allowance. The final allowance used will be returned.
	PriceEstimation(smodules.Allowance) (float64, smodules.Allowance, error)
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

// A HostDB is a database of hosts that the manager can use for figuring out
// who to upload to, and download from.
type HostDB interface {
	smodules.Alerter

	// ActiveHosts returns the list of hosts that are actively being selected
	// from.
	ActiveHosts() ([]smodules.HostDBEntry, error)

	// AllHosts returns the full list of hosts known to the hostdb, sorted in
	// order of preference.
	AllHosts() ([]smodules.HostDBEntry, error)

	// CheckForIPViolations accepts a number of host public keys and returns the
	// ones that violate the rules of the addressFilter.
	CheckForIPViolations([]types.SiaPublicKey) ([]types.SiaPublicKey, error)

	// Close closes the hostdb.
	Close() error

	// EstimateHostScore returns the estimated score breakdown of a host with the
	// provided settings.
	EstimateHostScore(smodules.HostDBEntry, smodules.Allowance) (smodules.HostScoreBreakdown, error)

	// Filter returns the hostdb's filterMode and filteredHosts.
	Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error)

	// SetFilterMode sets the renter's hostdb filter mode.
	SetFilterMode(smodules.FilterMode, []types.SiaPublicKey, []string) error

	// Host returns the HostDBEntry for a given host.
	Host(pk types.SiaPublicKey) (smodules.HostDBEntry, bool, error)

	// IncrementSuccessfulInteractions increments the number of successful
	// interactions with a host for a given key
	IncrementSuccessfulInteractions(types.SiaPublicKey) error

	// IncrementFailedInteractions increments the number of failed interactions with
	// a host for a given key
	IncrementFailedInteractions(types.SiaPublicKey) error

	// initialScanComplete returns a boolean indicating if the initial scan of the
	// hostdb is completed and the current block height of the hostdb.
	InitialScanComplete() (bool, types.BlockHeight, error)

	// IPViolationsCheck returns a boolean indicating if the IP violation check is
	// enabled or not.
	IPViolationsCheck() (bool, error)

	// RandomHosts returns a set of random hosts, weighted by their estimated
	// usefulness / attractiveness to the renter. RandomHosts will not return
	// any offline or inactive hosts.
	RandomHosts(int, []types.SiaPublicKey, []types.SiaPublicKey) ([]smodules.HostDBEntry, error)

	// RandomHostsWithAllowance is the same as RandomHosts but accepts an
	// allowance as an argument to be used instead of the allowance set in the
	// manager.
	RandomHostsWithAllowance(int, []types.SiaPublicKey, []types.SiaPublicKey, smodules.Allowance) ([]smodules.HostDBEntry, error)

	// RandomHostsWithLimits works as RandomHostsWithAllowance but uses the
	// limits set in the allowance instead of calculating the weight function.
	RandomHostsWithLimits(int, []types.SiaPublicKey, []types.SiaPublicKey, smodules.Allowance) ([]smodules.HostDBEntry, error)

	// ScoreBreakdown returns a detailed explanation of the various properties
	// of the host.
	ScoreBreakdown(smodules.HostDBEntry) (smodules.HostScoreBreakdown, error)

	// SetAllowance updates the allowance used by the hostdb for weighing hosts by
	// updating the host weight function. It will completely rebuild the hosttree so
	// it should be used with care.
	SetAllowance(smodules.Allowance) error

	// SetIPViolationCheck enables/disables the IP violation check within the
	// hostdb.
	SetIPViolationCheck(enabled bool) error

	// LoadingComplete indicates if the HostDB has finished loading the hosts
	// from the database.
	LoadingComplete() bool

	// UpdateContracts rebuilds the knownContracts of the HostBD using the
	// provided contracts.
	UpdateContracts([]RenterContract) error
}

// FundLocker is the minimal interface needed to lock and unlock funds.
type FundLocker interface {
	// LockSiacoins moves a part of the balance to "locked".
	LockSiacoins(string, float64) error

	// UnlockSiacoins moves a part of the amount from "locked" to "available",
	// while the other part (fees and other spent funds) is "burned".
	UnlockSiacoins(string, float64, float64) error
}
