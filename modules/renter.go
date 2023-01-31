package modules

import (
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

// Renter holds the data related to the specific renter.
type Renter struct {
	Allowance     smodules.Allowance
	CurrentPeriod types.BlockHeight
	PublicKey     types.SiaPublicKey
	Email         string // Link to the user account.
}

// contractEndHeight returns the height at which the renter's contracts
// end.
func (r *Renter) contractEndHeight() types.BlockHeight {
	return r.CurrentPeriod + r.Allowance.Period + r.Allowance.RenewWindow
}
