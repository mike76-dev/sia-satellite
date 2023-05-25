// satellite package declares the satellite module, which listens for
// renter connections and contacts hosts on the renter's behalf.
package satellite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/satellite/manager"
	"github.com/mike76-dev/sia-satellite/satellite/provider"
	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"

	"go.sia.tech/siad/crypto"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
	"go.sia.tech/siad/types"
)

var (
	// Nil dependency errors.
	errNilDB      = errors.New("satellite cannot use a nil database")
	errNilSMux    = errors.New("satellite cannot use a nil siamux")
	errNilCS      = errors.New("satellite cannot use a nil state")
	errNilTpool   = errors.New("satellite cannot use a nil transaction pool")
	errNilWallet  = errors.New("satellite cannot use a nil wallet")
	errNilGateway = errors.New("satellite cannot use nil gateway")
)

// blockHeightTimestamp combines the block height and the time.
type blockHeightTimestamp struct {
	BlockHeight types.BlockHeight `json:"blockheight"`
	Timestamp   uint64            `json:"timestamp"`
}

// A Satellite contains the information necessary to communicate both with
// the renters and with the hosts.
type Satellite struct {
	// Dependencies.
	db     *sql.DB
	cs     smodules.ConsensusSet
	g      smodules.Gateway
	tpool  smodules.TransactionPool
	wallet smodules.Wallet

	// ACID fields - these fields need to be updated in serial, ACID transactions.
	publicKey types.SiaPublicKey
	secretKey crypto.SecretKey
	exchRates map[string]float64
	scusdRate float64

	// Block heights at the start of the current and the previous months.
	currentMonth blockHeightTimestamp
	prevMonth    blockHeightTimestamp

	// Submodules.
	m *manager.Manager
	p *provider.Provider

	// Utilities.
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	threads       siasync.ThreadGroup
	staticAlerter *smodules.GenericAlerter
}

// PublicKey returns the satellite's public key
func (s *Satellite) PublicKey() types.SiaPublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicKey
}

// SecretKey returns the satellite's secret key
func (s *Satellite) SecretKey() crypto.SecretKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secretKey
}

// New returns an initialized Satellite.
func New(cs smodules.ConsensusSet, g smodules.Gateway, tpool smodules.TransactionPool, wallet smodules.Wallet, db *sql.DB, mux *siamux.SiaMux, satelliteAddr string, persistDir string) (*Satellite, <-chan error) {
	errChan := make(chan error, 1)
	defer close(errChan)

	// Check that all the dependencies were provided.
	if db == nil {
		errChan <- errNilDB
		return nil, errChan
	}
	if mux == nil {
		errChan <- errNilSMux
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

	// Create the perist directory if it does not yet exist.
	err := os.MkdirAll(persistDir, 0700)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the manager.
	m, errChanM := manager.New(cs, g, tpool, wallet, db, mux, persistDir)
	if err = smodules.PeekErr(errChanM); err != nil {
		errChan <- errors.AddContext(err, "unable to create manager")
		return nil, errChan
	}

	// Create the provider.
	p, errChanP := provider.New(g, satelliteAddr, persistDir)
	if err = smodules.PeekErr(errChanP); err != nil {
		errChan <- errors.AddContext(err, "unable to create provider")
		return nil, errChan 
	}

	// Create the satellite object.
	s := &Satellite{
		cs:     cs,
		g:      g,
		tpool:  tpool,
		wallet: wallet,

		exchRates: make(map[string]float64),

		db: db,
		m:  m,
		p:  p,

		persistDir:    persistDir,
		staticAlerter: smodules.NewAlerter("satellite"),
	}
	p.SetSatellite(s)
	m.SetSatellite(s)

	// Call stop in the event of a partial startup.
	defer func() {
		if err = smodules.PeekErr(errChan); err != nil {
			errChan <- errors.Compose(s.threads.Stop(), err)
		}
	}()

	// Create the logger.
	s.log, err = persist.NewFileLogger(filepath.Join(s.persistDir, logFile))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Establish the closing of the logger.
	s.threads.AfterStop(func() {
		err := s.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the satellite logger:", err)
		}
	})
	s.log.Println("INFO: satellite created, started logging")

	// Load the satellite persistence.
	if loadErr := s.load(); loadErr != nil && !os.IsNotExist(loadErr) {
		errChan <- errors.AddContext(loadErr, "unable to load satellite")
		return nil, errChan
	}

	// Make sure that the satellite saves on shutdown.
	s.threads.AfterStop(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		err := s.saveSync()
		if err != nil {
			s.log.Println("ERROR: Unable to save satellite:", err)
		}
	})

	// Spawn the threads to fetch the exchange rates.
	go s.threadedFetchExchangeRates()
	go s.threadedFetchSCUSDRate()

	// Unsubscribe from the consensus set upon shutdown.
	s.threads.AfterStop(func() {
		cs.Unsubscribe(s)
	})

	// Subscribe to the consensus set in a separate goroutine.
	go func() {
		if err := s.threads.Add(); err != nil {
			errChan <- err
			return
		}
		defer s.threads.Done()
		err := cs.ConsensusSetSubscribe(s, smodules.ConsensusChangeRecent, s.threads.StopChan())
		if err != nil {
			errChan <- err
		}
	}()

	return s, errChan
}

// ActiveHosts calls Manager.ActiveHosts.
func (s *Satellite) ActiveHosts() ([]smodules.HostDBEntry, error) { return s.m.ActiveHosts() }

// AllHosts calls Manager.AllHosts.
func (s *Satellite) AllHosts() ([]smodules.HostDBEntry, error) { return s.m.AllHosts() }

// Close shuts down the satellite.
func (s *Satellite) Close() error {
	var errP, errM, err error

	// Close the provider.
	errP = s.p.Close()

	// Close the manager.
	errM = s.m.Close()

	if err = s.threads.Stop(); err != nil {
		return errors.Compose(errP, errM, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSync()
}

// Filter calls Manager.Filter.
func (s *Satellite) Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error) { return s.m.Filter() }

// SetFilterMode calls Manager.SetFilterMode.
func (s *Satellite) SetFilterMode(lm smodules.FilterMode, hosts []types.SiaPublicKey, netAddresses []string) error { return s.m.SetFilterMode(lm, hosts, netAddresses) }

// Host calls Manager.Host.
func (s *Satellite) Host(spk types.SiaPublicKey) (smodules.HostDBEntry, bool, error) { return s.m.Host(spk) }

// InitialScanComplete calls Manager.InitialScanComplete.
func (s *Satellite) InitialScanComplete() (bool, types.BlockHeight, error) { return s.m.InitialScanComplete() }

// ScoreBreakdown calls Manager.ScoreBreakdown.
func (s *Satellite) ScoreBreakdown(e smodules.HostDBEntry) (smodules.HostScoreBreakdown, error) { return s.m.ScoreBreakdown(e) }

// EstimateHostScore calls Manager.EstimateHostScore.
func (s *Satellite) EstimateHostScore(e smodules.HostDBEntry, a modules.Allowance) (smodules.HostScoreBreakdown, error) { return s.m.EstimateHostScore(e, a) }

// RandomHosts calls Manager.RandomHosts.
func (s *Satellite) RandomHosts(n uint64, a modules.Allowance) ([]smodules.HostDBEntry, error) { return s.m.RandomHosts(n, a) }

// GetAverages calls Manager.GetAverages.
func (s *Satellite) GetAverages() modules.HostAverages { return s.m.GetAverages() }

// FeeEstimation returns the minimum and the maximum estimated fees for
// a transaction.
func (s *Satellite) FeeEstimation() (min, max types.Currency) { return s.tpool.FeeEstimation() }

// GetWalletSeed returns the wallet seed.
func (s *Satellite) GetWalletSeed() (seed smodules.Seed, err error) {
	seed, _, err = s.wallet.PrimarySeed()
	return
}

// UserExists returns true if the renter's public key is found in the
// database. An error is returned as well.
func (s *Satellite) UserExists(rpk types.SiaPublicKey) (exists bool, err error) {
	var count int
	err = s.db.QueryRow("SELECT COUNT(*) FROM renters WHERE public_key = ?", rpk.String()).Scan(&count)
	if err != nil {
		s.log.Println("ERROR: could not query database", err)
	}
	exists = count > 0
	return
}

// CreateNewRenter calls Manager.CreateNewRenter.
func (s *Satellite) CreateNewRenter(email string, pk types.SiaPublicKey) {
	s.m.CreateNewRenter(email, pk)
}

// GetRenter calls Manager.GetRenter.
func (s *Satellite) GetRenter(pk types.SiaPublicKey) (modules.Renter, error) {
	return s.m.GetRenter(pk)
}

// Renters calls Manager.Renters.
func (s *Satellite) Renters() []modules.Renter {
	return s.m.Renters()
}

// SetAllowance calls Manager.SetAllowance.
func (s *Satellite) SetAllowance(rpk types.SiaPublicKey, a modules.Allowance) error {
	return s.m.SetAllowance(rpk, a)
}

// FormContracts forms the specified number of contracts with the hosts
// and returns them.
func (s *Satellite) FormContracts(rpk types.SiaPublicKey, rsk crypto.SecretKey, a modules.Allowance) ([]modules.RenterContract, error) {
	// Get the estimated costs and update the allowance with them.
	estimation, a, err := s.m.PriceEstimation(a)
	if err != nil {
		return nil, err
	}

	// Check if the user balance is sufficient to cover the costs.
	renter, err := s.GetRenter(rpk)
	if err != nil {
		return nil, err
	}
	ub, err := s.GetBalance(renter.Email)
	if err != nil {
		return nil, err
	}
	if ub.Balance < estimation {
		return nil, errors.New("insufficient account balance")
	}

	// Set the allowance.
	err = s.m.SetAllowance(rpk, a)
	if err != nil {
		return nil, err
	}

	// Form the contracts.
	contractSet, err := s.m.FormContracts(rpk, rsk)

	return contractSet, err
}

// Contracts calls Manager.Contracts.
func (s *Satellite) Contracts() []modules.RenterContract {
	return s.m.Contracts()
}

// ContractsByRenter calls Manager.ContractsByRenter.
func (s *Satellite) ContractsByRenter(rpk types.SiaPublicKey) []modules.RenterContract {
	return s.m.ContractsByRenter(rpk)
}

// RefreshedContract calls Manager.RefreshedContract.
func (s *Satellite) RefreshedContract(fcid types.FileContractID) bool {
	return s.m.RefreshedContract(fcid)
}

// OldContracts calls Manager.OldContracts.
func (s *Satellite) OldContracts() []modules.RenterContract {
	return s.m.OldContracts()
}

// OldsContractsByRenter calls Manager.OldContractsByRenter.
func (s *Satellite) OldContractsByRenter(rpk types.SiaPublicKey) []modules.RenterContract {
	return s.m.OldContractsByRenter(rpk)
}

// BlockHeight returns the current block height.
func (s *Satellite) BlockHeight() types.BlockHeight {
	return s.cs.Height()
}

// RenewContracts tries to renew the given set of contracts and returns them.
// If the contracts are not up to being renewed yet, existing contracts are
// returned.
func (s *Satellite) RenewContracts(rpk types.SiaPublicKey, rsk crypto.SecretKey, a modules.Allowance, contracts []types.FileContractID) ([]modules.RenterContract, error) {
	// Get the estimated costs and update the allowance with them.
	estimation, a, err := s.m.PriceEstimation(a)
	if err != nil {
		return nil, err
	}

	// Check if the user balance is sufficient to cover the costs.
	renter, err := s.GetRenter(rpk)
	if err != nil {
		return nil, err
	}
	ub, err := s.GetBalance(renter.Email)
	if err != nil {
		return nil, err
	}
	if ub.Balance < estimation {
		return nil, errors.New("insufficient account balance")
	}

	// Set the allowance.
	err = s.m.SetAllowance(rpk, a)
	if err != nil {
		return nil, err
	}

	// Renew the contracts.
	contractSet, err := s.m.RenewContracts(rpk, rsk, contracts)

	return contractSet, err
}

// UpdateContract updates the contract with the new revision.
func (s *Satellite) UpdateContract(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	return s.m.UpdateContract(rev, sigs, uploads, downloads, fundAccount)
}

// WalletSeed returns the primary wallet seed.
func (s *Satellite) WalletSeed() (seed smodules.Seed, err error) {
	seed, _, err = s.wallet.PrimarySeed()
	return
}

// RenewedFrom returns the ID of the contract the given contract was renewed
// from, if any.
func (s *Satellite) RenewedFrom(fcid types.FileContractID) types.FileContractID {
	return s.m.RenewedFrom(fcid)
}

// ProcessConsensusChange updates the blockheight timestamps.
func (s *Satellite) ProcessConsensusChange(cc smodules.ConsensusChange) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Process the applied blocks till the first found in the following month.
	currentMonth := time.Unix(int64(s.currentMonth.Timestamp), 0).Month()
	for _, block := range cc.AppliedBlocks {
		newMonth := time.Unix(int64(block.Timestamp), 0).Month()
		if newMonth != currentMonth {
			s.prevMonth = s.currentMonth
			s.currentMonth = blockHeightTimestamp{
				BlockHeight: cc.BlockHeight,
				Timestamp:   uint64(block.Timestamp),
			}
			err := s.saveSync()
			if err != nil {
				s.log.Println("ERROR: couldn't save satellite")
			}

			// Move the current spendings of each renter to the previous ones.
			renters := s.Renters()
			for _, renter := range renters {
				us, err := s.getSpendings(renter.Email)
				if err == nil {
					us.PrevLocked = us.CurrentLocked
					us.PrevUsed = us.CurrentUsed
					us.PrevOverhead = us.CurrentOverhead
					us.CurrentLocked = 0
					us.CurrentUsed = 0
					us.CurrentOverhead = 0
					us.PrevFormed = us.CurrentFormed
					us.PrevRenewed = us.CurrentRenewed
					us.CurrentFormed = 0
					us.CurrentRenewed = 0
					err = s.updateSpendings(renter.Email, *us)
					if err != nil {
						s.log.Println("ERROR: couldn't update spendings")
					}
				}
			}

			break
		}
	}
}

// DeleteRenter deletes the renter data from the memory.
func (s *Satellite) DeleteRenter(email string) {
	s.m.DeleteRenter(email)
}

// FormContract creates a contract with a single host using the new
// Renter-Satellite protocol.
func (s *Satellite) FormContract(ss *modules.RPCSession, pk types.SiaPublicKey, rpk types.SiaPublicKey, hpk types.SiaPublicKey, endHeight types.BlockHeight, storage uint64, upload uint64, download uint64, minShards uint64, totalShards uint64) (modules.RenterContract, error) {
	// Get the estimated costs.
	funding, estimation, err := s.m.ContractPriceEstimation(hpk, endHeight, storage, upload, download, minShards, totalShards)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Check if the user balance is sufficient to cover the costs.
	renter, err := s.GetRenter(pk)
	if err != nil {
		return modules.RenterContract{}, err
	}
	ub, err := s.GetBalance(renter.Email)
	if err != nil {
		return modules.RenterContract{}, err
	}
	if ub.Balance < estimation {
		return modules.RenterContract{}, errors.New("insufficient account balance")
	}

	// Form the contract.
	contract, err := s.m.FormContract(ss, pk, rpk, hpk, endHeight, funding)

	return contract, err
}

// RenewContract renews a contract using the new Renter-Satellite protocol.
func (s *Satellite) RenewContract(ss *modules.RPCSession, pk types.SiaPublicKey, fcid types.FileContractID, endHeight types.BlockHeight, storage uint64, upload uint64, download uint64, minShards uint64, totalShards uint64) (modules.RenterContract, error) {
	// Get the contract to renew.
	contract, exists := s.Contract(fcid)
	if !exists {
		return modules.RenterContract{}, errors.New("contract not found")
	}

	// Get the estimated costs.
	funding, estimation, err := s.m.ContractPriceEstimation(contract.HostPublicKey, endHeight, storage, upload, download, minShards, totalShards)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Check if the user balance is sufficient to cover the costs.
	renter, err := s.GetRenter(pk)
	if err != nil {
		return modules.RenterContract{}, err
	}
	ub, err := s.GetBalance(renter.Email)
	if err != nil {
		return modules.RenterContract{}, err
	}
	if ub.Balance < estimation {
		return modules.RenterContract{}, errors.New("insufficient account balance")
	}

	// Renew the contract.
	newContract, err := s.m.RenewContract(ss, pk, contract, endHeight, funding)

	return newContract, err
}

// Contract calls Manager.Contract.
func (s *Satellite) Contract(fcid types.FileContractID) (modules.RenterContract, bool) {
	return s.m.Contract(fcid)
}

// UpdateRenterSettings calls Manager.UpdateRenterSettings.
func (s *Satellite) UpdateRenterSettings(rpk types.SiaPublicKey, settings modules.RenterSettings, sk crypto.SecretKey) error {
	return s.m.UpdateRenterSettings(rpk, settings, sk)
}

// enforce that Satellite satisfies the modules.Satellite interface
var _ modules.Satellite = (*Satellite)(nil)
