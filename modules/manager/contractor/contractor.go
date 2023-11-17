package contractor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/contractor/contractset"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"
)

var (
	errNilDB      = errors.New("cannot create contractor with nil database")
	errNilCS      = errors.New("cannot create contractor with nil consensus set")
	errNilHDB     = errors.New("cannot create contractor with nil HostDB")
	errNilManager = errors.New("cannot create contractor with nil manager")
	errNilTpool   = errors.New("cannot create contractor with nil transaction pool")
	errNilWallet  = errors.New("cannot create contractor with nil wallet")

	errHostNotFound     = errors.New("host not found")
	errContractNotFound = errors.New("contract not found")
)

// A Contractor negotiates, revises, renews, and provides access to file
// contracts.
type Contractor struct {
	// Dependencies.
	cs            modules.ConsensusSet
	m             modules.Manager
	db            *sql.DB
	hdb           modules.HostDB
	log           *persist.Logger
	mu            sync.RWMutex
	staticAlerter *modules.GenericAlerter
	tg            siasync.ThreadGroup
	tpool         modules.TransactionPool
	wallet        modules.Wallet

	// Only one thread should be performing contract maintenance at a time.
	interruptMaintenance chan struct{}
	maintenanceLock      siasync.TryMutex

	blockHeight uint64
	synced      chan struct{}
	lastChange  modules.ConsensusChangeID

	renters map[types.PublicKey]modules.Renter

	numFailedRenews map[types.FileContractID]uint64
	renewing        map[types.FileContractID]bool // Prevent revising during renewal.

	// pubKeysToContractID is a map of renter and host pubkeys to the latest contract ID
	// that is formed with the host. The contract also has to have an end height
	// in the future.
	pubKeysToContractID map[string]types.FileContractID

	// renewedFrom links the new contract's ID to the old contract's ID
	// renewedTo links the old contract's ID to the new contract's ID
	// doubleSpentContracts keep track of all contracts that were double spent by
	// either the renter or host.
	staticContracts      *contractset.ContractSet
	doubleSpentContracts map[types.FileContractID]uint64
	renewedFrom          map[types.FileContractID]types.FileContractID
	renewedTo            map[types.FileContractID]types.FileContractID

	dm             *downloadManager
	um             *uploadManager
	migrator       *migrator
	staticWatchdog *watchdog
}

// Allowance returns the current allowance of the renter specified.
func (c *Contractor) Allowance(rpk types.PublicKey) modules.Allowance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	renter, exists := c.renters[rpk]
	if !exists {
		return modules.Allowance{}
	}
	return renter.Allowance
}

// PeriodSpending returns the amount spent by the renter on contracts during
// the current billing period.
func (c *Contractor) PeriodSpending(rpk types.PublicKey) (modules.RenterSpending, error) {
	// Check if we know this renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return modules.RenterSpending{}, ErrRenterNotFound
	}

	allContracts := c.staticContracts.ByRenter(rpk)
	c.mu.RLock()
	defer c.mu.RUnlock()

	var spending modules.RenterSpending
	for _, contract := range allContracts {
		// Don't count double-spent contracts.
		if _, doubleSpent := c.doubleSpentContracts[contract.ID]; doubleSpent {
			continue
		}

		// Calculate ContractFees.
		spending.ContractFees = spending.ContractFees.Add(contract.ContractFee)
		spending.ContractFees = spending.ContractFees.Add(contract.TxnFee)
		spending.ContractFees = spending.ContractFees.Add(contract.SiafundFee)
		// Calculate TotalAllocated.
		spending.TotalAllocated = spending.TotalAllocated.Add(contract.TotalCost)
		// Calculate Spending.
		spending.DownloadSpending = spending.DownloadSpending.Add(contract.DownloadSpending)
		spending.FundAccountSpending = spending.FundAccountSpending.Add(contract.FundAccountSpending)
		spending.MaintenanceSpending = spending.MaintenanceSpending.Add(contract.MaintenanceSpending)
		spending.UploadSpending = spending.UploadSpending.Add(contract.UploadSpending)
		spending.StorageSpending = spending.StorageSpending.Add(contract.StorageSpending)
	}

	// Calculate needed spending to be reported from old contracts.
	for _, contract := range c.OldContracts() {
		// Filter out by renter.
		r, err := c.managedFindRenter(contract.ID)
		if err != nil {
			c.log.Println("ERROR: contract has no known renter associated with it:", contract.ID)
			continue
		}
		if r.PublicKey != rpk {
			continue
		}
		// Don't count double-spent contracts.
		if _, doubleSpent := c.doubleSpentContracts[contract.ID]; doubleSpent {
			continue
		}

		host, exist, err := c.hdb.Host(contract.HostPublicKey)
		if contract.StartHeight >= renter.CurrentPeriod {
			// Calculate spending from contracts that were renewed during the current period
			// Calculate ContractFees.
			spending.ContractFees = spending.ContractFees.Add(contract.ContractFee)
			spending.ContractFees = spending.ContractFees.Add(contract.TxnFee)
			spending.ContractFees = spending.ContractFees.Add(contract.SiafundFee)
			// Calculate TotalAllocated.
			spending.TotalAllocated = spending.TotalAllocated.Add(contract.TotalCost)
			// Calculate Spending.
			spending.DownloadSpending = spending.DownloadSpending.Add(contract.DownloadSpending)
			spending.FundAccountSpending = spending.FundAccountSpending.Add(contract.FundAccountSpending)
			spending.MaintenanceSpending = spending.MaintenanceSpending.Add(contract.MaintenanceSpending)
			spending.UploadSpending = spending.UploadSpending.Add(contract.UploadSpending)
			spending.StorageSpending = spending.StorageSpending.Add(contract.StorageSpending)
		} else if err != nil && exist && contract.EndHeight+host.Settings.WindowSize+modules.MaturityDelay > c.blockHeight {
			// Calculate funds that are being withheld in contracts.
			spending.WithheldFunds = spending.WithheldFunds.Add(contract.RenterFunds)
			// Record the largest window size for worst case when reporting the spending.
			if contract.EndHeight+host.Settings.WindowSize+modules.MaturityDelay >= spending.ReleaseBlock {
				spending.ReleaseBlock = contract.EndHeight + host.Settings.WindowSize + modules.MaturityDelay
			}
			// Calculate Previous spending.
			spending.PreviousSpending = spending.PreviousSpending.Add(contract.ContractFee).Add(contract.TxnFee).Add(contract.SiafundFee).Add(contract.DownloadSpending).Add(contract.UploadSpending).Add(contract.StorageSpending).Add(contract.FundAccountSpending).Add(contract.MaintenanceSpending.Sum())
		} else {
			// Calculate Previous spending.
			spending.PreviousSpending = spending.PreviousSpending.Add(contract.ContractFee).Add(contract.TxnFee).Add(contract.SiafundFee).Add(contract.DownloadSpending).Add(contract.UploadSpending).Add(contract.StorageSpending).Add(contract.FundAccountSpending).Add(contract.MaintenanceSpending.Sum())
		}
	}

	// Calculate amount of spent money to get unspent money.
	allSpending := spending.ContractFees
	allSpending = allSpending.Add(spending.DownloadSpending)
	allSpending = allSpending.Add(spending.UploadSpending)
	allSpending = allSpending.Add(spending.StorageSpending)
	allSpending = allSpending.Add(spending.FundAccountSpending)
	allSpending = allSpending.Add(spending.MaintenanceSpending.Sum())
	if renter.Allowance.Funds.Cmp(allSpending) >= 0 {
		spending.Unspent = renter.Allowance.Funds.Sub(allSpending)
	}

	return spending, nil
}

// CurrentPeriod returns the height at which the current allowance period
// of the renter began.
func (c *Contractor) CurrentPeriod(rpk types.PublicKey) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	renter, exists := c.renters[rpk]
	if !exists {
		return 0
	}
	return renter.CurrentPeriod
}

// RefreshedContract returns a bool indicating if the contract was a refreshed
// contract. A refreshed contract refers to a contract that ran out of funds
// prior to the end height and so was renewed with the host in the same period.
// Both the old and the new contract have the same end height.
func (c *Contractor) RefreshedContract(fcid types.FileContractID) bool {
	// Add thread and acquire lock.
	if err := c.tg.Add(); err != nil {
		return false
	}
	defer c.tg.Done()
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check if contract ID is found in the renewedTo map indicating that the
	// contract was renewed.
	newFCID, renewed := c.renewedTo[fcid]
	if !renewed {
		return false
	}

	// Grab the contract to check its end height.
	contract, ok := c.staticContracts.OldContract(fcid)
	if !ok {
		c.log.Println("ERROR: contract not found in oldContracts, despite there being a renewal to the contract")
		return false
	}

	// Grab the contract it was renewed to to check its end height.
	newContract, ok := c.staticContracts.View(newFCID)
	if !ok {
		newContract, ok = c.staticContracts.OldContract(newFCID)
		if !ok {
			c.log.Println("ERROR: contract was not found in the database, despite their being another contract that claims to have renewed to it.")
			return false
		}
	}

	// The contract was refreshed if the endheights are the same.
	return newContract.EndHeight == contract.EndHeight
}

// Synced returns a channel that is closed when the contractor is synced with
// the peer-to-peer network.
func (c *Contractor) Synced() <-chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.synced
}

// Close closes the Contractor.
func (c *Contractor) Close() error {
	c.dm.stop()
	c.um.stop()
	return c.tg.Stop()
}

// New returns a new Contractor.
func New(db *sql.DB, cs modules.ConsensusSet, m modules.Manager, tpool modules.TransactionPool, wallet modules.Wallet, hdb modules.HostDB, dir string) (*Contractor, <-chan error) {
	errChan := make(chan error, 1)
	defer close(errChan)
	// Check for nil inputs.
	if db == nil {
		errChan <- errNilDB
		return nil, errChan
	}
	if cs == nil {
		errChan <- errNilCS
		return nil, errChan
	}
	if m == nil {
		errChan <- errNilManager
		return nil, errChan
	}
	if wallet == nil {
		errChan <- errNilWallet
		return nil, errChan
	}
	if tpool == nil {
		errChan <- errNilTpool
		return nil, errChan
	}
	if hdb == nil {
		errChan <- errNilHDB
		return nil, errChan
	}

	// Create the logger.
	logger, err := persist.NewFileLogger(filepath.Join(dir, "contractor.log"))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Create the contract set.
	contractSet, err := contractset.NewContractSet(db, logger, cs.Height())
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Handle blocking startup.
	c, err := contractorBlockingStartup(db, cs, m, tpool, wallet, hdb, contractSet, logger)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Non-blocking startup.
	go func() {
		// Subscribe to the consensus set in a separate goroutine.
		if err := c.tg.Add(); err != nil {
			c.log.Println("ERROR: couldn't start a thread:", err)
			return
		}
		defer c.tg.Done()
		err := contractorAsyncStartup(c, cs)
		if err != nil {
			c.log.Println("ERROR: couldn't start contractor:", err)
		}
	}()

	return c, errChan
}

// contractorBlockingStartup handles the blocking portion of New.
func contractorBlockingStartup(db *sql.DB, cs modules.ConsensusSet, m modules.Manager, tp modules.TransactionPool, w modules.Wallet, hdb modules.HostDB, contractSet *contractset.ContractSet, l *persist.Logger) (*Contractor, error) {
	// Create the Contractor object.
	c := &Contractor{
		staticAlerter: modules.NewAlerter("contractor"),
		db:            db,
		cs:            cs,
		hdb:           hdb,
		log:           l,
		m:             m,
		tpool:         tp,
		wallet:        w,

		interruptMaintenance: make(chan struct{}),
		synced:               make(chan struct{}),

		renters: make(map[types.PublicKey]modules.Renter),

		staticContracts:      contractSet,
		doubleSpentContracts: make(map[types.FileContractID]uint64),
		renewing:             make(map[types.FileContractID]bool),
		renewedFrom:          make(map[types.FileContractID]types.FileContractID),
		renewedTo:            make(map[types.FileContractID]types.FileContractID),
		numFailedRenews:      make(map[types.FileContractID]uint64),
	}
	c.staticWatchdog = newWatchdog(c)
	c.dm = newDownloadManager(c, 5, 3*time.Second)
	c.um = newUploadManager(c, 5, 3*time.Second)
	c.migrator = newMigrator(c, 1)

	// Close the logger upon shutdown.
	c.tg.AfterStop(func() {
		if err := c.log.Close(); err != nil {
			fmt.Println("ERROR: failed to close the contractor logger")
		}
	})

	// Load the prior persistence structures.
	err := c.load()
	if err != nil {
		return nil, err
	}

	// Spin up a goroutine to periodically save the Contractor.
	go c.threadedSaveLoop()

	// Spin up a goroutine to periodically upload buffered files.
	go c.threadedUploadBufferedFiles()

	// Spin up a goroutine to periodically prune orphaned slabs.
	go c.threadedPruneOrphanedSlabs()

	// Update the pubkeysToContractID map.
	c.managedUpdatePubKeysToContractIDMap()

	// Unsubscribe from the consensus set upon shutdown.
	c.tg.OnStop(func() {
		cs.Unsubscribe(c)
	})

	// We may have resubscribed. Save now so that we don't lose our work.
	c.mu.Lock()
	err = c.save()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	return c, nil
}

// contractorAsyncStartup handles the async portion of New.
func contractorAsyncStartup(c *Contractor, cs modules.ConsensusSet) error {
	err := cs.ConsensusSetSubscribe(c, c.lastChange, c.tg.StopChan())
	if modules.ContainsError(err, modules.ErrInvalidConsensusChangeID) {
		// Reset the contractor consensus variables and try rescanning.
		c.blockHeight = 0
		c.lastChange = modules.ConsensusChangeBeginning
		err = cs.ConsensusSetSubscribe(c, c.lastChange, c.tg.StopChan())
	}
	if modules.ContainsError(err, siasync.ErrStopped) {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

// managedSynced returns true if the contractor is synced with the consensusset.
func (c *Contractor) managedSynced() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	select {
	case <-c.synced:
		return true
	default:
	}
	return false
}

// GetRenter returns the renter with the specified public key.
func (c *Contractor) GetRenter(rpk types.PublicKey) (modules.Renter, error) {
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return modules.Renter{}, ErrRenterNotFound
	}
	return renter, nil
}

// CreateNewRenter inserts a new renter into the map.
func (c *Contractor) CreateNewRenter(email string, rpk types.PublicKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.renters[rpk] = modules.Renter{
		Email:     email,
		PublicKey: rpk,
	}
}

// Renters returns the list of renters.
func (c *Contractor) Renters() []modules.Renter {
	c.mu.Lock()
	renters := make([]modules.Renter, 0, len(c.renters))
	for _, renter := range c.renters {
		renters = append(renters, renter)
	}
	c.mu.Unlock()
	return renters
}

// UnlockBalance unlocks the renter funds after the contract ends.
func (c *Contractor) UnlockBalance(fcid types.FileContractID) {
	contract, exists := c.staticContracts.View(fcid)
	if !exists {
		contract, exists = c.staticContracts.OldContract(fcid)
		if !exists {
			c.log.Println("ERROR: trying to unlock funds of a non-existing contract:", fcid)
			return
		}
	}

	c.mu.Lock()
	renter, err := c.managedFindRenter(fcid)
	c.mu.Unlock()
	if err != nil {
		c.log.Println("ERROR: trying to unlock funds of a non-existing renter:", fcid)
		return
	}

	revision := contract.Transaction.FileContractRevisions[0]
	payout := modules.Float64(revision.ValidProofOutputs[0].Value)
	cost := modules.Float64(contract.TotalCost)
	hastings := modules.Float64(types.HastingsPerSiacoin)
	amount := payout / hastings
	total := cost / hastings

	err = c.m.UnlockSiacoins(renter.Email, amount, total, contract.StartHeight)
	if err != nil {
		c.log.Println("ERROR: unable to unlock funds:", err)
		return
	}

	c.staticContracts.UnlockPayout(fcid)
}

// UpdateContract updates the contract with the new revision.
func (c *Contractor) UpdateContract(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	err := c.staticContracts.UpdateContract(rev, sigs, uploads, downloads, fundAccount)
	if err != nil {
		c.log.Println("ERROR: revision update failed:", rev.ParentID)
	}

	return err
}

// RenewedFrom returns the ID of the contract the given contract was renewed
// from, if any.
func (c *Contractor) RenewedFrom(fcid types.FileContractID) types.FileContractID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	from, ok := c.renewedFrom[fcid]
	if !ok {
		return types.FileContractID{}
	}
	return from
}

// DeleteRenter deletes the renter data from the memory.
func (c *Contractor) DeleteRenter(email string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for rpk, renter := range c.renters {
		if renter.Email == email {
			delete(c.renters, rpk)
			break
		}
	}
}

// Contract returns the contract with the given ID.
func (c *Contractor) Contract(fcid types.FileContractID) (modules.RenterContract, bool) {
	return c.staticContracts.View(fcid)
}

// UpdateRenterSettings updates the renter's opt-in settings.
func (c *Contractor) UpdateRenterSettings(rpk types.PublicKey, settings modules.RenterSettings, sk, ak types.PrivateKey) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	renter, exists := c.renters[rpk]
	if !exists {
		return ErrRenterNotFound
	}
	renter.Settings = settings
	renter.PrivateKey = sk
	renter.AccountKey = ak
	c.renters[rpk] = renter
	return c.UpdateRenter(renter)
}

// UpdateMetadata updates the file metadata in the database.
func (c *Contractor) UpdateMetadata(pk types.PublicKey, fm modules.FileMetadata) error {
	err := c.updateMetadata(pk, fm, true)
	if err != nil {
		c.log.Println("ERROR: couldn't update metadata:", err)
	}
	return err
}

// RetrieveMetadata retrieves the file metadata from the database.
func (c *Contractor) RetrieveMetadata(pk types.PublicKey, present []modules.BucketFiles) (fm []modules.FileMetadata, err error) {
	fm, err = c.retrieveMetadata(pk, present)
	if err != nil {
		c.log.Println("ERROR: couldn't retrieve metadata:", err)
	}
	return
}

// UpdateSlab updates a file slab after a successful migration.
func (c *Contractor) UpdateSlab(rpk types.PublicKey, slab modules.Slab, packed bool) error {
	err := c.updateSlab(rpk, slab, packed)
	if err != nil {
		c.log.Println("ERROR: couldn't update slab:", err)
	}
	return err
}

// AcceptContracts accepts a set of contracts from the renter
// and adds them to the contract set.
func (c *Contractor) AcceptContracts(rpk types.PublicKey, contracts []modules.ContractMetadata) {
	// Create a map of existing contracts.
	existingContracts := c.staticContracts.ByRenter(rpk)
	existing := make(map[types.FileContractID]struct{})
	for _, contract := range existingContracts {
		existing[contract.ID] = struct{}{}
	}

	// Iterate through the set and add only missing contracts.
	for _, contract := range contracts {
		if _, exists := existing[contract.ID]; exists {
			continue
		}

		// Find the contract txn.
		block, ok := c.cs.BlockAtHeight(contract.StartHeight + 1)
		if !ok {
			c.log.Println("ERROR: couldn't find block at height", contract.StartHeight+1)
			continue
		}

		var txnFee types.Currency
		var endHeight uint64
		var found bool
		for _, txn := range block.Transactions {
			if len(txn.FileContracts) > 0 && txn.FileContractID(0) == contract.ID {
				txnFee = txn.MinerFees[0]
				endHeight = txn.FileContracts[0].WindowStart
				found = true
				break
			}
		}
		if !found {
			c.log.Println("ERROR: couldn't find transaction for", contract.ID)
			continue
		}

		// Sanity check: the end height should not be in the past.
		// We use consensusset.Height instead of c.blockHeight here, because
		// the contractor may not be synced yet.
		height := c.cs.Height()
		if endHeight <= height {
			c.log.Printf("WARN: a contract was submitted with the end height in the past: %v <= %v\n", endHeight, height)
			continue
		}

		// We have no way to get some information from the data
		// provided, so we have to speculate a bit.
		rev := contract.Revision
		payout := rev.ValidRenterPayout().Add(rev.ValidHostPayout())
		tax := modules.Tax(contract.StartHeight, payout)
		host, ok, err := c.hdb.Host(contract.HostKey)
		if err != nil || !ok {
			c.log.Println("ERROR: couldn't find host for the contract", contract.ID)
			continue
		}
		contractFee := host.Settings.ContractPrice
		maxContractFee := contract.TotalCost.Sub(txnFee).Sub(tax).Sub(contract.UploadSpending).Sub(contract.DownloadSpending).Sub(contract.FundAccountSpending)
		if contractFee.Cmp(maxContractFee) > 0 {
			contractFee = maxContractFee
		}

		// Insert the contract.
		txn := types.Transaction{
			FileContractRevisions: []types.FileContractRevision{rev},
			Signatures: []types.TransactionSignature{
				types.TransactionSignature{},
				types.TransactionSignature{},
			},
		}
		rc, err := c.staticContracts.InsertContract(txn, contract.StartHeight, contract.TotalCost, contractFee, txnFee, tax, rpk, true)
		if err != nil {
			c.log.Printf("ERROR: couldn't accept contract %s: %v\n", contract.ID, err)
			continue
		}

		// Add a mapping from the contract's id to the public keys of the host
		// and the renter.
		c.mu.Lock()
		c.pubKeysToContractID[rc.RenterPublicKey.String()+rc.HostPublicKey.String()] = contract.ID
		c.mu.Unlock()

		if contract.RenewedFrom != (types.FileContractID{}) {
			c.renewedFrom[contract.ID] = contract.RenewedFrom
			c.renewedTo[contract.RenewedFrom] = contract.ID
			err = c.updateRenewedContract(contract.RenewedFrom, contract.ID)
			if err != nil {
				c.log.Println("ERROR: couldn't update renewal history:", err)
			}
		}
	}

	// Update the spendings.
	// NOTE: `renterd` doesn't store signatures, so we have to pass a nil.
	for _, contract := range contracts {
		err := c.UpdateContract(contract.Revision, nil, contract.UploadSpending, contract.DownloadSpending, contract.FundAccountSpending)
		if err != nil {
			c.log.Println("ERROR: couldn't update contract spendings", err)
		}
	}

	// Update the hostdb to include the new contracts.
	err := c.hdb.UpdateContracts(c.staticContracts.ViewAll())
	if err != nil {
		c.log.Println("ERROR: unable to update hostdb contracts:", err)
	}
}

// DownloadObject downloads an object and returns it.
func (c *Contractor) DownloadObject(w io.Writer, rpk types.PublicKey, bucket, path [255]byte) error {
	// Retrieve the object.
	obj, err := c.getObject(rpk, bucket, path)
	if err != nil {
		return err
	}

	// Calculate the object length.
	var length uint64
	for _, slab := range obj.Slabs {
		length += uint64(slab.Length)
	}
	for _, ps := range obj.PartialSlabs {
		length += uint64(ps.Length)
	}

	// Get the contracts.
	contracts := c.staticContracts.ByRenter(rpk)

	// Create the context and set up its cancelling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Download the object.
	err = c.dm.managedDownloadObject(ctx, w, rpk, obj, 0, length, contracts)

	return err
}
