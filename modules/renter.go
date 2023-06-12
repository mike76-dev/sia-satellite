package modules

import (
	"crypto/ed25519"
	"fmt"
	"time"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

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

// RenterSettings keep the opt-in settings of the renter.
type RenterSettings struct {
	AutoRenewContracts bool `json:"autorenew"`
}

// Renter holds the data related to the specific renter.
type Renter struct {
	Allowance     Allowance        `json:"allowance"`
	CurrentPeriod uint64           `json:"currentperiod"`
	PublicKey     types.PublicKey  `json:"publickey"`
	Email         string           `json:"email"` // Link to the user account.
	Settings      RenterSettings   `json:"settings"`
	PrivateKey    types.PrivateKey `json:"privatekey"`
}

// contractEndHeight returns the height at which the renter's contracts
// end.
func (r *Renter) ContractEndHeight() uint64 {
	return r.CurrentPeriod + r.Allowance.Period + r.Allowance.RenewWindow
}

// DeriveRenterSeed derives a seed to be used by the renter for accessing the
// file contracts.
// NOTE: The seed returned by this function should be wiped once it's no longer
// in use.
func DeriveRenterSeed(walletSeed []byte, email string) []byte {
	renterSeed := make([]byte, ed25519.SeedSize)
	rs := types.HashBytes(append(walletSeed, []byte(email)...))
	defer frand.Read(rs[:])
	copy(renterSeed, rs[:])
	return renterSeed
}

// DeriveEphemeralKey derives a secret key to be used by the renter for the
// exchange with the hosts.
func DeriveEphemeralKey(rsk types.PrivateKey, hpk types.PublicKey) types.PrivateKey {
	ers := types.HashBytes(append(rsk, hpk[:]...))
	defer frand.Read(ers[:])
	esk, _ := GenerateKeyPair(ers)
	return esk
}

// GenerateKeyPair generates a private/public keypair from a seed.
func GenerateKeyPair(seed types.Hash256) (sk types.PrivateKey, pk types.PublicKey) {
	xsk := types.NewPrivateKeyFromSeed(seed[:])
	defer frand.Read(seed[:])
	copy(sk, xsk)
	xpk := sk.PublicKey()
	copy(pk[:], xpk[:])
	return
}

// An Allowance dictates how much the renter is allowed to spend in a given
// period. Note that funds are spent on both storage and bandwidth.
type Allowance struct {
	Funds       types.Currency    `json:"funds"`
	Hosts       uint64            `json:"hosts"`
	Period      uint64            `json:"period"`
	RenewWindow uint64            `json:"renewwindow"`

	// ExpectedStorage is the amount of data that we expect to have in a contract.
	ExpectedStorage uint64 `json:"expectedstorage"`

	// ExpectedUpload is the expected amount of data uploaded through the API,
	// before redundancy, per block.
	ExpectedUpload uint64 `json:"expectedupload"`

	// ExpectedDownload is the expected amount of data downloaded through the
	// API per block.
	ExpectedDownload uint64 `json:"expecteddownload"`

	// Erasure coding parameters.
	MinShards   uint64 `json:"minshards"`
	TotalShards uint64 `json:"totalshards"`

	// The following fields provide price gouging protection for the user. By
	// setting a particular maximum price for each mechanism that a host can use
	// to charge users, the workers know to avoid hosts that go outside of the
	// safety range.
	MaxRPCPrice               types.Currency    `json:"maxrpcprice"`
	MaxContractPrice          types.Currency    `json:"maxcontractprice"`
	MaxDownloadBandwidthPrice types.Currency    `json:"maxdownloadbandwidthprice"`
	MaxSectorAccessPrice      types.Currency    `json:"maxsectoraccessprice"`
	MaxStoragePrice           types.Currency    `json:"maxstorageprice"`
	MaxUploadBandwidthPrice   types.Currency    `json:"maxuploadbandwidthprice"`
	MinMaxCollateral          types.Currency    `json:"minmaxcollateral"`
	BlockHeightLeeway         uint64            `json:"blockheightleeway"`
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

// ContractParams are supplied as an argument to FormContracts.
type ContractParams struct {
	Host           HostDBEntry
	Funding        types.Currency
	StartHeight    uint64
	EndHeight      uint64
	RefundAddress  types.Address
	Collateral     types.Currency
	PublicKey      types.PublicKey
	SecretKey      types.PrivateKey
}

// A HostDBEntry represents one host entry in the satellite's host DB. It
// aggregates the host's external settings and metrics with its public key.
type HostDBEntry struct {
	rhpv2.HostSettings

	// FirstSeen is the last block height at which this host was announced.
	FirstSeen uint64 `json:"firstseen"`

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

// HostScoreBreakdown provides a piece-by-piece explanation of why a host has
// the score that they do.
type HostScoreBreakdown struct {
	Score          types.Currency `json:"score"`
	ConversionRate float64        `json:"conversionrate"`

	AcceptContractAdjustment   float64 `json:"acceptcontractadjustment"`
	AgeAdjustment              float64 `json:"ageadjustment"`
	BasePriceAdjustment        float64 `json:"basepriceadjustment"`
	BurnAdjustment             float64 `json:"burnadjustment"`
	CollateralAdjustment       float64 `json:"collateraladjustment"`
	DurationAdjustment         float64 `json:"durationadjustment"`
	InteractionAdjustment      float64 `json:"interactionadjustment"`
	PriceAdjustment            float64 `json:"pricesmultiplier,siamismatch"`
	StorageRemainingAdjustment float64 `json:"storageremainingadjustment"`
	UptimeAdjustment           float64 `json:"uptimeadjustment"`
	VersionAdjustment          float64 `json:"versionadjustment"`
}

// FilterMode is the helper type for the enum constants for the HostDB filter
// mode.
type FilterMode int

// HostDBFilterError HostDBDisableFilter HostDBActivateBlacklist and
// HostDBActiveWhitelist are the constants used to enable and disable the filter
// mode of the satellite's hostdb.
const (
	HostDBFilterError FilterMode = iota
	HostDBDisableFilter
	HostDBActivateBlacklist
	HostDBActiveWhitelist
)

// String returns the string value for the FilterMode
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

// FromString assigned the FilterMode from the provide string
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

// MaintenanceSpending is a helper struct that contains a breakdown of costs
// related to the maintenance (a.k.a upkeep) of the RHP3 protocol. This includes
// the costs to sync the account balance, update the price table, etc.
type MaintenanceSpending struct {
	AccountBalanceCost   types.Currency `json:"accountbalancecost"`
	FundAccountCost      types.Currency `json:"fundaccountcost"`
	UpdatePriceTableCost types.Currency `json:"updatepricetablecost"`
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
	GoodForUpload bool `json:"goodforupload"`
	GoodForRenew  bool `json:"goodforrenew"`

	// BadContract will be set to true if there's good reason to believe that
	// the contract is unusable and will continue to be unusable. For example,
	// if the host is claiming that the contract does not exist, the contract
	// should be marked as bad.
	BadContract bool   `json:"badcontract"`
	LastOOSErr  uint64 `json:"lastooserr"` // OOS means Out Of Storage

	// If a contract is locked, the utility should not be updated. 'Locked' is a
	// value that gets persisted.
	Locked bool `json:"locked"`
}
