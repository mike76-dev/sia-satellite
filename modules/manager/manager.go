package manager

import (
	"database/sql"
	"errors"
	//"fmt"
	"sync"
	"time"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/contractor"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"
)

var (
	// Nil dependency errors.
	errNilDB      = errors.New("manager cannot use a nil database")
	errNilCS      = errors.New("manager cannot use a nil state")
	errNilTpool   = errors.New("manager cannot use a nil transaction pool")
	errNilWallet  = errors.New("manager cannot use a nil wallet")
	errNilGateway = errors.New("manager cannot use nil gateway")
)

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {
	modules.Alerter

	// SetAllowance sets the amount of money the contractor is allowed to
	// spend on contracts over a given time period, divided among the number
	// of hosts specified. Note that contractor can start forming contracts as
	// soon as SetAllowance is called; that is, it may block.
	SetAllowance(types.PublicKey, modules.Allowance) error

	// Allowance returns the current allowance of the renter.
	Allowance(types.PublicKey) modules.Allowance

	// Close closes the hostContractor.
	Close() error

	// CancelContract cancels the renter's contract.
	CancelContract(types.FileContractID) error

	// Contract returns the contract with the given ID.
	Contract(types.FileContractID) (modules.RenterContract, bool)

	// Contracts returns the staticContracts of the manager's hostContractor.
	Contracts() []modules.RenterContract

	// ContractsByRenter returns the list of the active contracts belonging
	// to a specific renter.
	ContractsByRenter(types.PublicKey) []modules.RenterContract

	// ContractUtility returns the utility field for a given contract, along
	// with a bool indicating if it exists.
	ContractUtility(types.PublicKey, types.PublicKey) (modules.ContractUtility, bool)

	// ContractStatus returns the status of the given contract within the
	// watchdog.
	ContractStatus(fcID types.FileContractID) (modules.ContractWatchStatus, bool)

	// CreateNewRenter inserts a new renter into the map.
	CreateNewRenter(string, types.PublicKey)

	// CurrentPeriod returns the height at which the current allowance period
	// of the renter began.
	CurrentPeriod(types.PublicKey) uint64

	// GetRenter returns the renter with the given public key.
	GetRenter(types.PublicKey) (modules.Renter, error)

	// FormContract forms a contract with the specified host, puts it
	// in the contract set, and returns it.
	//FormContract(*modules.RPCSession, types.SiaPublicKey, types.SiaPublicKey, types.SiaPublicKey, types.BlockHeight, types.Currency) (modules.RenterContract, error)

	// FormContracts forms up to the specified number of contracts, puts them
	// in the contract set, and returns them.
	//FormContracts(types.SiaPublicKey, crypto.SecretKey) ([]modules.RenterContract, error)

	// PeriodSpending returns the amount spent on contracts during the current
	// billing period of the renter.
	PeriodSpending(types.PublicKey) (modules.RenterSpending, error)

	// OldContracts returns the oldContracts of the manager's hostContractor.
	OldContracts() []modules.RenterContract

	// OldContractsByRenter returns the list of the old contracts of
	// a specific renter.
	OldContractsByRenter(types.PublicKey) []modules.RenterContract

	// IsOffline reports whether the specified host is considered offline.
	IsOffline(types.PublicKey) bool

	// RefreshedContract checks if the contract was previously refreshed.
	RefreshedContract(fcid types.FileContractID) bool

	// RenewContract tries to renew the given contract.
	//RenewContract(*modules.RPCSession, types.SiaPublicKey, modules.RenterContract, types.BlockHeight, types.Currency) (modules.RenterContract, error)

	// RenewContracts tries to renew the given set of contracts.
	//RenewContracts(types.SiaPublicKey, crypto.SecretKey, []types.FileContractID) ([]modules.RenterContract, error)

	// Renters return the list of renters.
	Renters() []modules.Renter

	// Synced returns a channel that is closed when the contractor is fully
	// synced with the peer-to-peer network.
	Synced() <-chan struct{}

	// UpdateContract updates the contract with the new revision.
	UpdateContract(types.FileContractRevision, []types.TransactionSignature, types.Currency, types.Currency, types.Currency) error

	// UpdateRenterSettings updates the renter's opt-in settings.
	UpdateRenterSettings(types.PublicKey, modules.RenterSettings, types.PrivateKey) error

	// RenewedFrom returns the ID of the contract the given contract was
	// renewed from, if any.
	RenewedFrom(types.FileContractID) types.FileContractID

	// DeleteRenter deletes the renter data from the memory.
	DeleteRenter(string)
}

// blockTimestamp combines the block height and the time.
type blockTimestamp struct {
	BlockHeight uint64
	Timestamp   time.Time
}

// A Manager contains the information necessary to communicate with the
// hosts.
type Manager struct {
	// Dependencies.
	db             *sql.DB
	cs             modules.ConsensusSet
	hostContractor hostContractor
	hostDB         modules.HostDB
	tpool          modules.TransactionPool
	wallet         modules.Wallet

	// Atomic properties.
	hostAverages modules.HostAverages
	exchRates    map[string]float64
	scusdRate    float64

	// Block heights at the start of the current and the previous months.
	currentMonth blockTimestamp
	prevMonth    blockTimestamp

	// A global DB transaction.
	dbTx    *sql.Tx
	syncing bool

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	tg            siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter
}

// New returns an initialized Manager.
func New(db *sql.DB, cs modules.ConsensusSet, g modules.Gateway, tpool modules.TransactionPool, wallet modules.Wallet, dir string) (*Manager, <-chan error) {
	errChan := make(chan error, 1)

	// Check that all the dependencies were provided.
	if db == nil {
		errChan <- errNilDB
		return nil, errChan
	}
	if cs == nil {
		errChan <- errNilCS
		return nil, errChan
	}
	if g == nil {
		errChan <- errNilGateway
		return nil, errChan
	}
	if tpool == nil {
		errChan <- errNilTpool
		return nil, errChan
	}
	if wallet == nil {
		errChan <- errNilWallet
		return nil, errChan
	}

	// Create the HostDB object.
	hdb, errChanHDB := hostdb.New(db, g, cs, dir)
	if err := modules.PeekErr(errChanHDB); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Manager object.
	m := &Manager{
		cs:             cs,
		db:             db,
		hostDB:         hdb,
		tpool:          tpool,
		wallet:         wallet,

		exchRates: make(map[string]float64),

		staticAlerter:  modules.NewAlerter("manager"),
	}

	// Create the Contractor.
	hc, errChanContractor := contractor.New(db, cs, m, tpool, wallet, hdb, dir)
	if err := modules.PeekErr(errChanContractor); err != nil {
		errChan <- err
		return nil, errChan
	}
	m.hostContractor = hc

	// Call stop in the event of a partial startup.
	defer func() {
		if err := modules.PeekErr(errChan); err != nil {
			errChan <- modules.ComposeErrors(m.tg.Stop(), m.hostDB.Close(), m.hostContractor.Close(), err)
		}
	}()

	// Initialize the Manager's persistence.
	err := m.initPersist(dir)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	return m, errChan
}

// ActiveHosts returns an array of hostDB's active hosts.
func (m *Manager) ActiveHosts() ([]modules.HostDBEntry, error) { return m.hostDB.ActiveHosts() }

// AllHosts returns an array of all hosts.
func (m *Manager) AllHosts() ([]modules.HostDBEntry, error) { return m.hostDB.AllHosts() }

// Close shuts down the manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return modules.ComposeErrors(m.tg.Stop(), m.hostDB.Close(), m.hostContractor.Close())
}

// Filter returns the hostdb's filterMode and filteredHosts.
func (m *Manager) Filter() (modules.FilterMode, map[string]types.PublicKey, []string, error) {
	var fm modules.FilterMode
	hosts := make(map[string]types.PublicKey)
	if err := m.tg.Add(); err != nil {
		return fm, hosts, nil, err
	}
	defer m.tg.Done()
	fm, hosts, netAddresses, err := m.hostDB.Filter()
	if err != nil {
		return fm, hosts, netAddresses, modules.AddContext(err, "error getting hostdb filter:")
	}
	return fm, hosts, netAddresses, nil
}

// SetFilterMode sets the hostdb filter mode.
func (m *Manager) SetFilterMode(lm modules.FilterMode, hosts []types.PublicKey, netAddresses []string) error {
	if err := m.tg.Add(); err != nil {
		return err
	}
	defer m.tg.Done()

	// Set list mode filter for the hostdb.
	if err := m.hostDB.SetFilterMode(lm, hosts, netAddresses); err != nil {
		return err
	}

	return nil
}

// Host returns the host associated with the given public key.
func (m *Manager) Host(pk types.PublicKey) (modules.HostDBEntry, bool, error) {
	return m.hostDB.Host(pk)
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (m *Manager) InitialScanComplete() (bool, uint64, error) { return m.hostDB.InitialScanComplete() }

// ScoreBreakdown returns the score breakdown of the specific host.
func (m *Manager) ScoreBreakdown(e modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	return m.hostDB.ScoreBreakdown(e)
}

// EstimateHostScore returns the estimated host score.
func (m *Manager) EstimateHostScore(a modules.Allowance, e modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	return m.hostDB.EstimateHostScore(a, e)
}

// RandomHosts picks up to the specified number of random hosts from the
// hostdb sorted by score.
func (m *Manager) RandomHosts(n uint64, a modules.Allowance) ([]modules.HostDBEntry, error) {
	return m.hostDB.RandomHostsWithAllowance(int(n), nil, nil, a)
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

// ContractsByRenter returns the contracts belonging to a specific renter.
func (m *Manager) ContractsByRenter(rpk types.PublicKey) []modules.RenterContract {
	return m.hostContractor.ContractsByRenter(rpk)
}

// RefreshedContract calls hostContractor.RefreshedContract
func (m *Manager) RefreshedContract(fcid types.FileContractID) bool {
	return m.hostContractor.RefreshedContract(fcid)
}

// OldContracts calls hostContractor.OldContracts expired.
func (m *Manager) OldContracts() []modules.RenterContract {
	return m.hostContractor.OldContracts()
}

// OldContractsByRenter returns the old contracts of a specific renter.
func (m *Manager) OldContractsByRenter(rpk types.PublicKey) []modules.RenterContract {
	return m.hostContractor.OldContractsByRenter(rpk)
}

// PriceEstimation estimates the cost in siacoins of forming contracts with
// the hosts. The estimation will be done using the provided allowance.
// The final allowance used will be returned.
/*func (m *Manager) PriceEstimation(allowance modules.Allowance) (float64, modules.Allowance, error) {
	if err := m.threads.Add(); err != nil {
		return 0, modules.Allowance{}, err
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
	totalStorageCost = totalStorageCost.Mul64(allowance.TotalShards).Div64(allowance.MinShards)
	totalUploadCost = totalUploadCost.Mul64(allowance.TotalShards).Div64(allowance.MinShards)

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
	txnsFees := feePerByte.Mul64(smodules.EstimatedFileContractTransactionSetSize).Mul64(uint64(allowance.Hosts)).Mul64(3)
	totalContractCost = totalContractCost.Add(txnsFees)
	totalContractCost = totalContractCost.Mul64(2)

	// Try to figure out what the total contract cost would be for the
	// Siafund fee.
	totalCost := totalContractCost.Add(totalDownloadCost)
	totalCost = totalCost.Add(totalUploadCost)
	totalCost = totalCost.Add(totalStorageCost)
	totalCost = totalCost.Mul64(10) // Quite generous.

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
}*/

// ContractPriceEstimation estimates the cost in siacoins of forming a contract
// with the given host.
/*func (m *Manager) ContractPriceEstimation(hpk types.SiaPublicKey, endHeight types.BlockHeight, storage uint64, upload uint64, download uint64, minShards uint64, totalShards uint64) (types.Currency, float64, error) {
	if err := m.threads.Add(); err != nil {
		return types.ZeroCurrency, 0, err
	}
	defer m.threads.Done()

	// Get the host.
	host, ok, err := m.hostDB.Host(hpk)
	if err != nil {
		return types.ZeroCurrency, 0, err
	}
	if !ok {
		return types.ZeroCurrency, 0, errors.New("host not found")
	}
	if host.Filtered {
		return types.ZeroCurrency, 0, errors.New("host filtered")
	}

	height := m.cs.Height()
	period := endHeight - height
	contractCost := host.ContractPrice
	downloadCost := host.DownloadBandwidthPrice
	storageCost := host.StoragePrice
	uploadCost := host.UploadBandwidthPrice

	// Convert to match the provided values.
	downloadCost = downloadCost.Mul64(download).Div64(uint64(period))
	storageCost = storageCost.Mul64(storage)
	uploadCost = uploadCost.Mul64(upload).Div64(uint64(period))

	// Factor in redundancy.
	storageCost = storageCost.Mul64(totalShards).Div64(minShards)
	uploadCost = uploadCost.Mul64(totalShards).Div64(minShards)

	// Add the cost of paying the transaction fees and then double the contract
	// cost to account for renewing.
	_, feePerByte := m.tpool.FeeEstimation()
	txnsFees := feePerByte.Mul64(smodules.EstimatedFileContractTransactionSetSize).Mul64(3)
	contractCost = contractCost.Add(txnsFees)
	contractCost = contractCost.Mul64(2)

	// Try to figure out what the total contract cost would be for the
	// Siafund fee.
	funding := contractCost.Add(downloadCost)
	funding = funding.Add(uploadCost)
	funding = funding.Add(storageCost)
	funding = funding.Mul64(10) // Quite generous.

	// Determine host collateral to be added to Siafund fee.
	// Assume that the ContractPrice equals contractCost and that
	// the txnFee was zero. It doesn't matter since RenterPayoutsPreTax
	// simply subtracts both values from the funding.
	host.ContractPrice = contractCost
	_, _, collateral, err := smodules.RenterPayoutsPreTax(host, funding, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, period, storage)
	if err != nil {
		return types.ZeroCurrency, 0, err
	}

	// Add in siafund fee. which should be around 10%. The 10% siafund fee
	// accounts for paying 3.9% siafund on transactions and host collateral. We
	// estimate the renter to spend all of it's allowance so the siafund fee
	// will be calculated on the sum of the allowance and the hosts collateral.
	totalPayout := funding.Add(collateral)
	siafundFee := types.Tax(height, totalPayout)
	contractCost = contractCost.Add(siafundFee)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions.
	contractCost = contractCost.MulFloat(1.2)
	downloadCost = downloadCost.MulFloat(1.2)
	storageCost = storageCost.MulFloat(1.2)
	uploadCost = uploadCost.MulFloat(1.2)

	est := contractCost.Add(downloadCost)
	est = est.Add(storageCost)
	est = est.Add(uploadCost)
	est.MulFloat(modules.SatelliteOverhead)

	cost, _ := est.Float64()
	h, _ := types.SiacoinPrecision.Float64()

	return funding, cost / h, nil
}*/

// SetAllowance calls hostContractor.SetAllowance.
func (m *Manager) SetAllowance(rpk types.PublicKey, a modules.Allowance) error {
	return m.hostContractor.SetAllowance(rpk, a)
}

// GetRenter calls hostContractor.GetRenter.
func (m *Manager) GetRenter(rpk types.PublicKey) (modules.Renter, error) {
	return m.hostContractor.GetRenter(rpk)
}

// CreateNewRenter calls hostContractor.CreateNewRenter.
func (m *Manager) CreateNewRenter(email string, pk types.PublicKey) {
	m.hostContractor.CreateNewRenter(email, pk)
}

// FormContracts calls hostContractor.FormContracts.
/*func (m *Manager) FormContracts(rpk types.SiaPublicKey, rsk crypto.SecretKey) ([]modules.RenterContract, error) {
	return m.hostContractor.FormContracts(rpk, rsk)
}*/

// RenewContracts calls hostContractor.RenewContracts.
/*func (m *Manager) RenewContracts(rpk types.SiaPublicKey, rsk crypto.SecretKey, contracts []types.FileContractID) ([]modules.RenterContract, error) {
	return m.hostContractor.RenewContracts(rpk, rsk, contracts)
}*/

// Renters calls hostContractor.Renters.
func (m *Manager) Renters() []modules.Renter {
	return m.hostContractor.Renters()
}

// UpdateContract updates the contract with the new revision.
func (m *Manager) UpdateContract(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	return m.hostContractor.UpdateContract(rev, sigs, uploads, downloads, fundAccount)
}

// RenewedFrom returns the ID of the contract the given contract was renewed
// from, if any.
func (m *Manager) RenewedFrom(fcid types.FileContractID) types.FileContractID {
	return m.hostContractor.RenewedFrom(fcid)
}

// DeleteRenter deletes the renter data from the memory.
func (m *Manager) DeleteRenter(email string) {
	m.hostContractor.DeleteRenter(email)
}

// Contract calls hostContractor.Contract.
func (m *Manager) Contract(fcid types.FileContractID) (modules.RenterContract, bool) {
	return m.hostContractor.Contract(fcid)
}

// FormContract calls hostContractor.FormContract.
/*func (m *Manager) FormContract(s *modules.RPCSession, pk, rpk, hpk types.SiaPublicKey, endHeight types.BlockHeight, funding types.Currency) (modules.RenterContract, error) {
	return m.hostContractor.FormContract(s, pk, rpk, hpk, endHeight, funding)
}*/

// RenewContract calls hostContractor.RenewContract.
/*func (m *Manager) RenewContract(s *modules.RPCSession, pk types.SiaPublicKey, contract modules.RenterContract, endHeight types.BlockHeight, funding types.Currency) (modules.RenterContract, error) {
	return m.hostContractor.RenewContract(s, pk, contract, endHeight, funding)
}*/

// UpdateRenterSettings calls hostContractor.UpdateRenterSettings.
func (m *Manager) UpdateRenterSettings(rpk types.PublicKey, settings modules.RenterSettings, sk types.PrivateKey) error {
	return m.hostContractor.UpdateRenterSettings(rpk, settings, sk)
}

// LockSiacoins locks the specified amount of Siacoins in the user balance.
func (m *Manager) LockSiacoins(email string, amount float64) error {
	// Sanity check.
	if amount <= 0 {
		return errors.New("wrong amount")
	}

	// Retrieve the user balance.
	ub, err := m.GetBalance(email)
	if err != nil {
		return err
	}

	// Include the Satellite fee.
	amountWithFee := amount * modules.SatelliteOverhead
	if amountWithFee > ub.Balance {
		m.log.Println("WARN: trying to lock more than the available balance")
		amountWithFee = ub.Balance
	}

	// Calculate the new balance.
	ub.Locked += amount
	ub.Balance -= amountWithFee

	// Save the new balance.
	err = m.UpdateBalance(email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	us, err := m.getSpendings(email)
	if err != nil {
		return err
	}
	us.CurrentLocked += amount
	us.CurrentOverhead += amountWithFee - amount

	return m.updateSpendings(email, *us)
}

// UnlockSiacoins unlocks the specified amount of Siacoins in the user balance.
func (m *Manager) UnlockSiacoins(email string, amount, total float64, height uint64) error {
	// Sanity check.
	if amount <= 0 || total <= 0 || amount > total {
		return errors.New("wrong amount")
	}

	// Retrieve the user balance.
	ub, err := m.GetBalance(email)
	if err != nil {
		return err
	}

	// Include the Satellite fee.
	totalWithFee := total * modules.SatelliteOverhead

	// Calculate the new balance.
	unlocked := amount
	burned := totalWithFee - amount
	if totalWithFee > ub.Locked {
		m.log.Println("WARN: trying to unlock more than the locked balance")
		if burned < ub.Locked {
			unlocked = ub.Locked - burned
		} else {
			burned = ub.Locked
			unlocked = 0
		}
	}
	ub.Locked -= (unlocked + burned)
	ub.Balance += unlocked

	// Save the new balance.
	err = m.UpdateBalance(email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	m.mu.Lock()
	prevMonth := m.prevMonth.BlockHeight
	currentMonth := m.currentMonth.BlockHeight
	m.mu.Unlock()
	if height < prevMonth {
		// Spending outside the reporting period.
		return nil
	}
	us, err := m.getSpendings(email)
	if err != nil {
		return err
	}
	if height < currentMonth {
		us.PrevLocked -= (unlocked + burned)
		us.PrevUsed += burned
	} else {
		us.CurrentLocked -= (unlocked + burned)
		us.CurrentUsed += burned
	}

	return m.updateSpendings(email, *us)
}

// RetrieveSpendings retrieves the user's spendings in the given currency.
func (m *Manager) RetrieveSpendings(email string, currency string) (*modules.UserSpendings, error) {
	// Get exchange rate.
	scRate, err := m.GetSiacoinRate(currency)
	if err != nil {
		return nil, err
	}
	if scRate == 0 {
		return nil, errors.New("couldn't get exchange rate")
	}

	// Get user spendings.
	us, err := m.getSpendings(email)
	if err != nil {
		return nil, err
	}
	us.SCRate = scRate

	return us, nil
}

// BlockHeight returns the current block height.
func (m *Manager) BlockHeight() uint64 {
	return m.cs.Height()
}

// FeeEstimation returns the minimum and the maximum estimated fees for
// a transaction.
func (m *Manager) FeeEstimation() (min, max types.Currency) { return m.tpool.FeeEstimation() }

// GetWalletSeed returns the wallet seed.
func (m *Manager) GetWalletSeed() (seed modules.Seed, err error) {
	seed, _, err = m.wallet.PrimarySeed()
	return
}