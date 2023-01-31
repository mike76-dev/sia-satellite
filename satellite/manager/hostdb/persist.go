package hostdb

import (
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/hostdb/hosttree"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	"go.sia.tech/siad/types"
)

var (
	// persistFilename defines the name of the file that holds the hostdb's
	// persistence.
	persistFilename = "hostdb.json"

	// persistMetadata defines the metadata that tags along with the most recent
	// version of the hostdb persistence file.
	persistMetadata = persist.Metadata{
		Header:  "HostDB Persistence",
		Version: "0.1",
	}
)

// hostEntry contains the host entry fields in the database.
type hostEntry struct {
	AcceptingContracts             bool
	MaxDownloadBatchSize           uint64
	MaxDuration                    uint64
	MaxReviseBatchSize             uint64
	NetAddress                     string
	RemainingStorage               uint64
	SectorSize                     uint64
	TotalStorage                   uint64
	UnlockHash                     string
	WindowSize                     uint64
	Collateral                     string
	MaxCollateral                  string
	BaseRPCPrice                   string
	ContractPrice                  string
	DownloadBandwidthPrice         string
	SectorAccessPrice              string
	StoragePrice                   string
	UploadBandwidthPrice           string
	EphemeralAccountExpiry         uint64
	MaxEphemeralAccountBalance     string
	RevisionNumber                 uint64
	Version                        string
	SiaMuxPort                     string
	FirstSeen                      uint64
	HistoricDowntime               uint64
	HistoricUptime                 uint64
	HistoricFailedInteractions     float64
	HistoricSuccessfulInteractions float64
	RecentFailedInteractions       float64
	RecentSuccessfulInteractions   float64
	LastHistoricUpdate             uint64
	LastIPNetChange                string
	PublicKey                      string
	Filtered                       bool
}

// hdbPersist defines what HostDB data persists across sessions.
type hdbPersist struct {
	FilteredDomains          []string
	BlockHeight              types.BlockHeight
	DisableIPViolationsCheck bool
	LastChange               smodules.ConsensusChangeID
	FilteredHosts            map[string]types.SiaPublicKey
	FilterMode               smodules.FilterMode
}

// persistData returns the data in the hostdb that will be saved to disk.
func (hdb *HostDB) persistData() (data hdbPersist) {
	data.FilteredDomains = hdb.filteredDomains.managedFilteredDomains()
	data.BlockHeight = hdb.blockHeight
	data.DisableIPViolationsCheck = hdb.disableIPViolationCheck
	data.LastChange = hdb.lastChange
	data.FilteredHosts = hdb.filteredHosts
	data.FilterMode = hdb.filterMode
	return data
}

// saveSync saves the hostdb persistence data to disk and then syncs to disk.
func (hdb *HostDB) saveSync() error {
	return persist.SaveJSON(persistMetadata, hdb.persistData(), filepath.Join(hdb.persistDir, persistFilename))
}

// load loads the hostdb persistence data from disk.
func (hdb *HostDB) load() error {
	// Fetch the data from the file.
	var data hdbPersist
	data.FilteredHosts = make(map[string]types.SiaPublicKey)
	err := persist.LoadJSON(persistMetadata, &data, filepath.Join(hdb.persistDir, persistFilename))
	if err != nil {
		return err
	}

	// Set the hostdb internal values.
	hdb.blockHeight = data.BlockHeight
	hdb.disableIPViolationCheck = data.DisableIPViolationsCheck
	hdb.lastChange = data.LastChange
	hdb.filteredHosts = data.FilteredHosts
	hdb.filterMode = data.FilterMode

	// Overwrite the initialized filteredDomains with the data loaded
	// from disk
	hdb.filteredDomains = newFilteredDomains(data.FilteredDomains)

	if len(hdb.filteredHosts) > 0 {
		hdb.filteredTree = hosttree.New(hdb.weightFunc, hdb.resolver)
	}

	// "Lazily" load the hosts into the host trees.
	go func() {
		rows, err := hdb.db.Query(`
			SELECT accepting_contracts, max_download_batch_size, max_duration,
				max_revise_batch_size, net_address, remaining_storage, sector_size,
				total_storage, unlock_hash, window_size, collateral, max_collateral,
				base_rpc_price, contract_price, download_bandwidth_price,
				sector_access_price, storage_price, upload_bandwidth_price,
				ephemeral_account_expiry, max_ephemeral_account_balance, revision_number,
				version, sia_mux_port, first_seen, historic_downtime, historic_uptime,
				historic_failed_interactions, historic_successful_interactions,
				recent_failed_interactions, recent_successful_interactions,
				last_historic_update, last_ip_net_change, public_key, filtered
			FROM hosts`)
		if err != nil {
			hdb.staticLog.Println("ERROR: could not load the hosts:", err)
			return
		}
		defer rows.Close()

		var entry hostEntry
		var scanRows, ipRows *sql.Rows
		var txt string
		var t time.Time
		var s bool
		var ip string
	
		for rows.Next() {
			if err := rows.Scan(&entry.AcceptingContracts, &entry.MaxDownloadBatchSize, &entry.MaxDuration, &entry.MaxReviseBatchSize, &entry.NetAddress, &entry.RemainingStorage, &entry.SectorSize, &entry.TotalStorage, &entry.UnlockHash, &entry.WindowSize, &entry.Collateral, &entry.MaxCollateral, &entry.BaseRPCPrice, &entry.ContractPrice, &entry.DownloadBandwidthPrice, &entry.SectorAccessPrice, &entry.StoragePrice, &entry.UploadBandwidthPrice, &entry.EphemeralAccountExpiry, &entry.MaxEphemeralAccountBalance, &entry.RevisionNumber, &entry.Version, &entry.SiaMuxPort, &entry.FirstSeen, &entry.HistoricDowntime, &entry.HistoricUptime, &entry.HistoricFailedInteractions, &entry.HistoricSuccessfulInteractions, &entry.RecentFailedInteractions, &entry.RecentSuccessfulInteractions, &entry.LastHistoricUpdate, &entry.LastIPNetChange, &entry.PublicKey, &entry.Filtered); err != nil {
				hdb.staticLog.Println("ERROR: could not load the host:", err)
				continue
			}

			host := fromHostEntry(entry)

			// Load the scan history.
			scanRows, err = hdb.db.Query("SELECT time, success FROM scanhistory WHERE public_key = ?", entry.PublicKey)
			if err != nil {
				scanRows.Close()
				hdb.staticLog.Println("ERROR: could not load the scan history:", err)
				continue
			}
			for scanRows.Next() {
				if err := scanRows.Scan(&txt, &s); err != nil {
					scanRows.Close()
					hdb.staticLog.Println("ERROR: could not load the scan history:", err)
					continue
				}
				_ = t.UnmarshalText([]byte(txt))
				host.ScanHistory = append(host.ScanHistory, smodules.HostDBScan{t, s})
			}
			scanRows.Close()

			// Load the IP subnets.
			ipRows, err = hdb.db.Query("SELECT ip_net FROM ipnets WHERE public_key = ?", entry.PublicKey)
			if err != nil {
				ipRows.Close()
				hdb.staticLog.Println("ERROR: could not load the IP subnets:", err)
				continue
			}
			for ipRows.Next() {
				if err := ipRows.Scan(&ip); err != nil {
					ipRows.Close()
					hdb.staticLog.Println("ERROR: could not load the IP subnets:", err)
					continue
				}
				host.IPNets = append(host.IPNets, ip)
			}
			ipRows.Close()

			// COMPATv1.1.0
			//
			// The host did not always track its block height correctly, meaning
			// that previously the FirstSeen values and the blockHeight values
			// could get out of sync.
			if hdb.blockHeight < host.FirstSeen {
				host.FirstSeen = hdb.blockHeight
			}

			err := hdb.insert(host)
			if err != nil {
				hdb.staticLog.Println("ERROR: could not insert host into hosttree while loading:", host.NetAddress)
			}

			// Make sure that all hosts have gone through the initial scanning.
			if len(host.ScanHistory) < 2 {
				hdb.queueScan(host)
			}
		}
		hdb.loadingComplete = true
	}()

	return nil
}

// threadedSaveLoop saves the hostdb to disk every 2 minutes, also saving when
// given the shutdown signal.
func (hdb *HostDB) threadedSaveLoop() {
	err := hdb.tg.Add()
	if err != nil {
		return
	}
	defer hdb.tg.Done()

	for {
		select {
		case <-hdb.tg.StopChan():
			return
		case <-time.After(saveFrequency):
			hdb.mu.Lock()
			err = hdb.saveSync()
			hdb.mu.Unlock()
			if err != nil {
				hdb.staticLog.Println("Difficulties saving the hostdb:", err)
			}
		}
	}
}

// updateHost updates the host entry in the database.
// A lock should be acquired before calling this function.
func (hdb *HostDB) updateHost(host smodules.HostDBEntry) error {
	// Fetch the data first.
	entry := toHostEntry(host)
	hostIPNets := host.IPNets

	// Check if the entry exists already.
	var count int
	err := hdb.db.QueryRow("SELECT COUNT(public_key) FROM hosts WHERE public_key = ?", entry.PublicKey).Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		// Insert as a new entry.
		_, err = hdb.db.Exec(`
			INSERT INTO hosts (
				accepting_contracts, max_download_batch_size, max_duration,
				max_revise_batch_size, net_address, remaining_storage,
				sector_size, total_storage, unlock_hash, window_size,
				collateral, max_collateral, base_rpc_price, contract_price,
				download_bandwidth_price, sector_access_price, storage_price,
				upload_bandwidth_price, ephemeral_account_expiry,
				max_ephemeral_account_balance, revision_number, version,
				sia_mux_port, first_seen, historic_downtime, historic_uptime,
				historic_failed_interactions, historic_successful_interactions,
				recent_failed_interactions, recent_successful_interactions,
				last_historic_update, last_ip_net_change, public_key, filtered)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, entry.AcceptingContracts, entry.MaxDownloadBatchSize, entry.MaxDuration, entry.MaxReviseBatchSize, entry.NetAddress, entry.RemainingStorage, entry.SectorSize, entry.TotalStorage, entry.UnlockHash, entry.WindowSize, entry.Collateral, entry.MaxCollateral, entry.BaseRPCPrice, entry.ContractPrice, entry.DownloadBandwidthPrice, entry.SectorAccessPrice, entry.StoragePrice, entry.UploadBandwidthPrice, entry.EphemeralAccountExpiry, entry.MaxEphemeralAccountBalance, entry.RevisionNumber, entry.Version, entry.SiaMuxPort, entry.FirstSeen, entry.HistoricDowntime, entry.HistoricUptime, entry.HistoricFailedInteractions, entry.HistoricSuccessfulInteractions, entry.RecentFailedInteractions, entry.RecentSuccessfulInteractions, entry.LastHistoricUpdate, entry.LastIPNetChange, entry.PublicKey, entry.Filtered)

		if err != nil {
			return err
		}

		// Save IP nets data.
		for _, subnet := range hostIPNets {
			_, err = hdb.db.Exec(`
				INSERT INTO ipnets (public_key, ip_net)
				VALUES (?, ?)`, entry.PublicKey, subnet)
			if err != nil {
				return err
			}
		}

		return nil
	}

	// Update the existing entry.
	_, err = hdb.db.Exec(`
		UPDATE hosts SET accepting_contracts = ?, max_download_batch_size = ?,
			max_duration = ?, max_revise_batch_size = ?, net_address = ?,
			remaining_storage = ?, sector_size = ?, total_storage = ?,
			unlock_hash = ?, window_size = ?, collateral = ?, max_collateral = ?,
			base_rpc_price = ?, contract_price = ?, download_bandwidth_price = ?,
			sector_access_price = ?, storage_price = ?, upload_bandwidth_price = ?,
			ephemeral_account_expiry = ?, max_ephemeral_account_balance = ?,
			revision_number = ?, version = ?, sia_mux_port = ?, first_seen = ?,
			historic_downtime = ?, historic_uptime = ?,
			historic_failed_interactions = ?, historic_successful_interactions = ?,
			recent_failed_interactions = ?, recent_successful_interactions = ?,
			last_historic_update = ?, last_ip_net_change = ?, filtered = ?
		WHERE public_key = ?`, entry.AcceptingContracts, entry.MaxDownloadBatchSize, entry.MaxDuration, entry.MaxReviseBatchSize, entry.NetAddress, entry.RemainingStorage, entry.SectorSize, entry.TotalStorage, entry.UnlockHash, entry.WindowSize, entry.Collateral, entry.MaxCollateral, entry.BaseRPCPrice, entry.ContractPrice, entry.DownloadBandwidthPrice, entry.SectorAccessPrice, entry.StoragePrice, entry.UploadBandwidthPrice, entry.EphemeralAccountExpiry, entry.MaxEphemeralAccountBalance, entry.RevisionNumber, entry.Version, entry.SiaMuxPort, entry.FirstSeen, entry.HistoricDowntime, entry.HistoricUptime, entry.HistoricFailedInteractions, entry.HistoricSuccessfulInteractions, entry.RecentFailedInteractions, entry.RecentSuccessfulInteractions, entry.LastHistoricUpdate, entry.LastIPNetChange, entry.Filtered, entry.PublicKey)

	if err != nil {
		return err
	}

	// Save IP nets data.
	rows, err := hdb.db.Query("SELECT ip_net FROM ipnets WHERE public_key = ?", entry.PublicKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	ipNets := make(map[string]struct{})
	var ipNet string
	for rows.Next() {
		if err := rows.Scan(&ipNet); err != nil {
			return err
		}
		ipNets[ipNet] = struct{}{}
	}

	for _, subnet := range hostIPNets {
		_, exists := ipNets[subnet]
		if !exists {
			// Insert into the database.
			_, err = hdb.db.Exec(`
				INSERT INTO ipnets (public_key, ip_net)
				VALUES (?, ?)`, entry.PublicKey, subnet)
			if err != nil {
				return err
			}
		}
		delete(ipNets, subnet)
	}

	// Delete the rest.
	for subnet, _ := range ipNets {
		_, err = hdb.db.Exec(`
			DELETE FROM ipnets
			WHERE public_key = ? AND ip_net = ?`, entry.PublicKey, subnet)
		if err != nil {
			return err
		}
	}

	return nil
}

// updateScanHistory updates the scan history of the host entry in the
// database. A lock should be acquired before calling this function.
func (hdb *HostDB) updateScanHistory(host smodules.HostDBEntry) error {
	// Fetch the variables first.
	pk := host.PublicKey.String()
	var history smodules.HostDBScans
	for _, scan := range host.ScanHistory {
		history = append(history, scan)
	}

	rows, err := hdb.db.Query("SELECT time, success FROM scanhistory WHERE public_key = ?", pk)
	if err != nil {
		return err
	}
	defer rows.Close()

	scans := make(map[string]bool)
	var t string
	var s bool
	var b []byte
	
	for rows.Next() {
		if err := rows.Scan(&t, &s); err != nil {
			return err
		}
		scans[t] = s
	}

	for _, scan := range history {
		b, _ = scan.Timestamp.MarshalText()
		t = string(b)
		_, exists := scans[t]
		if !exists {
			// Insert into the database.
			_, err = hdb.db.Exec(`
				INSERT INTO scanhistory (public_key, time, success)
				VALUES (?, ?, ?)`, pk, t, scan.Success)
			if err != nil {
				return err
			}
		}
		delete(scans, t)
	}

	// Delete the rest.
	for t, _ = range scans {
		_, err = hdb.db.Exec(`
			DELETE FROM scanhistory
			WHERE public_key = ? AND time = ?`, pk, t)
		if err != nil {
			return err
		}
	}

	return nil
}

// toHostEntry is a helper function converting the in-memory representation
// of a HostDB entry to a MySQL database entry.
func toHostEntry(entry smodules.HostDBEntry) (he hostEntry) {
	he.AcceptingContracts = entry.AcceptingContracts
	he.MaxDownloadBatchSize = entry.MaxDownloadBatchSize
	he.MaxDuration = uint64(entry.MaxDuration)
	he.MaxReviseBatchSize = entry.MaxReviseBatchSize
	he.NetAddress = string(entry.NetAddress)
	he.RemainingStorage = entry.RemainingStorage
	he.SectorSize = entry.SectorSize
	he.TotalStorage = entry.TotalStorage
	he.UnlockHash = hex.EncodeToString(entry.UnlockHash[:])
	he.WindowSize = uint64(entry.WindowSize)
	he.Collateral = entry.Collateral.String()
	he.MaxCollateral = entry.MaxCollateral.String()
	he.BaseRPCPrice = entry.BaseRPCPrice.String()
	he.ContractPrice = entry.ContractPrice.String()
	he.DownloadBandwidthPrice = entry.DownloadBandwidthPrice.String()
	he.SectorAccessPrice = entry.SectorAccessPrice.String()
	he.StoragePrice = entry.StoragePrice.String()
	he.UploadBandwidthPrice = entry.UploadBandwidthPrice.String()
	he.EphemeralAccountExpiry = uint64(entry.EphemeralAccountExpiry.Seconds())
	he.MaxEphemeralAccountBalance = entry.MaxEphemeralAccountBalance.String()
	he.RevisionNumber = entry.RevisionNumber
	he.Version = entry.Version
	he.SiaMuxPort = entry.SiaMuxPort
	he.FirstSeen = uint64(entry.FirstSeen)
	he.HistoricDowntime = uint64(entry.HistoricDowntime.Seconds())
	he.HistoricUptime = uint64(entry.HistoricUptime.Seconds())
	he.HistoricFailedInteractions = entry.HistoricFailedInteractions
	he.HistoricSuccessfulInteractions = entry.HistoricSuccessfulInteractions
	he.RecentFailedInteractions = entry.RecentFailedInteractions
	he.RecentSuccessfulInteractions = entry.RecentSuccessfulInteractions
	he.LastHistoricUpdate = uint64(entry.LastHistoricUpdate)
	l, _ := entry.LastIPNetChange.MarshalText()
	he.LastIPNetChange = string(l)
	he.PublicKey = entry.PublicKey.String()
	he.Filtered = entry.Filtered

	return
}

// fromHostEntry is a helper function converting a MySQL database entry
// to the in-memory representation of a HostDB entry.
func fromHostEntry(he hostEntry) (entry smodules.HostDBEntry) {
	entry.AcceptingContracts = he.AcceptingContracts
	entry.MaxDownloadBatchSize = he.MaxDownloadBatchSize
	entry.MaxDuration = types.BlockHeight(he.MaxDuration)
	entry.MaxReviseBatchSize = he.MaxReviseBatchSize
	entry.NetAddress = smodules.NetAddress(he.NetAddress)
	entry.RemainingStorage = he.RemainingStorage
	entry.SectorSize = he.SectorSize
	entry.TotalStorage = he.TotalStorage
	uh, _ := hex.DecodeString(he.UnlockHash)
	copy(entry.UnlockHash[:], uh[:])
	entry.WindowSize = types.BlockHeight(he.WindowSize)
	entry.Collateral = modules.ReadCurrency(he.Collateral)
	entry.MaxCollateral = modules.ReadCurrency(he.MaxCollateral)
	entry.BaseRPCPrice = modules.ReadCurrency(he.BaseRPCPrice)
	entry.ContractPrice = modules.ReadCurrency(he.ContractPrice)
	entry.DownloadBandwidthPrice = modules.ReadCurrency(he.DownloadBandwidthPrice)
	entry.SectorAccessPrice = modules.ReadCurrency(he.SectorAccessPrice)
	entry.SectorAccessPrice = modules.ReadCurrency(he.StoragePrice)
	entry.UploadBandwidthPrice = modules.ReadCurrency(he.UploadBandwidthPrice)
	entry.EphemeralAccountExpiry = time.Duration(he.EphemeralAccountExpiry) * time.Second
	entry.MaxEphemeralAccountBalance = modules.ReadCurrency(he.MaxEphemeralAccountBalance)
	entry.RevisionNumber = he.RevisionNumber
	entry.Version = he.Version
	entry.SiaMuxPort = he.SiaMuxPort
	entry.FirstSeen = types.BlockHeight(he.FirstSeen)
	entry.HistoricDowntime = time.Duration(he.HistoricDowntime) * time.Second
	entry.HistoricUptime = time.Duration(he.HistoricUptime) * time.Second
	entry.HistoricFailedInteractions = he.HistoricFailedInteractions
	entry.HistoricSuccessfulInteractions = he.HistoricSuccessfulInteractions
	entry.RecentFailedInteractions = he.RecentFailedInteractions
	entry.RecentSuccessfulInteractions = he.RecentSuccessfulInteractions
	entry.LastHistoricUpdate = types.BlockHeight(he.LastHistoricUpdate)
	_ = entry.LastIPNetChange.UnmarshalText([]byte(he.LastIPNetChange))
	_ = entry.PublicKey.LoadString(he.PublicKey)
	entry.Filtered = he.Filtered

	return
}

// removeHost removes the host entry from the database.
// A lock should be acquired before calling this function.
func (hdb *HostDB) removeHost(host smodules.HostDBEntry) error {
	// Fetch the host's public key.
	pk := host.PublicKey.String()

	// Clear the scan history.
	_, err := hdb.db.Exec("DELETE FROM scanhistory WHERE public_key = ?", pk)
	if err != nil {
		return err
	}

	// Clear the IP subnets.
	_, err = hdb.db.Exec("DELETE FROM ipnets WHERE public_key = ?", pk)
	if err != nil {
		return err
	}

	// Delete the host.
	_, err = hdb.db.Exec("DELETE FROM hosts WHERE public_key = ?", pk)

	return err
}
