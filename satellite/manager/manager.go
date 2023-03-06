// manager package is the host-facing part of the satellite module.
package manager

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/contractor"
	"github.com/mike76-dev/sia-satellite/satellite/manager/hostdb"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {
	smodules.Alerter

	// SetAllowance sets the amount of money the contractor is allowed to
	// spend on contracts over a given time period, divided among the number
	// of hosts specified. Note that contractor can start forming contracts as
	// soon as SetAllowance is called; that is, it may block.
	SetAllowance(types.SiaPublicKey, smodules.Allowance) error

	// Allowance returns the current allowance of the renter.
	Allowance(types.SiaPublicKey) smodules.Allowance

	// Close closes the hostContractor.
	Close() error

	// CancelContract cancels the renter's contract.
	CancelContract(types.FileContractID) error

	// Contracts returns the staticContracts of the manager's hostContractor.
	Contracts() []modules.RenterContract

	// ContractByPublicKeys returns the contract associated with the renter
	// and the host keys.
	ContractByPublicKeys(types.SiaPublicKey, types.SiaPublicKey) (modules.RenterContract, bool)

	// ContractPublicKey returns the public key capable of verifying the renter's
	// signature on a contract.
	ContractPublicKey(types.SiaPublicKey, types.SiaPublicKey) (crypto.PublicKey, bool)

	// ContractUtility returns the utility field for a given contract, along
	// with a bool indicating if it exists.
	ContractUtility(types.SiaPublicKey, types.SiaPublicKey) (smodules.ContractUtility, bool)

	// ContractStatus returns the status of the given contract within the
	// watchdog.
	ContractStatus(fcID types.FileContractID) (smodules.ContractWatchStatus, bool)

	// CreateNewRenter inserts a new renter into the map.
	CreateNewRenter(string, types.SiaPublicKey)

	// CurrentPeriod returns the height at which the current allowance period
	// of the renter began.
	CurrentPeriod(types.SiaPublicKey) types.BlockHeight

	// GetRenter returns the renter with the given public key.
	GetRenter(types.SiaPublicKey) (modules.Renter, error)

	// FormContracts forms up to the specified number of contracts, puts them
	// in the contract set, and returns them.
	FormContracts(types.SiaPublicKey) ([]modules.RenterContract, error)

	// PeriodSpending returns the amount spent on contracts during the current
	// billing period of the renter.
	PeriodSpending(types.SiaPublicKey) (smodules.ContractorSpending, error)

	// ProvidePayment takes a stream and a set of payment details and handles
	// the payment for an RPC by sending and processing payment request and
	// response objects to the host. It returns an error in case of failure.
	ProvidePayment(stream io.ReadWriter, pt *smodules.RPCPriceTable, details contractor.PaymentDetails) error

	// OldContracts returns the oldContracts of the manager's hostContractor.
	OldContracts() []modules.RenterContract

	// IsOffline reports whether the specified host is considered offline.
	IsOffline(types.SiaPublicKey) bool

	// Session creates a Session from the specified contract ID.
	Session(types.SiaPublicKey, types.SiaPublicKey, <-chan struct{}) (contractor.Session, error)

	// RefreshedContract checks if the contract was previously refreshed.
	RefreshedContract(fcid types.FileContractID) bool

	// RenewContracts tries to renew the given set of contracts.
	RenewContracts(types.SiaPublicKey, []types.FileContractID) ([]modules.RenterContract, error)

	// Renters return the list of renters.
	Renters() []modules.Renter

	// Synced returns a channel that is closed when the contractor is fully
	// synced with the peer-to-peer network.
	Synced() <-chan struct{}

	// SetSatellite sets the satellite dependency.
	SetSatellite(modules.FundLocker)
}

// A Manager contains the information necessary to communicate with the
// hosts.
type Manager struct {
	// Dependencies.
	cs             smodules.ConsensusSet
	db             *sql.DB
	hostContractor hostContractor
	hostDB         modules.HostDB
	tpool          smodules.TransactionPool

	// Atomic properties.
	hostAverages        modules.HostAverages
	lastEstimationHosts []smodules.HostDBEntry

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *smodules.GenericAlerter
}

// New returns an initialized Manager.
func New(cs smodules.ConsensusSet, g smodules.Gateway, tpool smodules.TransactionPool, wallet smodules.Wallet, db *sql.DB, mux *siamux.SiaMux, persistDir string) (*Manager, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the HostDB object.
	hdb, errChanHDB := hostdb.New(g, cs, tpool, db, mux, persistDir)
	if err := smodules.PeekErr(errChanHDB); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Contractor.
	hc, errChanContractor := contractor.New(cs, wallet, tpool, hdb, db, persistDir)
	if err := smodules.PeekErr(errChanContractor); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Manager object.
	m := &Manager{
		cs:             cs,
		db:             db,
		hostContractor: hc,
		hostDB:         hdb,
		persistDir:     persistDir,
		staticAlerter:  smodules.NewAlerter("manager"),
		tpool:          tpool,
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err = smodules.PeekErr(errChan); err != nil {
			errChan <- errors.Compose(m.threads.Stop(), err)
		}
	}()

	// Create the logger.
	m.log, err = persist.NewFileLogger(filepath.Join(persistDir, logFile))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Establish the closing of the logger.
	m.threads.AfterStop(func() {
		err := m.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the manager logger:", err)
		}
	})
	m.log.Println("INFO: manager created, started logging")

	// Load the manager persistence.
	if loadErr := m.load(); loadErr != nil && !os.IsNotExist(loadErr) {
		errChan <- errors.AddContext(loadErr, "unable to load manager")
		return nil, errChan
	}

	// Spawn the thread to periodically save the manager.
	go m.threadedSaveLoop()

	// Make sure that the manager saves on shutdown.
	m.threads.AfterStop(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		err := m.saveSync()
		if err != nil {
			m.log.Println("ERROR: Unable to save manager:", err)
		}
	})

	// Spawn a thread to calculate the host network averages.
	go m.threadedCalculateAverages()

	// Subscribe to the consensus set in a separate goroutine.
	go func() {
		defer close(errChan)
		if err := m.threads.Add(); err != nil {
			errChan <- err
			return
		}
		defer m.threads.Done()
		err := cs.ConsensusSetSubscribe(m, smodules.ConsensusChangeRecent, m.threads.StopChan())
		if err != nil {
			errChan <- err
		}
	}()

	// Unsubscribe on shutdown.
	m.threads.AfterStop(func() {
		cs.Unsubscribe(m)
	})

	return m, errChan
}

// ActiveHosts returns an array of hostDB's active hosts.
func (m *Manager) ActiveHosts() ([]smodules.HostDBEntry, error) { return m.hostDB.ActiveHosts() }

// AllHosts returns an array of all hosts.
func (m *Manager) AllHosts() ([]smodules.HostDBEntry, error) { return m.hostDB.AllHosts() }

// Close shuts down the manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return errors.Compose(m.threads.Stop(), m.hostDB.Close(), m.hostContractor.Close(), m.saveSync())
}

// Filter returns the hostdb's filterMode and filteredHosts.
func (m *Manager) Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error) {
	var fm smodules.FilterMode
	hosts := make(map[string]types.SiaPublicKey)
	if err := m.threads.Add(); err != nil {
		return fm, hosts, nil, err
	}
	defer m.threads.Done()
	fm, hosts, netAddresses, err := m.hostDB.Filter()
	if err != nil {
		return fm, hosts, netAddresses, errors.AddContext(err, "error getting hostdb filter:")
	}
	return fm, hosts, netAddresses, nil
}

// SetFilterMode sets the hostdb filter mode.
func (m *Manager) SetFilterMode(lm smodules.FilterMode, hosts []types.SiaPublicKey, netAddresses []string) error {
	if err := m.threads.Add(); err != nil {
		return err
	}
	defer m.threads.Done()

	// Set list mode filter for the hostdb.
	if err := m.hostDB.SetFilterMode(lm, hosts, netAddresses); err != nil {
		return err
	}

	return nil
}

// Host returns the host associated with the given public key.
func (m *Manager) Host(spk types.SiaPublicKey) (smodules.HostDBEntry, bool, error) {
	return m.hostDB.Host(spk)
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (m *Manager) InitialScanComplete() (bool, types.BlockHeight, error) { return m.hostDB.InitialScanComplete() }

// ScoreBreakdown returns the score breakdown of the specific host.
func (m *Manager) ScoreBreakdown(e smodules.HostDBEntry) (smodules.HostScoreBreakdown, error) {
	return m.hostDB.ScoreBreakdown(e)
}

// EstimateHostScore returns the estimated host score.
func (m *Manager) EstimateHostScore(e smodules.HostDBEntry, a smodules.Allowance) (smodules.HostScoreBreakdown, error) {
	return m.hostDB.EstimateHostScore(e, a)
}

// RandomHosts picks up to the specified number of random hosts from the
// hostdb sorted by weight.
func (m *Manager) RandomHosts(n uint64, a smodules.Allowance) ([]smodules.HostDBEntry, error) {
	return m.hostDB.RandomHostsWithLimits(int(n), nil, nil, a)
}

// GetAverages retrieves the host network averages from HostDB.
func (m *Manager) GetAverages() modules.HostAverages {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hostAverages
}

// Contracts returns the hostContractor's contracts.
func (m *Manager) Contracts() []modules.RenterContract {
	return m.hostContractor.Contracts()
}

// RefreshedContract calls hostContractor.RefreshedContract
func (m *Manager) RefreshedContract(fcid types.FileContractID) bool {
	return m.hostContractor.RefreshedContract(fcid)
}

// OldContracts calls hostContractor.OldContracts expired.
func (m *Manager) OldContracts() []modules.RenterContract {
	return m.hostContractor.OldContracts()
}

// ProcessConsensusChange processes the consensus change.
func (m *Manager) ProcessConsensusChange(cc smodules.ConsensusChange) {
	m.mu.Lock()
	m.lastEstimationHosts = []smodules.HostDBEntry{}
	m.mu.Unlock()
}

// PriceEstimation estimates the cost in siacoins of forming contracts with
// the hosts. The estimation will be done using the provided allowance.
// The final allowance used will be returned.
func (m *Manager) PriceEstimation(allowance smodules.Allowance) (float64, smodules.Allowance, error) {
	if err := m.threads.Add(); err != nil {
		return 0, smodules.Allowance{}, err
	}
	defer m.threads.Done()

	// Get hosts for the estimate.
	var hosts []smodules.HostDBEntry
	hostmap := make(map[string]struct{})

	// Start by grabbing hosts from the contracts.
	contracts := m.Contracts()
	var pks []types.SiaPublicKey
	for _, c := range contracts {
		u, ok := m.hostContractor.ContractUtility(c.RenterPublicKey, c.HostPublicKey)
		if !ok {
			continue
		}
		// Check for active contracts only.
		if !u.GoodForRenew {
			continue
		}
		pks = append(pks, c.HostPublicKey)
	}

	// Get hosts from the pubkeys.
	for _, pk := range pks {
		host, ok, err := m.hostDB.Host(pk)
		if !ok || host.Filtered || err != nil {
			continue
		}
		// Confirm if the host wasn't already added.
		if _, ok := hostmap[host.PublicKey.String()]; ok {
			continue
		}
		hosts = append(hosts, host)
		hostmap[host.PublicKey.String()] = struct{}{}
	}

	// Add hosts from the previous estimate cache if needed.
	if len(hosts) < int(allowance.Hosts) {
		m.mu.Lock()
		cachedHosts := m.lastEstimationHosts
		m.mu.Unlock()
		for _, host := range cachedHosts {
			// Confirm if the host wasn't already added.
			if _, ok := hostmap[host.PublicKey.String()]; ok {
				continue
			}
			hosts = append(hosts, host)
			hostmap[host.PublicKey.String()] = struct{}{}
		}
	}

	// Add random hosts if needed.
	if len(hosts) < int(allowance.Hosts) {
		// Re-initialize the list with SiaPublicKeys to hold the public keys
		// from the current set of hosts. This list will be used as address
		// filter when requesting random hosts.
		var pks []types.SiaPublicKey
		for _, host := range hosts {
			pks = append(pks, host.PublicKey)
		}
		// Grab hosts to perform the estimation.
		var err error
		randHosts, err := m.hostDB.RandomHostsWithLimits(int(allowance.Hosts) - len(hosts), pks, pks, allowance)
		if err != nil {
			return 0, allowance, errors.AddContext(err, "could not generate estimate, could not get random hosts")
		}
		// As the returned random hosts are checked for IP violations and double
		// entries against the current slice of hosts, the returned hosts can be
		// safely added to the current slice.
		hosts = append(hosts, randHosts...)
	}
	// Check if there are zero hosts, which means no estimation can be made.
	if len(hosts) == 0 {
		return 0, allowance, errors.New("estimate cannot be made, there are no hosts")
	}

	// Add up the costs for each host.
	var totalContractCost types.Currency
	var totalDownloadCost types.Currency
	var totalStorageCost types.Currency
	var totalUploadCost types.Currency
	for _, host := range hosts {
		totalContractCost = totalContractCost.Add(host.ContractPrice)
		totalDownloadCost = totalDownloadCost.Add(host.DownloadBandwidthPrice)
		totalStorageCost = totalStorageCost.Add(host.StoragePrice)
		totalUploadCost = totalUploadCost.Add(host.UploadBandwidthPrice)
	}

	// Convert values to match the allowance.
	totalDownloadCost = totalDownloadCost.Mul64(allowance.ExpectedDownload).Div64(uint64(allowance.Period))
	totalStorageCost = totalStorageCost.Mul64(allowance.ExpectedStorage)
	totalUploadCost = totalUploadCost.Mul64(allowance.ExpectedUpload).Div64(uint64(allowance.Period))

	// Factor in redundancy.
	totalStorageCost = totalStorageCost.MulFloat(allowance.ExpectedRedundancy)
	totalUploadCost = totalUploadCost.MulFloat(allowance.ExpectedRedundancy)

	// Perform averages.
	totalContractCost = totalContractCost.Div64(uint64(len(hosts)))
	totalDownloadCost = totalDownloadCost.Div64(uint64(len(hosts)))
	totalStorageCost = totalStorageCost.Div64(uint64(len(hosts)))
	totalUploadCost = totalUploadCost.Div64(uint64(len(hosts)))

	// Take the average of the host set to estimate the overall cost of the
	// contract forming. This is to protect against the case where less hosts
	// were gathered for the estimate that the allowance requires.
	totalContractCost = totalContractCost.Mul64(allowance.Hosts)

	// Add the cost of paying the transaction fees and then double the contract
	// costs to account for renewing a full set of contracts.
	_, feePerByte := m.tpool.FeeEstimation()
	txnsFees := feePerByte.Mul64(smodules.EstimatedFileContractTransactionSetSize).Mul64(uint64(allowance.Hosts))
	totalContractCost = totalContractCost.Add(txnsFees)
	totalContractCost = totalContractCost.Mul64(2)

	// Try to figure out what the total contract cost would be for the
	// Siafund fee.
	totalCost := totalContractCost.Add(totalDownloadCost)
	totalCost = totalCost.Add(totalUploadCost)
	totalCost = totalCost.Add(totalStorageCost)
	totalCost = totalCost.Mul64(3) // Quite generous.

	// Determine host collateral to be added to Siafund fee.
	var hostCollateral types.Currency
	contractCostPerHost := totalContractCost.Div64(allowance.Hosts)
	fundingPerHost := totalCost.Div64(allowance.Hosts)
	numHosts := uint64(0)
	for _, host := range hosts {
		// Assume that the ContractPrice equals contractCostPerHost and that
		// the txnFee was zero. It doesn't matter since RenterPayoutsPreTax
		// simply subtracts both values from the funding.
		host.ContractPrice = contractCostPerHost
		expectedStorage := allowance.ExpectedStorage / uint64(len(hosts))
		_, _, collateral, err := smodules.RenterPayoutsPreTax(host, fundingPerHost, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, allowance.Period, expectedStorage)
		if err != nil {
			continue
		}
		hostCollateral = hostCollateral.Add(collateral)
		numHosts++
	}

	// Divide by zero check. The only way to get 0 numHosts is if
	// RenterPayoutsPreTax errors for every host. This would happen if the
	// funding of the allowance is not enough as that would cause the
	// fundingPerHost to be less than the contract price
	if numHosts == 0 {
		return 0, allowance, errors.New("funding insufficient for number of hosts")
	}
	// Calculate average collateral and determine collateral for allowance.
	hostCollateral = hostCollateral.Div64(numHosts)
	hostCollateral = hostCollateral.Mul64(allowance.Hosts)

	// Add in siafund fee. which should be around 10%. The 10% siafund fee
	// accounts for paying 3.9% siafund on transactions and host collateral. We
	// estimate the renter to spend all of it's allowance so the siafund fee
	// will be calculated on the sum of the allowance and the hosts collateral
	totalPayout := totalCost.Add(hostCollateral)
	siafundFee := types.Tax(m.cs.Height(), totalPayout)
	totalContractCost = totalContractCost.Add(siafundFee)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions
	totalContractCost = totalContractCost.MulFloat(1.2)
	totalDownloadCost = totalDownloadCost.MulFloat(1.2)
	totalStorageCost = totalStorageCost.MulFloat(1.2)
	totalUploadCost = totalUploadCost.MulFloat(1.2)

	est := totalContractCost.Add(totalDownloadCost)
	est = est.Add(totalStorageCost)
	est = est.Add(totalUploadCost)
	est.MulFloat(modules.SatelliteOverhead)

	cost, _ := est.Float64()
	h, _ := types.SiacoinPrecision.Float64()
	allowance.Funds = totalCost

	m.mu.Lock()
	m.lastEstimationHosts = hosts
	m.mu.Unlock()

	return cost / h, allowance, nil
}

// SetAllowance calls hostContractor.SetAllowance.
func (m *Manager) SetAllowance(rpk types.SiaPublicKey, a smodules.Allowance) error {
	return m.hostContractor.SetAllowance(rpk, a)
}

// GetRenter calls hostContractor.GetRenter.
func (m *Manager) GetRenter(rpk types.SiaPublicKey) (modules.Renter, error) {
	return m.hostContractor.GetRenter(rpk)
}

// CreateNewRenter calls hostContractor.CreateNewRenter.
func (m *Manager) CreateNewRenter(email string, pk types.SiaPublicKey) {
	m.hostContractor.CreateNewRenter(email, pk)
}

// FormContracts calls hostContractor.FormContracts.
func (m *Manager) FormContracts(rpk types.SiaPublicKey) ([]modules.RenterContract, error) {
	return m.hostContractor.FormContracts(rpk)
}

// RenewContracts calls hostContractor.RenewContracts.
func (m *Manager) RenewContracts(rpk types.SiaPublicKey, contracts []types.FileContractID) ([]modules.RenterContract, error) {
	return m.hostContractor.RenewContracts(rpk, contracts)
}

// Renters calls hostContractor.Renters.
func (m *Manager) Renters() []modules.Renter {
	return m.hostContractor.Renters()
}

// SetSatellite sets the satellite dependency of the contractor.
func (m *Manager) SetSatellite(fl modules.FundLocker) {
	m.hostContractor.SetSatellite(fl)
}
