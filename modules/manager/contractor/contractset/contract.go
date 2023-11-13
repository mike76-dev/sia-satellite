package contractset

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// A revisionNumberMismatchError occurs if the host reports a different revision
// number than expected.
type revisionNumberMismatchError struct {
	ours, theirs uint64
}

func (e *revisionNumberMismatchError) Error() string {
	return fmt.Sprintf("our revision number (%v) does not match the host's (%v); the host may be acting maliciously", e.ours, e.theirs)
}

// IsRevisionMismatch returns true if err was caused by the host reporting a
// different revision number than expected.
func IsRevisionMismatch(err error) bool {
	_, ok := err.(*revisionNumberMismatchError)
	return ok
}

// contractHeader holds all the information about a contract apart from the
// sector roots themselves.
type contractHeader struct {
	// Transaction is the signed transaction containing the most recent
	// revision of the file contract.
	Transaction types.Transaction

	// Same as modules.RenterContract.
	StartHeight         uint64
	DownloadSpending    types.Currency
	FundAccountSpending types.Currency
	MaintenanceSpending modules.MaintenanceSpending
	StorageSpending     types.Currency
	UploadSpending      types.Currency
	TotalCost           types.Currency
	ContractFee         types.Currency
	TxnFee              types.Currency
	SiafundFee          types.Currency
	Utility             modules.ContractUtility
	Unlocked            bool
	Imported            bool
}

// validate returns an error if the contractHeader is invalid.
func (h *contractHeader) validate() error {
	if len(h.Transaction.FileContractRevisions) == 0 {
		return errors.New("no file contract revisions")
	}
	if len(h.Transaction.FileContractRevisions[0].ValidProofOutputs) == 0 {
		return errors.New("not enough valid proof outputs")
	}
	if len(h.Transaction.FileContractRevisions[0].UnlockConditions.PublicKeys) != 2 {
		return errors.New("wrong number of pubkeys")
	}
	return nil
}

// copyTransaction creates a deep copy of the txn struct.
func (h *contractHeader) copyTransaction() (txn types.Transaction) {
	txn = modules.CopyTransaction(h.Transaction)
	return
}

// LastRevision returns the last revision of the contract.
func (h *contractHeader) LastRevision() types.FileContractRevision {
	return h.Transaction.FileContractRevisions[0]
}

// ID returns the contract's ID.
func (h *contractHeader) ID() types.FileContractID {
	return h.LastRevision().ParentID
}

// RenterPublicKey returns the renter's public key from the last contract
// revision.
func (h *contractHeader) RenterPublicKey() (rpk types.PublicKey) {
	copy(rpk[:], h.LastRevision().UnlockConditions.PublicKeys[0].Key)
	return
}

// HostPublicKey returns the host's public key from the last contract revision.
func (h *contractHeader) HostPublicKey() (hpk types.PublicKey) {
	copy(hpk[:], h.LastRevision().UnlockConditions.PublicKeys[1].Key)
	return
}

// RenterFunds returns the remaining renter funds as per the last contract
// revision.
func (h *contractHeader) RenterFunds() types.Currency {
	rev := h.LastRevision()
	return rev.ValidRenterPayout()
}

// EndHeight returns the block height of the last constract revision.
func (h *contractHeader) EndHeight() uint64 {
	rev := h.LastRevision()
	return rev.EndHeight()
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

// EncodeTo implements types.EncoderTo.
func (c *FileContract) EncodeTo(e *types.Encoder) {
	c.header.Transaction.EncodeTo(e)
	e.WriteUint64(c.header.StartHeight)
	c.header.DownloadSpending.EncodeTo(e)
	c.header.FundAccountSpending.EncodeTo(e)
	c.header.MaintenanceSpending.AccountBalanceCost.EncodeTo(e)
	c.header.MaintenanceSpending.FundAccountCost.EncodeTo(e)
	c.header.MaintenanceSpending.UpdatePriceTableCost.EncodeTo(e)
	c.header.StorageSpending.EncodeTo(e)
	c.header.UploadSpending.EncodeTo(e)
	c.header.TotalCost.EncodeTo(e)
	c.header.ContractFee.EncodeTo(e)
	c.header.TxnFee.EncodeTo(e)
	c.header.SiafundFee.EncodeTo(e)
	e.WriteBool(c.header.Utility.GoodForUpload)
	e.WriteBool(c.header.Utility.GoodForRenew)
	e.WriteBool(c.header.Utility.BadContract)
	e.WriteUint64(c.header.Utility.LastOOSErr)
	e.WriteBool(c.header.Utility.Locked)
}

// DecodeFrom implements types.DecoderFrom.
func (c *FileContract) DecodeFrom(d *types.Decoder) {
	c.header.Transaction.DecodeFrom(d)
	c.header.StartHeight = d.ReadUint64()
	c.header.DownloadSpending.DecodeFrom(d)
	c.header.FundAccountSpending.DecodeFrom(d)
	c.header.MaintenanceSpending.AccountBalanceCost.DecodeFrom(d)
	c.header.MaintenanceSpending.FundAccountCost.DecodeFrom(d)
	c.header.MaintenanceSpending.UpdatePriceTableCost.DecodeFrom(d)
	c.header.StorageSpending.DecodeFrom(d)
	c.header.UploadSpending.DecodeFrom(d)
	c.header.TotalCost.DecodeFrom(d)
	c.header.ContractFee.DecodeFrom(d)
	c.header.TxnFee.DecodeFrom(d)
	c.header.SiafundFee.DecodeFrom(d)
	c.header.Utility.GoodForUpload = d.ReadBool()
	c.header.Utility.GoodForRenew = d.ReadBool()
	c.header.Utility.BadContract = d.ReadBool()
	c.header.Utility.LastOOSErr = d.ReadUint64()
	c.header.Utility.Locked = d.ReadBool()
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
		Unlocked:            h.Unlocked,
		Imported:            h.Imported,
	}
}

// UpdateUtility updates the utility field of a contract.
func (c *FileContract) UpdateUtility(utility modules.ContractUtility) error {
	// Construct new header.
	c.mu.Lock()
	newHeader := c.header
	newHeader.Utility = utility

	c.header = newHeader
	c.mu.Unlock()

	// Update the database.
	return c.saveContract(types.PublicKey{})
}

// Utility returns the contract utility for the contract.
func (c *FileContract) Utility() modules.ContractUtility {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header.Utility
}

// unlockPayout sets the 'Unlocked' flag to true.
func (c *FileContract) unlockPayout() error {
	c.header.Unlocked = true
	return c.saveContract(types.PublicKey{})
}

// applySetHeader directly makes changes to the contract header.
func (c *FileContract) applySetHeader(h contractHeader) error {
	c.header = h
	return c.saveContract(types.PublicKey{})
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

// Clear commits the changes we made to the revision when clearing a contract.
func (c *FileContract) Clear(txn types.Transaction) error {
	return c.managedCommitClearContract(txn, types.ZeroCurrency)
}

// managedSyncRevision checks whether rev accords with the FileContract's most
// recent revision. If the revisions do not match, and the host's revision is
// ahead of the renter's, managedSyncRevision uses the host's revision.
func (c *FileContract) managedSyncRevision(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update the spending.
	c.header.UploadSpending = c.header.UploadSpending.Add(uploads)
	c.header.DownloadSpending = c.header.DownloadSpending.Add(downloads)
	c.header.FundAccountSpending = c.header.FundAccountSpending.Add(fundAccount)

	// Our current revision should always be signed. If it isn't, we have no
	// choice but to accept the host's revision.
	if len(c.header.Transaction.Signatures) == 0 {
		c.header.Transaction.FileContractRevisions = []types.FileContractRevision{rev}
		c.header.Transaction.Signatures = sigs
		return nil
	}

	ourRev := c.header.LastRevision()

	// If the revision number and Merkle root match, we don't need to do
	// anything.
	if rev.RevisionNumber == ourRev.RevisionNumber && rev.FileMerkleRoot == ourRev.FileMerkleRoot {
		// If any other fields mismatch, it must be our fault, since we signed
		// the revision reported by the host. So, to ensure things are
		// consistent, we blindly overwrite our revision with the host's.
		c.header.Transaction.FileContractRevisions[0] = rev
		if len(sigs) != 0 {
			c.header.Transaction.Signatures = sigs
		}
		return nil
	}

	// The host should never report a lower revision number than ours. If they
	// do, it may mean they are intentionally (and maliciously) trying to
	// "rewind" the contract to an earlier state. Even if the host does not have
	// ill intent, this would mean that they failed to commit one or more
	// revisions to durable storage, which reflects very poorly on them.
	if rev.RevisionNumber < ourRev.RevisionNumber {
		return &revisionNumberMismatchError{ourRev.RevisionNumber, rev.RevisionNumber}
	}

	// The host's revision is still different. At this point, the best we can do
	// is accept their revision. This isn't ideal, but at least there's no
	// security risk, since we *did* sign the revision that the host is
	// claiming. Worst case, certain contract metadata (e.g. UploadSpending)
	// will be incorrect.
	c.header.Transaction.FileContractRevisions[0] = rev
	if len(sigs) != 0 {
		c.header.Transaction.Signatures = sigs
	}
	return c.saveContract(types.PublicKey{})
}

// UpdateContract updates the contract with the new revision.
func (cs *ContractSet) UpdateContract(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	contract, exists := cs.contracts[rev.ParentID]
	if !exists {
		contract, exists = cs.oldContracts[rev.ParentID]
		if !exists {
			return errors.New("contract not found")
		}
	}

	return contract.managedSyncRevision(rev, sigs, uploads, downloads, fundAccount)
}

// managedInsertContract inserts a contract into a set. This will overwrite
// existing contracts of the same name to make sure the update is idempotent.
func (cs *ContractSet) managedInsertContract(h contractHeader, rpk types.PublicKey) (modules.RenterContract, error) {
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
		cs.log.Println("CRITICAL: trying to overwrite existing contract")
	}

	cs.contracts[fc.header.ID()] = fc
	cs.pubKeys[h.RenterPublicKey().String()+h.HostPublicKey().String()] = fc.header.ID()
	cs.mu.Unlock()

	return fc.Metadata(), fc.saveContract(rpk)
}

// InsertContract prepares a new contract header and adds it to the set.
func (cs *ContractSet) InsertContract(revisionTxn types.Transaction, startHeight uint64, totalCost, contractFee, txnFee, siafundFee types.Currency, rpk types.PublicKey, imported bool) (modules.RenterContract, error) {
	header := contractHeader{
		Transaction: revisionTxn,
		StartHeight: startHeight,
		TotalCost:   totalCost,
		ContractFee: contractFee,
		TxnFee:      txnFee,
		SiafundFee:  siafundFee,
		Utility: modules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
		Imported: imported,
	}

	return cs.managedInsertContract(header, rpk)
}
