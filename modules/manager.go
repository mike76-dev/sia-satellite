package modules

import (
	"fmt"
	"time"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

// HostAverages contains the host network averages from HostDB.
type HostAverages struct {
	NumHosts               uint64
	Duration               uint64
	StoragePrice           types.Currency
	Collateral             types.Currency
	DownloadBandwidthPrice types.Currency
	UploadBandwidthPrice   types.Currency
	ContractPrice          types.Currency
	BaseRPCPrice           types.Currency
	SectorAccessPrice      types.Currency
}

// UserBalance holds the current balance as well as
// the data on the chosen payment scheme.
type UserBalance struct {
	IsUser     bool    `json:"isuser"`
	Subscribed bool    `json:"subscribed"`
	Balance    float64 `json:"balance"`
	Locked     float64 `json:"locked"`
	Currency   string  `json:"currency"`
	SCRate     float64 `json:"scrate"`
	StripeID   string  `json:"stripeid"`
}

// UserSpendings contains the spendings in the current and the
// previous months.
type UserSpendings struct {
	CurrentLocked   float64 `json:"currentlocked"`
	CurrentUsed     float64 `json:"currentused"`
	CurrentOverhead float64 `json:"currentoverhead"`
	PrevLocked      float64 `json:"prevlocked"`
	PrevUsed        float64 `json:"prevused"`
	PrevOverhead    float64 `json:"prevoverhead"`
	SCRate          float64 `json:"scrate"`
	CurrentFormed   uint64  `json:"currentformed"`
	CurrentRenewed  uint64  `json:"currentrenewed"`
	PrevFormed      uint64  `json:"prevformed"`
	PrevRenewed     uint64  `json:"prevrenewed"`
}

// HostScoreBreakdown breaks down the host scores.
type HostScoreBreakdown struct {
	Age              float64
	Collateral       float64
	Interactions     float64
	StorageRemaining float64
	Uptime           float64
	Version          float64
	Prices           float64
}

// A HostDBEntry represents one host entry in the Manager's host DB. It
// aggregates the host's external settings and metrics with its public key.
type HostDBEntry struct {
	rhpv2.HostSettings
	PriceTable rhpv3.HostPriceTable

	// FirstSeen is the first block height at which this host was announced.
	FirstSeen uint64

	// LastAnnouncement is the last block height at which this host was announced.
	LastAnnouncement uint64

	// Measurements that have been taken on the host. The most recent
	// measurements are kept in full detail, historic ones are compressed into
	// the historic values.
	HistoricDowntime time.Duration
	HistoricUptime   time.Duration
	ScanHistory      HostDBScans

	// Measurements that are taken whenever we interact with a host.
	HistoricFailedInteractions     float64
	HistoricSuccessfulInteractions float64
	RecentFailedInteractions       float64
	RecentSuccessfulInteractions   float64

	LastHistoricUpdate uint64

	// Measurements related to the IP subnet mask.
	IPNets          []string
	LastIPNetChange time.Time

	// The public key of the host, stored separately to minimize risk of certain
	// MitM based vulnerabilities.
	PublicKey types.PublicKey

	// Filtered says whether or not a HostDBEntry is being filtered out of the
	// filtered hosttree due to the filter mode of the hosttree.
	Filtered bool
}

// HostDBScan represents a single scan event.
type HostDBScan struct {
	Timestamp time.Time
	Success   bool
}

// HostDBScans represents a sortable slice of scans.
type HostDBScans []HostDBScan

func (s HostDBScans) Len() int           { return len(s) }
func (s HostDBScans) Less(i, j int) bool { return s[i].Timestamp.Before(s[j].Timestamp) }
func (s HostDBScans) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// FilterMode is the helper type for the enum constants for the HostDB filter
// mode.
type FilterMode int

// HostDBFilterError HostDBDisableFilter HostDBActivateBlacklist and
// HostDBActiveWhitelist are the constants used to enable and disable the filter
// mode of the manager's hostdb.
const (
	HostDBFilterError FilterMode = iota
	HostDBDisableFilter
	HostDBActivateBlacklist
	HostDBActiveWhitelist
)

// String returns the string value for the FilterMode.
func (fm FilterMode) String() string {
	switch fm {
	case HostDBFilterError:
		return "error"
	case HostDBDisableFilter:
		return "disable"
	case HostDBActivateBlacklist:
		return "blacklist"
	case HostDBActiveWhitelist:
		return "whitelist"
	default:
		return ""
	}
}

// FromString assigned the FilterMode from the provide string.
func (fm *FilterMode) FromString(s string) error {
	switch s {
	case "disable":
		*fm = HostDBDisableFilter
	case "blacklist":
		*fm = HostDBActivateBlacklist
	case "whitelist":
		*fm = HostDBActiveWhitelist
	default:
		*fm = HostDBFilterError
		return fmt.Errorf("could not assigned FilterMode from string %v", s)
	}
	return nil
}

// A HostDB is a database of hosts that the manager can use for figuring out
// who to upload to, and download from.
type HostDB interface {
	Alerter

	// ActiveHosts returns the list of hosts that are actively being selected
	// from.
	ActiveHosts() ([]HostDBEntry, error)

	// AllHosts returns the full list of hosts known to the hostdb, sorted in
	// order of preference.
	AllHosts() ([]HostDBEntry, error)

	// CheckForIPViolations accepts a number of host public keys and returns the
	// ones that violate the rules of the addressFilter.
	CheckForIPViolations([]types.PublicKey) ([]types.PublicKey, error)

	// Close closes the hostdb.
	Close() error

	// EstimateHostScore returns the estimated score breakdown of a host with the
	// provided settings.
	EstimateHostScore(Allowance, HostDBEntry) (HostScoreBreakdown, error)

	// Filter returns the hostdb's filterMode and filteredHosts.
	Filter() (FilterMode, map[string]types.PublicKey, []string, error)

	// SetFilterMode sets the renter's hostdb filter mode.
	SetFilterMode(FilterMode, []types.PublicKey, []string) error

	// Host returns the HostDBEntry for a given host.
	Host(pk types.PublicKey) (HostDBEntry, bool, error)

	// IncrementSuccessfulInteractions increments the number of successful
	// interactions with a host for a given key
	IncrementSuccessfulInteractions(types.PublicKey) error

	// IncrementFailedInteractions increments the number of failed interactions with
	// a host for a given key
	IncrementFailedInteractions(types.PublicKey) error

	// initialScanComplete returns a boolean indicating if the initial scan of the
	// hostdb is completed and the current block height of the hostdb.
	InitialScanComplete() (bool, uint64, error)

	// IPViolationsCheck returns a boolean indicating if the IP violation check is
	// enabled or not.
	IPViolationsCheck() (bool, error)

	// RandomHosts returns a set of random hosts, weighted by their estimated
	// usefulness / attractiveness to the renter. RandomHosts will not return
	// any offline or inactive hosts.
	RandomHosts(int, []types.PublicKey, []types.PublicKey) ([]HostDBEntry, error)

	// RandomHostsWithAllowance is the same as RandomHosts but accepts an
	// allowance as an argument to be used instead of the allowance set in the
	// manager.
	RandomHostsWithAllowance(int, []types.PublicKey, []types.PublicKey, Allowance) ([]HostDBEntry, error)

	// ScoreBreakdown returns a detailed explanation of the various properties
	// of the host.
	ScoreBreakdown(HostDBEntry) (HostScoreBreakdown, error)

	// SetAllowance updates the allowance used by the hostdb for weighing hosts by
	// updating the host weight function. It will completely rebuild the hosttree so
	// it should be used with care.
	SetAllowance(Allowance) error

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

// Manager implements the methods necessary to communicate with the
// hosts.
type Manager interface {
	Alerter

	// Close safely shuts down the manager.
	Close() error

	// GetSiacoinRate calculates the SC price in a given currency.
	GetSiacoinRate(string) (float64, error)

	// GetExchangeRate returns the exchange rate of a given currency.
	GetExchangeRate(string) (float64, error)

	// PriceEstimation estimates the cost in siacoins of performing various
	// storage and data operations. The estimation will be done using the provided
	// allowance. The final allowance used will be returned.
	//PriceEstimation(Allowance) (float64, Allowance, error)
}

// MaintenanceSpending is a helper struct that contains a breakdown of costs
// related to the maintenance (a.k.a upkeep) of the RHP3 protocol. This includes
// the costs to sync the account balance, update the price table, etc.
type MaintenanceSpending struct {
	AccountBalanceCost   types.Currency
	FundAccountCost      types.Currency
	UpdatePriceTableCost types.Currency
}

// Add is a convenience function that sums the fields of the spending object
// with the corresponding fields of the given object.
func (x MaintenanceSpending) Add(y MaintenanceSpending) MaintenanceSpending {
	return MaintenanceSpending{
		AccountBalanceCost:   x.AccountBalanceCost.Add(y.AccountBalanceCost),
		FundAccountCost:      x.FundAccountCost.Add(y.FundAccountCost),
		UpdatePriceTableCost: x.UpdatePriceTableCost.Add(y.UpdatePriceTableCost),
	}
}

// Sum is a convenience function that sums up all of the fields in the spending
// object and returns the total as a types.Currency.
func (x MaintenanceSpending) Sum() types.Currency {
	return x.AccountBalanceCost.Add(x.FundAccountCost).Add(x.UpdatePriceTableCost)
}

// ContractUtility contains metrics internal to the contractor that reflect the
// utility of a given contract.
type ContractUtility struct {
	GoodForUpload bool
	GoodForRenew  bool

	// BadContract will be set to true if there's good reason to believe that
	// the contract is unusable and will continue to be unusable. For example,
	// if the host is claiming that the contract does not exist, the contract
	// should be marked as bad.
	BadContract bool
	LastOOSErr  uint64 // OOS means Out Of Storage.

	// If a contract is locked, the utility should not be updated. 'Locked' is a
	// value that gets persisted.
	Locked bool
}

// A RenterContract contains metadata about a file contract. It is read-only;
// modifying a RenterContract does not modify the actual file contract.
type RenterContract struct {
	ID              types.FileContractID
	HostPublicKey   types.PublicKey
	RenterPublicKey types.PublicKey
	Transaction     types.Transaction

	StartHeight uint64
	EndHeight   uint64

	// RenterFunds is the amount remaining in the contract that the renter can
	// spend.
	RenterFunds types.Currency

	// The FileContract does not indicate what funds were spent on, so we have
	// to track the various costs manually.
	DownloadSpending    types.Currency
	FundAccountSpending types.Currency
	MaintenanceSpending MaintenanceSpending
	StorageSpending     types.Currency
	UploadSpending      types.Currency

	// Utility contains utility information about the renter.
	Utility ContractUtility

	// TotalCost indicates the amount of money that the renter spent and/or
	// locked up while forming a contract. This includes fees, and includes
	// funds which were allocated (but not necessarily committed) to spend on
	// uploads/downloads/storage.
	TotalCost types.Currency

	// ContractFee is the amount of money paid to the host to cover potential
	// future transaction fees that the host may incur, and to cover any other
	// overheads the host may have.
	//
	// TxnFee is the amount of money spent on the transaction fee when putting
	// the renter contract on the blockchain.
	//
	// SiafundFee is the amount of money spent on siafund fees when creating the
	// contract. The siafund fee that the renter pays covers both the renter and
	// the host portions of the contract, and therefore can be unexpectedly high
	// if the the host collateral is high.
	ContractFee types.Currency
	TxnFee      types.Currency
	SiafundFee  types.Currency
}

// Size returns the contract size.
func (rc *RenterContract) Size() uint64 {
	var size uint64
	if len(rc.Transaction.FileContractRevisions) != 0 {
		size = rc.Transaction.FileContractRevisions[0].Filesize
	}
	return size
}

// An Allowance dictates how much the renter is allowed to spend in a given
// period. Note that funds are spent on both storage and bandwidth.
type Allowance struct {
	Funds       types.Currency
	Hosts       uint64
	Period      uint64
	RenewWindow uint64

	// ExpectedStorage is the amount of data that we expect to have in a contract.
	ExpectedStorage uint64

	// ExpectedUpload is the expected amount of data uploaded through the API,
	// before redundancy, per block.
	ExpectedUpload uint64

	// ExpectedDownload is the expected amount of data downloaded through the
	// API per block.
	ExpectedDownload uint64

	// Erasure coding parameters.
	MinShards   uint64
	TotalShards uint64

	// The following fields provide price gouging protection for the user. By
	// setting a particular maximum price for each mechanism that a host can use
	// to charge users, the workers know to avoid hosts that go outside of the
	// safety range.
	MaxRPCPrice               types.Currency
	MaxContractPrice          types.Currency
	MaxDownloadBandwidthPrice types.Currency
	MaxSectorAccessPrice      types.Currency
	MaxStoragePrice           types.Currency
	MaxUploadBandwidthPrice   types.Currency
	MinMaxCollateral          types.Currency
	BlockHeightLeeway         uint64
}

// DefaultAllowance is the set of default allowance settings that will be
// used when allowances are not set or not fully set.
var DefaultAllowance = Allowance{
	Funds:       types.HastingsPerSiacoin.Mul64(2500),
	Hosts:       50,
	Period:      2 * BlocksPerMonth,
	RenewWindow: BlocksPerMonth,

	ExpectedStorage:    1e12,                           // 1 TB
	ExpectedUpload:     uint64(200e9) / BlocksPerMonth, // 200 GB per month
	ExpectedDownload:   uint64(100e9) / BlocksPerMonth, // 100 GB per month
	MinShards:          10,
	TotalShards:        30,
	BlockHeightLeeway:  3,
}

// Active returns true if and only if this allowance has been set in the
// contractor.
func (a Allowance) Active() bool {
	return a.Period != 0
}
