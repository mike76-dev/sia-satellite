// Package hostdb provides a HostDB object that implements the Manager.HostDB
// interface. The blockchain is scanned for host announcements and hosts that
// are found get added to the host database. The database continually scans the
// set of hosts it has found and updates who is online.
package hostdb

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb/hosttree"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
)

var (
	// ErrInitialScanIncomplete is returned whenever an operation is not
	// allowed to be executed before the initial host scan has finished.
	ErrInitialScanIncomplete = errors.New("initial hostdb scan is not yet completed")

	// errHostNotFoundInTree is returned when the host is not found in the
	// hosttree.
	errHostNotFoundInTree = errors.New("host not found in hosttree")
)

// filteredDomains manages a list of blocked domains.
type filteredDomains struct {
	domains map[string]struct{}
	mu      sync.Mutex
}

// newFilteredDomains initializes a new filteredDomains.
func newFilteredDomains(domains []string) *filteredDomains {
	filtered := &filteredDomains{
		domains: make(map[string]struct{}),
	}
	filtered.managedAddDomains(domains)
	return filtered
}

// managedAddDomains adds domains to the map of blocked domains.
func (bd *filteredDomains) managedAddDomains(domains []string) {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	for _, domain := range domains {
		bd.domains[domain] = struct{}{}

		addrs, err := net.LookupHost(domain)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			bd.domains[addr] = struct{}{}
		}
	}
}

// managedFilteredDomains returns a list of the blocked domains.
func (bd *filteredDomains) managedFilteredDomains() []string {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	var domains []string
	for domain := range bd.domains {
		domains = append(domains, domain)
	}

	return domains
}

// managedIsFiltered checks to see if the domain is blocked.
func (bd *filteredDomains) managedIsFiltered(addr modules.NetAddress) bool {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	// See if this specific host:port combination was blocked.
	if _, blocked := bd.domains[string(addr)]; blocked {
		return true
	}

	// Now check if this host was blocked.
	hostname := addr.Host()
	_, blocked := bd.domains[hostname]
	if blocked {
		return true
	}

	ip := net.ParseIP(hostname)
	if ip != nil {
		for domain := range bd.domains {
			_, ipnet, _ := net.ParseCIDR(domain)
			if ipnet != nil && ipnet.Contains(ip) {
				return true
			}
		}
	}

	// Check for subdomains being blocked by a root domain.
	//
	// Split the hostname into elements.
	elements := strings.Split(hostname, ".")
	if len(elements) <= 1 {
		return blocked
	}

	// Check domains.
	//
	// We want to stop at the second last element so that the last domain
	// we check is of the format domain.com. This is to protect the user
	// from accidentally submitting `com`, or some other TLD, and blocking
	// every host in the HostDB.
	for i := 0; i < len(elements)-1; i++ {
		domainToCheck := strings.Join(elements[i:], ".")
		if _, blocked := bd.domains[domainToCheck]; blocked {
			return true
		}
	}
	return false
}

// contractInfo contains information about a contract relevant to the HostDB.
type contractInfo struct {
	RenterPublicKey types.PublicKey
	HostPublicKey   types.PublicKey
	StoredData      uint64
}

// The HostDB is a database of potential hosts. It assigns a score to each
// host based on their hosting parameters, and then can select hosts at random
// for uploading files.
type HostDB struct {
	// Dependencies.
	db *sql.DB
	cm *chain.Manager
	s  modules.Syncer

	log *zap.Logger
	mu  sync.RWMutex
	tg  siasync.ThreadGroup

	// knownContracts are contracts which the HostDB was informed about by the
	// Contractor. It contains infos about active contracts we have formed with
	// hosts. The mapkey is a serialized PublicKey.
	knownContracts map[string][]contractInfo

	// The hostdb gets initialized with an allowance that can be modified. The
	// allowance is used to build a scoreFunc that the hosttree depends on to
	// determine the score of a host.
	allowance modules.Allowance
	scoreFunc hosttree.ScoreFunc

	// The staticHostTree is the root node of the tree that organizes hosts by
	// score. The tree is necessary for selecting scored hosts at random.
	staticHostTree *hosttree.HostTree

	// The scan pool is a set of hosts that need to be scanned. There are a
	// handful of goroutines constantly waiting on the channel for hosts to
	// scan. The scan map is used to prevent duplicates from entering the scan
	// pool.
	initialScanComplete     bool
	initialScanLatencies    []time.Duration
	disableIPViolationCheck bool
	scanList                []modules.HostDBEntry
	scanMap                 map[string]struct{}
	scanWait                bool
	scanningThreads         int
	synced                  bool
	loadingComplete         bool

	// staticFilteredTree is a hosttree that only contains the hosts that align
	// with the filterMode. The filteredHosts are the hosts that are submitted
	// with the filterMode to determine which host should be in the
	// staticFilteredTree.
	filteredTree  *hosttree.HostTree
	filteredHosts map[string]types.PublicKey
	filterMode    modules.FilterMode

	// filteredDomains tracks blocked domains for the hostdb.
	filteredDomains *filteredDomains

	tip types.ChainIndex
}

// Enforce that HostDB satisfies the modules.HostDB interface.
var _ modules.HostDB = (*HostDB)(nil)

// insert inserts the HostDBEntry into both hosttrees.
func (hdb *HostDB) insert(host modules.HostDBEntry) error {
	err := hdb.staticHostTree.Insert(host)
	if hdb.filteredDomains.managedIsFiltered(modules.NetAddress(host.Settings.NetAddress)) {
		hdb.filteredHosts[host.PublicKey.String()] = host.PublicKey
		err = modules.ComposeErrors(err, hdb.staticHostTree.SetFiltered(host.PublicKey, true))
	}

	_, ok := hdb.filteredHosts[host.PublicKey.String()]
	isWhitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	if isWhitelist == ok {
		errF := hdb.filteredTree.Insert(host)
		if errF != nil && errF != hosttree.ErrHostExists {
			err = modules.ComposeErrors(err, errF)
		}
	}
	return err
}

// modify modifies the HostDBEntry in both hosttrees.
func (hdb *HostDB) modify(host modules.HostDBEntry) error {
	isWhitelist := hdb.filterMode == modules.HostDBActiveWhitelist

	err := hdb.staticHostTree.Modify(host)
	if hdb.filteredDomains.managedIsFiltered(modules.NetAddress(host.Settings.NetAddress)) {
		hdb.filteredHosts[host.PublicKey.String()] = host.PublicKey
		err = modules.ComposeErrors(err, hdb.staticHostTree.SetFiltered(host.PublicKey, true))
		if isWhitelist {
			errF := hdb.filteredTree.Insert(host)
			if errF != nil && errF != hosttree.ErrHostExists {
				err = modules.ComposeErrors(err, errF)
			}
		}
	}

	_, ok := hdb.filteredHosts[host.PublicKey.String()]
	if isWhitelist == ok {
		err = modules.ComposeErrors(err, hdb.filteredTree.Modify(host))
	}
	return err
}

// remove removes the HostDBEntry from both hosttrees.
func (hdb *HostDB) remove(pk types.PublicKey) error {
	err := hdb.staticHostTree.Remove(pk)
	_, ok := hdb.filteredHosts[pk.String()]
	isWhitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	if isWhitelist == ok {
		errF := hdb.filteredTree.Remove(pk)
		if err == nil && errF == hosttree.ErrNoSuchHost {
			return nil
		}
		err = modules.ComposeErrors(err, errF)
	}
	return err
}

// managedSetScoreFunction is a helper function that sets the scoreFunc field
// of the hostdb and also updates the the score function used by the hosttrees
// by rebuilding them. Apart from the constructor of the hostdb, this method
// should be used to update the score function in the hostdb and hosttrees.
func (hdb *HostDB) managedSetScoreFunction(sf hosttree.ScoreFunc) error {
	// Set the score function in the hostdb.
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.scoreFunc = sf
	// Update the hosttree and also the filteredTree if they are not the same.
	err := hdb.staticHostTree.SetScoreFunction(sf)
	if hdb.filteredTree != hdb.staticHostTree {
		err = modules.ComposeErrors(err, hdb.filteredTree.SetScoreFunction(sf))
	}
	return err
}

// managedSynced returns true if the hostdb is synced with the consensusset.
func (hdb *HostDB) managedSynced() bool {
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	return hdb.synced
}

// updateContracts rebuilds the knownContracts of the HostDB using the provided
// contracts.
func (hdb *HostDB) updateContracts(contracts []modules.RenterContract) {
	// Build a new set of known contracts.
	knownContracts := make(map[string][]contractInfo)
	for _, contract := range contracts {
		if n := len(contract.Transaction.FileContractRevisions); n != 1 {
			hdb.log.Error(fmt.Sprintf("contract's transaction should contain 1 revision but had %d", n))
			continue
		}
		kc, exists := knownContracts[contract.HostPublicKey.String()]
		if exists {
			found := false
			var i int
			for i = 0; i < len(kc); i++ {
				if kc[i].RenterPublicKey.String() == contract.RenterPublicKey.String() {
					found = true
					break
				}
			}
			if found {
				kc[i].StoredData += contract.Transaction.FileContractRevisions[0].Filesize
			} else {
				kc = append(kc, contractInfo{
					RenterPublicKey: contract.RenterPublicKey,
					HostPublicKey:   contract.HostPublicKey,
					StoredData:      contract.Transaction.FileContractRevisions[0].Filesize,
				})
			}
			knownContracts[contract.HostPublicKey.String()] = kc
		} else {
			knownContracts[contract.HostPublicKey.String()] = append(knownContracts[contract.HostPublicKey.String()], contractInfo{
				RenterPublicKey: contract.RenterPublicKey,
				HostPublicKey:   contract.HostPublicKey,
				StoredData:      contract.Transaction.FileContractRevisions[0].Filesize,
			})
		}
	}

	// Update the set of known contracts in the hostdb, log if the number of
	// contracts has decreased.
	if len(hdb.knownContracts) > len(knownContracts) {
		hdb.log.Info(fmt.Sprintf("hostdb is decreasing from %v known contracts to %v known contracts", len(hdb.knownContracts), len(knownContracts)))
	}
	hdb.knownContracts = knownContracts

	// Save the hostdb to persist the update.
	err := hdb.saveKnownContracts()
	if err != nil {
		hdb.log.Error("couldn't save set of known contracts", zap.Error(err))
	}
}

// hostdbBlockingStartup handles the blocking portion of New.
func hostdbBlockingStartup(db *sql.DB, cm *chain.Manager, s modules.Syncer, dir string) (*HostDB, error) {
	// Create the HostDB object.
	hdb := &HostDB{
		db: db,
		cm: cm,
		s:  s,

		filteredDomains: newFilteredDomains(nil),
		filteredHosts:   make(map[string]types.PublicKey),
		knownContracts:  make(map[string][]contractInfo),
		scanMap:         make(map[string]struct{}),
	}

	// Set the allowance and hostscore function.
	hdb.allowance = modules.DefaultAllowance
	hdb.scoreFunc = hdb.managedCalculateHostScoreFn(hdb.allowance)

	// Create the logger.
	logger, closeFn, err := persist.NewFileLogger(filepath.Join(dir, "hostdb.log"))
	if err != nil {
		return nil, err
	}
	hdb.log = logger
	hdb.tg.AfterStop(func() {
		closeFn()
	})

	// The host tree is used to manage hosts and query them at random. The
	// filteredTree is used when whitelist or blacklist is enabled.
	hdb.staticHostTree = hosttree.New(hdb.scoreFunc, hdb.log)
	hdb.filteredTree = hdb.staticHostTree

	// Load the prior persistence structures.
	err = hdb.initDB()
	if err != nil {
		return nil, err
	}
	hdb.mu.Lock()
	err = hdb.load()
	hdb.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// Spawn the scan loop.
	go hdb.threadedScan()
	hdb.tg.OnStop(func() {
		cm.RemoveSubscriber(hdb)
	})

	return hdb, nil
}

// hostdbAsyncStartup handles the async portion of New.
func hostdbAsyncStartup(hdb *HostDB, cm *chain.Manager) error {
	err := cm.AddSubscriber(hdb, hdb.tip)
	if err != nil {
		// Subscribe again using the new ID. This will cause a triggered scan
		// on all of the hosts, but that should be acceptable.
		hdb.mu.Lock()
		hdb.tip = types.ChainIndex{}
		err = hdb.reset()
		hdb.mu.Unlock()
		if err != nil {
			return err
		}
		err = cm.AddSubscriber(hdb, hdb.tip)
	}
	if err != nil {
		return err
	}
	return nil
}

// New returns a new HostDB.
func New(db *sql.DB, cm *chain.Manager, s modules.Syncer, dir string) (*HostDB, <-chan error) {
	errChan := make(chan error, 1)

	// Blocking startup.
	hdb, err := hostdbBlockingStartup(db, cm, s, dir)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Non-blocking startup.
	go func() {
		defer close(errChan)
		if err := hdb.tg.Add(); err != nil {
			errChan <- err
			return
		}
		defer hdb.tg.Done()
		// Subscribe to the consensus set in a separate goroutine.
		err := hostdbAsyncStartup(hdb, cm)
		if err != nil {
			errChan <- err
		}
	}()

	return hdb, errChan
}

// ActiveHosts returns a list of hosts that are currently online, sorted by
// score. If hostdb is in black or white list mode, then only active hosts from
// the filteredTree will be returned.
func (hdb *HostDB) ActiveHosts() (activeHosts []modules.HostDBEntry, err error) {
	if err = hdb.tg.Add(); err != nil {
		return activeHosts, err
	}
	defer hdb.tg.Done()

	hdb.mu.RLock()
	allHosts := hdb.filteredTree.All()
	hdb.mu.RUnlock()
	for _, entry := range allHosts {
		if len(entry.ScanHistory) == 0 {
			continue
		}
		if !entry.ScanHistory[len(entry.ScanHistory)-1].Success {
			continue
		}
		if !entry.Settings.AcceptingContracts {
			continue
		}
		activeHosts = append(activeHosts, entry)
	}
	return activeHosts, err
}

// AllHosts returns all of the hosts known to the hostdb, including the inactive
// ones. AllHosts is not filtered by blacklist or whitelist mode.
func (hdb *HostDB) AllHosts() (allHosts []modules.HostDBEntry, err error) {
	if err := hdb.tg.Add(); err != nil {
		return allHosts, err
	}
	defer hdb.tg.Done()
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	return hdb.staticHostTree.All(), nil
}

// CheckForIPViolations accepts a number of host public keys and returns the
// ones that violate the rules of the addressFilter.
func (hdb *HostDB) CheckForIPViolations(hosts []types.PublicKey) ([]types.PublicKey, error) {
	if err := hdb.tg.Add(); err != nil {
		return nil, err
	}
	defer hdb.tg.Done()

	// If the check was disabled we don't return any bad hosts.
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	disabled := hdb.disableIPViolationCheck
	if disabled {
		return nil, nil
	}

	var entries []modules.HostDBEntry
	var badHosts []types.PublicKey

	// Get the entries which correspond to the keys.
	for _, host := range hosts {
		entry, exists := hdb.staticHostTree.Select(host)
		if !exists {
			// A host that's not in the hostdb is bad.
			badHosts = append(badHosts, host)
			continue
		}
		entries = append(entries, entry)
	}

	// Sort the entries by the amount of time they have occupied their
	// corresponding subnets. This is the order in which they will be passed
	// into the filter which prioritizes entries which are passed in earlier.
	// That means 'younger' entries will be replaced in case of a violation.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastIPNetChange.Before(entries[j].LastIPNetChange)
	})

	// Create a filter and apply it.
	filter := hosttree.NewFilter()
	for _, entry := range entries {
		// Check if the host violates the rules.
		if filter.Filtered(modules.NetAddress(entry.Settings.NetAddress)) {
			badHosts = append(badHosts, entry.PublicKey)
			continue
		}
		// If it didn't then we add it to the filter.
		filter.Add(modules.NetAddress(entry.Settings.NetAddress))
	}
	return badHosts, nil
}

// Close closes the hostdb, terminating its scanning threads.
func (hdb *HostDB) Close() error {
	return hdb.tg.Stop()
}

// Host returns the HostSettings associated with the specified pubkey. If no
// matching host is found, Host returns false.  For black and white list modes,
// the Filtered field for the HostDBEntry is set to indicate it the host is
// being filtered from the filtered hosttree.
func (hdb *HostDB) Host(pk types.PublicKey) (modules.HostDBEntry, bool, error) {
	if err := hdb.tg.Add(); err != nil {
		return modules.HostDBEntry{}, false, modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	hdb.mu.Lock()
	whitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	filteredHosts := hdb.filteredHosts
	hdb.mu.Unlock()
	host, exists := hdb.staticHostTree.Select(pk)
	if !exists {
		return host, exists, errHostNotFoundInTree
	}
	_, ok := filteredHosts[pk.String()]
	host.Filtered = whitelist != ok
	hdb.mu.RLock()
	updateHostHistoricInteractions(&host, hdb.tip.Height)
	hdb.mu.RUnlock()
	return host, exists, nil
}

// Filter returns the hostdb's filterMode and filteredHosts.
func (hdb *HostDB) Filter() (modules.FilterMode, map[string]types.PublicKey, []string, error) {
	if err := hdb.tg.Add(); err != nil {
		return modules.HostDBFilterError, nil, nil, modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	filteredHosts := make(map[string]types.PublicKey)
	for k, v := range hdb.filteredHosts {
		filteredHosts[k] = v
	}
	return hdb.filterMode, filteredHosts, hdb.filteredDomains.managedFilteredDomains(), nil
}

// SetFilterMode sets the hostdb filter mode.
func (hdb *HostDB) SetFilterMode(fm modules.FilterMode, hosts []types.PublicKey, netAddresses []string) error {
	if err := hdb.tg.Add(); err != nil {
		return modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	// Check for error.
	if fm == modules.HostDBFilterError {
		return errors.New("cannot set hostdb filter mode, provided filter mode is an error")
	}
	// Check if disabling.
	if fm == modules.HostDBDisableFilter {
		// Reset filtered field for hosts.
		for _, pk := range hdb.filteredHosts {
			err := hdb.staticHostTree.SetFiltered(pk, false)
			if err != nil {
				hdb.log.Error("unable to mark entry as not filtered", zap.Error(err))
			}
			if err := hdb.filterHost(pk, false); err != nil {
				hdb.log.Error("unable to save unfiltered host", zap.Error(err))
			}
		}
		// Reset filtered fields.
		hdb.filteredTree = hdb.staticHostTree
		hdb.filteredHosts = make(map[string]types.PublicKey)
		hdb.filteredDomains = newFilteredDomains(nil)
		hdb.filterMode = fm
		return hdb.saveFilter()
	}

	// Check for no hosts submitted with whitelist enabled.
	isWhitelist := fm == modules.HostDBActiveWhitelist
	if (len(hosts) == 0 && len(netAddresses) == 0) && isWhitelist {
		return errors.New("cannot enable whitelist without hosts")
	}

	// Create filtered HostTree.
	hdb.filteredTree = hosttree.New(hdb.scoreFunc, hdb.log)
	filteredDomains := newFilteredDomains(netAddresses)

	// Create filteredHosts map.
	filteredHosts := make(map[string]types.PublicKey)
	for _, h := range hosts {
		// Add host to filtered host map.
		if _, ok := filteredHosts[h.String()]; ok {
			continue
		}
		filteredHosts[h.String()] = h

		// Update host in unfiltered hosttree.
		err := hdb.staticHostTree.SetFiltered(h, true)
		if err != nil {
			hdb.log.Error("unable to mark entry as filtered", zap.Error(err))
		}
		if err := hdb.filterHost(h, true); err != nil {
			hdb.log.Error("unable to save filtered host", zap.Error(err))
		}
	}

	var allErrs error
	allHosts := hdb.staticHostTree.All()
	for _, host := range allHosts {
		if !filteredDomains.managedIsFiltered(modules.NetAddress(host.Settings.NetAddress)) {
			continue
		}
		filteredHosts[host.PublicKey.String()] = host.PublicKey

		// Update host in unfiltered hosttree.
		err := hdb.staticHostTree.SetFiltered(host.PublicKey, true)
		if err != nil {
			hdb.log.Error("unable to mark entry as filtered", zap.Error(err))
		}
		if err := hdb.filterHost(host.PublicKey, true); err != nil {
			hdb.log.Error("unable to save filtered host", zap.Error(err))
		}
	}
	for _, host := range allHosts {
		// Add hosts to filtered tree.
		_, ok := filteredHosts[host.PublicKey.String()]
		if isWhitelist != ok {
			continue
		}
		err := hdb.filteredTree.Insert(host)
		if err != nil {
			allErrs = modules.ComposeErrors(allErrs, err)
		}
	}
	hdb.filteredHosts = filteredHosts
	hdb.filterMode = fm
	hdb.filteredDomains = filteredDomains

	return modules.ComposeErrors(allErrs, hdb.saveFilter())
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (hdb *HostDB) InitialScanComplete() (complete bool, height uint64, err error) {
	if err = hdb.tg.Add(); err != nil {
		return false, 0, modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	complete = hdb.initialScanComplete
	height = hdb.tip.Height
	return
}

// IPViolationsCheck returns a boolean indicating if the IP violation check is
// enabled or not.
func (hdb *HostDB) IPViolationsCheck() (bool, error) {
	if err := hdb.tg.Add(); err != nil {
		return false, modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	return !hdb.disableIPViolationCheck, nil
}

// SetAllowance updates the allowance used by the hostdb for scoring hosts by
// updating the host score function. It will completely rebuild the hosttree so
// it should be used with care.
func (hdb *HostDB) SetAllowance(allowance modules.Allowance) error {
	if err := hdb.tg.Add(); err != nil {
		return modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	// If the allowance is empty, set it to the default allowance. This ensures
	// that the estimates are at least moderately grounded.
	if reflect.DeepEqual(allowance, modules.Allowance{}) {
		allowance = modules.DefaultAllowance
	}

	// Update the allowance.
	hdb.mu.Lock()
	hdb.allowance = allowance
	hdb.mu.Unlock()

	// Update the weight function.
	sf := hdb.managedCalculateHostScoreFn(allowance)
	return hdb.managedSetScoreFunction(sf)
}

// SetIPViolationCheck enables or disables the IP violation check. If disabled,
// CheckForIPViolations won't return bad hosts and RandomHosts will return the
// address blacklist.
func (hdb *HostDB) SetIPViolationCheck(enabled bool) error {
	if err := hdb.tg.Add(); err != nil {
		return modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.disableIPViolationCheck = !enabled
	return hdb.setIPCheck(!enabled)
}

// LoadingComplete indicates if the HostDB has finished loading the hosts
// from the database.
func (hdb *HostDB) LoadingComplete() bool {
	return hdb.loadingComplete
}

// UpdateContracts rebuilds the knownContracts of the HostBD using the provided
// contracts.
func (hdb *HostDB) UpdateContracts(contracts []modules.RenterContract) error {
	if err := hdb.tg.Add(); err != nil {
		return modules.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.updateContracts(contracts)
	return nil
}
