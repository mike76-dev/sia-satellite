package modules

import (
	"fmt"
	"io"
	"time"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

// HostAverages contains the host network averages from HostDB.
type HostAverages struct {
	NumHosts               uint64         `json:"numhosts"`
	Duration               uint64         `json:"duration"`
	StoragePrice           types.Currency `json:"storageprice"`
	Collateral             types.Currency `json:"collateral"`
	DownloadBandwidthPrice types.Currency `json:"downloadbandwidthprice"`
	UploadBandwidthPrice   types.Currency `json:"uploadbandwidthprice"`
	ContractPrice          types.Currency `json:"contractprice"`
	BaseRPCPrice           types.Currency `json:"baserpcprice"`
	SectorAccessPrice      types.Currency `json:"sectoraccessprice"`
}

// EncodeTo implements types.EncoderTo.
func (ha *HostAverages) EncodeTo(e *types.Encoder) {
	e.WriteUint64(ha.NumHosts)
	e.WriteUint64(ha.Duration)
	ha.StoragePrice.EncodeTo(e)
	ha.Collateral.EncodeTo(e)
	ha.DownloadBandwidthPrice.EncodeTo(e)
	ha.UploadBandwidthPrice.EncodeTo(e)
	ha.ContractPrice.EncodeTo(e)
	ha.BaseRPCPrice.EncodeTo(e)
	ha.SectorAccessPrice.EncodeTo(e)
}

// DecodeFrom implements types.DecoderFrom.
func (ha *HostAverages) DecodeFrom(d *types.Decoder) {
	ha.NumHosts = d.ReadUint64()
	ha.Duration = d.ReadUint64()
	ha.StoragePrice.DecodeFrom(d)
	ha.Collateral.DecodeFrom(d)
	ha.DownloadBandwidthPrice.DecodeFrom(d)
	ha.UploadBandwidthPrice.DecodeFrom(d)
	ha.ContractPrice.DecodeFrom(d)
	ha.BaseRPCPrice.DecodeFrom(d)
	ha.SectorAccessPrice.DecodeFrom(d)
}

// UserBalance holds the current balance as well as
// the data on the chosen payment scheme.
type UserBalance struct {
	IsUser     bool    `json:"isuser"`
	IsRenter   bool    `json:"isrenter"`
	Subscribed bool    `json:"subscribed"`
	Balance    float64 `json:"balance"`
	Locked     float64 `json:"locked"`
	Currency   string  `json:"currency"`
	SCRate     float64 `json:"scrate"`
	StripeID   string  `json:"stripeid"`
	Invoice    string  `json:"-"`
	OnHold     uint64  `json:"-"`
}

// UserSpendings contains the spendings in the current and the
// previous months.
type UserSpendings struct {
	CurrentLocked         float64 `json:"currentlocked"`
	CurrentUsed           float64 `json:"currentused"`
	CurrentOverhead       float64 `json:"currentoverhead"`
	PrevLocked            float64 `json:"prevlocked"`
	PrevUsed              float64 `json:"prevused"`
	PrevOverhead          float64 `json:"prevoverhead"`
	SCRate                float64 `json:"scrate"`
	CurrentFormed         uint64  `json:"currentformed"`
	CurrentRenewed        uint64  `json:"currentrenewed"`
	CurrentSlabsSaved     uint64  `json:"currentslabssaved"`
	CurrentSlabsRetrieved uint64  `json:"currentslabsretrieved"`
	CurrentSlabsMigrated  uint64  `json:"currentslabsmigrated"`
	PrevFormed            uint64  `json:"prevformed"`
	PrevRenewed           uint64  `json:"prevrenewed"`
	PrevSlabsSaved        uint64  `json:"prevslabssaved"`
	PrevSlabsRetrieved    uint64  `json:"prevslabsretrieved"`
	PrevSlabsMigrated     uint64  `json:"prevslabsmigrated"`
}

// HostScoreBreakdown breaks down the host scores.
type HostScoreBreakdown struct {
	Score            types.Currency `json:"score"`
	Age              float64        `json:"agescore"`
	Collateral       float64        `json:"collateralscore"`
	Interactions     float64        `json:"interactionscore"`
	StorageRemaining float64        `json:"storageremainingscore"`
	Uptime           float64        `json:"uptimescore"`
	Version          float64        `json:"versionscore"`
	Prices           float64        `json:"pricescore"`
}

// A HostDBEntry represents one host entry in the Manager's host DB. It
// aggregates the host's external settings and metrics with its public key.
type HostDBEntry struct {
	// Need to wrap rhpv2.HostSettings in a separate named field due to
	// issues with JSON encoding.
	Settings   rhpv2.HostSettings   `json:"settings"`
	PriceTable rhpv3.HostPriceTable `json:"pricetable"`

	// FirstSeen is the first block height at which this host was announced.
	FirstSeen uint64 `json:"firstseen"`

	// LastAnnouncement is the last block height at which this host was announced.
	LastAnnouncement uint64 `json:"lastannouncement"`

	// Measurements that have been taken on the host. The most recent
	// measurements are kept in full detail, historic ones are compressed into
	// the historic values.
	HistoricDowntime time.Duration `json:"historicdowntime"`
	HistoricUptime   time.Duration `json:"historicuptime"`
	ScanHistory      HostDBScans   `json:"scanhistory"`

	// Measurements that are taken whenever we interact with a host.
	HistoricFailedInteractions     float64 `json:"historicfailedinteractions"`
	HistoricSuccessfulInteractions float64 `json:"historicsuccessfulinteractions"`
	RecentFailedInteractions       float64 `json:"recentfailedinteractions"`
	RecentSuccessfulInteractions   float64 `json:"recentsuccessfulinteractions"`

	LastHistoricUpdate uint64 `json:"lasthistoricupdate"`

	// Measurements related to the IP subnet mask.
	IPNets          []string  `json:"ipnets"`
	LastIPNetChange time.Time `json:"lastipnetchange"`

	// The public key of the host, stored separately to minimize risk of certain
	// MitM based vulnerabilities.
	PublicKey types.PublicKey `json:"publickey"`

	// Filtered says whether or not a HostDBEntry is being filtered out of the
	// filtered hosttree due to the filter mode of the hosttree.
	Filtered bool `json:"filtered"`
}

// HostDBScan represents a single scan event.
type HostDBScan struct {
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
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

	// AcceptContracts accepts a set of contracts from the renter
	// and adds them to the contract set.
	AcceptContracts(types.PublicKey, []ContractMetadata)

	// ActiveHosts returns an array of HostDB's active hosts.
	ActiveHosts() ([]HostDBEntry, error)

	// AllHosts returns an array of all hosts.
	AllHosts() ([]HostDBEntry, error)

	// BlockHeight returns the current block height.
	BlockHeight() uint64

	// BufferedFilesDir returns the path to the buffered files directory.
	BufferedFilesDir() string

	// BytesUploaded returns the size of the file already uploaded.
	BytesUploaded(types.PublicKey, string, string) (string, uint64, error)

	// Close safely shuts down the manager.
	Close() error

	// Contracts returns storage contracts.
	Contracts() []RenterContract

	// ContractsByRenter returns storage contracts filtered by the renter.
	ContractsByRenter(types.PublicKey) []RenterContract

	// CreateNewRenter inserts a new renter into the map.
	CreateNewRenter(string, types.PublicKey)

	// DeleteBufferedFile deletes the specified file and the associated
	// database record.
	DeleteBufferedFile(pk types.PublicKey, bucket, path string) error

	// DeleteBufferedFiles deletes the files waiting to be uploaded.
	DeleteBufferedFiles(types.PublicKey) error

	// DeleteMetadata deletes the renter's saved file metadata.
	DeleteMetadata(types.PublicKey) error

	// DeleteObject deletes the saved file metadata object.
	DeleteObject(types.PublicKey, string, string) error

	// DeleteRenter deletes the renter data from the memory.
	DeleteRenter(string)

	// DownloadObject downloads an object and returns it.
	DownloadObject(io.Writer, types.PublicKey, string, string) error

	// FeeEstimation returns the minimum and the maximum estimated fees for
	// a transaction.
	FeeEstimation() (min, max types.Currency)

	// Filter returns the HostDB's filterMode and filteredHosts.
	Filter() (FilterMode, map[string]types.PublicKey, []string, error)

	// FormContract forms a contract with the specified host.
	FormContract(*RPCSession, types.PublicKey, types.PublicKey, types.PublicKey, uint64, uint64, uint64, uint64, uint64, uint64) (RenterContract, error)

	// FormContracts forms the required number of contracts with the hosts.
	FormContracts(types.PublicKey, types.PrivateKey, Allowance) ([]RenterContract, error)

	// GetAverages retrieves the host network averages.
	GetAverages() HostAverages

	// GetBalance retrieves the balance information on the account.
	GetBalance(string) (UserBalance, error)

	// GetBufferSize returns the total size of the temporary files.
	GetBufferSize() (uint64, error)

	// GetEmailPreferences returns the email preferences.
	GetEmailPreferences() (string, types.Currency)

	// GetExchangeRate returns the exchange rate of a given currency.
	GetExchangeRate(string) (float64, error)

	// GetModifiedSlabs returns the slabs modified since the last retrieval.
	GetModifiedSlabs(types.PublicKey) ([]Slab, error)

	// GetRenter returns the renter by the public key.
	GetRenter(types.PublicKey) (Renter, error)

	// GetSiacoinRate calculates the SC price in a given currency.
	GetSiacoinRate(string) (float64, error)

	// GetSpendings retrieves the user's spendings.
	GetSpendings(string) (UserSpendings, error)

	// GetWalletSeed returns the wallet seed.
	GetWalletSeed() (Seed, error)

	// Host returns the host associated with the given public key.
	Host(types.PublicKey) (HostDBEntry, bool, error)

	// IncrementStats increments the number of formed or renewed contracts.
	IncrementStats(string, bool) error

	// InitialScanComplete returns a boolean indicating if the initial scan of the
	// HostDB is completed.
	InitialScanComplete() (bool, uint64, error)

	// LockSiacoins moves a part of the balance to "locked".
	LockSiacoins(string, float64) error

	// Maintenance returns true if the maintenance mode is active.
	Maintenance() bool

	// OldContracts returns the contracts that have expired.
	OldContracts() []RenterContract

	// OldContractsByRenter returns expired contracts filtered by the renter.
	OldContractsByRenter(types.PublicKey) []RenterContract

	// RandomHosts picks up to the specified number of random hosts from the
	// hostdb sorted by weight.
	RandomHosts(uint64, Allowance) ([]HostDBEntry, error)

	// RefreshedContract returns a bool indicating if the contract was refreshed.
	RefreshedContract(types.FileContractID) bool

	// RegisterUpload associates the uploaded file with the object.
	RegisterUpload(types.PublicKey, string, string, string, bool) error

	// RenewContract renews a contract.
	RenewContract(*RPCSession, types.PublicKey, types.FileContractID, uint64, uint64, uint64, uint64, uint64, uint64) (RenterContract, error)

	// RenewContracts renews a set of contracts and returns a new set.
	RenewContracts(types.PublicKey, types.PrivateKey, Allowance, []types.FileContractID) ([]RenterContract, error)

	// RenewedFrom returns the ID of the contract the given contract was renewed
	// from, if any.
	RenewedFrom(types.FileContractID) types.FileContractID

	// Renters retrieves the list of renters.
	Renters() []Renter

	// RetrieveMetadata retrieves the file metadata from the database.
	RetrieveMetadata(types.PublicKey, []BucketFiles) ([]FileMetadata, error)

	// RetrieveSpendings retrieves the user's spendings.
	RetrieveSpendings(string, string) (UserSpendings, error)

	// ScoreBreakdown returns the score breakdown of the specific host.
	ScoreBreakdown(HostDBEntry) (HostScoreBreakdown, error)

	// SetAllowance sets the renter's allowance.
	SetAllowance(types.PublicKey, Allowance) error

	// SetEmailPreferences changes the email preferences.
	SetEmailPreferences(string, types.Currency) error

	// SetFilterMode sets the HostDB's filter mode.
	SetFilterMode(FilterMode, []types.PublicKey, []string) error

	// StartMaintenance switches the maintenance mode on and off.
	StartMaintenance(bool) error

	// UnlockSiacoins moves a part of the amount from "locked" to "available",
	// while the other part (fees and other spent funds) is "burned".
	UnlockSiacoins(string, float64, float64, uint64) error

	// UpdateBalance updates the balance information on the account.
	UpdateBalance(string, UserBalance) error

	// UpdateContract updates the contract with the new revision and spending
	// details.
	UpdateContract(types.FileContractRevision, []types.TransactionSignature, types.Currency, types.Currency, types.Currency) error

	// UpdateMetadata updates the file metadata in the database.
	UpdateMetadata(types.PublicKey, FileMetadata) error

	// UpdatePrices updates the pricing table in the database.
	UpdatePrices(Pricing) error

	// UpdateRenterSettings updates the renter's opt-in settings.
	UpdateRenterSettings(types.PublicKey, RenterSettings, types.PrivateKey, types.PrivateKey) error

	// UpdateSlab updates a file slab after a successful migration.
	UpdateSlab(types.PublicKey, Slab, bool) error

	// UpdateSpendings updates the user's spendings.
	UpdateSpendings(string, UserSpendings) error
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

// SpendingDetails is a helper struct that contains a breakdown of where exactly
// the money was spent. The MaintenanceSpending field is an aggregate of costs
// spent on RHP3 maintenance, this includes updating the price table, syncing
// the account balance, etc.
type SpendingDetails struct {
	DownloadSpending    types.Currency
	FundAccountSpending types.Currency
	MaintenanceSpending MaintenanceSpending
	StorageSpending     types.Currency
	UploadSpending      types.Currency
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

	// Imported indicates if the contract was imported via RPCShareContracts.
	Imported bool

	// Unlocked indicates if the contract payout has been unlocked.
	Unlocked bool
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

	// UploadPacking indicates whether the upload packing is turned on.
	UploadPacking bool
}

// DefaultAllowance is the set of default allowance settings that will be
// used when allowances are not set or not fully set.
var DefaultAllowance = Allowance{
	Funds:       types.HastingsPerSiacoin.Mul64(2500),
	Hosts:       50,
	Period:      2 * BlocksPerMonth,
	RenewWindow: BlocksPerMonth,

	ExpectedStorage:   1e12,                           // 1 TB
	ExpectedUpload:    uint64(200e9) / BlocksPerMonth, // 200 GB per month
	ExpectedDownload:  uint64(100e9) / BlocksPerMonth, // 100 GB per month
	MinShards:         10,
	TotalShards:       30,
	BlockHeightLeeway: 3,
}

// Active returns true if and only if this allowance has been set in the
// contractor.
func (a Allowance) Active() bool {
	return a.Period != 0
}

// EncodeTo implements types.EncoderTo.
func (a *Allowance) EncodeTo(e *types.Encoder) {
	a.Funds.EncodeTo(e)
	e.WriteUint64(a.Hosts)
	e.WriteUint64(a.Period)
	e.WriteUint64(a.RenewWindow)
	e.WriteUint64(a.ExpectedStorage)
	e.WriteUint64(a.ExpectedUpload)
	e.WriteUint64(a.ExpectedDownload)
	e.WriteUint64(a.MinShards)
	e.WriteUint64(a.TotalShards)
	a.MaxRPCPrice.EncodeTo(e)
	a.MaxContractPrice.EncodeTo(e)
	a.MaxDownloadBandwidthPrice.EncodeTo(e)
	a.MaxSectorAccessPrice.EncodeTo(e)
	a.MaxStoragePrice.EncodeTo(e)
	a.MaxUploadBandwidthPrice.EncodeTo(e)
	a.MinMaxCollateral.EncodeTo(e)
	e.WriteUint64(a.BlockHeightLeeway)
	e.WriteBool(a.UploadPacking)
}

// DecodeFrom implements types.DecoderFrom.
func (a *Allowance) DecodeFrom(d *types.Decoder) {
	a.Funds.DecodeFrom(d)
	a.Hosts = d.ReadUint64()
	a.Period = d.ReadUint64()
	a.RenewWindow = d.ReadUint64()
	a.ExpectedStorage = d.ReadUint64()
	a.ExpectedUpload = d.ReadUint64()
	a.ExpectedDownload = d.ReadUint64()
	a.MinShards = d.ReadUint64()
	a.TotalShards = d.ReadUint64()
	a.MaxRPCPrice.DecodeFrom(d)
	a.MaxContractPrice.DecodeFrom(d)
	a.MaxDownloadBandwidthPrice.DecodeFrom(d)
	a.MaxSectorAccessPrice.DecodeFrom(d)
	a.MaxStoragePrice.DecodeFrom(d)
	a.MaxUploadBandwidthPrice.DecodeFrom(d)
	a.MinMaxCollateral.DecodeFrom(d)
	a.BlockHeightLeeway = d.ReadUint64()
	a.UploadPacking = d.ReadBool()
}

// ContractWatchStatus provides information about the status of a contract in
// the manager's watchdog.
type ContractWatchStatus struct {
	Archived                  bool
	FormationSweepHeight      uint64
	ContractFound             bool
	LatestRevisionFound       uint64
	StorageProofFoundAtHeight uint64
	DoubleSpendHeight         uint64
	WindowStart               uint64
	WindowEnd                 uint64
}

// RenterSpending contains the metrics about how much the renter has
// spent during the current billing period.
type RenterSpending struct {
	// ContractFees are the sum of all fees in the contract. This means it
	// includes the ContractFee, TxnFee and SiafundFee
	ContractFees types.Currency
	// DownloadSpending is the money currently spent on downloads.
	DownloadSpending types.Currency
	// FundAccountSpending is the money used to fund an ephemeral account on the
	// host.
	FundAccountSpending types.Currency
	// MaintenanceSpending is the money spent on maintenance tasks in support of
	// the RHP3 protocol, this includes updating the price table as well as
	// syncing the ephemeral account balance.
	MaintenanceSpending MaintenanceSpending
	// StorageSpending is the money currently spent on storage.
	StorageSpending types.Currency
	// ContractSpending is the total amount of money that the renter has put
	// into contracts, whether it's locked and the renter gets that money
	// back or whether it's spent and the renter won't get the money back.
	TotalAllocated types.Currency
	// UploadSpending is the money currently spent on uploads.
	UploadSpending types.Currency
	// Unspent is locked-away, unspent money.
	Unspent types.Currency
	// WithheldFunds are the funds from the previous period that are tied up
	// in contracts and have not been released yet
	WithheldFunds types.Currency
	// ReleaseBlock is the block at which the WithheldFunds should be
	// released to the renter, based on worst case.
	// Contract End Height + Host Window Size + Maturity Delay
	ReleaseBlock uint64
	// PreviousSpending is the total spend funds from old contracts
	// that are not included in the current period spending
	PreviousSpending types.Currency
}

// SpendingBreakdown provides a breakdown of a few fields in the
// RenterSpending.
func (rs RenterSpending) SpendingBreakdown() (totalSpent, unspentAllocated, unspentUnallocated types.Currency) {
	totalSpent = rs.ContractFees.Add(rs.UploadSpending).
		Add(rs.DownloadSpending).Add(rs.StorageSpending)
	// Calculate unspent allocated.
	if rs.TotalAllocated.Cmp(totalSpent) >= 0 {
		unspentAllocated = rs.TotalAllocated.Sub(totalSpent)
	}
	// Calculate unspent unallocated.
	if rs.Unspent.Cmp(unspentAllocated) >= 0 {
		unspentUnallocated = rs.Unspent.Sub(unspentAllocated)
	}
	return totalSpent, unspentAllocated, unspentUnallocated
}

// RenterSettings keep the opt-in settings of the renter.
type RenterSettings struct {
	AutoRenewContracts bool `json:"autorenew"`
	BackupFileMetadata bool `json:"backupmetadata"`
	AutoRepairFiles    bool `json:"autorepair"`
	ProxyUploads       bool `json:"proxyuploads"`
}

// Renter holds the data related to the specific renter.
type Renter struct {
	Allowance     Allowance
	CurrentPeriod uint64
	PublicKey     types.PublicKey
	Email         string // Link to the user account.
	Settings      RenterSettings
	PrivateKey    types.PrivateKey
	AccountKey    types.PrivateKey
}

// contractEndHeight returns the height at which the renter's contracts
// end.
func (r *Renter) ContractEndHeight() uint64 {
	return r.CurrentPeriod + r.Allowance.Period + r.Allowance.RenewWindow
}

// FileMetadata contains the uploaded file metadata.
type FileMetadata struct {
	Key      types.Hash256 `json:"key"`
	Bucket   string        `json:"bucket"`
	Path     string        `json:"path"`
	ETag     string        `json:"etag"`
	MimeType string        `json:"mime"`
	Slabs    []Slab        `json:"slabs"`
	Data     []byte        `json:"data"`
}

// Slab is a collection of shards.
type Slab struct {
	Key       types.Hash256 `json:"key"`
	MinShards uint8         `json:"minShards"`
	Offset    uint64        `json:"offset"`
	Length    uint64        `json:"length"`
	Partial   bool          `json:"partial"`
	Shards    []Shard       `json:"shards"`
}

// Shard represents an individual shard.
type Shard struct {
	Host types.PublicKey `json:"host"`
	Root types.Hash256   `json:"root"`
}

// EncodeTo implements types.ProtocolObject.
func (ss *Shard) EncodeTo(e *types.Encoder) {
	ss.Host.EncodeTo(e)
	ss.Root.EncodeTo(e)
}

// DecodeFrom implements types.ProtocolObject.
func (ss *Shard) DecodeFrom(d *types.Decoder) {
	ss.Host.DecodeFrom(d)
	ss.Root.DecodeFrom(d)
}

// EncodeTo implements types.ProtocolObject.
func (s *Slab) EncodeTo(e *types.Encoder) {
	s.Key.EncodeTo(e)
	e.WriteUint64(uint64(s.MinShards))
	e.WriteUint64(s.Offset)
	e.WriteUint64(s.Length)
	e.WriteBool(s.Partial)
	e.WritePrefix(len(s.Shards))
	for _, ss := range s.Shards {
		ss.EncodeTo(e)
	}
}

// DecodeFrom implements types.ProtocolObject.
func (s *Slab) DecodeFrom(d *types.Decoder) {
	s.Key.DecodeFrom(d)
	s.MinShards = uint8(d.ReadUint64())
	s.Offset = d.ReadUint64()
	s.Length = d.ReadUint64()
	s.Partial = d.ReadBool()
	s.Shards = make([]Shard, d.ReadPrefix())
	for i := 0; i < len(s.Shards); i++ {
		s.Shards[i].DecodeFrom(d)
	}
}

// EncodeTo implements types.ProtocolObject.
func (fm *FileMetadata) EncodeTo(e *types.Encoder) {
	fm.Key.EncodeTo(e)
	e.WriteString(fm.Bucket)
	e.WriteString(fm.Path)
	e.WriteString(fm.ETag)
	e.WriteString(fm.MimeType)
	e.WritePrefix(len(fm.Slabs))
	for _, s := range fm.Slabs {
		s.EncodeTo(e)
	}
	e.WriteBytes(fm.Data)
}

// DecodeFrom implements types.ProtocolObject.
func (fm *FileMetadata) DecodeFrom(d *types.Decoder) {
	fm.Key.DecodeFrom(d)
	fm.Bucket = d.ReadString()
	fm.Path = d.ReadString()
	fm.ETag = d.ReadString()
	fm.MimeType = d.ReadString()
	fm.Slabs = make([]Slab, d.ReadPrefix())
	for i := 0; i < len(fm.Slabs); i++ {
		fm.Slabs[i].DecodeFrom(d)
	}
	fm.Data = d.ReadBytes()
}

// BucketFiles contains a list of filepaths within a single bucket.
type BucketFiles struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

// EncodeTo implements types.ProtocolObject.
func (bf *BucketFiles) EncodeTo(e *types.Encoder) {
	e.WriteString(bf.Name)
	e.WritePrefix(len(bf.Paths))
	for _, p := range bf.Paths {
		e.WriteString(p)
	}
}

// DecodeFrom implements types.ProtocolObject.
func (bf *BucketFiles) DecodeFrom(d *types.Decoder) {
	bf.Name = d.ReadString()
	bf.Paths = make([]string, d.ReadPrefix())
	for i := 0; i < len(bf.Paths); i++ {
		bf.Paths[i] = d.ReadString()
	}
}
