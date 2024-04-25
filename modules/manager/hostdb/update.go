package hostdb

import (
	"fmt"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
)

// findHostAnnouncements returns a list of the host announcements found within
// a given block. No check is made to see that the ip address found in the
// announcement is actually a valid ip address.
func findHostAnnouncements(b types.Block) (announcements []modules.HostDBEntry) {
	for _, t := range b.Transactions {
		// the HostAnnouncement must be prefaced by the standard host
		// announcement string.
		for _, arb := range t.ArbitraryData {
			addr, pubKey, err := modules.DecodeAnnouncement(arb)
			if err != nil {
				continue
			}

			// Add the announcement to the slice being returned.
			var host modules.HostDBEntry
			host.Settings.NetAddress = string(addr)
			host.PublicKey = pubKey
			announcements = append(announcements, host)
		}
	}
	for _, t := range b.V2Transactions() {
		for _, at := range t.Attestations {
			addr, pubKey, err := modules.DecodeV2Announcement(at)
			if err != nil {
				continue
			}

			// Add the announcement to the slice being returned.
			var host modules.HostDBEntry
			host.Settings.NetAddress = string(addr)
			host.PublicKey = pubKey
			announcements = append(announcements, host)
		}
	}
	return
}

// insertBlockchainHost adds a host entry to the state. The host will be inserted
// into the set of all hosts, and if it is online and responding to requests it
// will be put into the list of active hosts.
func (hdb *HostDB) insertBlockchainHost(host modules.HostDBEntry) {
	// Remove garbage hosts and local hosts.
	if err := modules.NetAddress(host.Settings.NetAddress).IsValid(); err != nil {
		hdb.log.Warn(fmt.Sprintf("host '%v' has an invalid NetAddress", host.Settings.NetAddress), zap.Error(err))
		return
	}
	// Ignore all local hosts announced through the blockchain.
	if modules.NetAddress(host.Settings.NetAddress).IsLocal() {
		return
	}

	// Make sure the host gets into the host tree so it does not get dropped if
	// shutdown occurs before a scan can be performed.
	oldEntry, exists := hdb.staticHostTree.Select(host.PublicKey)
	if exists {
		// Replace the netaddress with the most recently announced netaddress.
		// Also replace the FirstSeen value with the current block height if
		// the first seen value has been set to zero (no hosts actually have a
		// first seen height of zero, but due to rescans hosts can end up with
		// a zero-value FirstSeen field.
		oldEntry.Settings.NetAddress = host.Settings.NetAddress
		if oldEntry.FirstSeen == 0 {
			oldEntry.FirstSeen = hdb.tip.Height
		}
		oldEntry.LastAnnouncement = hdb.tip.Height

		// Resolve the host's used subnets and update the timestamp if they
		// changed. We only update the timestamp if resolving the ipNets was
		// successful.
		ipNets, err := hdb.staticLookupIPNets(modules.NetAddress(oldEntry.Settings.NetAddress))
		if err == nil && !equalIPNets(ipNets, oldEntry.IPNets) {
			oldEntry.IPNets = ipNets
			oldEntry.LastIPNetChange = time.Now()
		}

		// Modify hosttree.
		err = hdb.modify(oldEntry)
		if err != nil {
			hdb.log.Error("unable to modify host entry of host tree after a blockchain scan", zap.Error(err))
		}

		// Update the database.
		err = hdb.updateHost(oldEntry)
		if err != nil {
			hdb.log.Error("unable to update host entry in the database", zap.Error(err))
		}
	} else {
		host.FirstSeen = hdb.tip.Height

		// Insert into hosttree.
		err := hdb.insert(host)
		if err != nil {
			hdb.log.Error("unable to insert host entry into host tree after a blockchain scan", zap.Error(err))
		}

		// Update the database.
		err = hdb.updateHost(host)
		if err != nil {
			hdb.log.Error("unable to insert host entry into database", zap.Error(err))
		}
	}

	// Add the host to the scan queue.
	hdb.queueScan(host)
}

// UpdateChainState applies the ChainManager updates.
func (hdb *HostDB) UpdateChainState(_ []chain.RevertUpdate, applied []chain.ApplyUpdate) error {
	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	for _, cau := range applied {
		hdb.tip = cau.State.Index

		for _, host := range findHostAnnouncements(cau.Block) {
			hdb.insertBlockchainHost(host)
		}
	}

	if err := hdb.updateState(); err != nil {
		hdb.log.Error("unable to save hostdb state", zap.Error(err))
		return err
	}

	return nil
}
