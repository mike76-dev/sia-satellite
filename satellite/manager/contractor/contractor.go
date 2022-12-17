// Contractor package defines the contractor object responsible for
// forming contracts with the hosts.
package contractor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/threadgroup"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/modules/renter/proto"
	"go.sia.tech/siad/persist"
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
	// dependencies
	cs            modules.ConsensusSet
	hdb           modules.HostDB
	log           *persist.Logger
	mu            sync.RWMutex
	persistDir    string
	staticAlerter *modules.GenericAlerter
	tg            threadgroup.ThreadGroup
	tpool         modules.TransactionPool
	wallet        modules.Wallet

	allowance     modules.Allowance
	blockHeight   types.BlockHeight
	synced        chan struct{}
	currentPeriod types.BlockHeight
	lastChange    modules.ConsensusChangeID

	numFailedRenews map[types.FileContractID]types.BlockHeight
	renewing        map[types.FileContractID]bool // prevent revising during renewal

	// pubKeysToContractID is a map of host pubkeys to the latest contract ID
	// that is formed with the host. The contract also has to have an end height
	// in the future.
	pubKeysToContractID map[string]types.FileContractID

	// renewedFrom links the new contract's ID to the old contract's ID
	// renewedTo links the old contract's ID to the new contract's ID
	// doubleSpentContracts keep track of all contracts that were double spent by
	// either the renter or host.
	staticContracts      *proto.ContractSet
	oldContracts         map[types.FileContractID]modules.RenterContract
	doubleSpentContracts map[types.FileContractID]types.BlockHeight
	recoverableContracts map[types.FileContractID]modules.RecoverableContract
	renewedFrom          map[types.FileContractID]types.FileContractID
	renewedTo            map[types.FileContractID]types.FileContractID
}

// Allowance returns the current allowance.
func (c *Contractor) Allowance() modules.Allowance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.allowance
}

// ContractPublicKey returns the public key capable of verifying the renter's
// signature on a contract.
func (c *Contractor) ContractPublicKey(pk types.SiaPublicKey) (crypto.PublicKey, bool) {
	c.mu.RLock()
	id, ok := c.pubKeysToContractID[pk.String()]
	c.mu.RUnlock()
	if !ok {
		return crypto.PublicKey{}, false
	}
	return c.staticContracts.PublicKey(id)
}

// PeriodSpending returns the amount spent on contracts during the current
// billing period.
func (c *Contractor) PeriodSpending() (modules.ContractorSpending, error) {
	allContracts := c.staticContracts.ViewAll()
	c.mu.RLock()
	defer c.mu.RUnlock()

	var spending modules.ContractorSpending
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
	for _, contract := range c.oldContracts {
		// Don't count double-spent contracts.
		if _, doubleSpent := c.doubleSpentContracts[contract.ID]; doubleSpent {
			continue
		}

		host, exist, err := c.hdb.Host(contract.HostPublicKey)
		if contract.StartHeight >= c.currentPeriod {
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
			spending.PreviousSpending = spending.PreviousSpending.Add(contract.ContractFee).Add(contract.TxnFee).
				Add(contract.SiafundFee).Add(contract.DownloadSpending).Add(contract.UploadSpending).Add(contract.StorageSpending).Add(contract.FundAccountSpending).Add(contract.MaintenanceSpending.Sum())
		} else {
			// Calculate Previous spending.
			spending.PreviousSpending = spending.PreviousSpending.Add(contract.ContractFee).Add(contract.TxnFee).
				Add(contract.SiafundFee).Add(contract.DownloadSpending).Add(contract.UploadSpending).Add(contract.StorageSpending).Add(contract.FundAccountSpending).Add(contract.MaintenanceSpending.Sum())
		}
	}

	// Calculate amount of spent money to get unspent money.
	allSpending := spending.ContractFees
	allSpending = allSpending.Add(spending.DownloadSpending)
	allSpending = allSpending.Add(spending.UploadSpending)
	allSpending = allSpending.Add(spending.StorageSpending)
	allSpending = allSpending.Add(spending.FundAccountSpending)
	allSpending = allSpending.Add(spending.MaintenanceSpending.Sum())
	if c.allowance.Funds.Cmp(allSpending) >= 0 {
		spending.Unspent = c.allowance.Funds.Sub(allSpending)
	}

	return spending, nil
}

// CurrentPeriod returns the height at which the current allowance period
// began.
func (c *Contractor) CurrentPeriod() types.BlockHeight {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentPeriod
}

// RefreshedContract returns a bool indicating if the contract was a refreshed
// contract. A refreshed contract refers to a contract that ran out of funds
// prior to the end height and so was renewed with the host in the same period.
// Both the old and the new contract have the same end height
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
	contract, ok := c.oldContracts[fcid]
	if !ok {
		c.log.Debugln("Contract not found in oldContracts, despite there being a renewal to the contract")
		return false
	}

	// Grab the contract it was renewed to to check its end height.
	newContract, ok := c.staticContracts.View(newFCID)
	if !ok {
		newContract, ok = c.oldContracts[newFCID]
		if !ok {
			c.log.Debugln("Contract was not found in the database, despite their being another contract that claims to have renewed to it.")
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

// contractorBlockingStartup handles the blocking portion of New.
func contractorBlockingStartup(cs modules.ConsensusSet, w modules.Wallet, tp modules.TransactionPool, hdb modules.HostDB, persistDir string, contractSet *proto.ContractSet, l *persist.Logger) (*Contractor, error) {
	// Create the Contractor object.
	c := &Contractor{
		staticAlerter: modules.NewAlerter("contractor"),
		cs:            cs,
		hdb:           hdb,
		log:           l,
		persistDir:    persistDir,
		tpool:         tp,
		wallet:        w,

		synced:               make(chan struct{}),

		staticContracts:      contractSet,
		oldContracts:         make(map[types.FileContractID]modules.RenterContract),
		doubleSpentContracts: make(map[types.FileContractID]types.BlockHeight),
		recoverableContracts: make(map[types.FileContractID]modules.RecoverableContract),
		renewing:             make(map[types.FileContractID]bool),
		renewedFrom:          make(map[types.FileContractID]types.FileContractID),
		renewedTo:            make(map[types.FileContractID]types.FileContractID),
	}

	// Close the contract set and logger upon shutdown.
	err := c.tg.AfterStop(func() error {
		if err := c.staticContracts.Close(); err != nil {
			return errors.AddContext(err, "failed to close contract set")
		}
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

	// Update the pubkeyToContractID map
	c.managedUpdatePubKeyToContractIDMap()

	// Unsubscribe from the consensus set upon shutdown.
	err = c.tg.OnStop(func() error {
		cs.Unsubscribe(c)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// We may have upgraded persist or resubscribed. Save now so that we don't
	// lose our work.
	c.mu.Lock()
	err = c.save()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// Update the allowance in the hostdb with the one that was loaded from
	// disk.
	err = c.hdb.SetAllowance(c.allowance)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// contractorAsyncStartup handles the async portion of New.
func contractorAsyncStartup(c *Contractor, cs modules.ConsensusSet) error {
	err := cs.ConsensusSetSubscribe(c, c.lastChange, c.tg.StopChan())
	if errors.Contains(err, modules.ErrInvalidConsensusChangeID) {
		// Reset the contractor consensus variables and try rescanning.
		c.blockHeight = 0
		c.lastChange = modules.ConsensusChangeBeginning
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

// New returns a new Contractor.
func New(cs modules.ConsensusSet, wallet modules.Wallet, tpool modules.TransactionPool, hdb modules.HostDB, persistDir string) (*Contractor, <-chan error) {
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

	// Create the contract set.
	contractSet, err := proto.NewContractSet(filepath.Join(persistDir, "contracts"), nil, modules.ProdDependencies)
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Create the logger.
	logger, err := persist.NewFileLogger(filepath.Join(persistDir, "contractor.log"))
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Handle blocking startup.
	c, err := contractorBlockingStartup(cs, wallet, tpool, hdb, persistDir, contractSet, logger)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Non-blocking startup.
	go func() {
		// Subscribe to the consensus set in a separate goroutine.
		defer close(errChan)
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
