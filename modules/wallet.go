package modules

import (
	"errors"
	"time"

	"go.sia.tech/core/types"
)

var (
	// ErrInsufficientBalance is returned when there aren't enough unused outputs
	// to cover the requested amount.
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// Wallet stores and manages Siacoins.
type Wallet interface {
	// AddAddress adds the given address to the wallet.
	AddAddress(addr types.Address) error

	// Addresses returns the addresses of the wallet.
	Addresses() (addrs []types.Address)

	// Annotate annotates a transaction set.
	Annotate(txns []types.Transaction) (ptxns []PoolTransaction)

	// Close shuts down the wallet.
	Close() error

	// ConfirmedBalance returns the total balance of the wallet.
	ConfirmedBalance() (siacoins, immatureSiacoins types.Currency, siafunds uint64)

	// Fund adds Siacoin inputs with the required amount to the transaction.
	//Fund(txn *types.Transaction, amount types.Currency) (parents []types.Transaction, toSign []types.Hash256, err error)

	// Key returns the wallet key.
	//Key() types.PrivateKey

	// Release marks the outputs as unused.
	Release(txnSet []types.Transaction)

	// RemoveAddress removes the given address from the wallet.
	RemoveAddress(addr types.Address) error

	// Reserve reserves the given ids for the given duration.
	Reserve(ids []types.Hash256, duration time.Duration) error

	// RenterSeed derives a renter seed.
	//RenterSeed(email string) [16]byte

	// Sign adds signatures corresponding to toSign elements to the transaction.
	//Sign(txn *types.Transaction, toSign []types.Hash256, cf types.CoveredFields) error

	// Tip returns the wallet's internal processed chain index.
	Tip() types.ChainIndex

	// UnspentSiacoinOutputs returns the unspent SC outputs of the wallet.
	UnspentSiacoinOutputs() (sces []types.SiacoinElement)

	// UnspentSiafundOutputs returns the unspent SF outputs of the wallet.
	UnspentSiafundOutputs() (sfes []types.SiafundElement)
}

// A PoolTransaction summarizes the wallet-relevant data in a txpool
// transaction.
type PoolTransaction struct {
	ID       types.TransactionID `json:"id"`
	Raw      types.Transaction   `json:"raw"`
	Type     string              `json:"type"`
	Sent     types.Currency      `json:"sent"`
	Received types.Currency      `json:"received"`
	Locked   types.Currency      `json:"locked"`
}
