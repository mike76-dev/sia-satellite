package api

import (
	"go.sia.tech/core/types"
)

// DaemonVersion holds the version information for satd.
type DaemonVersion struct {
	Version     string `json:"version"`
	GitRevision string `json:"gitRevision"`
	BuildTime   string `json:"buildTime"`
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
