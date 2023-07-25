package hostdb

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb/hosttree"

	"go.sia.tech/core/types"
)

// RandomHosts implements the HostDB interface's RandomHosts() method. It takes
// a number of hosts to return, and a slice of netaddresses to ignore, and
// returns a slice of entries. If the IP violation check was disabled, the
// addressBlacklist is ignored.
func (hdb *HostDB) RandomHosts(n int, blacklist, addressBlacklist []types.PublicKey) ([]modules.HostDBEntry, error) {
	hdb.mu.RLock()
	initialScanComplete := hdb.initialScanComplete
	ipCheckDisabled := hdb.disableIPViolationCheck
	filteredTree := hdb.filteredTree
	hdb.mu.RUnlock()
	if !initialScanComplete {
		return []modules.HostDBEntry{}, ErrInitialScanIncomplete
	}
	if ipCheckDisabled {
		return filteredTree.SelectRandom(n, blacklist, nil), nil
	}
	return filteredTree.SelectRandom(n, blacklist, addressBlacklist), nil
}

// RandomHostsWithAllowance works as RandomHosts but uses a temporary hosttree
// created from the specified allowance. This is a very expensive call and
// should be used with caution.
func (hdb *HostDB) RandomHostsWithAllowance(n int, blacklist, addressBlacklist []types.PublicKey, allowance modules.Allowance) ([]modules.HostDBEntry, error) {
	hdb.mu.RLock()
	initialScanComplete := hdb.initialScanComplete
	filteredHosts := hdb.filteredHosts
	filterType := hdb.filterMode
	hdb.mu.RUnlock()
	if !initialScanComplete {
		return []modules.HostDBEntry{}, ErrInitialScanIncomplete
	}
	// Create a temporary hosttree from the given allowance.
	ht := hosttree.New(hdb.managedCalculateHostScoreFn(allowance), hdb.staticLog)

	// Insert all known hosts.
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	var insertErrs error
	allHosts := hdb.staticHostTree.All()
	isWhitelist := filterType == modules.HostDBActiveWhitelist
	for _, host := range allHosts {
		// Filter out listed hosts.
		_, ok := filteredHosts[host.PublicKey.String()]
		if isWhitelist != ok {
			continue
		}
		// Check if the allowance limits are not exceeded.
		if limitsExceeded(host, allowance) {
			continue
		}
		// Insert the host.
		if err := ht.Insert(host); err != nil {
			insertErrs = modules.ComposeErrors(insertErrs, err)
		}
	}

	// Select hosts from the temporary hosttree.
	return ht.SelectRandom(n, blacklist, addressBlacklist), insertErrs
}

// limitsExceeded checks if the host falls out of the limits set
// in the allowance.
func limitsExceeded(host modules.HostDBEntry, allowance modules.Allowance) bool {
	if host.Settings.MaxDuration < allowance.Period {
		return true
	}
	if !allowance.MaxRPCPrice.IsZero() && host.Settings.BaseRPCPrice.Cmp(allowance.MaxRPCPrice) > 0 {
		return true
	}
	if !allowance.MaxContractPrice.IsZero() && host.Settings.ContractPrice.Cmp(allowance.MaxContractPrice) > 0 {
		return true
	}
	if !allowance.MaxDownloadBandwidthPrice.IsZero() && host.Settings.DownloadBandwidthPrice.Cmp(allowance.MaxDownloadBandwidthPrice) > 0 {
		return true
	}
	if !allowance.MaxSectorAccessPrice.IsZero() && host.Settings.SectorAccessPrice.Cmp(allowance.MaxSectorAccessPrice) > 0 {
		return true
	}
	if !allowance.MaxStoragePrice.IsZero() && host.Settings.StoragePrice.Cmp(allowance.MaxStoragePrice) > 0 {
		return true
	}
	if !allowance.MaxUploadBandwidthPrice.IsZero() && host.Settings.UploadBandwidthPrice.Cmp(allowance.MaxUploadBandwidthPrice) > 0 {
		return true
	}
	if !allowance.MinMaxCollateral.IsZero() && host.Settings.MaxCollateral.Cmp(allowance.MinMaxCollateral) < 0 {
		return true
	}
	return false
}
