package manager

import (
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/siad/types"
)

const (
	calculateAveragesFrequency = 10 * time.Minute
)

// calculateAverages calculates the host network averages from HostDB.
func (m *Manager) calculateAverages() {
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
	for _, entry := range hosts {
		m.hostAverages.Duration = types.BlockHeight(int64(m.hostAverages.Duration) + int64(entry.MaxDuration))
		m.hostAverages.StoragePrice = m.hostAverages.StoragePrice.Add(entry.StoragePrice)
		m.hostAverages.Collateral = m.hostAverages.Collateral.Add(entry.Collateral)
		m.hostAverages.DownloadBandwidthPrice = m.hostAverages.DownloadBandwidthPrice.Add(entry.DownloadBandwidthPrice)
		m.hostAverages.UploadBandwidthPrice = m.hostAverages.UploadBandwidthPrice.Add(entry.UploadBandwidthPrice)
		m.hostAverages.ContractPrice = m.hostAverages.ContractPrice.Add(entry.ContractPrice)
		m.hostAverages.BaseRPCPrice = m.hostAverages.BaseRPCPrice.Add(entry.BaseRPCPrice)
		m.hostAverages.SectorAccessPrice = m.hostAverages.SectorAccessPrice.Add(entry.SectorAccessPrice)
	}

	// Divide by the number of active hosts.
	m.hostAverages.NumHosts = uint64(len(hosts))
	m.hostAverages.Duration = types.BlockHeight(int(m.hostAverages.Duration) / len(hosts))
	m.hostAverages.StoragePrice = m.hostAverages.StoragePrice.Div64(uint64(len(hosts)))
	m.hostAverages.Collateral = m.hostAverages.Collateral.Div64(uint64(len(hosts)))
	m.hostAverages.DownloadBandwidthPrice = m.hostAverages.DownloadBandwidthPrice.Div64(uint64(len(hosts)))
	m.hostAverages.UploadBandwidthPrice = m.hostAverages.UploadBandwidthPrice.Div64(uint64(len(hosts)))
	m.hostAverages.ContractPrice = m.hostAverages.ContractPrice.Div64(uint64(len(hosts)))
	m.hostAverages.BaseRPCPrice = m.hostAverages.BaseRPCPrice.Div64(uint64(len(hosts)))
	m.hostAverages.SectorAccessPrice = m.hostAverages.SectorAccessPrice.Div64(uint64(len(hosts)))

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
