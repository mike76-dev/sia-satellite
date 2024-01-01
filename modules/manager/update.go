package manager

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/mike76-dev/sia-satellite/external"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

const (
	// Intervals for the threads.
	calculateAveragesInterval = 10 * time.Minute
	exchangeRateFetchInterval = 10 * time.Minute
	checkOutOfSyncInterval    = 10 * time.Minute
	outOfSyncThreshold        = 90 * time.Minute
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

// fetchExchangeRates retrieves the SC exchange rates.
func (m *Manager) fetchExchangeRates() {
	data, err := external.FetchSCRates()
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

// GetSiacoinRate calculates the SC price in a given currency.
func (m *Manager) GetSiacoinRate(currency string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rate, ok := m.exchRates[strings.ToLower(currency)]
	if !ok {
		return 0, errors.New("unsupported currency")
	}

	return rate, nil
}

// reportTemplate contains the monthly report send by email.
const reportTemplate = `
	<!-- template.html -->
	<!DOCTYPE html>
	<html>
	<head>
	<style>td {padding-right: 10px;}</style>
	</head>
	<body>
   	<h2>Your Monthly Report</h2>
    <p>Your monthly report on <strong>{{.Name}}</strong> for {{.Month}} {{.Year}} is ready.</p>
	<table>
	<tr>
	<td>Total renters</td><td>{{.NumRenters}}</td><td></td>
	</tr>
	<tr>
	<td>Contracts formed</td><td>{{.NumFormed}}</td><td>{{.FeeFormed}}</td>
	</tr>
	<tr>
	<td>Contracts renewed</td><td>{{.NumRenewed}}</td><td>{{.FeeRenewed}}</td>
	</tr>
	<tr>
	<td>Slabs stored</td><td>{{.NumStored}}</td><td>{{.FeeStored}}</td>
	</tr>
	<tr>
	<td>Slabs saved</td><td>{{.NumSaved}}</td><td>{{.FeeSaved}}</td>
	</tr>
	<tr>
	<td>Slabs retrieved</td><td>{{.NumRetrieved}}</td><td>{{.FeeRetrieved}}</td>
	</tr>
	<tr>
	<td>Slabs migrated</td><td>{{.NumMigrated}}</td><td>{{.FeeMigrated}}</td>
	</tr>
	<tr>
	<td>Partial slab data stored</td><td>{{.Partial}}</td><td>{{.FeePartial}}</td>
	</tr>
	<tr>
	<td><strong>Total revenue</strong></td><td></td><td><strong>{{.Revenue}}</strong></td>
	</tr>
	</table>
	</body>
	</html>
`

// ProcessConsensusChange gets called to inform Manager about the
// changes in the consensus set.
func (m *Manager) ProcessConsensusChange(cc modules.ConsensusChange) {
	// Define a helper.
	convertSize := func(size uint64) string {
		if size < 1024 {
			return fmt.Sprintf("%d B", size)
		}
		sizes := []string{"KiB", "MiB", "GiB", "TiB"}
		i := 0
		s := float64(size)
		for {
			s = s / 1024
			if i >= len(sizes)-1 || s < 1024 {
				break
			}
			i++
		}

		return fmt.Sprintf("%.2f %s", s, sizes[i])
	}

	// Process the applied blocks till the first found in the following month.
	for _, block := range cc.AppliedBlocks {
		m.mu.Lock()
		m.lastBlockTimestamp = block.Timestamp
		currentMonth := m.currentMonth.Timestamp.Month()
		currentYear := m.currentMonth.Timestamp.Year()
		m.mu.Unlock()
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

			// Calculate the monthly spendings of each renter.
			renters := m.Renters()
			var formed, renewed, stored, saved, retrieved, migrated, partial uint64
			var formedFee, renewedFee, storedFee, savedFee, retrievedFee, migratedFee, partialFee float64
			for _, renter := range renters {
				ub, err := m.GetBalance(renter.Email)
				if err != nil {
					m.log.Println("ERROR: couldn't retrieve balance:", err)
					continue
				}
				us, err := m.GetSpendings(renter.Email, int(currentMonth), currentYear)
				if err != nil {
					m.log.Println("ERROR: couldn't retrieve renter spendings:", err)
					continue
				}
				formed += us.Formed
				if ub.Subscribed {
					formedFee += float64(us.Formed) * modules.StaticPricing.FormContract.Invoicing
				} else {
					formedFee += float64(us.Formed) * modules.StaticPricing.FormContract.PrePayment
				}
				renewed += us.Renewed
				if ub.Subscribed {
					renewedFee += float64(us.Renewed) * modules.StaticPricing.FormContract.Invoicing
				} else {
					renewedFee += float64(us.Renewed) * modules.StaticPricing.FormContract.PrePayment
				}
				saved += us.SlabsSaved
				if ub.Subscribed {
					savedFee += float64(us.SlabsSaved) * modules.StaticPricing.SaveMetadata.Invoicing
				} else {
					savedFee += float64(us.SlabsSaved) * modules.StaticPricing.SaveMetadata.PrePayment
				}
				retrieved += us.SlabsRetrieved
				if ub.Subscribed {
					retrievedFee += float64(us.SlabsRetrieved) * modules.StaticPricing.RetrieveMetadata.Invoicing
				} else {
					retrievedFee += float64(us.SlabsRetrieved) * modules.StaticPricing.RetrieveMetadata.PrePayment
				}
				migrated += us.SlabsMigrated
				if ub.Subscribed {
					migratedFee += float64(us.SlabsMigrated) * modules.StaticPricing.MigrateSlab.Invoicing
				} else {
					migratedFee += float64(us.SlabsMigrated) * modules.StaticPricing.MigrateSlab.PrePayment
				}
				count, data, err := m.numSlabs(renter.PublicKey)
				if err != nil {
					m.log.Println("ERROR: couldn't retrieve slab count:", err)
					continue
				}
				var storageFee, dataFee float64
				if ub.Subscribed {
					storageFee = modules.StaticPricing.StoreMetadata.Invoicing
					dataFee = modules.StaticPricing.StorePartialData.Invoicing
				} else {
					storageFee = modules.StaticPricing.StoreMetadata.PrePayment
					dataFee = modules.StaticPricing.StorePartialData.PrePayment
				}
				storageCost := storageFee * float64(count)
				stored += uint64(count)
				storedFee += storageCost
				us.Used += storageCost
				us.Overhead += storageCost
				partialCost := dataFee * float64(data) / 1024 / 1024
				partial += data
				partialFee += partialCost
				us.Used += partialCost
				us.Overhead += partialCost
				err = m.UpdateSpendings(renter.Email, us, int(currentMonth), currentYear)
				if err != nil {
					m.log.Println("ERROR: couldn't update spendings:", err)
				}
				// Deduct from the account balance.
				if !ub.Subscribed && ub.Balance < storageCost+partialCost {
					// Insufficient balance, delete the file metadata.
					m.log.Println("WARN: insufficient account balance, deleting stored metadata")
					m.DeleteBufferedFiles(renter.PublicKey)
					m.DeleteMultipartUploads(renter.PublicKey)
					m.DeleteMetadata(renter.PublicKey)
					continue
				}
				if ub.OnHold > 0 && ub.OnHold < uint64(time.Now().Unix()-int64(modules.OnHoldThreshold.Seconds())) {
					// Account on hold, delete the file metadata.
					m.log.Println("WARN: account on hold, deleting stored metadata")
					m.DeleteBufferedFiles(renter.PublicKey)
					m.DeleteMultipartUploads(renter.PublicKey)
					m.DeleteMetadata(renter.PublicKey)
					continue
				}
				ub.Balance -= (storageCost + partialCost)
				if err := m.UpdateBalance(renter.Email, ub); err != nil {
					m.log.Println("ERROR: couldn't update balance", err)
				}
			}

			// Send a monthly report by email.
			func() {
				if m.email == "" {
					return
				}
				type report struct {
					Name         string
					Month        string
					Year         int
					NumRenters   int
					NumFormed    uint64
					FeeFormed    string
					NumRenewed   uint64
					FeeRenewed   string
					NumStored    uint64
					FeeStored    string
					NumSaved     uint64
					FeeSaved     string
					NumRetrieved uint64
					FeeRetrieved string
					NumMigrated  uint64
					FeeMigrated  string
					Partial      string
					FeePartial   string
					Revenue      string
				}
				revenue := formedFee + renewedFee + storedFee + savedFee + retrievedFee + migratedFee + partialFee
				t := template.New("report")
				t, err := t.Parse(reportTemplate)
				if err != nil {
					m.log.Printf("ERROR: unable to parse HTML template: %v\n", err)
					return
				}
				var b bytes.Buffer
				t.Execute(&b, report{
					Name:         m.name,
					Month:        currentMonth.String(),
					Year:         currentYear,
					NumRenters:   len(renters),
					NumFormed:    formed,
					FeeFormed:    fmt.Sprintf("%.2f SC", formedFee),
					NumRenewed:   renewed,
					FeeRenewed:   fmt.Sprintf("%.2f SC", renewedFee),
					NumStored:    stored,
					FeeStored:    fmt.Sprintf("%.2f SC", storedFee),
					NumSaved:     saved,
					FeeSaved:     fmt.Sprintf("%.2f SC", savedFee),
					NumRetrieved: retrieved,
					FeeRetrieved: fmt.Sprintf("%.2f SC", retrievedFee),
					NumMigrated:  migrated,
					FeeMigrated:  fmt.Sprintf("%.2f SC", migratedFee),
					Partial:      convertSize(partial),
					FeePartial:   fmt.Sprintf("%.2f SC", partialFee),
					Revenue:      fmt.Sprintf("%.2f SC", revenue),
				})
				err = m.ms.SendMail("Sia Satellite", m.email, "Your Monthly Report", &b)
				if err != nil {
					m.log.Println("ERROR: unable to send a monthly report:", err)
				}
			}()

			// Delete old spendings records from the database.
			err = m.deleteOldSpendings()
			if err != nil {
				m.log.Println("ERROR: couldn't delete old spendings:", err)
			}

			// Spin a thread to invoice the subscribed accounts.
			go m.threadedSettleAccounts()

			m.syncDB()
			break
		}
	}

	// Send a warning email if the wallet balance becomes low.
	m.sendWarning()
}

// outOfSyncTemplate contains the text send by email when the last
// block was found too long ago, meaning that the satellite is possibly
// out of sync.
const outOfSyncTemplate = `
	<!-- template.html -->
	<!DOCTYPE html>
	<html>
	<body>
   	<h2>Satellite Is Possibly Out Of Sync</h2>
    <p>The satellite <strong>{{.Name}}</strong> is possibly out of sync.</p>
	<p>The last block {{.Height}} was found {{.Since}} ago.</p>
	</body>
	</html>
`

// sendOutOfSyncWarning sends an email to the satellite operator
// that the satellite is possibly out of sync.
func (m *Manager) sendOutOfSyncWarning() {
	// Skip if the email was not provided.
	if m.email == "" {
		return
	}

	// Skip if a warning has been sent already.
	m.mu.RLock()
	timestamp := m.lastBlockTimestamp
	m.mu.RUnlock()
	if timestamp.Unix() <= 0 {
		return
	}

	// Check the last found block timestamp.
	since := time.Since(timestamp)
	if since < outOfSyncThreshold {
		return
	}

	// Send a warning.
	type warning struct {
		Name   string
		Height uint64
		Since  string
	}
	t := template.New("warning")
	t, err := t.Parse(outOfSyncTemplate)
	if err != nil {
		m.log.Printf("ERROR: unable to parse HTML template: %v\n", err)
		return
	}
	var b bytes.Buffer
	t.Execute(&b, warning{
		Name:   m.name,
		Height: m.cs.Height(),
		Since:  fmt.Sprintf("%.0fh%.0fm", since.Hours(), since.Minutes()),
	})
	err = m.ms.SendMail("Sia Satellite", m.email, "Out Of Sync Warning", &b)
	if err != nil {
		m.log.Println("ERROR: unable to send a warning:", err)
		return
	}

	// Update the timestamp to prevent spamming.
	m.mu.Lock()
	m.lastBlockTimestamp = time.Unix(0, 0)
	m.mu.Unlock()
}

// threadedCheckOutOfSync does periodical out-of-sync checks.
func (m *Manager) threadedCheckOutOfSync() {
	if err := m.tg.Add(); err != nil {
		return
	}
	defer m.tg.Done()

	for {
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(checkOutOfSyncInterval):
		}
		m.sendOutOfSyncWarning()
	}
}
