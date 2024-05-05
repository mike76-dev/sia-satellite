package manager

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/mail"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/contractor"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
)

// A directory for storing temporary files.
const bufferedFilesDir = "temp"

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {
	// SetAllowance sets the amount of money the contractor is allowed to
	// spend on contracts over a given time period, divided among the number
	// of hosts specified. Note that contractor can start forming contracts as
	// soon as SetAllowance is called; that is, it may block.
	SetAllowance(types.PublicKey, modules.Allowance) error

	// Allowance returns the current allowance of the renter.
	Allowance(types.PublicKey) modules.Allowance

	// AcceptContracts accepts a set of contracts from the renter
	// and adds them to the contract set.
	AcceptContracts(types.PublicKey, []modules.ContractMetadata)

	// Close closes the hostContractor.
	Close() error

	// CancelContract cancels the renter's contract.
	CancelContract(types.FileContractID) error

	// CancelUpload cancels a running upload.
	CancelUpload(types.PublicKey, []byte, []byte)

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

	// DeleteMetadata deletes the renter's saved file metadata.
	DeleteMetadata(types.PublicKey) error

	// DeleteObject deletes the saved file metadata object.
	DeleteObject(types.PublicKey, []byte, []byte) error

	// DownloadObject downloads an object and returns it.
	DownloadObject(io.Writer, types.PublicKey, []byte, []byte) error

	// UpdateMetadata updates the file metadata in the database.
	UpdateMetadata(types.PublicKey, modules.FileMetadata) error

	// GetRenter returns the renter with the given public key.
	GetRenter(types.PublicKey) (modules.Renter, error)

	// FormContract forms a contract with the specified host, puts it
	// in the contract set, and returns it.
	FormContract(*modules.RPCSession, types.PublicKey, types.PublicKey, types.PublicKey, types.Currency, uint64) (modules.RenterContract, error)

	// FormContracts forms up to the specified number of contracts, puts them
	// in the contract set, and returns them.
	FormContracts(types.PublicKey, types.PrivateKey) ([]modules.RenterContract, error)

	// GetModifiedSlabs returns the slabs modified since the last retrieval.
	GetModifiedSlabs(types.PublicKey) ([]modules.Slab, error)

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
	RenewContract(*modules.RPCSession, types.PublicKey, modules.RenterContract, types.Currency, uint64) (modules.RenterContract, error)

	// RenewContracts tries to renew the given set of contracts.
	RenewContracts(types.PublicKey, types.PrivateKey, []types.FileContractID) ([]modules.RenterContract, error)

	// Renters return the list of renters.
	Renters() []modules.Renter

	// RetrieveMetadata retrieves the file metadata from the database.
	RetrieveMetadata(types.PublicKey, []modules.BucketFiles) ([]modules.FileMetadata, error)

	// StartUploading initiates uploading buffered files.
	StartUploading()

	// Synced returns a channel that is closed when the contractor is fully
	// synced with the peer-to-peer network.
	Synced() <-chan struct{}

	// UpdateContract updates the contract with the new revision.
	UpdateContract(types.FileContractRevision, []types.TransactionSignature, types.Currency, types.Currency, types.Currency) error

	// UpdateRenterSettings updates the renter's opt-in settings.
	UpdateRenterSettings(types.PublicKey, modules.RenterSettings, types.PrivateKey, types.PrivateKey) error

	// UpdateSlab updates a file slab after a successful migration.
	UpdateSlab(types.PublicKey, modules.Slab, bool) error

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
	ms             mail.MailSender
	cm             *chain.Manager
	s              modules.Syncer
	hostContractor hostContractor
	hostDB         modules.HostDB
	wallet         modules.Wallet

	// Atomic properties.
	hostAverages        modules.HostAverages
	lastEstimationHosts []modules.HostDBEntry
	exchRates           map[string]float64
	maintenance         bool
	bufferSize          uint64
	multipartUploads    map[types.Hash256]struct{}
	tip                 types.ChainIndex

	// Block heights at the start of the current and the previous months.
	currentMonth blockTimestamp
	prevMonth    blockTimestamp

	// Email preferences.
	name          string // Name of the satellite node.
	email         string
	warnThreshold types.Currency

	// Last found block timestamp.
	lastBlockTimestamp time.Time

	// A global DB transaction.
	dbTx    *sql.Tx
	syncing bool

	// Utilities.
	log *zap.Logger
	mu  sync.RWMutex
	tg  siasync.ThreadGroup
	dir string
}

// New returns an initialized Manager.
func New(db *sql.DB, ms mail.MailSender, cm *chain.Manager, s modules.Syncer, wallet modules.Wallet, dir string, name string) (*Manager, <-chan error) {
	errChan := make(chan error, 1)

	// Create the HostDB object.
	hdb, errChanHDB := hostdb.New(db, cm, s, dir)
	if err := modules.PeekErr(errChanHDB); err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the Manager object.
	m := &Manager{
		name:   name,
		cm:     cm,
		db:     db,
		ms:     ms,
		hostDB: hdb,
		s:      s,
		wallet: wallet,

		exchRates:        make(map[string]float64),
		multipartUploads: make(map[types.Hash256]struct{}),

		dir: dir,
	}

	// Create the Contractor.
	hc, errChanContractor := contractor.New(db, cm, s, m, wallet, hdb, dir)
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

	// Create the temporary directory if it doesn't exist.
	err := os.MkdirAll(filepath.Join(dir, bufferedFilesDir), 0700)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Initialize the Manager's persistence.
	err = m.initPersist(dir)
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
func (m *Manager) PriceEstimation(allowance modules.Allowance, invoicing bool) (float64, modules.Allowance, error) {
	if err := m.tg.Add(); err != nil {
		return 0, modules.Allowance{}, err
	}
	defer m.tg.Done()

	// If the funding is zero, set the default one for the estimation.
	if allowance.Funds.IsZero() {
		allowance.Funds = types.HastingsPerSiacoin.Mul64(2500)
	}

	// Get hosts for the estimate.
	var hosts []modules.HostDBEntry
	hostmap := make(map[string]struct{})

	// Start by grabbing hosts from the contracts.
	contracts := m.Contracts()
	var pks []types.PublicKey
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
		// Re-initialize the list with PublicKeys to hold the public keys
		// from the current set of hosts. This list will be used as address
		// filter when requesting random hosts.
		var pks []types.PublicKey
		for _, host := range hosts {
			pks = append(pks, host.PublicKey)
		}
		// Grab hosts to perform the estimation.
		var err error
		randHosts, err := m.hostDB.RandomHostsWithAllowance(int(allowance.Hosts)-len(hosts), pks, pks, allowance)
		if err != nil {
			return 0, allowance, modules.AddContext(err, "could not generate estimate, could not get random hosts")
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
		totalContractCost = totalContractCost.Add(host.Settings.ContractPrice)
		totalDownloadCost = totalDownloadCost.Add(host.Settings.DownloadBandwidthPrice)
		totalStorageCost = totalStorageCost.Add(host.Settings.StoragePrice)
		totalUploadCost = totalUploadCost.Add(host.Settings.UploadBandwidthPrice)
	}

	// Convert values to match the allowance.
	totalDownloadCost = totalDownloadCost.Mul64(allowance.ExpectedDownload).Div64(allowance.Period)
	totalStorageCost = totalStorageCost.Mul64(allowance.ExpectedStorage)
	totalUploadCost = totalUploadCost.Mul64(allowance.ExpectedUpload).Div64(allowance.Period)

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
	feePerByte := m.cm.RecommendedFee()
	txnsFees := feePerByte.Mul64(2048).Mul64(allowance.Hosts).Mul64(3)
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
		host.Settings.ContractPrice = contractCostPerHost
		expectedStorage := allowance.ExpectedStorage / uint64(len(hosts))
		_, _, collateral, err := modules.RenterPayoutsPreTax(host, fundingPerHost, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, allowance.Period, expectedStorage)
		if err != nil {
			continue
		}
		hostCollateral = hostCollateral.Add(collateral)
		numHosts++
	}

	// Divide by zero check. The only way to get 0 numHosts is if
	// RenterPayoutsPreTax errors for every host. This would happen if the
	// funding of the allowance is not enough as that would cause the
	// fundingPerHost to be less than the contract price.
	if numHosts == 0 {
		return 0, allowance, errors.New("funding insufficient for number of hosts")
	}

	// Calculate average collateral and determine collateral for allowance.
	hostCollateral = hostCollateral.Div64(numHosts)
	hostCollateral = hostCollateral.Mul64(allowance.Hosts)

	// Add in Siafund fee. which should be around 10%. The 10% Siafund fee
	// accounts for paying 3.9% Siafund on transactions and host collateral. We
	// estimate the renter to spend all of its allowance so the Siafund fee
	// will be calculated on the sum of the allowance and the hosts collateral.
	totalPayout := totalCost.Add(hostCollateral)
	siafundFee := modules.Tax(m.cm.Tip().Height, totalPayout)
	totalContractCost = totalContractCost.Add(siafundFee)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions.
	totalContractCost = totalContractCost.Mul64(6).Div64(5)
	totalDownloadCost = totalDownloadCost.Mul64(6).Div64(5)
	totalStorageCost = totalStorageCost.Mul64(6).Div64(5)
	totalUploadCost = totalUploadCost.Mul64(6).Div64(5)

	est := totalContractCost.Add(totalDownloadCost)
	est = est.Add(totalStorageCost)
	est = est.Add(totalUploadCost)
	var fee float64
	if invoicing {
		fee = modules.StaticPricing.FormContract.Invoicing
	} else {
		fee = modules.StaticPricing.FormContract.PrePayment
	}
	cost := modules.Float64(est) * (1 + fee)
	h := modules.Float64(types.HastingsPerSiacoin)
	allowance.Funds = totalCost

	m.mu.Lock()
	m.lastEstimationHosts = hosts
	m.mu.Unlock()

	return cost / h, allowance, nil
}

// ContractPriceEstimation estimates the cost in siacoins of forming a contract
// with the given host.
func (m *Manager) ContractPriceEstimation(hpk types.PublicKey, endHeight uint64, storage uint64, upload uint64, download uint64, minShards uint64, totalShards uint64, invoicing bool) (types.Currency, float64, error) {
	if err := m.tg.Add(); err != nil {
		return types.ZeroCurrency, 0, err
	}
	defer m.tg.Done()

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

	height := m.cm.Tip().Height
	period := endHeight - height
	contractCost := host.Settings.ContractPrice
	downloadCost := host.Settings.DownloadBandwidthPrice
	storageCost := host.Settings.StoragePrice
	uploadCost := host.Settings.UploadBandwidthPrice

	// Convert to match the provided values.
	downloadCost = downloadCost.Mul64(download).Div64(uint64(period))
	storageCost = storageCost.Mul64(storage)
	uploadCost = uploadCost.Mul64(upload).Div64(uint64(period))

	// Factor in redundancy.
	storageCost = storageCost.Mul64(totalShards).Div64(minShards)
	uploadCost = uploadCost.Mul64(totalShards).Div64(minShards)

	// Add the cost of paying the transaction fees and then double the contract
	// cost to account for renewing.
	feePerByte := m.cm.RecommendedFee()
	txnsFees := feePerByte.Mul64(2048).Mul64(3)
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
	host.Settings.ContractPrice = contractCost
	_, _, collateral, err := modules.RenterPayoutsPreTax(host, funding, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, period, storage)
	if err != nil {
		return types.ZeroCurrency, 0, err
	}

	// Add in Siafund fee. which should be around 10%. The 10% siafund fee
	// accounts for paying 3.9% siafund on transactions and host collateral. We
	// estimate the renter to spend all of its allowance so the siafund fee
	// will be calculated on the sum of the allowance and the hosts collateral.
	totalPayout := funding.Add(collateral)
	siafundFee := modules.Tax(height, totalPayout)
	contractCost = contractCost.Add(siafundFee)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions.
	contractCost = contractCost.Mul64(6).Div64(5)
	downloadCost = downloadCost.Mul64(6).Div64(5)
	storageCost = storageCost.Mul64(6).Div64(5)
	uploadCost = uploadCost.Mul64(6).Div64(5)

	est := contractCost.Add(downloadCost)
	est = est.Add(storageCost)
	est = est.Add(uploadCost)
	var fee float64
	if invoicing {
		fee = modules.StaticPricing.FormContract.Invoicing
	} else {
		fee = modules.StaticPricing.FormContract.PrePayment
	}
	cost := modules.Float64(est) * (1 + fee)
	h := modules.Float64(types.HastingsPerSiacoin)

	return funding, cost / h, nil
}

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

// FormContracts forms the specified number of contracts with the hosts
// and returns them.
func (m *Manager) FormContracts(rpk types.PublicKey, rsk types.PrivateKey, a modules.Allowance) ([]modules.RenterContract, error) {
	// Get the user balance.
	renter, err := m.GetRenter(rpk)
	if err != nil {
		return nil, err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return nil, err
	}

	// Get the estimated costs and update the allowance with them.
	estimation, a, err := m.PriceEstimation(a, ub.Subscribed)
	if err != nil {
		return nil, err
	}

	// Check if the balance is sufficient to cover the costs.
	if !ub.Subscribed && ub.Balance < estimation {
		return nil, errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return nil, errors.New("account on hold")
	}

	// Set the allowance.
	err = m.SetAllowance(rpk, a)
	if err != nil {
		return nil, err
	}

	// Form the contracts.
	contractSet, err := m.hostContractor.FormContracts(rpk, rsk)

	return contractSet, err
}

// RenewContracts renews a set of contracts and returns a new set.
func (m *Manager) RenewContracts(rpk types.PublicKey, rsk types.PrivateKey, a modules.Allowance, contracts []types.FileContractID) ([]modules.RenterContract, error) {
	// Get the user balance.
	renter, err := m.GetRenter(rpk)
	if err != nil {
		return nil, err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return nil, err
	}

	// Get the estimated costs and update the allowance with them.
	estimation, a, err := m.PriceEstimation(a, ub.Subscribed)
	if err != nil {
		return nil, err
	}

	// Check if the balance is sufficient to cover the costs.
	if !ub.Subscribed && ub.Balance < estimation {
		return nil, errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return nil, errors.New("account on hold")
	}

	// Set the allowance.
	err = m.SetAllowance(rpk, a)
	if err != nil {
		return nil, err
	}

	// Renew the contracts.
	contractSet, err := m.hostContractor.RenewContracts(rpk, rsk, contracts)

	return contractSet, err
}

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

// FormContract creates a contract with a single host using the new
// Renter-Satellite protocol.
func (m *Manager) FormContract(s *modules.RPCSession, rpk types.PublicKey, epk types.PublicKey, hpk types.PublicKey, endHeight uint64, storage uint64, upload uint64, download uint64, minShards uint64, totalShards uint64) (modules.RenterContract, error) {
	// Get the user balance.
	renter, err := m.GetRenter(rpk)
	if err != nil {
		return modules.RenterContract{}, err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Get the estimated costs.
	funding, estimation, err := m.ContractPriceEstimation(hpk, endHeight, storage, upload, download, minShards, totalShards, ub.Subscribed)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Check if the balance is sufficient to cover the costs.
	if !ub.Subscribed && ub.Balance < estimation {
		return modules.RenterContract{}, errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return modules.RenterContract{}, errors.New("account on hold")
	}

	// Form the contract.
	contract, err := m.hostContractor.FormContract(s, rpk, epk, hpk, funding, endHeight)

	return contract, err
}

// RenewContract renews a contract using the new Renter-Satellite protocol.
func (m *Manager) RenewContract(s *modules.RPCSession, rpk types.PublicKey, fcid types.FileContractID, endHeight uint64, storage uint64, upload uint64, download uint64, minShards uint64, totalShards uint64) (modules.RenterContract, error) {
	// Get the contract to renew.
	contract, exists := m.Contract(fcid)
	if !exists {
		return modules.RenterContract{}, errors.New("contract not found")
	}

	// Get the user balance.
	renter, err := m.GetRenter(rpk)
	if err != nil {
		return modules.RenterContract{}, err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Get the estimated costs.
	funding, estimation, err := m.ContractPriceEstimation(contract.HostPublicKey, endHeight, storage, upload, download, minShards, totalShards, ub.Subscribed)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Check if the balance is sufficient to cover the costs.
	if !ub.Subscribed && ub.Balance < estimation {
		return modules.RenterContract{}, errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return modules.RenterContract{}, errors.New("account on hold")
	}

	// Renew the contract.
	newContract, err := m.hostContractor.RenewContract(s, rpk, contract, funding, endHeight)

	return newContract, err
}

// UpdateRenterSettings calls hostContractor.UpdateRenterSettings.
func (m *Manager) UpdateRenterSettings(rpk types.PublicKey, settings modules.RenterSettings, sk, ak types.PrivateKey) error {
	return m.hostContractor.UpdateRenterSettings(rpk, settings, sk, ak)
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
	var fee float64
	if ub.Subscribed {
		fee = modules.StaticPricing.FormContract.Invoicing
	} else {
		fee = modules.StaticPricing.FormContract.PrePayment
	}
	amountWithFee := amount * (1 + fee)
	if !ub.Subscribed && amountWithFee > ub.Balance {
		m.log.Warn("trying to lock more than the available balance")
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
	year, month, _ := time.Now().Date()
	us, err := m.GetSpendings(email, int(month), year)
	if err != nil {
		return err
	}
	us.Locked += amount
	us.Used += amountWithFee - amount
	us.Overhead += amountWithFee - amount

	return m.UpdateSpendings(email, us, int(month), year)
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

	// Calculate the new balance.
	unlocked := amount
	burned := total - amount
	if total > ub.Locked {
		m.log.Warn("trying to unlock more than the locked balance")
		if burned < ub.Locked {
			unlocked = ub.Locked - burned
		} else {
			burned = ub.Locked
			unlocked = 0
		}
	}
	ub.Locked -= (unlocked + burned)
	ub.Balance += unlocked

	// Remove the hold if there was any.
	if ub.Balance >= 0 {
		ub.OnHold = 0
	}

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
	var month int
	year, mon, _ := time.Now().Date()
	month = int(mon)
	if height < currentMonth {
		month--
		if month == 0 {
			month = 12
			year--
		}
	}
	us, err := m.GetSpendings(email, month, year)
	if err != nil {
		return err
	}
	us.Locked -= (unlocked + burned)
	us.Used += burned

	return m.UpdateSpendings(email, us, month, year)
}

// RetrieveSpendings retrieves the user's spendings in the given currency.
func (m *Manager) RetrieveSpendings(email string, currency string) ([]modules.UserSpendings, error) {
	// Get exchange rate.
	scRate, err := m.GetSiacoinRate(currency)
	if err != nil {
		return nil, err
	}
	if scRate == 0 {
		return nil, errors.New("couldn't get exchange rate")
	}

	// Get user spendings.
	var cm, pm, cy, py int
	year, month, _ := time.Now().Date()
	cm = int(month)
	cy = year
	pm = cm - 1
	py = year
	if pm == 0 {
		pm = 12
		py--
	}
	cus, err := m.GetSpendings(email, cm, cy)
	if err != nil {
		return nil, err
	}
	cus.SCRate = scRate
	pus, err := m.GetSpendings(email, pm, py)
	if err != nil {
		return nil, err
	}
	pus.SCRate = scRate

	return append([]modules.UserSpendings{cus}, pus), nil
}

// BlockHeight returns the current block height.
func (m *Manager) BlockHeight() uint64 {
	return m.cm.Tip().Height
}

// FeeEstimation returns the minimum and the maximum estimated fees for
// a transaction.
func (m *Manager) FeeEstimation() types.Currency { return m.cm.RecommendedFee() }

// DeleteMetadata deletes the renter's saved file metadata.
func (m *Manager) DeleteMetadata(pk types.PublicKey) error {
	return m.hostContractor.DeleteMetadata(pk)
}

// UpdateMetadata updates the file metadata in the database.
func (m *Manager) UpdateMetadata(pk types.PublicKey, fm modules.FileMetadata) error {
	// Get the balance and check if it is sufficient.
	renter, err := m.GetRenter(pk)
	if err != nil {
		return err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return err
	}
	var fee float64
	if ub.Subscribed {
		fee = modules.StaticPricing.SaveMetadata.Invoicing
	} else {
		fee = modules.StaticPricing.SaveMetadata.PrePayment
	}
	cost := float64(len(fm.Slabs)) * fee
	if !ub.Subscribed && ub.Balance < cost {
		return errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return errors.New("account on hold")
	}

	// Update the metadata.
	err = m.hostContractor.UpdateMetadata(pk, fm)
	if err != nil {
		return err
	}

	// Deduct from the account balance.
	ub.Balance -= cost
	err = m.UpdateBalance(renter.Email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	year, month, _ := time.Now().Date()
	us, err := m.GetSpendings(renter.Email, int(month), year)
	if err != nil {
		return err
	}
	us.Used += cost
	us.Overhead += cost
	us.SlabsSaved += uint64(len(fm.Slabs))

	return m.UpdateSpendings(renter.Email, us, int(month), year)
}

// DeleteObject deletes the saved file metadata object.
func (m *Manager) DeleteObject(pk types.PublicKey, bucket, path []byte) error {
	return m.hostContractor.DeleteObject(pk, bucket, path)
}

// RetrieveMetadata retrieves the file metadata from the database.
func (m *Manager) RetrieveMetadata(pk types.PublicKey, present []modules.BucketFiles) ([]modules.FileMetadata, error) {
	// Get the account balance.
	renter, err := m.GetRenter(pk)
	if err != nil {
		return nil, err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return nil, err
	}

	// Retrieve metadata.
	md, err := m.hostContractor.RetrieveMetadata(pk, present)
	if err != nil {
		return nil, err
	}

	// Deduct from the balance.
	var cost float64
	var numRetrieved uint64
	var fee float64
	if ub.Subscribed {
		fee = modules.StaticPricing.RetrieveMetadata.Invoicing
	} else {
		fee = modules.StaticPricing.RetrieveMetadata.PrePayment
	}
	for _, fm := range md {
		cost += float64(len(fm.Slabs)) * fee
		numRetrieved += uint64(len(fm.Slabs))
	}
	if !ub.Subscribed && ub.Balance < cost {
		return nil, errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return nil, errors.New("account on hold")
	}
	ub.Balance -= cost
	err = m.UpdateBalance(renter.Email, ub)
	if err != nil {
		return nil, err
	}

	// Update the spendings.
	year, month, _ := time.Now().Date()
	us, err := m.GetSpendings(renter.Email, int(month), year)
	if err != nil {
		return nil, err
	}
	us.Used += cost
	us.Overhead += cost
	us.SlabsRetrieved += numRetrieved

	err = m.UpdateSpendings(renter.Email, us, int(month), year)
	if err != nil {
		return nil, err
	}

	return md, nil
}

// UpdateSlab updates a file slab after a successful migration.
func (m *Manager) UpdateSlab(pk types.PublicKey, slab modules.Slab, packed bool) error {
	// No fee on saving a packed slab's metadata.
	if packed {
		return m.hostContractor.UpdateSlab(pk, slab, packed)
	}

	// Get the balance and check if it is sufficient.
	renter, err := m.GetRenter(pk)
	if err != nil {
		return err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return err
	}
	var fee float64
	if ub.Subscribed {
		fee = modules.StaticPricing.SaveMetadata.Invoicing
	} else {
		fee = modules.StaticPricing.SaveMetadata.PrePayment
	}
	if !ub.Subscribed && ub.Balance < fee {
		return errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return errors.New("account on hold")
	}

	// Update the slab.
	err = m.hostContractor.UpdateSlab(pk, slab, packed)
	if err != nil {
		return err
	}

	// Deduct from the account balance.
	ub.Balance -= fee
	err = m.UpdateBalance(renter.Email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	year, month, _ := time.Now().Date()
	us, err := m.GetSpendings(renter.Email, int(month), year)
	if err != nil {
		return err
	}
	us.Used += fee
	us.Overhead += fee
	us.SlabsSaved += 1

	return m.UpdateSpendings(renter.Email, us, int(month), year)
}

// AcceptContracts accepts a set of contracts from the renter
// and adds them to the contract set.
func (m *Manager) AcceptContracts(rpk types.PublicKey, contracts []modules.ContractMetadata) {
	m.hostContractor.AcceptContracts(rpk, contracts)
}

// DownloadObject downloads an object and returns it.
func (m *Manager) DownloadObject(w io.Writer, rpk types.PublicKey, bucket, path []byte) error {
	return m.hostContractor.DownloadObject(w, rpk, bucket, path)
}

// GetEmailPreferences returns the email preferences.
func (m *Manager) GetEmailPreferences() (string, types.Currency) {
	return m.email, m.warnThreshold
}

// SetEmailPreferences changes the email preferences.
func (m *Manager) SetEmailPreferences(email string, threshold types.Currency) error {
	err := m.setEmailPreferences(email, threshold)
	if err != nil {
		m.log.Error("couldn't save email preferences", zap.Error(err))
	}
	return err
}

// Maintenance returns true if the maintenance mode is active.
func (m *Manager) Maintenance() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maintenance
}

// GetModifiedSlabs returns the slabs modified since the last retrieval.
func (m *Manager) GetModifiedSlabs(rpk types.PublicKey) ([]modules.Slab, error) {
	// Retrieve the slabs.
	slabs, err := m.hostContractor.GetModifiedSlabs(rpk)
	if err != nil {
		return nil, err
	}

	// Get the user's balance and check if it is sufficient.
	renter, err := m.GetRenter(rpk)
	if err != nil {
		return nil, err
	}
	ub, err := m.GetBalance(renter.Email)
	if err != nil {
		return nil, err
	}
	var fee float64
	if ub.Subscribed {
		fee = modules.StaticPricing.RetrieveMetadata.Invoicing * float64(len(slabs))
	} else {
		fee = modules.StaticPricing.RetrieveMetadata.Invoicing * float64(len(slabs))
	}
	if !ub.Subscribed && ub.Balance < fee {
		return nil, errors.New("insufficient account balance")
	}
	if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
		return nil, errors.New("account on hold")
	}

	// Deduct from the account balance.
	ub.Balance -= fee
	err = m.UpdateBalance(renter.Email, ub)
	if err != nil {
		return nil, err
	}

	// Update the spendings.
	year, month, _ := time.Now().Date()
	us, err := m.GetSpendings(renter.Email, int(month), year)
	if err != nil {
		return nil, err
	}
	us.Used += fee
	us.Overhead += fee
	us.SlabsRetrieved += uint64(len(slabs))

	err = m.UpdateSpendings(renter.Email, us, int(month), year)
	if err != nil {
		return nil, err
	}

	return slabs, nil
}

// BufferedFilesDir returns the path to the buffered files directory.
func (m *Manager) BufferedFilesDir() string {
	return filepath.Join(m.dir, bufferedFilesDir)
}
