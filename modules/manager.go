package modules

import (
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
