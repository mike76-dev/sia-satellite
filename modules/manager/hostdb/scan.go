package hostdb

// scan.go contains the functions which periodically scan the list of all hosts
// to see which hosts are online or offline, and to get any updates to the
// settings of the hosts.

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb/hosttree"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"

	"lukechampine.com/frand"
)

const (
	// scanTimeElapsedRequirement defines the amount of time that must elapse
	// between scans in order for a new scan to be accepted into the hostdb as
	// part of the scan history.
	scanTimeElapsedRequirement = 60 * time.Minute
)

// equalIPNets checks if two slices of IP subnets contain the same subnets.
func equalIPNets(ipNetsA, ipNetsB []string) bool {
	// Check the length first.
	if len(ipNetsA) != len(ipNetsB) {
		return false
	}

	// Create a map of all the subnets in ipNetsA.
	mapNetsA := make(map[string]struct{})
	for _, subnet := range ipNetsA {
		mapNetsA[subnet] = struct{}{}
	}

	// Make sure that all the subnets from ipNetsB are in the map.
	for _, subnet := range ipNetsB {
		if _, exists := mapNetsA[subnet]; !exists {
			return false
		}
	}
	return true
}

// queueScan will add a host to the queue to be scanned. The host will be added
// at a random position which means that the order in which queueScan is called
// is not necessarily the order in which the hosts get scanned. That guarantees
// a random scan order during the initial scan.
func (hdb *HostDB) queueScan(entry modules.HostDBEntry) {
	// If this entry is already in the scan pool, can return immediately.
	_, exists := hdb.scanMap[entry.PublicKey.String()]
	if exists {
		return
	}
	// Add the entry to a random position in the waitlist.
	hdb.scanMap[entry.PublicKey.String()] = struct{}{}
	hdb.scanList = append(hdb.scanList, entry)
	if len(hdb.scanList) > 1 {
		i := len(hdb.scanList) - 1
		j := frand.Intn(i)
		hdb.scanList[i], hdb.scanList[j] = hdb.scanList[j], hdb.scanList[i]
	}
	// Check if any thread is currently emptying the waitlist. If not, spawn a
	// thread to empty the waitlist.
	if hdb.scanWait {
		// Another thread is emptying the scan list, nothing to worry about.
		return
	}

	// Sanity check - the scan map and the scan list should have the same
	// length.
	if len(hdb.scanMap) > len(hdb.scanList) + maxScanningThreads {
		hdb.staticLog.Println("CRITICAL: the hostdb scan map has seemingly grown too large:", len(hdb.scanMap), len(hdb.scanList), maxScanningThreads)
	}

	// Nobody is emptying the scan list, create and run a scan thread.
	hdb.scanWait = true
	go func() {
		scanPool := make(chan modules.HostDBEntry)
		defer close(scanPool)

		if hdb.tg.Add() != nil {
			// Hostdb is shutting down, don't spin up another thread.  It is
			// okay to leave scanWait set to true as that will not affect
			// shutdown.
			return
		}
		defer hdb.tg.Done()

		// Due to the patterns used to spin up scanning threads, it's possible
		// that we get to this point while all scanning threads are currently
		// used up, completing jobs that were sent out by the previous pool
		// managing thread. This thread is at risk of deadlocking if there's
		// not at least one scanning thread accepting work that it created
		// itself, so we use a starterThread exception and spin up
		// one-thread-too-many on the first iteration to ensure that we do not
		// deadlock.
		starterThread := false
		for {
			// If the scanList is empty, this thread can spin down.
			hdb.mu.Lock()
			if len(hdb.scanList) == 0 {
				// Scan list is empty, can exit. Let the world know that nobody
				// is emptying the scan list anymore.
				hdb.scanWait = false
				hdb.mu.Unlock()
				return
			}

			// Get the next host, shrink the scan list.
			entry := hdb.scanList[0]
			hdb.scanList = hdb.scanList[1:]
			delete(hdb.scanMap, entry.PublicKey.String())

			// Grab the most recent entry for this host.
			recentEntry, exists := hdb.staticHostTree.Select(entry.PublicKey)
			if exists {
				entry = recentEntry
			}

			// Try to send this entry to an existing idle worker (non-blocking).
			select {
			case scanPool <- entry:
				hdb.mu.Unlock()
				continue
			default:
			}

			// Create new worker thread.
			if hdb.scanningThreads < maxScanningThreads || !starterThread {
				starterThread = true
				hdb.scanningThreads++
				if err := hdb.tg.Add(); err != nil {
					hdb.mu.Unlock()
					return
				}
				go func() {
					defer hdb.tg.Done()
					hdb.threadedProbeHosts(scanPool)
					hdb.mu.Lock()
					hdb.scanningThreads--
					hdb.mu.Unlock()
				}()
			}
			hdb.mu.Unlock()

			// Block while waiting for an opening in the scan pool.
			select {
			case scanPool <- entry:
				continue
			case <-hdb.tg.StopChan():
				return
			}
		}
	}()
}

// updateEntry updates an entry in the hostdb after a scan has taken place.
//
// CAUTION: This function will automatically add multiple entries to a new host
// to give that host some base uptime. This makes this function co-dependent
// with the host score functions. Adjustment of the host score functions need
// to keep this function in mind, and vice-versa.
func (hdb *HostDB) updateEntry(entry modules.HostDBEntry, netErr error) {
	// If the scan failed because we don't have Internet access, toss out this update.
	if netErr != nil && !hdb.gateway.Online() {
		return
	}

	// Grab the host from the host tree, and update it with the new settings.
	newEntry, exists := hdb.staticHostTree.Select(entry.PublicKey)
	if exists {
		newEntry.Settings = entry.Settings
		newEntry.IPNets = entry.IPNets
		newEntry.LastIPNetChange = entry.LastIPNetChange
	} else {
		newEntry = entry
	}

	// Update the recent interactions with this host.
	//
	// No decay applied because block height is unknown.
	if netErr == nil {
		newEntry.RecentSuccessfulInteractions++
	} else {
		newEntry.RecentFailedInteractions++
	}

	// Add the datapoints for the scan.
	if len(newEntry.ScanHistory) < 2 {
		// Add two scans to the scan history. Two are needed because the scans
		// are forward looking, but we want this first scan to represent as
		// much as one week of uptime or downtime.
		earliestStartTime := time.Now().Add(time.Hour * 2 * 24 * -1) // Permit up two days starting uptime or downtime.
		suggestedStartTime := time.Now().Add(time.Minute * 10 * time.Duration(hdb.blockHeight - entry.FirstSeen + 1) * -1) // Add one to the FirstSeen in case FirstSeen is this block, guarantees incrementing order.
		if suggestedStartTime.Before(earliestStartTime) {
			suggestedStartTime = earliestStartTime
		}
		newEntry.ScanHistory = modules.HostDBScans{
			{Timestamp: suggestedStartTime, Success: netErr == nil},
			{Timestamp: time.Now(), Success: netErr == nil},
		}
	} else {
		// Do not add a new timestamp for the scan unless more than an hour has
		// passed since the previous scan.
		newTimestamp := time.Now()
		prevTimestamp := newEntry.ScanHistory[len(newEntry.ScanHistory) - 1].Timestamp
		if newTimestamp.After(prevTimestamp.Add(scanTimeElapsedRequirement)) {
			if newEntry.ScanHistory[len(newEntry.ScanHistory) - 1].Success && netErr != nil {
				hdb.staticLog.Printf("Host %v is being downgraded from an online host to an offline host: %v\n", newEntry.PublicKey.String(), netErr)
			}
			newEntry.ScanHistory = append(newEntry.ScanHistory, modules.HostDBScan{Timestamp: newTimestamp, Success: netErr == nil})
		}
	}

	// Check whether any of the recent scans demonstrate uptime. The pruning and
	// compression of the history ensure that there are only relatively recent
	// scans represented.
	var recentUptime bool
	for _, scan := range newEntry.ScanHistory {
		if scan.Success {
			recentUptime = true
		}
	}

	// If the host has been offline for too long, delete the host from the
	// hostdb. Only delete if there have been enough scans over a long enough
	// period to be confident that the host really is offline for good.
	cis, haveContractWithHost := hdb.knownContracts[newEntry.PublicKey.String()]
	downPastMaxDowntime := time.Since(newEntry.ScanHistory[0].Timestamp) > maxHostDowntime && !recentUptime
	if !haveContractWithHost && downPastMaxDowntime && len(newEntry.ScanHistory) >= minScans {
		if newEntry.HistoricUptime > 0 {
			hdb.staticLog.Printf("Removing %v with historic uptime from hostdb. Recent downtime timestamp is %v. Hostdb knows about %v contracts.", newEntry.PublicKey.String(), newEntry.ScanHistory[0].Timestamp, len(cis))
		}
		// Remove the host from the hostdb.
		err := hdb.remove(newEntry.PublicKey)
		if err != nil {
			hdb.staticLog.Println("ERROR: unable to remove host newEntry which has had a ton of downtime:", err)
		}
		if err = hdb.removeHost(newEntry); err != nil {
			hdb.staticLog.Println("ERROR: unable to remove host from the database:", err)
		}

		// The function should terminate here as no more interaction is needed
		// with this host.
		return
	}

	// Compress any old scans into the historic values.
	for len(newEntry.ScanHistory) > minScans && time.Now().Sub(newEntry.ScanHistory[0].Timestamp) > maxHostDowntime {
		timePassed := newEntry.ScanHistory[1].Timestamp.Sub(newEntry.ScanHistory[0].Timestamp)
		if newEntry.ScanHistory[0].Success {
			newEntry.HistoricUptime += timePassed
		} else {
			newEntry.HistoricDowntime += timePassed
		}
		newEntry.ScanHistory = newEntry.ScanHistory[1:]
	}

	// Add the updated entry.
	if !exists {
		// Insert into Hosttrees.
		err := hdb.insert(newEntry)
		if err != nil {
			hdb.staticLog.Println("ERROR: unable to insert entry which is thought to be new:", err)
		}
	} else {
		// Modify hosttrees.
		err := hdb.modify(newEntry)
		if err != nil {
			hdb.staticLog.Println("ERROR: unable to modify entry which is thought to exist:", err)
		}
	}
	if err := hdb.updateHost(newEntry); err != nil {
		hdb.staticLog.Println("ERROR: unable to update the host in the database:", err)
	}
	if err := hdb.updateScanHistory(newEntry); err != nil {
		hdb.staticLog.Println("ERROR: unable to update the scan history:", err)
	}
}

// staticLookupIPNets returns string representations of the CIDR subnets used by
// the host. In case of an error we return nil. We don't really care about the
// error because we don't update host entries if we are offline anyway. So if we
// fail to resolve a hostname, the problem is not related to us.
func (hdb *HostDB) staticLookupIPNets(address modules.NetAddress) (ipNets []string, err error) {
	// Lookup the IP addresses of the host.
	addresses, err := net.LookupIP(address.Host())
	if err != nil {
		return nil, err
	}

	// Get the subnets of the addresses.
	for _, ip := range addresses {
		// Set the filterRange according to the type of IP address.
		var filterRange int
		if ip.To4() != nil {
			filterRange = hosttree.IPv4FilterRange
		} else {
			filterRange = hosttree.IPv6FilterRange
		}

		// Get the subnet.
		_, ipnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", ip.String(), filterRange))
		if err != nil {
			return nil, err
		}
		// Add the subnet to the host.
		ipNets = append(ipNets, ipnet.String())
	}
	return
}

// managedScanHost will connect to a host and grab the settings, verifying
// uptime and updating to the host's preferences.
func (hdb *HostDB) managedScanHost(entry modules.HostDBEntry) {
	// Request settings from the queued host entry.
	netAddr := entry.Settings.NetAddress
	pubKey := entry.PublicKey

	// Resolve the host's used subnets and update the timestamp if they
	// changed. We only update the timestamp if resolving the ipNets was
	// successful.
	ipNets, err := hdb.staticLookupIPNets(modules.NetAddress(netAddr))
	if err == nil && !equalIPNets(ipNets, entry.IPNets) {
		entry.IPNets = ipNets
		entry.LastIPNetChange = time.Now()
	}
	if err != nil {
		hdb.staticLog.Println("ERROR: managedScanHost: failed to look up IP nets", err)
	}

	// Update historic interactions of entry if necessary.
	hdb.mu.Lock()
	updateHostHistoricInteractions(&entry, hdb.blockHeight)

	// We don't want to override the NetAddress during a scan so we need to
	// retrieve the most recent NetAddress from the tree first.
	oldEntry, exists := hdb.staticHostTree.Select(entry.PublicKey)
	hdb.mu.Unlock()

	var settings rhpv2.HostSettings
	var pt rhpv3.HostPriceTable
	var latency time.Duration
	err = func() error {
		timeout := hostRequestTimeout
		hdb.mu.RLock()
		if len(hdb.initialScanLatencies) > minScansForSpeedup {
			hdb.staticLog.Println("CRITICAL: initialScanLatencies should never be greater than minScansForSpeedup")
		}
		if !hdb.initialScanComplete && len(hdb.initialScanLatencies) == minScansForSpeedup {
			// During an initial scan, when we have at least minScansForSpeedup
			// active scans in initialScanLatencies, we use
			// 5*median(initialScanLatencies) as the new hostRequestTimeout to
			// speedup the scanning process.
			timeout = hdb.initialScanLatencies[len(hdb.initialScanLatencies) / 2]
			timeout *= scanSpeedupMedianMultiplier
			if hostRequestTimeout < timeout {
				timeout = hostRequestTimeout
			}
		}
		hdb.mu.RUnlock()

		// Create a context and set up its cancelling.
		ctx, cancel := context.WithTimeout(context.Background(), timeout + hostScanDeadline)
		connCloseChan := make(chan struct{})
		go func() {
			select {
			case <-hdb.tg.StopChan():
			case <-connCloseChan:
			}
			cancel()
		}()
		defer close(connCloseChan)

		// Initiate RHP2 protocol.
		start := time.Now()
		err := proto.WithTransportV2(ctx, netAddr, pubKey, func(t *rhpv2.Transport) error {
			var err error
			settings, err = proto.RPCSettings(t)
			return err
		})
		latency = time.Since(start)
		if err != nil {
			return modules.AddContext(err, "could not fetch host settings")
		}

		// Initiate RHP3 protocol.
		if exists {
			settings.NetAddress = oldEntry.Settings.NetAddress
		}
		err = proto.WithTransportV3(ctx, settings.SiamuxAddr(), pubKey, func (t *rhpv3.Transport) error {
			var err error
			pt, err = proto.RPCPriceTable(t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
				return nil, nil
			})
			return err
		})
		if err != nil {
			return modules.AddContext(err, "could not fetch host price table")
		}
		return nil
	}()
	if err != nil {
		hdb.staticLog.Printf("INFO: scan of host at %v failed: %v\n", pubKey, err)
	} else {
		entry.Settings = settings
		entry.PriceTable = pt
	}
	success := err == nil

	// Update the host tree to have a new entry, including the new error. Then
	// delete the entry from the scan map as the scan has been successful.
	hdb.updateEntry(entry, err)

	// Add the scan to the initialScanLatencies if it was successful.
	if success && len(hdb.initialScanLatencies) < minScansForSpeedup {
		hdb.initialScanLatencies = append(hdb.initialScanLatencies, latency)
		// If the slice has reached its maximum size we sort it.
		if len(hdb.initialScanLatencies) == minScansForSpeedup {
			sort.Slice(hdb.initialScanLatencies, func(i, j int) bool {
				return hdb.initialScanLatencies[i] < hdb.initialScanLatencies[j]
			})
		}
	}
}

// waitForScans is a helper function that blocks until the hostDB's scanList is
// empty.
func (hdb *HostDB) managedWaitForScans() {
	for {
		hdb.mu.Lock()
		length := len(hdb.scanList)
		hdb.mu.Unlock()
		if length == 0 {
			break
		}
		select {
		case <-hdb.tg.StopChan():
		case <-time.After(scanCheckInterval):
		}
	}
}

// threadedProbeHosts pulls hosts from the thread pool and runs a scan on them.
func (hdb *HostDB) threadedProbeHosts(scanPool <-chan modules.HostDBEntry) {
	for hostEntry := range scanPool {
		// Block until hostdb has internet connectivity.
		for {
			hdb.mu.RLock()
			online := hdb.gateway.Online()
			hdb.mu.RUnlock()
			if online {
				break
			}
			select {
			case <-time.After(time.Second * 30):
				continue
			case <-hdb.tg.StopChan():
				return
			}
		}

		// There appears to be internet connectivity, continue with the
		// scan.
		hdb.managedScanHost(hostEntry)
	}
}

// threadedScan is an ongoing function which will query the full set of hosts
// every few hours to see who is online and available for uploading.
func (hdb *HostDB) threadedScan() {
	err := hdb.tg.Add()
	if err != nil {
		hdb.staticLog.Println("ERROR: couldn't start hostdb threadgroup:", err)
		return
	}
	defer hdb.tg.Done()

	// Wait until the consensus set is synced. Only then we can be sure that
	// the initial scan covers the whole network.
	for {
		if hdb.managedSynced() {
			break
		}
		select {
		case <-hdb.tg.StopChan():
			return
		case <-time.After(scanCheckInterval):
		}
	}

	// The initial scan might have been interrupted. Queue one scan for every
	// announced host that was missed by the initial scan and wait for the
	// scans to finish before starting the scan loop.
	allHosts := hdb.staticHostTree.All()
	hdb.mu.Lock()
	for _, host := range allHosts {
		if len(host.ScanHistory) == 0 && host.HistoricUptime == 0 && host.HistoricDowntime == 0 {
			hdb.queueScan(host)
		}
	}
	hdb.mu.Unlock()

	// Do nothing until the scan list is empty. If there are hosts in the scan
	// list, other threads are ensuring they all get scanned.
	hdb.managedWaitForScans()

	hdb.mu.Lock()
	// Set the flag to indicate that the initial scan is complete.
	hdb.initialScanComplete = true
	err = hdb.updateState()
	if err != nil {
		hdb.staticLog.Println("ERROR: couldn't save hostdb state:", err)
		return
	}
	// Copy the known contracts to avoid having to lock the hdb later.
	knownContracts := make(map[string][]contractInfo)
	for k, cis := range hdb.knownContracts {
		for _, ci := range cis {
			knownContracts[k] = append(knownContracts[k], ci)
		}
	}
	hdb.mu.Unlock()

	for {
		// Set up a scan for the hostCheckupQuantity most valuable hosts in the
		// hostdb. Hosts that fail their scans will be docked significantly,
		// pushing them further back in the hierarchy, ensuring that for the
		// most part only online hosts are getting scanned unless there are
		// fewer than hostCheckupQuantity of them.

		// Grab a set of hosts to scan, grab hosts that are active, inactive, offline
		// and known to get high diversity.
		var onlineHosts, offlineHosts, knownHosts []modules.HostDBEntry
		allHosts := hdb.staticHostTree.All()
		for i := len(allHosts) - 1; i >= 0; i-- {
			if len(onlineHosts) >= hostCheckupQuantity &&
				len(offlineHosts) >= hostCheckupQuantity &&
				len(knownHosts) == len(knownContracts) {
				break
			}

			// Figure out if the host is online or offline.
			host := allHosts[i]
			online := len(host.ScanHistory) > 0 && host.ScanHistory[len(host.ScanHistory) - 1].Success
			_, known := knownContracts[host.PublicKey.String()]
			if known {
				knownHosts = append(knownHosts, host)
			} else if online && len(onlineHosts) < hostCheckupQuantity {
				onlineHosts = append(onlineHosts, host)
			} else if !online && len(offlineHosts) < hostCheckupQuantity {
				offlineHosts = append(offlineHosts, host)
			}
		}

		// Queue the scans for each host.
		hdb.staticLog.Println("INFO: performing scan on", len(onlineHosts), "online hosts and", len(offlineHosts), "offline hosts and", len(knownHosts), "known hosts.")
		hdb.mu.Lock()
		for _, host := range knownHosts {
			hdb.queueScan(host)
		}
		for _, host := range onlineHosts {
			hdb.queueScan(host)
		}
		for _, host := range offlineHosts {
			hdb.queueScan(host)
		}
		hdb.mu.Unlock()

		// Sleep for a random amount of time before doing another round of
		// scanning. The minimums and maximums keep the scan time reasonable,
		// while the randomness prevents the scanning from always happening at
		// the same time of day or week.
		sleepRange := uint64(maxScanSleep - minScanSleep)
		sleepTime := minScanSleep + time.Duration(frand.Uint64n(sleepRange))

		// Sleep until it's time for the next scan cycle.
		select {
		case <-hdb.tg.StopChan():
			return
		case <-time.After(sleepTime):
		}
	}
}
