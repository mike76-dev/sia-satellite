package hostdb

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
)

// initDB initializes the necessary fields in HostDB.
func (hdb *HostDB) initDB() error {
	var count int
	err := hdb.db.QueryRow("SELECT COUNT(*) FROM hdb_info").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = hdb.db.Exec(`INSERT INTO hdb_info
		(height, scan_complete, disable_ip_check, bid, filter_mode)
		VALUES (?, ?, ?, ?, ?)
	`, 0, false, false, []byte{}, modules.HostDBDisableFilter)
	return err
}

// reset zeroes out the sync status of the database.
func (hdb *HostDB) reset() error {
	_, err := hdb.db.Exec("UPDATE hdb_info SET height = ?, bid = ?", 0, []byte{})
	return err
}

// loadDB loads the persisted HostDB data.
func (hdb *HostDB) loadDB() error {
	cc := make([]byte, 32)
	err := hdb.db.QueryRow(`
		SELECT height, scan_complete, disable_ip_check, bid, filter_mode
		FROM hdb_info
		WHERE id = 1
	`).Scan(&hdb.tip.Height, &hdb.initialScanComplete, &hdb.disableIPViolationCheck, &cc, &hdb.filterMode)
	if err != nil {
		return modules.AddContext(err, "couldn't load HostDB data")
	}
	copy(hdb.tip.ID[:], cc)

	// Load filtered hosts.
	rows, err := hdb.db.Query("SELECT public_key FROM hdb_fhosts")
	if err != nil {
		return modules.AddContext(err, "couldn't load filtered hosts")
	}
	for rows.Next() {
		pkBytes := make([]byte, 32)
		var pk types.PublicKey
		if err := rows.Scan(&pkBytes); err != nil {
			rows.Close()
			return modules.AddContext(err, "couldn't load filtered hosts")
		}
		copy(pk[:], pkBytes)
		hdb.filteredHosts[pk.String()] = pk
	}
	rows.Close()

	// Load filtered domains.
	var domains []string
	rows, err = hdb.db.Query("SELECT dom FROM hdb_fdomains")
	if err != nil {
		return modules.AddContext(err, "couldn't load filtered domains")
	}
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			rows.Close()
			return modules.AddContext(err, "couldn't load filtered domains")
		}
		domains = append(domains, domain)
	}
	rows.Close()
	hdb.filteredDomains = newFilteredDomains(domains)

	// Load known contracts.
	rows, err = hdb.db.Query("SELECT host_pk, renter_pk, data FROM hdb_contracts")
	if err != nil {
		return modules.AddContext(err, "couldn't load known contracts")
	}
	for rows.Next() {
		hpk := make([]byte, 32)
		rpk := make([]byte, 32)
		var stored uint64
		if err := rows.Scan(&hpk, &rpk, &stored); err != nil {
			rows.Close()
			return modules.AddContext(err, "couldn't load known contracts")
		}
		var ci contractInfo
		copy(ci.RenterPublicKey[:], rpk)
		copy(ci.HostPublicKey[:], hpk)
		ci.StoredData = stored
		contracts := hdb.knownContracts[ci.HostPublicKey.String()]
		contracts = append(contracts, ci)
		hdb.knownContracts[ci.HostPublicKey.String()] = contracts
	}
	rows.Close()

	return nil
}

// updateState saves the sync state of HostDB.
func (hdb *HostDB) updateState() error {
	_, err := hdb.db.Exec(`
		UPDATE hdb_info
		SET height = ?, scan_complete = ?, disable_ip_check = ?, bid = ?
		WHERE id = 1
	`, hdb.tip.Height, hdb.initialScanComplete, hdb.disableIPViolationCheck, hdb.tip.ID[:])
	return err
}

// updateHost updates the host entry in the database.
// A lock should be acquired before calling this function.
func (hdb *HostDB) updateHost(host modules.HostDBEntry) error {
	// Insert the host. If it exists already, replace it.
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	encodeHostEntry(&host, e)
	e.Flush()
	_, err := hdb.db.Exec(`
		INSERT INTO hdb_hosts (public_key, filtered, bytes)
		VALUES (?, ?, ?) AS new
		ON DUPLICATE KEY UPDATE public_key = new.public_key,
		filtered = new.filtered, bytes = new.bytes
	`, host.PublicKey[:], host.Filtered, buf.Bytes())
	if err != nil {
		return modules.AddContext(err, "couldn't save host")
	}

	// Save IP nets data.
	tx, err := hdb.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM hdb_ipnets WHERE public_key = ?", host.PublicKey[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "couldn't clear IP nets")
	}
	for _, subnet := range host.IPNets {
		_, err = tx.Exec(`
			INSERT INTO hdb_ipnets (public_key, ip_net)
			VALUES (?, ?)`, host.PublicKey[:], subnet)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't save IP nets")
		}
	}

	return tx.Commit()
}

// updateScanHistory updates the scan history of the host entry in the
// database. A lock should be acquired before calling this function.
func (hdb *HostDB) updateScanHistory(host modules.HostDBEntry) error {
	tx, err := hdb.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM hdb_scanhistory WHERE public_key = ?", host.PublicKey[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "couldn't clear scan history")
	}

	for _, scan := range host.ScanHistory {
		_, err = tx.Exec(`
			INSERT INTO hdb_scanhistory (public_key, time, success)
			VALUES (?, ?, ?)
		`, host.PublicKey[:], uint64(scan.Timestamp.Unix()), scan.Success)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't save scan history")
		}
	}

	return tx.Commit()
}

// removeHost removes the host entry from the database.
// A lock should be acquired before calling this function.
func (hdb *HostDB) removeHost(host modules.HostDBEntry) error {
	tx, err := hdb.db.Begin()
	if err != nil {
		return err
	}

	// Clear the scan history.
	_, err = tx.Exec("DELETE FROM hdb_scanhistory WHERE public_key = ?", host.PublicKey[:])
	if err != nil {
		tx.Rollback()
		return err
	}

	// Clear the IP subnets.
	_, err = tx.Exec("DELETE FROM hdb_ipnets WHERE public_key = ?", host.PublicKey[:])
	if err != nil {
		tx.Rollback()
		return err
	}

	// Delete the host.
	_, err = tx.Exec("DELETE FROM hdb_hosts WHERE public_key = ?", host.PublicKey[:])
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// filterHost only changes the `filtered` field of a host entry.
func (hdb *HostDB) filterHost(pk types.PublicKey, filtered bool) error {
	_, err := hdb.db.Exec(`
		UPDATE hdb_hosts
		SET filtered = ?
		WHERE public_key = ?
	`, filtered, pk[:])
	return err
}

// saveFilter saves the HostDB filter info to disk.
func (hdb *HostDB) saveFilter() error {
	tx, err := hdb.db.Begin()
	if err != nil {
		return err
	}

	// Save filtered hosts.
	_, err = tx.Exec("DELETE FROM hdb_fhosts")
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, pk := range hdb.filteredHosts {
		_, err = tx.Exec("INSERT INTO hdb_fhosts (public_key) VALUES (?)", pk[:])
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// Save filtered domains.
	_, err = tx.Exec("DELETE FROM hdb_fdomains")
	if err != nil {
		tx.Rollback()
		return err
	}
	for domain := range hdb.filteredDomains.domains {
		_, err = tx.Exec("INSERT INTO hdb_fdomains (dom) VALUES (?)", domain)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// Save filter mode.
	_, err = tx.Exec("UPDATE hdb_info SET filter_mode = ? WHERE id = 1", hdb.filterMode)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// setIPCheck sets the disableIPViolationCheck flag in the database.
func (hdb *HostDB) setIPCheck(disabled bool) error {
	_, err := hdb.db.Exec("UPDATE hdb_info SET disable_ip_check = ? WHERE id = 1", disabled)
	return err
}

// saveKnownContracts saves the known contracts to disk.
func (hdb *HostDB) saveKnownContracts() error {
	tx, err := hdb.db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM hdb_contracts")
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, contracts := range hdb.knownContracts {
		for _, ci := range contracts {
			_, err = tx.Exec(`
				INSERT INTO hdb_contracts (host_pk, renter_pk, data)
				VALUES (?, ?, ?)
			`, ci.HostPublicKey[:], ci.RenterPublicKey[:], ci.StoredData)
			if err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	return tx.Commit()
}

// threadedLoadHosts loads the host entries from the database.
func (hdb *HostDB) threadedLoadHosts() {
	err := hdb.tg.Add()
	if err != nil {
		hdb.log.Error("couldn't start hostdb threadgroup", zap.Error(err))
		return
	}
	defer hdb.tg.Done()

	scanHistory := make(map[types.PublicKey][]modules.HostDBScan)
	ipNets := make(map[types.PublicKey][]string)

	// Load the scan history.
	scanRows, err := hdb.db.Query("SELECT public_key, time, success FROM hdb_scanhistory")
	if err != nil {
		hdb.log.Error("could not load the scan history", zap.Error(err))
		return
	}

	for scanRows.Next() {
		// Return if HostDB was shut down.
		select {
		case <-hdb.tg.StopChan():
			return
		default:
		}

		var timestamp uint64
		var success bool
		pkBytes := make([]byte, 32)
		if err := scanRows.Scan(&pkBytes, &timestamp, &success); err != nil {
			hdb.log.Error("could not load the scan history", zap.Error(err))
			continue
		}
		var pk types.PublicKey
		copy(pk[:], pkBytes)
		history := scanHistory[pk]
		scanHistory[pk] = append(history, modules.HostDBScan{
			Timestamp: time.Unix(int64(timestamp), 0),
			Success:   success,
		})
	}
	scanRows.Close()

	// Load the IP subnets.
	ipRows, err := hdb.db.Query("SELECT public_key, ip_net FROM hdb_ipnets")
	if err != nil {
		hdb.log.Error("could not load the IP subnets", zap.Error(err))
		return
	}

	for ipRows.Next() {
		// Return if HostDB was shut down.
		select {
		case <-hdb.tg.StopChan():
			return
		default:
		}

		var ip string
		pkBytes := make([]byte, 32)
		if err := ipRows.Scan(&pkBytes, &ip); err != nil {
			hdb.log.Error("could not load the IP subnets", zap.Error(err))
			continue
		}
		var pk types.PublicKey
		copy(pk[:], pkBytes)
		subnets := ipNets[pk]
		ipNets[pk] = append(subnets, ip)
	}
	ipRows.Close()

	// Load the hosts.
	rows, err := hdb.db.Query("SELECT public_key, filtered, bytes FROM hdb_hosts")
	if err != nil {
		hdb.log.Error("could not load the hosts", zap.Error(err))
		return
	}

	for rows.Next() {
		// Return if HostDB was shut down.
		select {
		case <-hdb.tg.StopChan():
			return
		default:
		}

		var host modules.HostDBEntry
		var filtered bool
		var hostBytes []byte
		pkBytes := make([]byte, 32)
		if err := rows.Scan(&pkBytes, &filtered, &hostBytes); err != nil {
			hdb.log.Error("could not load the host", zap.Error(err))
			continue
		}

		buf := bytes.NewBuffer(hostBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(hostBytes))})
		decodeHostEntry(&host, d)
		if err := d.Err(); err != nil {
			hdb.log.Error("could not load the host", zap.Error(err))
			continue
		}
		copy(host.PublicKey[:], pkBytes)
		host.Filtered = filtered
		host.ScanHistory = scanHistory[host.PublicKey]
		host.IPNets = ipNets[host.PublicKey]

		// COMPATv1.1.0
		//
		// The host did not always track its block height correctly, meaning
		// that previously the FirstSeen values and the blockHeight values
		// could get out of sync.
		hdb.mu.Lock()
		if hdb.tip.Height < host.FirstSeen {
			host.FirstSeen = hdb.tip.Height
		}

		err := hdb.insert(host)
		if err != nil {
			hdb.log.Error(fmt.Sprintf("could not insert host %v into hosttree while loading", host.Settings.NetAddress), zap.Error(err))
			hdb.mu.Unlock()
			continue
		}

		// Make sure that all hosts have gone through the initial scanning.
		if len(host.ScanHistory) < 2 {
			hdb.queueScan(host)
		}
		hdb.mu.Unlock()
	}
	rows.Close()

	if hdb.initialScanComplete {
		hdb.loadingComplete = true
	}
}
