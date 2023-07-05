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
	saneStoragePrice = types.HastingsPerSiacoin.Mul64(1e4)  // 10KS
	saneCollateral = types.HastingsPerSiacoin.Mul64(2e4)    // 20KS
	saneUploadPrice = types.HastingsPerSiacoin.Mul64(1e4)   // 10KS
	saneDownloadPrice = types.HastingsPerSiacoin.Mul64(1e5) // 100KS
	saneContractPrice = types.HastingsPerSiacoin.Mul64(100) // 100SC
	saneBaseRPCPrice = types.HastingsPerSiacoin             // 1SC
	saneSectorAccessPrice = types.HastingsPerSiacoin        // 1SC
)

// capAverages checks if the host settings exceed the sane values.
/*func capAverages(entry smodules.HostDBEntry) bool {
	if entry.StoragePrice.Cmp(saneStoragePrice) > 0 {
		return true
	}
	if entry.Collateral.Cmp(saneCollateral) > 0 {
		return true
	}
	if entry.UploadBandwidthPrice.Cmp(saneUploadPrice) > 0 {
		return true
	}
	if entry.DownloadBandwidthPrice.Cmp(saneDownloadPrice) > 0 {
		return true
	}
	if entry.ContractPrice.Cmp(saneContractPrice) > 0 {
		return true
	}
	if entry.BaseRPCPrice.Cmp(saneBaseRPCPrice) > 0 {
		return true
	}
	if entry.SectorAccessPrice.Cmp(saneSectorAccessPrice) > 0 {
		return true
	}
	return false
}*/

// calculateAverages calculates the host network averages from HostDB.
/*func (m *Manager) calculateAverages() {
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
		m.hostAverages.Duration = types.BlockHeight(uint64(m.hostAverages.Duration) + uint64(entry.MaxDuration))
		m.hostAverages.StoragePrice = m.hostAverages.StoragePrice.Add(entry.StoragePrice)
		m.hostAverages.Collateral = m.hostAverages.Collateral.Add(entry.Collateral)
		m.hostAverages.DownloadBandwidthPrice = m.hostAverages.DownloadBandwidthPrice.Add(entry.DownloadBandwidthPrice)
		m.hostAverages.UploadBandwidthPrice = m.hostAverages.UploadBandwidthPrice.Add(entry.UploadBandwidthPrice)
		m.hostAverages.ContractPrice = m.hostAverages.ContractPrice.Add(entry.ContractPrice)
		m.hostAverages.BaseRPCPrice = m.hostAverages.BaseRPCPrice.Add(entry.BaseRPCPrice)
		m.hostAverages.SectorAccessPrice = m.hostAverages.SectorAccessPrice.Add(entry.SectorAccessPrice)
		numHosts++
	}

	m.hostAverages.NumHosts = numHosts

	// Zero check.
	if numHosts == 0 {
		return
	}

	// Divide by the number of hosts.
	m.hostAverages.Duration = types.BlockHeight(uint64(m.hostAverages.Duration) / numHosts)
	m.hostAverages.StoragePrice = m.hostAverages.StoragePrice.Div64(numHosts)
	m.hostAverages.Collateral = m.hostAverages.Collateral.Div64(numHosts)
	m.hostAverages.DownloadBandwidthPrice = m.hostAverages.DownloadBandwidthPrice.Div64(numHosts)
	m.hostAverages.UploadBandwidthPrice = m.hostAverages.UploadBandwidthPrice.Div64(numHosts)
	m.hostAverages.ContractPrice = m.hostAverages.ContractPrice.Div64(numHosts)
	m.hostAverages.BaseRPCPrice = m.hostAverages.BaseRPCPrice.Div64(numHosts)
	m.hostAverages.SectorAccessPrice = m.hostAverages.SectorAccessPrice.Div64(numHosts)

	return
}*/

// threadedCalculateAverages performs the calculation with set intervals.
/*func (m *Manager) threadedCalculateAverages() {
	if err := m.threads.Add(); err != nil {
		return
	}
	defer m.threads.Done()

	m.calculateAverages()

	for {
		select {
		case <-m.threads.StopChan():
			return
		case <-time.After(calculateAveragesInterval):
		}
		m.calculateAverages()
	}
}*/

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
	m.mu.Lock()
	defer m.mu.Unlock()

	// Process the applied blocks till the first found in the following month.
	currentMonth := m.currentMonth.Timestamp.Month()
	for _, block := range cc.AppliedBlocks {
		newMonth := block.Timestamp.Month()
		if newMonth != currentMonth {
			m.prevMonth = m.currentMonth
			m.currentMonth = blockTimestamp{
				BlockHeight: cc.BlockHeight,
				Timestamp:   block.Timestamp,
			}
			err := dbPutBlockTimestamps(m.dbTx, m.currentMonth, m.prevMonth)
			if err != nil {
				m.log.Println("ERROR: couldn't save block timestamps", err)
			}

			// Move the current spendings of each renter to the previous ones.
			/*renters := s.Renters()
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
			}*/

			m.syncDB()
			break
		}
	}
}

