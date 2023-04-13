package manager

import (
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

const (
	calculateAveragesFrequency = 10 * time.Minute
)

var (
	// Some sane values to cap the averages.
	saneStoragePrice = types.SiacoinPrecision.Mul64(1e4)  // 10KS
	saneCollateral = types.SiacoinPrecision.Mul64(2e4)    // 20KS
	saneUploadPrice = types.SiacoinPrecision.Mul64(1e4)   // 10KS
	saneDownloadPrice = types.SiacoinPrecision.Mul64(1e5) // 100KS
	saneContractPrice = types.SiacoinPrecision.Mul64(100) // 100SC
	saneBaseRPCPrice = types.SiacoinPrecision             // 1SC
	saneSectorAccessPrice = types.SiacoinPrecision        // 1SC
)

// capAverages checks if the host settings exceed the sane values.
func capAverages(entry smodules.HostDBEntry) bool {
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
}

// threadedCalculateAverages performs the calculation with set intervals.
func (m *Manager) threadedCalculateAverages() {
	if err := m.threads.Add(); err != nil {
		return
	}
	defer m.threads.Done()

	m.calculateAverages()

	for {
		select {
		case <-m.threads.StopChan():
			return
		case <-time.After(calculateAveragesFrequency):
		}
		m.calculateAverages()
	}
}
