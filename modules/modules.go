package modules

import (
	"go.sia.tech/core/types"
	siad "go.sia.tech/siad/modules"
)

// HostAverages contains the host network averages from HostDB.
type HostAverages struct {
	NumHosts               uint64         `json:"numhosts"`
	Duration               uint64         `json:"height"`
	StoragePrice           types.Currency `json:"storageprice"`
	Collateral             types.Currency `json:"collateral"`
	DownloadBandwidthPrice types.Currency `json:"downloadprice"`
	UploadBandwidthPrice   types.Currency `json:"uploadprice"`
	ContractPrice          types.Currency `json:"contractprice"`
	BaseRPCPrice           types.Currency `json:"baserpcprice"`
	SectorAccessPrice      types.Currency `json:"sectoraccessprice"`
}

// userBalance holds the current balance as well as
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

// CreditData contains the information about any running promotion.
type CreditData struct {
	Amount    float64 `json:"amount"`
	Remaining uint64  `json:"remaining"`
}

// Satellite implements the methods necessary to communicate both with the
// renters and the hosts.
type Satellite interface {
	siad.Alerter

	// ActiveHosts provides the list of hosts that the manager is selecting,
	// sorted by preference.
	ActiveHosts() ([]HostDBEntry, error)

	// AllHosts returns the full list of hosts known to the manager.
	AllHosts() ([]HostDBEntry, error)

	// Close safely shuts down the satellite.
	Close() error

	// DeleteRenter deletes the renter data from the memory.
	DeleteRenter(string)

	// EstimateHostScore will return the score for a host with the provided
	// settings, assuming perfect age and uptime adjustments.
	EstimateHostScore(HostDBEntry, Allowance) (HostScoreBreakdown, error)

	// Filter returns the hostdb's filterMode and filteredHosts.
	Filter() (FilterMode, map[string]types.PublicKey, []string, error)

	// SetFilterMode sets the hostdb's filter mode.
	SetFilterMode(FilterMode, []types.PublicKey, []string) error

	// Host provides the DB entry and score breakdown for the requested host.
	Host(types.PublicKey) (HostDBEntry, bool, error)

	// InitialScanComplete returns a boolean indicating if the initial scan of
	// the hostdb is completed.
	InitialScanComplete() (bool, uint64, error)

	// ScoreBreakdown will return the score for a host db entry using the
	// hostdb's weighting algorithm.
	ScoreBreakdown(HostDBEntry) (HostScoreBreakdown, error)

	// RandomHosts picks up to the specified number of random hosts from the
	// hostdb sorted by weight.
	RandomHosts(uint64, Allowance) ([]HostDBEntry, error)

	// PublicKey returns the satellite's public key.
	PublicKey() types.PublicKey

	// SecretKey returns the satellite's secret key.
	SecretKey() types.PrivateKey

	// GetAverages retrieves the host network averages.
	GetAverages() HostAverages

	// FeeEstimation returns the minimum and the maximum estimated fees for
	// a transaction.
	FeeEstimation() (min, max types.Currency)

	// GetWalletSeed returns the wallet seed.
	GetWalletSeed() ([]byte, error)

	// GetRenter returns the renter by the public key.
	GetRenter(types.PublicKey) (Renter, error)

	// Renters retrieves the list of renters.
	Renters() []Renter

	// GetBalance retrieves the balance information on the account.
	GetBalance(string) (*UserBalance, error)

	// Contracts returns storage contracts.
	Contracts() []RenterContract

	// ContractsByRenter returns storage contracts filtered by the renter.
	ContractsByRenter(types.PublicKey) []RenterContract

	// RefreshedContract returns a bool indicating if the contract was refreshed.
	RefreshedContract(types.FileContractID) bool

	// OldContracts returns the contracts that have expired.
	OldContracts() []RenterContract

	// OldContractsByRenter returns expired contracts filtered by the renter.
	OldContractsByRenter(types.PublicKey) []RenterContract

	// RetrieveSpendings retrieves the user's spendings.
	RetrieveSpendings(string, string) (*UserSpendings, error)
}

// Manager implements the methods necessary to communicate with the
// hosts.
type Manager interface {
	siad.Alerter

	// Close safely shuts down the manager.
	Close() error

	// PriceEstimation estimates the cost in siacoins of performing various
	// storage and data operations. The estimation will be done using the provided
	// allowance. The final allowance used will be returned.
	PriceEstimation(Allowance) (float64, Allowance, error)
}

// Provider implements the methods necessary to communicate with the
// renters.
type Provider interface {
	siad.Alerter

	// Close safely shuts down the provider.
	Close() error
}

// Portal implements the portal server.
type Portal interface {
	siad.Alerter

	// Close safely shuts down the portal.
	Close() error

	// GetCredits retrieves the credits data.
	GetCredits() CreditData

	// SetCredits updates the credit data.
	SetCredits(CreditData)
}

// A HostDB is a database of hosts that the manager can use for figuring out
// who to upload to, and download from.
type HostDB interface {
	siad.Alerter

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
	EstimateHostScore(HostDBEntry, Allowance) (HostScoreBreakdown, error)

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

	// RandomHostsWithLimits works as RandomHostsWithAllowance but uses the
	// limits set in the allowance instead of calculating the weight function.
	RandomHostsWithLimits(int, []types.PublicKey, []types.PublicKey, Allowance) ([]HostDBEntry, error)

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

// FundLocker is the minimal interface needed to lock and unlock funds.
type FundLocker interface {
	// LockSiacoins moves a part of the balance to "locked".
	LockSiacoins(string, float64) error

	// UnlockSiacoins moves a part of the amount from "locked" to "available",
	// while the other part (fees and other spent funds) is "burned".
	UnlockSiacoins(string, float64, float64, uint64) error

	// GetBalance retrieves the balance information on the account.
	GetBalance(string) (*UserBalance, error)

	// IncrementStats increments the number of formed or renewed contracts.
	IncrementStats(string, bool) error
}

// ContractFormer is the minimal interface to be used by Provider.
type ContractFormer interface {
	PublicKey() types.PublicKey
	SecretKey() types.PrivateKey
	UserExists(types.PublicKey) (bool, error)
	FormContracts(types.PublicKey, types.PrivateKey, Allowance) ([]RenterContract, error)
	RenewContracts(types.PublicKey, types.PrivateKey, Allowance, []types.FileContractID) ([]RenterContract, error)
	UpdateContract(types.FileContractRevision, []types.TransactionSignature, types.Currency, types.Currency, types.Currency) error
	GetRenter(types.PublicKey) (Renter, error)
	ContractsByRenter(types.PublicKey) []RenterContract
	OldContractsByRenter(types.PublicKey) []RenterContract
	WalletSeed() ([]byte, error)
	RenewedFrom(types.FileContractID) types.FileContractID
	BlockHeight() uint64
	FormContract(*RPCSession, types.PublicKey, types.PublicKey, types.PublicKey, uint64, uint64, uint64, uint64, uint64, uint64) (RenterContract, error)
	RenewContract(*RPCSession, types.PublicKey, types.FileContractID, uint64, uint64, uint64, uint64, uint64, uint64) (RenterContract, error)
	UpdateRenterSettings(types.PublicKey, RenterSettings, types.PrivateKey) error
	SetAllowance(types.PublicKey, Allowance) error
}
