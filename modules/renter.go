package modules

import (
	"gitlab.com/NebulousLabs/fastrand"

	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// RecoverableContract is a types.FileContract as it appears on the blockchain
// with additional fields which contain the information required to recover its
// latest revision from a host.
type RecoverableContract struct {
	types.FileContract
	// ID is the FileContract's ID.
	ID types.FileContractID `json:"id"`
	// RenterPublicKey is the public key of the renter that formed this contract.
	RenterPublicKey types.SiaPublicKey `json:"renterpublickey"`
	// HostPublicKey is the public key of the host we formed this contract
	// with.
	HostPublicKey types.SiaPublicKey `json:"hostpublickey"`
	// InputParentID is the ParentID of the first SiacoinInput of the
	// transaction that contains this contract.
	InputParentID types.SiacoinOutputID `json:"inputparentid"`
	// StartHeight is the estimated startheight of a recoverable contract.
	StartHeight types.BlockHeight `json:"startheight"`
	// TxnFee of the transaction which contains the contract.
	TxnFee types.Currency `json:"txnfee"`
}

// A RenterContract contains metadata about a file contract. It is read-only;
// modifying a RenterContract does not modify the actual file contract.
type RenterContract struct {
	ID              types.FileContractID
	HostPublicKey   types.SiaPublicKey
	RenterPublicKey types.SiaPublicKey
	Transaction     types.Transaction

	StartHeight types.BlockHeight
	EndHeight   types.BlockHeight

	// RenterFunds is the amount remaining in the contract that the renter can
	// spend.
	RenterFunds types.Currency

	// The FileContract does not indicate what funds were spent on, so we have
	// to track the various costs manually.
	DownloadSpending    types.Currency
	FundAccountSpending types.Currency
	MaintenanceSpending smodules.MaintenanceSpending
	StorageSpending     types.Currency
	UploadSpending      types.Currency

	// Utility contains utility information about the renter.
	Utility smodules.ContractUtility

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
		size = rc.Transaction.FileContractRevisions[0].NewFileSize
	}
	return size
}

// RenterSettings keep the opt-in settings of the renter.
type RenterSettings struct {
	AutoRenewContracts bool `json:"autorenew"`
}

// Renter holds the data related to the specific renter.
type Renter struct {
	Allowance     Allowance          `json:"allowance"`
	CurrentPeriod types.BlockHeight  `json:"currentperiod"`
	PublicKey     types.SiaPublicKey `json:"publickey"`
	Email         string             `json:"email"` // Link to the user account.
	Settings      RenterSettings     `json:"settings"`
	PrivateKey    crypto.SecretKey   `json:"privatekey"`
}

// contractEndHeight returns the height at which the renter's contracts
// end.
func (r *Renter) ContractEndHeight() types.BlockHeight {
	return r.CurrentPeriod + r.Allowance.Period + r.Allowance.RenewWindow
}

// DeriveRenterSeed derives a seed to be used by the renter for accessing the
// file contracts.
// NOTE: The seed returned by this function should be wiped once it's no longer
// in use.
func DeriveRenterSeed(walletSeed smodules.Seed, email string) smodules.RenterSeed {
	var renterSeed smodules.RenterSeed
	rs := crypto.HashBytes(append(walletSeed[:], []byte(email)...))
	defer fastrand.Read(rs[:])
	copy(renterSeed[:], rs[:])
	return renterSeed
}

// DeriveEphemeralKey derives a secret key to be used by the renter for the
// exchange with the hosts.
func DeriveEphemeralKey(rsk crypto.SecretKey, hpk types.SiaPublicKey) crypto.SecretKey {
	ers := crypto.HashBytes(append(rsk[:], hpk.Key...))
	defer fastrand.Read(ers[:])
	esk, _ := GenerateKeyPair(ers)
	return esk
}

// GenerateKeyPair generates a private/public keypair from a seed.
func GenerateKeyPair(seed crypto.Hash) (sk crypto.SecretKey, pk crypto.PublicKey) {
	xsk := core.NewPrivateKeyFromSeed(seed[:])
	defer fastrand.Read(seed[:])
	copy(sk[:], xsk[:])
	xpk := sk.PublicKey()
	copy(pk[:], xpk[:])
	return
}

// An Allowance dictates how much the renter is allowed to spend in a given
// period. Note that funds are spent on both storage and bandwidth.
type Allowance struct {
	Funds       types.Currency    `json:"funds"`
	Hosts       uint64            `json:"hosts"`
	Period      types.BlockHeight `json:"period"`
	RenewWindow types.BlockHeight `json:"renewwindow"`

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
	BlockHeightLeeway         types.BlockHeight `json:"blockheightleeway"`
}

// DefaultAllowance is the set of default allowance settings that will be
// used when allowances are not set or not fully set.
var DefaultAllowance = Allowance{
	Funds:       types.SiacoinPrecision.Mul64(2500),
	Hosts:       50,
	Period:      2 * types.BlocksPerMonth,
	RenewWindow: types.BlocksPerMonth,

	ExpectedStorage:    1e12,                                         // 1 TB
	ExpectedUpload:     uint64(200e9) / uint64(types.BlocksPerMonth), // 200 GB per month
	ExpectedDownload:   uint64(100e9) / uint64(types.BlocksPerMonth), // 100 GB per month
	MinShards:          10,
	TotalShards:        30,
	BlockHeightLeeway:  types.BlockHeight(3),
}

// Active returns true if and only if this allowance has been set in the
// contractor.
func (a Allowance) Active() bool {
	return a.Period != 0
}

// ContractParams are supplied as an argument to FormContracts.
type ContractParams struct {
	Allowance      Allowance
	Host           smodules.HostDBEntry
	Funding        types.Currency
	StartHeight    types.BlockHeight
	EndHeight      types.BlockHeight
	RefundAddress  types.UnlockHash
	PublicKey      types.SiaPublicKey
	SecretKey      crypto.SecretKey
}
