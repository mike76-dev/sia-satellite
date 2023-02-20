package contractor

import (
	"bytes"
	"database/sql"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/proto"

	"gitlab.com/NebulousLabs/errors"
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

// emptyWorkerPool is the workerpool that a contractor is initialized with.
type emptyWorkerPool struct{}

// Worker implements the WorkerPool interface.
func (emptyWorkerPool) Worker(_ types.SiaPublicKey) (smodules.Worker, error) {
	return nil, errors.New("empty worker pool")
}

// A Contractor negotiates, revises, renews, and provides access to file
// contracts.
type Contractor struct {
	// Dependencies.
	cs            smodules.ConsensusSet
	db            *sql.DB
	hdb           modules.HostDB
	log           *persist.Logger
	mu            sync.RWMutex
	persistDir    string
	staticAlerter *smodules.GenericAlerter
	tg            threadgroup.ThreadGroup
	tpool         smodules.TransactionPool
	wallet        smodules.Wallet
	workerPool    smodules.WorkerPool

	// Only one thread should be performing contract maintenance at a time.
	interruptMaintenance chan struct{}
	maintenanceLock      siasync.TryMutex

	blockHeight   types.BlockHeight
	synced        chan struct{}
	lastChange    smodules.ConsensusChangeID

	renters       map[string]modules.Renter

	sessions        map[types.FileContractID]*hostSession
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
	oldContracts         map[types.FileContractID]modules.RenterContract
	doubleSpentContracts map[types.FileContractID]types.BlockHeight
	renewedFrom          map[types.FileContractID]types.FileContractID
	renewedTo            map[types.FileContractID]types.FileContractID

	staticWatchdog *watchdog
}

// PaymentDetails is a helper struct that contains extra information on a
// payment. Most notably it includes a breakdown of the spending details for a
// payment, the contractor uses this information to update its spending details
// accordingly.
type PaymentDetails struct {
	// Source and destination details.
	Renter types.SiaPublicKey
	Host   types.SiaPublicKey

	// Payment details.
	Amount        types.Currency
	RefundAccount smodules.AccountID

	// Spending details.
	SpendingDetails smodules.SpendingDetails
}

// Allowance returns the current allowance of the renter specified.
func (c *Contractor) Allowance(rpk types.SiaPublicKey) smodules.Allowance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	renter, exists := c.renters[rpk.String()]
	if !exists {
		return smodules.Allowance{}
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

	allContracts := c.staticContracts.ByRenter(rpk)
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
	for _, contract := range c.oldContracts {
		// Filter out by renter.
		if contract.RenterPublicKey.String() != key {
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

// UpdateWorkerPool updates the workerpool currently in use by the contractor.
func (c *Contractor) UpdateWorkerPool(wp smodules.WorkerPool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workerPool = wp
}

// ProvidePayment takes a stream and a set of payment details and handles
// the payment for an RPC by sending and processing payment request and
// response objects to the host. It returns an error in case of failure.
func (c *Contractor) ProvidePayment(stream io.ReadWriter, pt *smodules.RPCPriceTable, details PaymentDetails) error {
	// Convenience variables.
	host := details.Host
	renter := details.Renter
	refundAccount := details.RefundAccount
	amount := details.Amount
	bh := pt.HostBlockHeight

	// Find a contract for the given host and renter.
	contract, exists := c.ContractByPublicKeys(renter, host)
	if !exists {
		return errContractNotFound
	}

	// Acquire a file contract.
	fc, exists := c.staticContracts.Acquire(contract.ID)
	if !exists {
		return errContractNotFound
	}
	defer c.staticContracts.Return(fc)

	// Create a new revision.
	current := fc.LastRevision()
	rev, err := current.EAFundRevision(amount)
	if err != nil {
		return errors.AddContext(err, "Failed to create a payment revision")
	}

	// Create transaction containing the revision.
	signedTxn := rev.ToTransaction()
	sig := fc.Sign(signedTxn.SigHash(0, bh))
	signedTxn.TransactionSignatures[0].Signature = sig[:]

	// Prepare a buffer so we can optimize our writes.
	buffer := bytes.NewBuffer(nil)

	// Send PaymentRequest.
	err = smodules.RPCWrite(buffer, smodules.PaymentRequest{Type: smodules.PayByContract})
	if err != nil {
		return errors.AddContext(err, "unable to write payment request to host")
	}

	// Send PayByContractRequest.
	err = smodules.RPCWrite(buffer, newPayByContractRequest(rev, sig, refundAccount))
	if err != nil {
		return errors.AddContext(err, "unable to write the pay by contract request")
	}

	// Write contents of the buffer to the stream.
	_, err = stream.Write(buffer.Bytes())
	if err != nil {
		return errors.AddContext(err, "could not write the buffer contents")
	}

	// Receive PayByContractResponse.
	var payByResponse smodules.PayByContractResponse
	if err := smodules.RPCRead(stream, &payByResponse); err != nil {
		if strings.Contains(err.Error(), "storage obligation not found") {
			c.log.Printf("Marking contract %v as bad because host %v did not recognize it: %v\n", contract.ID, host, err)
			mbcErr := c.managedMarkContractBad(fc)
			if mbcErr != nil {
				c.log.Printf("Unable to mark contract %v on host %v as bad: %v\n", contract.ID, host, mbcErr)
			}
		}
		return errors.AddContext(err, "unable to read the pay by contract response")
	}

	// Verify the host's signature.
	hash := crypto.HashAll(rev)
	hpk := fc.Metadata().HostPublicKey
	err = crypto.VerifyHash(hash, hpk.ToPublicKey(), payByResponse.Signature)
	if err != nil {
		return errors.New("could not verify host's signature")
	}

	// Commit payment intent.
	err = fc.CommitPaymentIntent(signedTxn, amount, details.SpendingDetails)
	if err != nil {
		return errors.AddContext(err, "Failed to commit unknown spending intent")
	}

	return nil
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
	contract, ok := c.oldContracts[fcid]
	if !ok {
		c.log.Println("Contract not found in oldContracts, despite there being a renewal to the contract")
		return false
	}

	// Grab the contract it was renewed to to check its end height.
	newContract, ok := c.staticContracts.View(newFCID)
	if !ok {
		newContract, ok = c.oldContracts[newFCID]
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
	contractSet, err := proto.NewContractSet(db, logger)
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
		sessions:             make(map[types.FileContractID]*hostSession),
		oldContracts:         make(map[types.FileContractID]modules.RenterContract),
		doubleSpentContracts: make(map[types.FileContractID]types.BlockHeight),
		renewing:             make(map[types.FileContractID]bool),
		renewedFrom:          make(map[types.FileContractID]types.FileContractID),
		renewedTo:            make(map[types.FileContractID]types.FileContractID),
		workerPool:           emptyWorkerPool{},
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

// newPayByContractRequest is a helper function that takes a revision,
// signature and refund account and creates a PayByContractRequest object.
func newPayByContractRequest(rev types.FileContractRevision, sig crypto.Signature, refundAccount smodules.AccountID) smodules.PayByContractRequest {
	req := smodules.PayByContractRequest{
		ContractID:           rev.ID(),
		NewRevisionNumber:    rev.NewRevisionNumber,
		NewValidProofValues:  make([]types.Currency, len(rev.NewValidProofOutputs)),
		NewMissedProofValues: make([]types.Currency, len(rev.NewMissedProofOutputs)),
		RefundAccount:        refundAccount,
		Signature:            sig[:],
	}
	for i, o := range rev.NewValidProofOutputs {
		req.NewValidProofValues[i] = o.Value
	}
	for i, o := range rev.NewMissedProofOutputs {
		req.NewMissedProofValues[i] = o.Value
	}
	return req
}

// RenewContract takes an established connection to a host and renews the
// contract with that host.
func (c *Contractor) RenewContract(conn net.Conn, fcid types.FileContractID, params smodules.ContractParams, txnBuilder smodules.TransactionBuilder, tpool smodules.TransactionPool, hdb modules.HostDB, pt *smodules.RPCPriceTable) (modules.RenterContract, []types.Transaction, error) {
	newContract, txnSet, err := c.staticContracts.RenewContract(conn, fcid, params, txnBuilder, tpool, hdb, pt)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "RenewContract: failed to renew contract")
	}
	// Update various mappings in the contractor after a successful renewal.
	c.mu.Lock()
	c.renewedFrom[newContract.ID] = fcid
	c.renewedTo[fcid] = newContract.ID
	c.pubKeysToContractID[newContract.RenterPublicKey.String() + newContract.HostPublicKey.String()] = newContract.ID
	c.mu.Unlock()
	return newContract, txnSet, nil
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
