package modules

import (
	"errors"
	"time"

	"go.sia.tech/core/types"
)

const (
	// PublicKeysPerSeed define the number of public keys that get pregenerated
	// for a seed at startup when searching for balances in the blockchain.
	PublicKeysPerSeed = 2500
)

var (
	// ErrBadEncryptionKey is returned if the incorrect encryption key to a
	// file is provided.
	ErrBadEncryptionKey = errors.New("provided encryption key is incorrect")

	// ErrIncompleteTransactions is returned if the wallet has incomplete
	// transactions being built that are using all of the current outputs, and
	// therefore the wallet is unable to spend money despite it not technically
	// being 'unconfirmed' yet.
	ErrIncompleteTransactions = errors.New("wallet has coins spent in incomplete transactions - not enough remaining coins")

	// ErrLockedWallet is returned when an action cannot be performed due to
	// the wallet being locked.
	ErrLockedWallet = errors.New("wallet must be unlocked before it can be used")

	// ErrLowBalance is returned if the wallet does not have enough funds to
	// complete the desired action.
	ErrLowBalance = errors.New("insufficient balance")

	// ErrWalletShutdown is returned when a method can't continue execution due
	// to the wallet shutting down.
	ErrWalletShutdown = errors.New("wallet is shutting down")
)

type (
	// Seed is cryptographic entropy that is used to derive spendable wallet
	// addresses.
	Seed [16]byte

	// WalletKey is the key used to encrypt the wallet.
	WalletKey []byte

	// WalletTransactionID is a unique identifier for a wallet transaction.
	WalletTransactionID types.Hash256

	// A ProcessedInput represents funding to a transaction. The input is
	// coming from an address and going to the outputs. The fund types are
	// 'SiacoinInput', 'SiafundInput'.
	ProcessedInput struct {
		ParentID       types.Hash256   `json:"parentid"`
		FundType       types.Specifier `json:"fundtype"`
		WalletAddress  bool            `json:"walletaddress"`
		RelatedAddress types.Address   `json:"relatedaddress"`
		Value          types.Currency  `json:"value"`
	}

	// A ProcessedOutput is a Siacoin output that appears in a transaction.
	// Some outputs mature immediately, some are delayed, and some may never
	// mature at all (in the event of storage proofs).
	//
	// Fund type can either be 'SiacoinOutput', 'SiafundOutput', 'ClaimOutput',
	// 'MinerPayout', or 'MinerFee'. All outputs except the miner fee create
	// outputs accessible to an address. Miner fees are not spendable, and
	// instead contribute to the block subsidy.
	//
	// MaturityHeight indicates at what block height the output becomes
	// available. SiacoinInputs and SiafundInputs become available immediately.
	// ClaimInputs and MinerPayouts become available after 144 confirmations.
	ProcessedOutput struct {
		ID             types.Hash256   `json:"id"`
		FundType       types.Specifier `json:"fundtype"`
		MaturityHeight uint64          `json:"maturityheight"`
		WalletAddress  bool            `json:"walletaddress"`
		RelatedAddress types.Address   `json:"relatedaddress"`
		Value          types.Currency  `json:"value"`
	}

	// A ProcessedTransaction is a transaction that has been processed into
	// explicit inputs and outputs and tagged with some header data such as
	// confirmation height + timestamp.
	//
	// Because of the block subsidy, a block is considered as a transaction.
	// Since there is technically no transaction id for the block subsidy, the
	// block id is used instead.
	ProcessedTransaction struct {
		Transaction           types.Transaction   `json:"transaction"`
		TransactionID         types.TransactionID `json:"transactionid"`
		ConfirmationHeight    uint64              `json:"confirmationheight"`
		ConfirmationTimestamp time.Time           `json:"confirmationtimestamp"`

		Inputs  []ProcessedInput  `json:"inputs"`
		Outputs []ProcessedOutput `json:"outputs"`
	}

	// ValuedTransaction is a transaction that has been given incoming and
	// outgoing siacoin value fields.
	ValuedTransaction struct {
		ProcessedTransaction

		ConfirmedIncomingValue types.Currency `json:"confirmedincomingvalue"`
		ConfirmedOutgoingValue types.Currency `json:"confirmedoutgoingvalue"`
	}

	// A UnspentOutput is a SiacoinOutput or SiafundOutput that the wallet
	// is tracking.
	UnspentOutput struct {
		ID                 types.Hash256   `json:"id"`
		FundType           types.Specifier `json:"fundtype"`
		UnlockHash         types.Address   `json:"unlockhash"`
		Value              types.Currency  `json:"value"`
		ConfirmationHeight uint64          `json:"confirmationheight"`
		IsWatchOnly        bool            `json:"iswatchonly"`
	}

	// TransactionBuilder is used to construct transactions.
	TransactionBuilder interface {
		// FundTransaction adds siacoin inputs worth at least the requested
		// amount to the provided transaction. A change output is also added,
		// if necessary. The inputs will not be available to future calls to
		// FundTransaction unless ReleaseInputs is called.
		FundTransaction(txn *types.Transaction, amount types.Currency) (types.Transaction, error)

		// ReleaseInputs is a helper function that releases the inputs of txn
		// for use in other transactions. It should only be called on
		// transactions that are invalid or will never be broadcast.
		ReleaseInputs(txn types.Transaction)
	}

	// EncryptionManager can encrypt, lock, unlock, and indicate the current
	// status of the EncryptionManager.
	EncryptionManager interface {
		// Encrypt will encrypt the wallet using the input key. Upon
		// encryption, a primary seed will be created for the wallet (no seed
		// exists prior to this point). If the key is blank, then the hash of
		// the seed that is generated will be used as the key.
		//
		// Encrypt can only be called once throughout the life of the wallet
		// and will return an error on subsequent calls (even after restarting
		// the wallet). To reset the wallet, the wallet must be deleted.
		Encrypt(masterKey WalletKey) (Seed, error)

		// Reset will reset the wallet, clearing the database and returning it to
		// the unencrypted state. Reset can only be called on a wallet that has
		// already been encrypted.
		Reset() error

		// Encrypted returns whether or not the wallet has been encrypted yet.
		// After being encrypted for the first time, the wallet can only be
		// unlocked using the encryption password.
		Encrypted() (bool, error)

		// InitFromSeed functions like Encrypt, but using a specified seed.
		// Unlike Encrypt, the blockchain will be scanned to determine the
		// seed's progress. For this reason, InitFromSeed should not be called
		// until the blockchain is fully synced.
		InitFromSeed(masterKey WalletKey, seed Seed) error

		// Lock deletes all keys in memory and prevents the wallet from being
		// used to spend coins or extract keys until 'Unlock' is called.
		Lock() error

		// Unlock must be called before the wallet is usable. All wallets and
		// wallet seeds are encrypted by default, and the wallet will not know
		// which addresses to watch for on the blockchain until unlock has been
		// called.
		//
		// All items in the wallet are encrypted using different keys which are
		// derived from the master key.
		Unlock(masterKey WalletKey) error

		// UnlockAsync must be called before the wallet is usable. All wallets and
		// wallet seeds are encrypted by default, and the wallet will not know
		// which addresses to watch for on the blockchain until unlock has been
		// called.
		// UnlockAsync will return a channel as soon as the wallet is unlocked but
		// before the wallet is caught up to consensus.
		//
		// All items in the wallet are encrypted using different keys which are
		// derived from the master key.
		UnlockAsync(masterKey WalletKey) <-chan error

		// ChangeKey changes the wallet's materKey from masterKey to newKey,
		// re-encrypting the wallet with the provided key.
		ChangeKey(masterKey WalletKey, newKey WalletKey) error

		// IsMasterKey verifies that the masterKey is the key used to encrypt
		// the wallet.
		IsMasterKey(masterKey WalletKey) (bool, error)

		// ChangeKeyWithSeed is the same as ChangeKey but uses the primary seed
		// instead of the current masterKey.
		ChangeKeyWithSeed(seed Seed, newKey WalletKey) error

		// Unlocked returns true if the wallet is currently unlocked, false
		// otherwise.
		Unlocked() (bool, error)
	}

	// KeyManager manages wallet keys, including the use of seeds, creating and
	// loading backups, and providing a layer of compatibility for older wallet
	// files.
	KeyManager interface {
		// AllAddresses returns all addresses that the wallet is able to spend
		// from, including unseeded addresses. Addresses are returned sorted in
		// byte-order.
		AllAddresses() ([]types.Address, error)

		// AllSeeds returns all of the seeds that are being tracked by the
		// wallet, including the primary seed. Only the primary seed is used to
		// generate new addresses, but the wallet can spend funds sent to
		// public keys generated by any of the seeds returned.
		AllSeeds() ([]Seed, error)

		// LastAddresses returns the last n addresses starting at the last seedProgress
		// for which an address was generated.
		LastAddresses(n uint64) ([]types.Address, error)

		// LoadSeed will recreate a wallet file using the recovery phrase.
		// LoadSeed only needs to be called if the original seed file or
		// encryption password was lost. The master key is used to encrypt the
		// recovery seed before saving it to disk.
		LoadSeed(WalletKey, Seed) error

		// MarkAddressUnused marks the provided address as unused which causes it to be
		// handed out by a subsequent call to `NextAddresses` again.
		MarkAddressUnused(...types.UnlockConditions) error

		// NextAddress returns a new coin address generated from the
		// primary seed.
		NextAddress() (types.UnlockConditions, error)

		// NextAddresses returns n new coin addresses generated from the
		// primary seed.
		NextAddresses(uint64) ([]types.UnlockConditions, error)

		// PrimarySeed returns the unencrypted primary seed of the wallet,
		// along with a uint64 indicating how many addresses may be safely
		// generated from the seed.
		PrimarySeed() (Seed, uint64, error)

		// SignTransaction adds a signature to each of the specified inputs.
		SignTransaction(txn *types.Transaction, toSign []types.Hash256, cf types.CoveredFields) error

		// SweepSeed scans the blockchain for outputs generated from seed and
		// creates a transaction that transfers them to the wallet. Note that
		// this incurs a transaction fee. It returns the total value of the
		// outputs, minus the fee. If only Siafunds were found, the fee is
		// deducted from the wallet.
		SweepSeed(seed Seed) (coins types.Currency, funds uint64, err error)
	}

	// SiacoinSenderMulti is the minimal interface for an object that can send
	// money to multiple siacoin outputs at once.
	SiacoinSenderMulti interface {
		// SendSiacoinsMulti sends coins to multiple addresses.
		SendSiacoinsMulti(outputs []types.SiacoinOutput) ([]types.Transaction, error)
	}

	// Wallet stores and manages Siacoins and Siafunds. The wallet file is
	// encrypted using a user-specified password. Common addresses are all
	// derived from a single address seed.
	Wallet interface {
		Alerter
		EncryptionManager
		KeyManager
		TransactionBuilder

		// AddUnlockConditions adds a set of UnlockConditions to the wallet database.
		AddUnlockConditions(uc types.UnlockConditions) error

		// AddWatchAddresses instructs the wallet to begin tracking a set of
		// addresses, in addition to the addresses it was previously tracking.
		// If none of the addresses have appeared in the blockchain, the
		// unused flag may be set to true. Otherwise, the wallet must rescan
		// the blockchain to search for transactions containing the addresses.
		AddWatchAddresses(addrs []types.Address, unused bool) error

		// Close permits clean shutdown during testing and serving.
		Close() error

		// ConfirmedBalance returns the confirmed balance of the wallet, minus
		// any outgoing transactions. ConfirmedBalance will include unconfirmed
		// refund transactions.
		ConfirmedBalance() (siacoinBalance types.Currency, siafundBalance uint64, siacoinClaimBalance types.Currency, err error)

		// UnconfirmedBalance returns the unconfirmed balance of the wallet.
		// Outgoing funds and incoming funds are reported separately. Refund
		// outputs are included, meaning that sending a single coin to
		// someone could result in 'outgoing: 12, incoming: 11'. Siafunds are
		// not considered in the unconfirmed balance.
		UnconfirmedBalance() (outgoingSiacoins types.Currency, incomingSiacoins types.Currency, err error)

		// Height returns the wallet's internal processed consensus height.
		Height() (uint64, error)

		// AddressTransactions returns all of the transactions that are related
		// to a given address.
		AddressTransactions(types.Address) ([]ProcessedTransaction, error)

		// AddressUnconfirmedHistory returns all of the unconfirmed
		// transactions related to a given address.
		AddressUnconfirmedTransactions(types.Address) ([]ProcessedTransaction, error)

		// Transaction returns the transaction with the given id. The bool
		// indicates whether the transaction is in the wallet database. The
		// wallet only stores transactions that are related to the wallet.
		Transaction(types.TransactionID) (ProcessedTransaction, bool, error)

		// Transactions returns all of the transactions that were confirmed at
		// heights [startHeight, endHeight]. Unconfirmed transactions are not
		// included.
		Transactions(startHeight uint64, endHeight uint64) ([]ProcessedTransaction, error)

		// UnconfirmedTransactions returns all unconfirmed transactions
		// relative to the wallet.
		UnconfirmedTransactions() ([]ProcessedTransaction, error)

		// RemoveWatchAddresses instructs the wallet to stop tracking a set of
		// addresses and delete their associated transactions. If none of the
		// addresses have appeared in the blockchain, the unused flag may be
		// set to true. Otherwise, the wallet must rescan the blockchain to
		// rebuild its transaction history.
		RemoveWatchAddresses(addrs []types.Address, unused bool) error

		// Rescanning reports whether the wallet is currently rescanning the
		// blockchain.
		Rescanning() (bool, error)

		// Settings returns the Wallet's current settings.
		Settings() (WalletSettings, error)

		// SetSettings sets the Wallet's settings.
		SetSettings(WalletSettings) error

		// SendSiacoins is a tool for sending Siacoins from the wallet to an
		// address. Sending money usually results in multiple transactions. The
		// transactions are automatically given to the transaction pool, and are
		// also returned to the caller.
		SendSiacoins(amount types.Currency, dest types.Address) ([]types.Transaction, error)

		// SendSiacoinsFeeIncluded sends Siacoins with fees included.
		SendSiacoinsFeeIncluded(amount types.Currency, dest types.Address) ([]types.Transaction, error)

		SiacoinSenderMulti

		// DustThreshold returns the quantity per byte below which a Currency is
		// considered to be Dust.
		DustThreshold() (types.Currency, error)

		// UnspentOutputs returns the unspent outputs tracked by the wallet.
		UnspentOutputs() ([]UnspentOutput, error)

		// UnlockConditions returns the UnlockConditions for the specified
		// address, if they are known to the wallet.
		UnlockConditions(addr types.Address) (types.UnlockConditions, error)

		// WatchAddresses returns the set of addresses that the wallet is
		// currently watching.
		WatchAddresses() ([]types.Address, error)
	}

	// WalletSettings control the behavior of the Wallet.
	WalletSettings struct {
		NoDefrag bool `json:"nodefrag"`
	}
)

// EncodeTo implements types.EncoderTo interface.
func (pt * ProcessedTransaction) EncodeTo(e *types.Encoder) {
	pt.Transaction.EncodeTo(e)
	pt.TransactionID.EncodeTo(e)
	e.WriteUint64(pt.ConfirmationHeight)
	e.WriteUint64(uint64(pt.ConfirmationTimestamp.Unix()))
	e.WritePrefix(len(pt.Inputs))
	for _, input := range pt.Inputs {
		input.EncodeTo(e)
	}
	e.WritePrefix(len(pt.Outputs))
	for _, output := range pt.Outputs {
		output.EncodeTo(e)
	}
}

// DecodeFrom implements types.DecoderFrom interface.
func (pt * ProcessedTransaction) DecodeFrom(d *types.Decoder) {
	pt.Transaction.DecodeFrom(d)
	pt.TransactionID.DecodeFrom(d)
	pt.ConfirmationHeight = d.ReadUint64()
	pt.ConfirmationTimestamp = time.Unix(int64(d.ReadUint64()), 0)
	n := d.ReadPrefix()
	pt.Inputs = make([]ProcessedInput, n)
	for i := 0; i < n; i++ {
		pt.Inputs[i].DecodeFrom(d)
	}
	n = d.ReadPrefix()
	pt.Outputs = make([]ProcessedOutput, n)
	for i := 0; i < n; i++ {
		pt.Outputs[i].DecodeFrom(d)
	}
}

// EncodeTo implements types.EncoderTo interface.
func (pi *ProcessedInput) EncodeTo(e *types.Encoder) {
	pi.ParentID.EncodeTo(e)
	pi.FundType.EncodeTo(e)
	e.WriteBool(pi.WalletAddress)
	pi.RelatedAddress.EncodeTo(e)
	pi.Value.EncodeTo(e)
}

// DecodeFrom implements types.DecoderFrom interface.
func (pi *ProcessedInput) DecodeFrom(d *types.Decoder) {
	pi.ParentID.DecodeFrom(d)
	pi.FundType.DecodeFrom(d)
	pi.WalletAddress = d.ReadBool()
	pi.RelatedAddress.DecodeFrom(d)
	pi.Value.DecodeFrom(d)
}

// EncodeTo implements types.EncoderTo interface.
func (po *ProcessedOutput) EncodeTo(e *types.Encoder) {
	po.ID.EncodeTo(e)
	po.FundType.EncodeTo(e)
	e.WriteUint64(po.MaturityHeight)
	e.WriteBool(po.WalletAddress)
	po.RelatedAddress.EncodeTo(e)
	po.Value.EncodeTo(e)
}

// DecodeFrom implements types.DecoderFrom interface.
func (po *ProcessedOutput) DecodeFrom(d *types.Decoder) {
	po.ID.DecodeFrom(d)
	po.FundType.DecodeFrom(d)
	po.MaturityHeight = d.ReadUint64()
	po.WalletAddress = d.ReadBool()
	po.RelatedAddress.DecodeFrom(d)
	po.Value.DecodeFrom(d)
}

// CalculateWalletTransactionID is a helper function for determining the id of
// a wallet transaction.
func CalculateWalletTransactionID(tid types.TransactionID, oid types.Hash256) WalletTransactionID {
	h := types.NewHasher()
	tid.EncodeTo(h.E)
	oid.EncodeTo(h.E)
	return WalletTransactionID(h.Sum())
}
