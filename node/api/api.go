package api

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"go.sia.tech/core/types"
)

// DaemonVersion holds the version information for satd.
type DaemonVersion struct {
	Version     string `json:"version"`
	GitRevision string `json:"gitRevision"`
	BuildTime   string `json:"buildTime"`
}

// SyncerPeer contains the information about a peer.
type SyncerPeer struct {
	Address string `json:"address"`
	Version string `json:"version"`
	Inbound bool   `json:"inbound"`
}

// ConsensusTipResponse is the response type for /consensus/tip.
type ConsensusTipResponse struct {
	Height  uint64        `json:"height"`
	BlockID types.BlockID `json:"id"`
	Synced  bool          `json:"synced"`
}

// TxpoolBroadcastRequest is the request type for /txpool/broadcast.
type TxpoolBroadcastRequest struct {
	Transactions   []types.Transaction   `json:"transactions"`
	V2Transactions []types.V2Transaction `json:"v2transactions"`
}

// TxpoolTransactionsResponse is the response type for /txpool/transactions.
type TxpoolTransactionsResponse struct {
	Transactions   []types.Transaction   `json:"transactions"`
	V2Transactions []types.V2Transaction `json:"v2transactions"`
}

// WalletBalanceResponse is the response type for /wallet/balance.
type WalletBalanceResponse struct {
	Height           uint64         `json:"height"`
	Siacoins         types.Currency `json:"siacoins"`
	ImmatureSiacoins types.Currency `json:"immatureSiacoins"`
	IncomingSiacoins types.Currency `json:"incomingSiacoins"`
	OutgoingSiacoins types.Currency `json:"outgoingSiacoins"`
	Siafunds         uint64         `json:"siafunds"`
	RecommendedFee   types.Currency `json:"recommendedFee"`
}

// WalletOutputsResponse is the response type for /wallet/outputs.
type WalletOutputsResponse struct {
	SiacoinOutputs []types.SiacoinElement `json:"siacoinOutputs"`
	SiafundOutputs []types.SiafundElement `json:"siafundOutputs"`
}

// WalletSendRequest is the request type for /wallet/send.
type WalletSendRequest struct {
	Amount      types.Currency `json:"amount"`
	Destination types.Address  `json:"destination"`
}

// ExchangeRate contains the exchange rate of a given currency.
type ExchangeRate struct {
	Currency string  `json:"currency"`
	Rate     float64 `json:"rate"`
}

// HostAverages contains the host network averages.
type HostAverages struct {
	modules.HostAverages
	Rate float64 `json:"rate"`
}

// Renter contains information about the renter.
type Renter struct {
	Email     string          `json:"email"`
	PublicKey types.PublicKey `json:"publickey"`
}

// RentersGET contains the list of the renters.
type RentersGET struct {
	Renters []Renter `json:"renters"`
}

// RenterContract represents a contract formed by the renter.
type RenterContract struct {
	// Amount of contract funds that have been spent on downloads.
	DownloadSpending types.Currency `json:"downloadspending"`
	// Block height that the file contract ends on.
	EndHeight uint64 `json:"endheight"`
	// Fees paid in order to form the file contract.
	Fees types.Currency `json:"fees"`
	// Amount of contract funds that have been spent on funding an ephemeral
	// account on the host.
	FundAccountSpending types.Currency `json:"fundaccountspending"`
	// Public key of the renter that formed the contract.
	RenterPublicKey types.PublicKey `json:"renterpublickey"`
	// Public key of the host the contract was formed with.
	HostPublicKey types.PublicKey `json:"hostpublickey"`
	// HostVersion is the version of Sia that the host is running.
	HostVersion string `json:"hostversion"`
	// ID of the file contract.
	ID types.FileContractID `json:"id"`
	// A signed transaction containing the most recent contract revision.
	LastTransaction types.Transaction `json:"lasttransaction"`
	// Amount of contract funds that have been spent on maintenance tasks
	// such as updating the price table or syncing the ephemeral account
	// balance.
	MaintenanceSpending modules.MaintenanceSpending `json:"maintenancespending"`
	// Address of the host the file contract was formed with.
	NetAddress string `json:"netaddress"`
	// Remaining funds left to spend on uploads & downloads.
	RenterFunds types.Currency `json:"renterfunds"`
	// Size of the file contract, which is typically equal to the number of
	// bytes that have been uploaded to the host.
	Size uint64 `json:"size"`
	// Block height that the file contract began on.
	StartHeight uint64 `json:"startheight"`
	// Amount of contract funds that have been spent on storage.
	StorageSpending types.Currency `json:"storagespending"`
	// Total cost to the wallet of forming the file contract.
	TotalCost types.Currency `json:"totalcost"`
	// Amount of contract funds that have been spent on uploads.
	UploadSpending types.Currency `json:"uploadspending"`
	// Signals if contract is good for uploading data.
	GoodForUpload bool `json:"goodforupload"`
	// Signals if contract is good for a renewal.
	GoodForRenew bool `json:"goodforrenew"`
	// Signals if a contract has been marked as bad.
	BadContract bool `json:"badcontract"`
}

// RenterContracts contains the renter's contracts.
type RenterContracts struct {
	ActiveContracts           []RenterContract `json:"activecontracts"`
	PassiveContracts          []RenterContract `json:"passivecontracts"`
	RefreshedContracts        []RenterContract `json:"refreshedcontracts"`
	DisabledContracts         []RenterContract `json:"disabledcontracts"`
	ExpiredContracts          []RenterContract `json:"expiredcontracts"`
	ExpiredRefreshedContracts []RenterContract `json:"expiredrefreshedcontracts"`
}

// EmailPreferences contains the email preferences.
type EmailPreferences struct {
	Email         string         `json:"email"`
	WarnThreshold types.Currency `json:"threshold"`
}

// ExtendedHostDBEntry is an extension to modules.HostDBEntry that includes
// the string representation of the public key.
type ExtendedHostDBEntry struct {
	modules.HostDBEntry
	PublicKeyString string `json:"publickeystring"`
}

// HostdbHostsGET lists active hosts on the network.
type HostdbHostsGET struct {
	Hosts []ExtendedHostDBEntry `json:"hosts"`
}

// HostdbHostGET lists detailed statistics for a particular host, selected
// by pubkey.
type HostdbHostGET struct {
	Entry          ExtendedHostDBEntry        `json:"entry"`
	ScoreBreakdown modules.HostScoreBreakdown `json:"scorebreakdown"`
}

// HostdbGET holds information about the hostdb.
type HostdbGET struct {
	BlockHeight         uint64 `json:"blockheight"`
	InitialScanComplete bool   `json:"initialscancomplete"`
}

// HostdbFilterModeGET contains the information about the HostDB's
// filtermode.
type HostdbFilterModeGET struct {
	FilterMode   string   `json:"filtermode"`
	Hosts        []string `json:"hosts"`
	NetAddresses []string `json:"netaddresses"`
}

// HostdbFilterModePOST contains the information needed to set the the
// FilterMode of the hostDB.
type HostdbFilterModePOST struct {
	FilterMode   string            `json:"filtermode"`
	Hosts        []types.PublicKey `json:"hosts"`
	NetAddresses []string          `json:"netaddresses"`
}

// Announcement contains the information about a portal announcement.
type Announcement struct {
	Text    string `json:"text"`
	Expires uint64 `json:"expires"`
}
