package modules

import (
	"errors"
	"time"

	"go.sia.tech/core/consensus"
	"go.sia.tech/core/types"
)

var (
	// ErrInsufficientBalance is returned when there aren't enough unused outputs
	// to cover the requested amount.
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// Wallet stores and manages Siacoins.
type Wallet interface {
	// AddressBalance returns the balance of the given address.
	AddressBalance(addr types.Address) (siacoins types.Currency, siafunds uint64)

	// Addresses returns the addresses of the wallet.
	Addresses() (addrs []types.Address)

	// AddWatch adds the given watched address to the wallet.
	AddWatch(addr types.Address) error

	// Annotate annotates a transaction set.
	Annotate(txns []types.Transaction) (ptxns []PoolTransaction)

	// Close shuts down the wallet.
	Close() error

	// ConfirmedBalance returns the total balance of the wallet.
	ConfirmedBalance() (siacoins, immatureSiacoins types.Currency, siafunds uint64)

	// Fund adds Siacoin inputs with the required amount to the transaction.
	Fund(txn *types.Transaction, amount types.Currency) (parents []types.Transaction, toSign []types.Hash256, err error)

	// MarkAddressUnused marks the provided address as unused which causes it to be
	// handed out by a subsequent call to `NextAddresses` again.
	MarkAddressUnused(addrs ...types.UnlockConditions) error

	// MarkWalletInputs scans a transaction and infers which inputs belong to this
	// wallet. This allows those inputs to be signed.
	MarkWalletInputs(txn types.Transaction) (toSign []types.Hash256)

	// NextAddress returns an unlock hash that is ready to receive Siacoins or
	// Siafunds.
	NextAddress() (types.UnlockConditions, error)

	// Release marks the outputs as unused.
	Release(txnSet []types.Transaction)

	// RemoveWatch removes the given watched address from the wallet.
	RemoveWatch(addr types.Address) error

	// Reserve reserves the given ids for the given duration.
	Reserve(ids []types.Hash256, duration time.Duration) error

	// RenterSeed derives a renter seed.
	//RenterSeed(email string) [16]byte

	// SendSiacoins creates a transaction sending 'amount' to 'dest'. The
	// transaction is submitted to the transaction pool and is also returned. Fees
	// are added to the amount sent.
	SendSiacoins(amount types.Currency, dest types.Address) ([]types.Transaction, error)

	// Sign signs the specified transaction using keys derived from the wallet seed.
	Sign(cs consensus.State, txn *types.Transaction, toSign []types.Hash256) error

	// Tip returns the wallet's internal processed chain index.
	Tip() types.ChainIndex

	// UnconfirmedBalance returns the balance of the wallet contained in
	// the unconfirmed transactions.
	UnconfirmedBalance() (outgoing, incoming types.Currency)

	// UnspentSiacoinOutputs returns the unspent SC outputs of the wallet.
	UnspentSiacoinOutputs() (sces []types.SiacoinElement)

	// UnspentSiafundOutputs returns the unspent SF outputs of the wallet.
	UnspentSiafundOutputs() (sfes []types.SiafundElement)

	// WatchedAddresses returns a list of the addresses watched by the wallet.
	WatchedAddresses() (addrs []types.Address)
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
