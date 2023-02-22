package proto

import (
	"database/sql"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/encoding"
	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// contractHeader holds all the information about a contract apart from the
// sector roots themselves.
type contractHeader struct {
	// Transaction is the signed transaction containing the most recent
	// revision of the file contract.
	Transaction types.Transaction

	// SecretKey is the key used by the renter to sign the file contract
	// transaction.
	SecretKey crypto.SecretKey

	// Same as modules.RenterContract.
	StartHeight         types.BlockHeight
	DownloadSpending    types.Currency
	FundAccountSpending types.Currency
	MaintenanceSpending smodules.MaintenanceSpending
	StorageSpending     types.Currency
	UploadSpending      types.Currency
	TotalCost           types.Currency
	ContractFee         types.Currency
	TxnFee              types.Currency
	SiafundFee          types.Currency
	Utility             smodules.ContractUtility
}

// validate returns an error if the contractHeader is invalid.
func (h *contractHeader) validate() error {
	if len(h.Transaction.FileContractRevisions) == 0 {
		return errors.New("no file contract revisions")
	}
	if len(h.Transaction.FileContractRevisions[0].NewValidProofOutputs) == 0 {
		return errors.New("not enough valid proof outputs")
	}
	if len(h.Transaction.FileContractRevisions[0].UnlockConditions.PublicKeys) != 2 {
		return errors.New("wrong number of pubkeys")
	}
	return nil
}

// copyTransaction creates a deep copy of the txn struct.
func (h *contractHeader) copyTransaction() (txn types.Transaction) {
	encoding.Unmarshal(encoding.Marshal(h.Transaction), &txn)
	return
}

// LastRevision returns the last revision of the contract.
func (h *contractHeader) LastRevision() types.FileContractRevision {
	return h.Transaction.FileContractRevisions[0]
}

// ID returns the contract's ID.
func (h *contractHeader) ID() types.FileContractID {
	return h.LastRevision().ID()
}

// RenterPublicKey returns the renter's public key from the last contract
// revision.
func (h *contractHeader) RenterPublicKey() types.SiaPublicKey {
	return h.LastRevision().UnlockConditions.PublicKeys[0]
}

// HostPublicKey returns the host's public key from the last contract revision.
func (h *contractHeader) HostPublicKey() types.SiaPublicKey {
	return h.LastRevision().HostPublicKey()
}

// RenterFunds returns the remaining renter funds as per the last contract
// revision.
func (h *contractHeader) RenterFunds() types.Currency {
	return h.LastRevision().ValidRenterPayout()
}

// EndHeight returns the block height of the last constract revision.
func (h *contractHeader) EndHeight() types.BlockHeight {
	return h.LastRevision().EndHeight()
}

// A FileContract contains the most recent revision transaction negotiated
// with a host, and the secret key used to sign it.
type FileContract struct {
	header contractHeader
	mu     sync.Mutex
	db     *sql.DB
	// revisionMu serializes revisions to the contract. It is acquired by
	// (ContractSet).Acquire and released by (ContractSet).Return. When holding
	// revisionMu, it is still necessary to lock mu when modifying fields
	// of the FileContract.
	revisionMu sync.Mutex
}

// CommitPaymentIntent will commit the intent to pay a host for an rpc by
// committing the signed txn in the contract's header.
func (c *FileContract) CommitPaymentIntent(signedTxn types.Transaction, amount types.Currency, details smodules.SpendingDetails) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Construct new header.
	newHeader := c.header
	newHeader.Transaction = signedTxn
	newHeader.FundAccountSpending = newHeader.FundAccountSpending.Add(details.FundAccountSpending)
	newHeader.MaintenanceSpending = newHeader.MaintenanceSpending.Add(details.MaintenanceSpending)

	c.header = newHeader

	// Update the database.
	return c.saveContract()
}

// LastRevision returns the most recent revision.
func (c *FileContract) LastRevision() types.FileContractRevision {
	c.mu.Lock()
	h := c.header
	c.mu.Unlock()
	return h.LastRevision()
}

// Metadata returns the metadata of a renter contract.
func (c *FileContract) Metadata() modules.RenterContract {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.header
	return modules.RenterContract{
		ID:                  h.ID(),
		Transaction:         h.copyTransaction(),
		RenterPublicKey:     h.RenterPublicKey(),
		HostPublicKey:       h.HostPublicKey(),
		StartHeight:         h.StartHeight,
		EndHeight:           h.EndHeight(),
		RenterFunds:         h.RenterFunds(),
		DownloadSpending:    h.DownloadSpending,
		FundAccountSpending: h.FundAccountSpending,
		MaintenanceSpending: h.MaintenanceSpending,
		StorageSpending:     h.StorageSpending,
		UploadSpending:      h.UploadSpending,
		TotalCost:           h.TotalCost,
		ContractFee:         h.ContractFee,
		TxnFee:              h.TxnFee,
		SiafundFee:          h.SiafundFee,
		Utility:             h.Utility,
	}
}

// PublicKey returns the public key capable of verifying the renter's signature
// on a contract.
func (c *FileContract) PublicKey() crypto.PublicKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header.SecretKey.PublicKey()
}

// RecordPaymentIntent will records the changes we are about to make to the
// revision in order to pay a host for an RPC.
func (c *FileContract) RecordPaymentIntent(rev types.FileContractRevision, amount types.Currency, details smodules.SpendingDetails) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	newHeader := c.header
	newHeader.Transaction.FileContractRevisions = []types.FileContractRevision{rev}
	newHeader.Transaction.TransactionSignatures = nil
	newHeader.FundAccountSpending = newHeader.FundAccountSpending.Add(details.FundAccountSpending)
	newHeader.MaintenanceSpending = newHeader.MaintenanceSpending.Add(details.MaintenanceSpending)

	c.header = newHeader

	// Update the database.
	return c.saveContract()
}

// Sign will sign the given hash using the FileContract's secret key.
func (c *FileContract) Sign(hash crypto.Hash) crypto.Signature {
	c.mu.Lock()
	defer c.mu.Unlock()
	return crypto.SignHash(hash, c.header.SecretKey)
}

// UpdateUtility updates the utility field of a contract.
func (c *FileContract) UpdateUtility(utility smodules.ContractUtility) error {
	// Construct new header.
	c.mu.Lock()
	newHeader := c.header
	newHeader.Utility = utility

	c.header = newHeader
	c.mu.Unlock()

	// Update the database.
	return c.saveContract()
}

// Utility returns the contract utility for the contract.
func (c *FileContract) Utility() smodules.ContractUtility {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header.Utility
}

// applySetHeader directly makes changes to the contract header.
func (c *FileContract) applySetHeader(h contractHeader) error {
	c.header = h
	return c.saveContract()
}

// managedCommitAppend applies the contract update based on the provided
// signedTxn. This is necessary if we run into a desync of contract
// revisions between renter and host.
func (c *FileContract) managedCommitAppend(signedTxn types.Transaction, storageCost, bandwidthCost types.Currency) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Construct new header.
	newHeader := c.header
	newHeader.Transaction = signedTxn
	newHeader.StorageSpending = newHeader.StorageSpending.Add(storageCost)
	newHeader.UploadSpending = newHeader.UploadSpending.Add(bandwidthCost)

	return c.applySetHeader(newHeader)
}

// managedCommitDownload applies the contract update based on the provided
// signedTxn. See managedCommitAppend.
func (c *FileContract) managedCommitDownload(signedTxn types.Transaction, bandwidthCost types.Currency) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Construct new header.
	newHeader := c.header
	newHeader.Transaction = signedTxn
	newHeader.DownloadSpending = newHeader.DownloadSpending.Add(bandwidthCost)

	return c.applySetHeader(newHeader)
}

// managedCommitClearContract commits the changes we made to the revision when
// clearing a contract.
func (c *FileContract) managedCommitClearContract(signedTxn types.Transaction, bandwidthCost types.Currency) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Construct new header.
	newHeader := c.header
	newHeader.Transaction = signedTxn
	newHeader.UploadSpending = newHeader.UploadSpending.Add(bandwidthCost)
	newHeader.Utility.GoodForRenew = false
	newHeader.Utility.GoodForUpload = false
	newHeader.Utility.Locked = true

	return c.applySetHeader(newHeader)
}

// managedSyncRevision checks whether rev accords with the FileContract's most
// recent revision. If the revisions do not match, and the host's revision is
// ahead of the renter's, managedSyncRevision uses the host's revision.
func (c *FileContract) managedSyncRevision(rev types.FileContractRevision, sigs []types.TransactionSignature) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Our current revision should always be signed. If it isn't, we have no
	// choice but to accept the host's revision.
	if len(c.header.Transaction.TransactionSignatures) == 0 {
		c.header.Transaction.FileContractRevisions = []types.FileContractRevision{rev}
		c.header.Transaction.TransactionSignatures = sigs
		return nil
	}

	ourRev := c.header.LastRevision()

	// If the revision number and Merkle root match, we don't need to do
	// anything.
	if rev.NewRevisionNumber == ourRev.NewRevisionNumber && rev.NewFileMerkleRoot == ourRev.NewFileMerkleRoot {
		// If any other fields mismatch, it must be our fault, since we signed
		// the revision reported by the host. So, to ensure things are
		// consistent, we blindly overwrite our revision with the host's.
		c.header.Transaction.FileContractRevisions[0] = rev
		c.header.Transaction.TransactionSignatures = sigs
		return nil
	}

	// The host should never report a lower revision number than ours. If they
	// do, it may mean they are intentionally (and maliciously) trying to
	// "rewind" the contract to an earlier state. Even if the host does not have
	// ill intent, this would mean that they failed to commit one or more
	// revisions to durable storage, which reflects very poorly on them.
	if rev.NewRevisionNumber < ourRev.NewRevisionNumber {
		return &revisionNumberMismatchError{ourRev.NewRevisionNumber, rev.NewRevisionNumber}
	}

	// The host's revision is still different. At this point, the best we can do
	// is accept their revision. This isn't ideal, but at least there's no
	// security risk, since we *did* sign the revision that the host is
	// claiming. Worst case, certain contract metadata (e.g. UploadSpending)
	// will be incorrect.
	c.header.Transaction.FileContractRevisions[0] = rev
	c.header.Transaction.TransactionSignatures = sigs

	return c.saveContract()
}

// managedInsertContract inserts a contract into a set. This will overwrite
// existing contracts of the same name to make sure the update is idempotent.
func (cs *ContractSet) managedInsertContract(h contractHeader) (modules.RenterContract, error) {
	// Validate header.
	if err := h.validate(); err != nil {
		return modules.RenterContract{}, err
	}

	fc := &FileContract{
		header: h,
		db:     cs.db,
	}

	// Check if this contract already exists in the set.
	cs.mu.Lock()
	if _, exists := cs.contracts[fc.header.ID()]; exists {
		cs.log.Println("CRITICAL: Trying to overwrite existing contract")
	}

	cs.contracts[fc.header.ID()] = fc
	cs.pubKeys[h.RenterPublicKey().String() + h.HostPublicKey().String()] = fc.header.ID()
	cs.mu.Unlock()
	
	return fc.Metadata(), fc.saveContract()
}

// loadFileContract loads a contract and adds it to the contractset if it is valid.
func (cs *ContractSet) loadFileContract(fcid types.FileContractID) (err error) {
	header, err := loadContract(fcid, cs.db)
	if err != nil {
		return errors.AddContext(err, "unable to load contract header")
	}

	if err := header.validate(); err != nil {
		return errors.AddContext(err, "invalid contract header")
	}

	// Add to the set.
	fc := &FileContract{
		header: header,
		db:     cs.db,
	}

	if _, exists := cs.contracts[fcid]; exists {
		cs.log.Println("CRITICAL: Trying to overwrite existing contract")
	}
	cs.contracts[fcid] = fc

	return nil
}
