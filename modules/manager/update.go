package manager

import (
	"errors"
	"time"

	"github.com/mike76-dev/sia-satellite/external"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

const (
	// Intervals for the threads.
	calculateAveragesInterval = 10 * time.Minute
	exchangeRateFetchInterval = 24 * time.Hour
	scusdRateFetchInterval    = 10 * time.Minute
)

var (
	// Some sane values to cap the averages.
	saneStoragePrice      = types.HastingsPerSiacoin.Mul64(1e4) // 10KS
	saneCollateral        = types.HastingsPerSiacoin.Mul64(2e4) // 20KS
	saneUploadPrice       = types.HastingsPerSiacoin.Mul64(1e4) // 10KS
	saneDownloadPrice     = types.HastingsPerSiacoin.Mul64(1e5) // 100KS
	saneContractPrice     = types.HastingsPerSiacoin.Mul64(100) // 100SC
	saneBaseRPCPrice      = types.HastingsPerSiacoin            // 1SC
	saneSectorAccessPrice = types.HastingsPerSiacoin            // 1SC
)

// capAverages checks if the host settings exceed the sane values.
func capAverages(entry modules.HostDBEntry) bool {
	if entry.Settings.StoragePrice.Cmp(saneStoragePrice) > 0 {
		return true
	}
	if entry.Settings.Collateral.Cmp(saneCollateral) > 0 {
		return true
	}
	if entry.Settings.UploadBandwidthPrice.Cmp(saneUploadPrice) > 0 {
		return true
	}
	if entry.Settings.DownloadBandwidthPrice.Cmp(saneDownloadPrice) > 0 {
		return true
	}
	if entry.Settings.ContractPrice.Cmp(saneContractPrice) > 0 {
		return true
	}
	if entry.Settings.BaseRPCPrice.Cmp(saneBaseRPCPrice) > 0 {
		return true
	}
	if entry.Settings.SectorAccessPrice.Cmp(saneSectorAccessPrice) > 0 {
		return true
	}
	return false
}

// calculateAverages calculates the host network averages from HostDB.
func (m *Manager) calculateAverages() {
	// Skip calculating if HostDB is not done loading the hosts.
	if !m.hostDB.LoadingComplete() {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.hostAverages = modules.HostAverages{}

	hosts, err := m.ActiveHosts()
	if err != nil {
		m.log.Println("ERROR: could not fetch active hosts", err)
		return
	}

	// No active hosts, return.
	if len(hosts) == 0 {
		return
	}

	// Sum up the values.
	var numHosts uint64
	for _, entry := range hosts {
		if capAverages(entry) {
			continue
		}
		m.hostAverages.Duration = m.hostAverages.Duration + entry.Settings.MaxDuration
		m.hostAverages.StoragePrice = m.hostAverages.StoragePrice.Add(entry.Settings.StoragePrice)
		m.hostAverages.Collateral = m.hostAverages.Collateral.Add(entry.Settings.Collateral)
		m.hostAverages.DownloadBandwidthPrice = m.hostAverages.DownloadBandwidthPrice.Add(entry.Settings.DownloadBandwidthPrice)
		m.hostAverages.UploadBandwidthPrice = m.hostAverages.UploadBandwidthPrice.Add(entry.Settings.UploadBandwidthPrice)
		m.hostAverages.ContractPrice = m.hostAverages.ContractPrice.Add(entry.Settings.ContractPrice)
		m.hostAverages.BaseRPCPrice = m.hostAverages.BaseRPCPrice.Add(entry.Settings.BaseRPCPrice)
		m.hostAverages.SectorAccessPrice = m.hostAverages.SectorAccessPrice.Add(entry.Settings.SectorAccessPrice)
		numHosts++
	}

	m.hostAverages.NumHosts = numHosts

	// Zero check.
	if numHosts == 0 {
		return
	}

	// Divide by the number of hosts.
	m.hostAverages.Duration = m.hostAverages.Duration / numHosts
	m.hostAverages.StoragePrice = m.hostAverages.StoragePrice.Div64(numHosts)
	m.hostAverages.Collateral = m.hostAverages.Collateral.Div64(numHosts)
	m.hostAverages.DownloadBandwidthPrice = m.hostAverages.DownloadBandwidthPrice.Div64(numHosts)
	m.hostAverages.UploadBandwidthPrice = m.hostAverages.UploadBandwidthPrice.Div64(numHosts)
	m.hostAverages.ContractPrice = m.hostAverages.ContractPrice.Div64(numHosts)
	m.hostAverages.BaseRPCPrice = m.hostAverages.BaseRPCPrice.Div64(numHosts)
	m.hostAverages.SectorAccessPrice = m.hostAverages.SectorAccessPrice.Div64(numHosts)

	// Save to disk.
	if err := dbPutAverages(m.dbTx, m.hostAverages); err != nil {
		m.log.Println("ERROR: couldn't save network averages:", err)
	}

	return
}

// threadedCalculateAverages performs the calculation with set intervals.
func (m *Manager) threadedCalculateAverages() {
	if err := m.tg.Add(); err != nil {
		return
	}
	defer m.tg.Done()

	m.calculateAverages()

	for {
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(calculateAveragesInterval):
		}
		m.calculateAverages()
	}
}

// fetchExchangeRates retrieves the fiat currency exchange rates.
func (m *Manager) fetchExchangeRates() {
	data, err := external.FetchExchangeRates()
	if err != nil {
		m.log.Println("ERROR:", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for k, v := range data {
		m.exchRates[k] = v
	}
}

// threadedFetchExchangeRates performs the fetch with set intervals.
func (m *Manager) threadedFetchExchangeRates() {
	err := m.tg.Add()
	if err != nil {
		return
	}
	defer m.tg.Done()

	m.fetchExchangeRates()

	for {
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(exchangeRateFetchInterval):
		}

		m.fetchExchangeRates()
	}
}

// fetchSCUSDRate retrieves the SC-USD rate.
func (m *Manager) fetchSCUSDRate() {
	data, err := external.FetchSCUSDRate()
	if err != nil {
		m.log.Println("ERROR:", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.scusdRate = data
}

// threadedFetchSCUSDRate performs the fetch with set intervals.
func (m *Manager) threadedFetchSCUSDRate() {
	err := m.tg.Add()
	if err != nil {
		return
	}
	defer m.tg.Done()

	m.fetchSCUSDRate()

	for {
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(scusdRateFetchInterval):
		}

		m.fetchSCUSDRate()
	}
}

// GetSiacoinRate calculates the SC price in a given currency.
func (m *Manager) GetSiacoinRate(currency string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fiatRate, ok := m.exchRates[currency]
	if !ok {
		return 0, errors.New("unsupported currency")
	}

	return fiatRate * m.scusdRate, nil
}

// GetExchangeRate returns the exchange rate of a given currency.
func (m *Manager) GetExchangeRate(currency string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rate, ok := m.exchRates[currency]
	if !ok {
		return 0, errors.New("unsupported currency")
	}

	return rate, nil
}

// ProcessConsensusChange gets called to inform Manager about the
// changes in the consensus set.
func (m *Manager) ProcessConsensusChange(cc modules.ConsensusChange) {
	// Process the applied blocks till the first found in the following month.
	for _, block := range cc.AppliedBlocks {
		m.mu.RLock()
		currentMonth := m.currentMonth.Timestamp.Month()
		m.mu.RUnlock()
		newMonth := block.Timestamp.Month()
		if newMonth != currentMonth {
			m.mu.Lock()
			m.prevMonth = m.currentMonth
			m.currentMonth = blockTimestamp{
				BlockHeight: cc.BlockHeight,
				Timestamp:   block.Timestamp,
			}
			err := dbPutBlockTimestamps(m.dbTx, m.currentMonth, m.prevMonth)
			m.mu.Unlock()
			if err != nil {
				m.log.Println("ERROR: couldn't save block timestamps", err)
			}

			// Move the current spendings of each renter to the previous ones.
			renters := m.Renters()
			for _, renter := range renters {
				us, err := m.GetSpendings(renter.Email)
				if err != nil {
					m.log.Println("ERROR: couldn't retrieve renter spendings:", err)
					continue
				}
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
				us.PrevSlabsSaved = us.CurrentSlabsSaved
				us.PrevSlabsRetrieved = us.CurrentSlabsRetrieved
				us.PrevSlabsMigrated = us.CurrentSlabsMigrated
				us.CurrentSlabsSaved = 0
				us.CurrentSlabsRetrieved = 0
				us.CurrentSlabsMigrated = 0
				count, err := m.numSlabs(renter.PublicKey)
				if err != nil {
					m.log.Println("ERROR: couldn't retrieve slab count:", err)
					continue
				}
				fee := float64(modules.StoreMetadataFee * count)
				us.PrevUsed += fee
				us.PrevOverhead += fee
				err = m.UpdateSpendings(renter.Email, us)
				if err != nil {
					m.log.Println("ERROR: couldn't update spendings:", err)
				}
				// Deduct from the account balance.
				ub, err := m.GetBalance(renter.Email)
				if err != nil {
					m.log.Println("ERROR: couldn't retrieve balance:", err)
				}
				if ub.Balance < fee {
					// Insufficient balance, delete the file metadata.
					m.log.Println("WARN: insufficient account balance, deleting stored metadata")
					m.DeleteMetadata(renter.PublicKey)
					continue
				}
				ub.Balance -= fee
				if err := m.UpdateBalance(renter.Email, ub); err != nil {
					m.log.Println("ERROR: couldn't update balance", err)
				}
			}

			m.syncDB()
			break
		}
	}
}
