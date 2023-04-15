package contractor

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/proto"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/NebulousLabs/threadgroup"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

var (
	errNilCS     = errors.New("cannot create contractor with nil consensus set")
	errNilHDB    = errors.New("cannot create contractor with nil HostDB")
	errNilTpool  = errors.New("cannot create contractor with nil transaction pool")
	errNilWallet = errors.New("cannot create contractor with nil wallet")

	errHostNotFound     = errors.New("host not found")
	errContractNotFound = errors.New("contract not found")
)

// A Contractor negotiates, revises, renews, and provides access to file
// contracts.
type Contractor struct {
	// Dependencies.
	cs            smodules.ConsensusSet
	db            *sql.DB
	hdb           modules.HostDB
	satellite     modules.FundLocker
	log           *persist.Logger
	mu            sync.RWMutex
	persistDir    string
	staticAlerter *smodules.GenericAlerter
	tg            threadgroup.ThreadGroup
	tpool         smodules.TransactionPool
	wallet        smodules.Wallet

	// Only one thread should be performing contract maintenance at a time.
	interruptMaintenance chan struct{}
	maintenanceLock      siasync.TryMutex

	blockHeight   types.BlockHeight
	synced        chan struct{}
	lastChange    smodules.ConsensusChangeID

	renters       map[string]modules.Renter

	numFailedRenews map[types.FileContractID]types.BlockHeight
	renewing        map[types.FileContractID]bool // Prevent revising during renewal.

	// pubKeysToContractID is a map of renter and host pubkeys to the latest contract ID
	// that is formed with the host. The contract also has to have an end height
	// in the future.
	pubKeysToContractID map[string]types.FileContractID

	// renewedFrom links the new contract's ID to the old contract's ID
	// renewedTo links the old contract's ID to the new contract's ID
	// doubleSpentContracts keep track of all contracts that were double spent by
	// either the renter or host.
	staticContracts      *proto.ContractSet
	doubleSpentContracts map[types.FileContractID]types.BlockHeight
	renewedFrom          map[types.FileContractID]types.FileContractID
	renewedTo            map[types.FileContractID]types.FileContractID

	staticWatchdog *watchdog
}

// Allowance returns the current allowance of the renter specified.
func (c *Contractor) Allowance(rpk types.SiaPublicKey) modules.Allowance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	renter, exists := c.renters[rpk.String()]
	if !exists {
		return modules.Allowance{}
	}
	return renter.Allowance
}

// ContractPublicKey returns the public key capable of verifying the renter's
// signature on a contract.
func (c *Contractor) ContractPublicKey(rpk, hpk types.SiaPublicKey) (crypto.PublicKey, bool) {
	c.mu.RLock()
	id, ok := c.pubKeysToContractID[rpk.String() + hpk.String()]
	c.mu.RUnlock()
	if !ok {
		return crypto.PublicKey{}, false
	}
	return c.staticContracts.PublicKey(id)
}

// PeriodSpending returns the amount spent by the renter on contracts during
// the current billing period.
func (c *Contractor) PeriodSpending(rpk types.SiaPublicKey) (smodules.ContractorSpending, error) {
	// Check if we know this renter.
	key := rpk.String()
	c.mu.RLock()
	renter, exists := c.renters[key]
	c.mu.RUnlock()
	if !exists {
		return smodules.ContractorSpending{}, ErrRenterNotFound
	}

	seed, _, err := c.wallet.PrimarySeed()
	if err != nil {
		return smodules.ContractorSpending{}, err
	}
	rs := modules.DeriveRenterSeed(seed, renter.Email)
	defer fastrand.Read(rs[:])

	allContracts := c.staticContracts.ByRenter(rs)
	c.mu.RLock()
	defer c.mu.RUnlock()

	var spending smodules.ContractorSpending
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
		spending.ContractSpendingDeprecated = spending.TotalAllocated
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
		epk := modules.EphemeralPublicKey(modules.DeriveEphemeralRenterSeed(rs, contract.HostPublicKey))
		if contract.RenterPublicKey.String() != epk.String() {
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
		} else if err != nil && exist && contract.EndHeight + host.WindowSize + types.MaturityDelay > c.blockHeight {
			// Calculate funds that are being withheld in contracts.
			spending.WithheldFunds = spending.WithheldFunds.Add(contract.RenterFunds)
			// Record the largest window size for worst case when reporting the spending.
			if contract.EndHeight + host.WindowSize + types.MaturityDelay >= spending.ReleaseBlock {
				spending.ReleaseBlock = contract.EndHeight + host.WindowSize + types.MaturityDelay
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
func (c *Contractor) CurrentPeriod(rpk types.SiaPublicKey) types.BlockHeight {
	c.mu.RLock()
	defer c.mu.RUnlock()
	renter, exists := c.renters[rpk.String()]
	if !exists {
		return types.BlockHeight(0)
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
		c.log.Println("Contract not found in oldContracts, despite there being a renewal to the contract")
		return false
	}

	// Grab the contract it was renewed to to check its end height.
	newContract, ok := c.staticContracts.View(newFCID)
	if !ok {
		newContract, ok = c.staticContracts.OldContract(newFCID)
		if !ok {
			c.log.Println("Contract was not found in the database, despite their being another contract that claims to have renewed to it.")
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
	return c.tg.Stop()
}

// New returns a new Contractor.
func New(cs smodules.ConsensusSet, wallet smodules.Wallet, tpool smodules.TransactionPool, hdb modules.HostDB, db *sql.DB, persistDir string) (*Contractor, <-chan error) {
	errChan := make(chan error, 1)
	defer close(errChan)
	// Check for nil inputs.
	if cs == nil {
		errChan <- errNilCS
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

	// Create the persist directory if it does not yet exist.
	if err := os.MkdirAll(persistDir, 0700); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the logger.
	logger, err := persist.NewFileLogger(filepath.Join(persistDir, "contractor.log"))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Create the contract set.
	contractSet, err := proto.NewContractSet(db, logger, cs.Height())
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Handle blocking startup.
	c, err := contractorBlockingStartup(cs, wallet, tpool, hdb, persistDir, contractSet, db, logger)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Non-blocking startup.
	go func() {
		// Subscribe to the consensus set in a separate goroutine.
		if err := c.tg.Add(); err != nil {
			errChan <- err
			return
		}
		defer c.tg.Done()
		err := contractorAsyncStartup(c, cs)
		if err != nil {
			errChan <- err
		}
	}()

	return c, errChan
}

// contractorBlockingStartup handles the blocking portion of New.
func contractorBlockingStartup(cs smodules.ConsensusSet, w smodules.Wallet, tp smodules.TransactionPool, hdb modules.HostDB, persistDir string, contractSet *proto.ContractSet, db *sql.DB, l *persist.Logger) (*Contractor, error) {
	// Create the Contractor object.
	c := &Contractor{
		staticAlerter: smodules.NewAlerter("contractor"),
		cs:            cs,
		db:            db,
		hdb:           hdb,
		log:           l,
		persistDir:    persistDir,
		tpool:         tp,
		wallet:        w,

		interruptMaintenance: make(chan struct{}),
		synced:               make(chan struct{}),

		renters:              make(map[string]modules.Renter),

		staticContracts:      contractSet,
		doubleSpentContracts: make(map[types.FileContractID]types.BlockHeight),
		renewing:             make(map[types.FileContractID]bool),
		renewedFrom:          make(map[types.FileContractID]types.FileContractID),
		renewedTo:            make(map[types.FileContractID]types.FileContractID),
	}
	c.staticWatchdog = newWatchdog(c)

	// Close the logger upon shutdown.
	err := c.tg.AfterStop(func() error {
		if err := c.log.Close(); err != nil {
			return errors.AddContext(err, "failed to close the contractor logger")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Load the prior persistence structures.
	err = c.load()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Spin up a goroutine to periodically save the Contractor.
	go c.threadedSaveLoop()

	// Update the pubkeysToContractID map.
	c.managedUpdatePubKeysToContractIDMap()

	// Unsubscribe from the consensus set upon shutdown.
	err = c.tg.OnStop(func() error {
		cs.Unsubscribe(c)
		return nil
	})
	if err != nil {
		return nil, err
	}

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
func contractorAsyncStartup(c *Contractor, cs smodules.ConsensusSet) error {
	err := cs.ConsensusSetSubscribe(c, c.lastChange, c.tg.StopChan())
	if errors.Contains(err, smodules.ErrInvalidConsensusChangeID) {
		// Reset the contractor consensus variables and try rescanning.
		c.blockHeight = 0
		c.lastChange = smodules.ConsensusChangeBeginning
		err = cs.ConsensusSetSubscribe(c, c.lastChange, c.tg.StopChan())
	}
	if err != nil && strings.Contains(err.Error(), threadgroup.ErrStopped.Error()) {
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
func (c *Contractor) GetRenter(rpk types.SiaPublicKey) (modules.Renter, error) {
	c.mu.RLock()
	renter, exists := c.renters[rpk.String()]
	c.mu.RUnlock()
	if !exists {
		return modules.Renter{}, ErrRenterNotFound
	}
	return renter, nil
}

// CreateNewRenter inserts a new renter into the map.
func (c *Contractor) CreateNewRenter(email string, pk types.SiaPublicKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.renters[pk.String()] = modules.Renter{
		Email:     email,
		PublicKey: pk,
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

// SetSatellite sets the satellite dependency.
func (c *Contractor) SetSatellite(fl modules.FundLocker) {
	c.satellite = fl
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

	c.mu.RLock()
	renter, err := c.managedFindRenter(contract.RenterPublicKey, contract.HostPublicKey)
	c.mu.RUnlock()
	if err != nil {
		c.log.Println("ERROR: trying to unlock funds of a non-existing renter:", contract.RenterPublicKey.String())
		return
	}

	revision := contract.Transaction.FileContractRevisions[0]
	payout, _ := revision.NewValidProofOutputs[0].Value.Float64()
	cost, _ := contract.TotalCost.Float64()
	hastings, _ := types.SiacoinPrecision.Float64()
	amount := payout / hastings
	total := cost / hastings

	err = c.satellite.UnlockSiacoins(renter.Email, amount, total, contract.StartHeight)
	if err != nil {
		c.log.Println("ERROR: unable to unlock funds:", err)
	}
}

// UpdateContract updates the contract with the new revision.
func (c *Contractor) UpdateContract(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	err := c.staticContracts.UpdateContract(rev, sigs, uploads, downloads, fundAccount)
	if err != nil {
		c.log.Println("ERROR: revision update failed:", rev.ParentID)
	}

	return err
}

// managedFindRenter tries to find a renter by the ephemeral public key.
func (c *Contractor) managedFindRenter(epk, hpk types.SiaPublicKey) (renter modules.Renter, err error) {
	seed, _, err := c.wallet.PrimarySeed()
	if err != nil {
		return
	}

	var rs smodules.RenterSeed
	defer fastrand.Read(rs[:])

	renters := c.renters
	for _, renter = range renters {
		rs = modules.DeriveRenterSeed(seed, renter.Email)
		key := modules.EphemeralPublicKey(modules.DeriveEphemeralRenterSeed(rs, hpk))
		if key.String() == epk.String() {
			return
		}
	}

	return modules.Renter{}, ErrRenterNotFound
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
