// Package hostdb provides a HostDB object that implements the Manager.HostDB
// interface. The blockchain is scanned for host announcements and hosts that
// are found get added to the host database. The database continually scans the
// set of hosts it has found and updates who is online.
package hostdb

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/satellite/manager/hostdb/hosttree"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"
	"gitlab.com/NebulousLabs/threadgroup"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	"go.sia.tech/siad/types"
)

var (
	// ErrInitialScanIncomplete is returned whenever an operation is not
	// allowed to be executed before the initial host scan has finished.
	ErrInitialScanIncomplete = errors.New("initial hostdb scan is not yet completed")
	errNilCS                 = errors.New("cannot create hostdb with nil consensus set")
	errNilGateway            = errors.New("cannot create hostdb with nil gateway")
	errNilTPool              = errors.New("cannot create hostdb with nil transaction pool")
	errNilSiaMux             = errors.New("cannot create hostdb with nil siamux")
	errNilDB                 = errors.New("cannot create hostdb with nil database")

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
func (bd *filteredDomains) managedIsFiltered(addr smodules.NetAddress) bool {
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
	for i := 0; i < len(elements) - 1; i++ {
		domainToCheck := strings.Join(elements[i:], ".")
		if _, blocked := bd.domains[domainToCheck]; blocked {
			return true
		}
	}
	return false
}

// contractInfo contains information about a contract relevant to the HostDB.
type contractInfo struct {
	RenterPublicKey types.SiaPublicKey `json:"renterpublickey"`
	HostPublicKey   types.SiaPublicKey `json:"hostpublickey"`
	StoredData      uint64             `json:"storeddata"`
}

// The HostDB is a database of potential hosts. It assigns a weight to each
// host based on their hosting parameters, and then can select hosts at random
// for uploading files.
type HostDB struct {
	// Dependencies.
	cs          smodules.ConsensusSet
	gateway     smodules.Gateway
	staticMux   *siamux.SiaMux
	staticTpool smodules.TransactionPool

	staticLog     *persist.Logger
	persistDir    string
	db            *sql.DB
	mu            sync.RWMutex
	staticAlerter *smodules.GenericAlerter
	tg            threadgroup.ThreadGroup
	resolver      smodules.Resolver

	// knownContracts are contracts which the HostDB was informed about by the
	// Contractor. It contains infos about active contracts we have formed with
	// hosts. The mapkey is a serialized SiaPublicKey.
	knownContracts map[string][]contractInfo

	// The hostdb gets initialized with an allowance that can be modified. The
	// allowance is used to build a weightFunc that the hosttree depends on to
	// determine the weight of a host.
	allowance  modules.Allowance
	weightFunc hosttree.WeightFunc

	// txnFees are the most recent fees used in the score estimation. It is
	// used to determine if the transaction fees have changed enough to warrant
	// rebuilding the hosttree with an updated weight function.
	txnFees types.Currency

	// The staticHostTree is the root node of the tree that organizes hosts by
	// weight. The tree is necessary for selecting weighted hosts at random.
	staticHostTree *hosttree.HostTree

	// The scan pool is a set of hosts that need to be scanned. There are a
	// handful of goroutines constantly waiting on the channel for hosts to
	// scan. The scan map is used to prevent duplicates from entering the scan
	// pool.
	initialScanComplete     bool
	initialScanLatencies    []time.Duration
	disableIPViolationCheck bool
	scanList                []smodules.HostDBEntry
	scanMap                 map[string]struct{}
	scanWait                bool
	scanningThreads         int
	synced                  bool
	loadingComplete         bool

	// staticFilteredTree is a hosttree that only contains the hosts that align
	// with the filterMode. The filteredHosts are the hosts that are submitted
	// with the filterMode to determine which host should be in the
	// staticFilteredTree
	filteredTree  *hosttree.HostTree
	filteredHosts map[string]types.SiaPublicKey
	filterMode    smodules.FilterMode

	// filteredDomains tracks blocked domains for the hostdb.
	filteredDomains *filteredDomains

	blockHeight types.BlockHeight
	lastChange  smodules.ConsensusChangeID
}

// Enforce that HostDB satisfies the modules.HostDB interface.
var _ modules.HostDB = (*HostDB)(nil)

// insert inserts the HostDBEntry into both hosttrees.
func (hdb *HostDB) insert(host smodules.HostDBEntry) error {
	err := hdb.staticHostTree.Insert(host)
	if hdb.filteredDomains.managedIsFiltered(host.NetAddress) {
		hdb.filteredHosts[host.PublicKey.String()] = host.PublicKey
		err = errors.Compose(err, hdb.staticHostTree.SetFiltered(host.PublicKey, true))
	}

	_, ok := hdb.filteredHosts[host.PublicKey.String()]
	isWhitelist := hdb.filterMode == smodules.HostDBActiveWhitelist
	if isWhitelist == ok {
		errF := hdb.filteredTree.Insert(host)
		if errF != nil && errF != hosttree.ErrHostExists {
			err = errors.Compose(err, errF)
		}
	}
	return err
}

// modify modifies the HostDBEntry in both hosttrees.
func (hdb *HostDB) modify(host smodules.HostDBEntry) error {
	isWhitelist := hdb.filterMode == smodules.HostDBActiveWhitelist

	err := hdb.staticHostTree.Modify(host)
	if hdb.filteredDomains.managedIsFiltered(host.NetAddress) {
		hdb.filteredHosts[host.PublicKey.String()] = host.PublicKey
		err = errors.Compose(err, hdb.staticHostTree.SetFiltered(host.PublicKey, true))
		if isWhitelist {
			errF := hdb.filteredTree.Insert(host)
			if errF != nil && errF != hosttree.ErrHostExists {
				err = errors.Compose(err, errF)
			}
		}
	}

	_, ok := hdb.filteredHosts[host.PublicKey.String()]
	if isWhitelist == ok {
		err = errors.Compose(err, hdb.filteredTree.Modify(host))
	}
	return err
}

// remove removes the HostDBEntry from both hosttrees.
func (hdb *HostDB) remove(pk types.SiaPublicKey) error {
	err := hdb.staticHostTree.Remove(pk)
	_, ok := hdb.filteredHosts[pk.String()]
	isWhitelist := hdb.filterMode == smodules.HostDBActiveWhitelist
	if isWhitelist == ok {
		errF := hdb.filteredTree.Remove(pk)
		if err == nil && errF == hosttree.ErrNoSuchHost {
			return nil
		}
		err = errors.Compose(err, errF)
	}
	return err
}

// managedSetWeightFunction is a helper function that sets the weightFunc field
// of the hostdb and also updates the the weight function used by the hosttrees
// by rebuilding them. Apart from the constructor of the hostdb, this method
// should be used to update the weight function in the hostdb and hosttrees.
func (hdb *HostDB) managedSetWeightFunction(wf hosttree.WeightFunc) error {
	// Set the weight function in the hostdb.
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.weightFunc = wf
	// Update the hosttree and also the filteredTree if they are not the same.
	err := hdb.staticHostTree.SetWeightFunction(wf)
	if hdb.filteredTree != hdb.staticHostTree {
		err = errors.Compose(err, hdb.filteredTree.SetWeightFunction(wf))
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
			hdb.staticLog.Println("CRITICAL: contract's transaction should contain 1 revision but had ", n)
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
				kc[i].StoredData += contract.Transaction.FileContractRevisions[0].NewFileSize
			} else {
				kc = append(kc, contractInfo{
					RenterPublicKey: contract.RenterPublicKey,
					HostPublicKey:   contract.HostPublicKey,
		  		StoredData:      contract.Transaction.FileContractRevisions[0].NewFileSize,
				})
			}
			knownContracts[contract.HostPublicKey.String()] = kc
		} else {
			knownContracts[contract.HostPublicKey.String()] = append(knownContracts[contract.HostPublicKey.String()], contractInfo{
				RenterPublicKey: contract.RenterPublicKey,
				HostPublicKey:   contract.HostPublicKey,
				StoredData:      contract.Transaction.FileContractRevisions[0].NewFileSize,
			})
		}
	}

	// Update the set of known contracts in the hostdb, log if the number of
	// contracts has decreased.
	if len(hdb.knownContracts) > len(knownContracts) {
		hdb.staticLog.Printf("Hostdb is decreasing from %v known contracts to %v known contracts", len(hdb.knownContracts), len(knownContracts))
	}
	hdb.knownContracts = knownContracts

	// Save the hostdb to persist the update.
	err := hdb.saveSync()
	if err != nil {
		hdb.staticLog.Println("Error saving set of known contracts:", err)
	}
}

// hostdbBlockingStartup handles the blocking portion of New.
func hostdbBlockingStartup(g smodules.Gateway, cs smodules.ConsensusSet, tpool smodules.TransactionPool, persistDir string, db *sql.DB, siamux *siamux.SiaMux) (*HostDB, error) {
	// Check for nil inputs.
	if g == nil {
		return nil, errNilGateway
	}
	if cs == nil {
		return nil, errNilCS
	}
	if tpool == nil {
		return nil, errNilTPool
	}
	if siamux == nil {
		return nil, errNilSiaMux
	}
	if db == nil {
		return nil, errNilDB
	}

	// Create the HostDB object.
	hdb := &HostDB{
		cs:          cs,
		resolver:    new(smodules.ProductionResolver),
		gateway:     g,
		persistDir:  persistDir,
		db:          db,
		staticMux:   siamux,
		staticTpool: tpool,

		filteredDomains: newFilteredDomains(nil),
		filteredHosts:   make(map[string]types.SiaPublicKey),
		knownContracts:  make(map[string][]contractInfo),
		scanMap:         make(map[string]struct{}),
		staticAlerter:   smodules.NewAlerter("hostdb"),
	}

	// Set the allowance, txnFees and hostweight function.
	hdb.allowance = modules.DefaultAllowance
	_, hdb.txnFees = hdb.staticTpool.FeeEstimation()
	hdb.weightFunc = hdb.managedCalculateHostWeightFn(hdb.allowance)

	// Create the persist directory if it does not yet exist.
	err := os.MkdirAll(persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Create the logger.
	logger, err := persist.NewFileLogger(filepath.Join(persistDir, "hostdb.log"))
	if err != nil {
		return nil, err
	}
	hdb.staticLog = logger
	err = hdb.tg.AfterStop(func() error {
		if err := hdb.staticLog.Close(); err != nil {
			// Resort to println as the logger is in an uncertain state.
			fmt.Println("Failed to close the hostdb logger:", err)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The host tree is used to manage hosts and query them at random. The
	// filteredTree is used when whitelist or blacklist is enabled.
	hdb.staticHostTree = hosttree.New(hdb.weightFunc, hdb.resolver)
	hdb.filteredTree = hdb.staticHostTree

	// Load the prior persistence structures.
	hdb.mu.Lock()
	err = hdb.load()
	hdb.mu.Unlock()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	err = hdb.tg.AfterStop(func() error {
		hdb.mu.Lock()
		err := hdb.saveSync()
		hdb.mu.Unlock()
		if err != nil {
			hdb.staticLog.Println("Unable to save the hostdb:", err)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Loading is complete, establish the save loop.
	go hdb.threadedSaveLoop()

	// Spawn the scan loop.
	go hdb.threadedScan()
	err = hdb.tg.OnStop(func() error {
		cs.Unsubscribe(hdb)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hdb, nil
}

// hostdbAsyncStartup handles the async portion of New.
func hostdbAsyncStartup(hdb *HostDB, cs smodules.ConsensusSet) error {
	err := cs.ConsensusSetSubscribe(hdb, hdb.lastChange, hdb.tg.StopChan())
	if err != nil && strings.Contains(err.Error(), threadgroup.ErrStopped.Error()) {
		return err
	}
	if errors.Contains(err, smodules.ErrInvalidConsensusChangeID) {
		// Subscribe again using the new ID. This will cause a triggered scan
		// on all of the hosts, but that should be acceptable.
		hdb.mu.Lock()
		hdb.blockHeight = 0
		hdb.lastChange = smodules.ConsensusChangeBeginning
		hdb.mu.Unlock()
		err = cs.ConsensusSetSubscribe(hdb, hdb.lastChange, hdb.tg.StopChan())
	}
	if err != nil && strings.Contains(err.Error(), threadgroup.ErrStopped.Error()) {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

// New returns a new HostDB.
func New(g smodules.Gateway, cs smodules.ConsensusSet, tpool smodules.TransactionPool, db *sql.DB, siamux *siamux.SiaMux, persistDir string) (*HostDB, <-chan error) {
	errChan := make(chan error, 1)

	// Blocking startup.
	hdb, err := hostdbBlockingStartup(g, cs, tpool, persistDir, db, siamux)
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
		err := hostdbAsyncStartup(hdb, cs)
		if err != nil {
			errChan <- err
		}
	}()
	return hdb, errChan
}

// ActiveHosts returns a list of hosts that are currently online, sorted by
// weight. If hostdb is in black or white list mode, then only active hosts from
// the filteredTree will be returned.
func (hdb *HostDB) ActiveHosts() (activeHosts []smodules.HostDBEntry, err error) {
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
		if !entry.ScanHistory[len(entry.ScanHistory) - 1].Success {
			continue
		}
		if !entry.AcceptingContracts {
			continue
		}
		activeHosts = append(activeHosts, entry)
	}
	return activeHosts, err
}

// AllHosts returns all of the hosts known to the hostdb, including the inactive
// ones. AllHosts is not filtered by blacklist or whitelist mode.
func (hdb *HostDB) AllHosts() (allHosts []smodules.HostDBEntry, err error) {
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
func (hdb *HostDB) CheckForIPViolations(hosts []types.SiaPublicKey) ([]types.SiaPublicKey, error) {
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

	var entries []smodules.HostDBEntry
	var badHosts []types.SiaPublicKey

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
	filter := hosttree.NewFilter(hdb.resolver)
	for _, entry := range entries {
		// Check if the host violates the rules.
		if filter.Filtered(entry.NetAddress) {
			badHosts = append(badHosts, entry.PublicKey)
			continue
		}
		// If it didn't then we add it to the filter.
		filter.Add(entry.NetAddress)
	}
	return badHosts, nil
}

// Close closes the hostdb, terminating its scanning threads
func (hdb *HostDB) Close() error {
	return hdb.tg.Stop()
}

// Host returns the HostSettings associated with the specified pubkey. If no
// matching host is found, Host returns false.  For black and white list modes,
// the Filtered field for the HostDBEntry is set to indicate it the host is
// being filtered from the filtered hosttree.
func (hdb *HostDB) Host(spk types.SiaPublicKey) (smodules.HostDBEntry, bool, error) {
	if err := hdb.tg.Add(); err != nil {
		return smodules.HostDBEntry{}, false, errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	hdb.mu.Lock()
	whitelist := hdb.filterMode == smodules.HostDBActiveWhitelist
	filteredHosts := hdb.filteredHosts
	hdb.mu.Unlock()
	host, exists := hdb.staticHostTree.Select(spk)
	if !exists {
		return host, exists, errHostNotFoundInTree
	}
	_, ok := filteredHosts[spk.String()]
	host.Filtered = whitelist != ok
	hdb.mu.RLock()
	updateHostHistoricInteractions(&host, hdb.blockHeight)
	hdb.mu.RUnlock()
	return host, exists, nil
}

// Filter returns the hostdb's filterMode and filteredHosts.
func (hdb *HostDB) Filter() (smodules.FilterMode, map[string]types.SiaPublicKey, []string, error) {
	if err := hdb.tg.Add(); err != nil {
		return smodules.HostDBFilterError, nil, nil, errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	filteredHosts := make(map[string]types.SiaPublicKey)
	for k, v := range hdb.filteredHosts {
		filteredHosts[k] = v
	}
	return hdb.filterMode, filteredHosts, hdb.filteredDomains.managedFilteredDomains(), nil
}

// SetFilterMode sets the hostdb filter mode.
func (hdb *HostDB) SetFilterMode(fm smodules.FilterMode, hosts []types.SiaPublicKey, netAddresses []string) error {
	if err := hdb.tg.Add(); err != nil {
		return errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	// Check for error.
	if fm == smodules.HostDBFilterError {
		return errors.New("Cannot set hostdb filter mode, provided filter mode is an error")
	}
	// Check if disabling.
	if fm == smodules.HostDBDisableFilter {
		// Reset filtered field for hosts.
		for _, pk := range hdb.filteredHosts {
			err := hdb.staticHostTree.SetFiltered(pk, false)
			if err != nil {
				hdb.staticLog.Println("Unable to mark entry as not filtered:", err)
			}
		}
		// Reset filtered fields.
		hdb.filteredTree = hdb.staticHostTree
		hdb.filteredHosts = make(map[string]types.SiaPublicKey)
		hdb.filteredDomains = newFilteredDomains(nil)
		hdb.filterMode = fm
		return nil
	}

	// Check for no hosts submitted with whitelist enabled.
	isWhitelist := fm == smodules.HostDBActiveWhitelist
	if (len(hosts) == 0 && len(netAddresses) == 0) && isWhitelist {
		return errors.New("cannot enable whitelist without hosts")
	}

	// Create filtered HostTree.
	hdb.filteredTree = hosttree.New(hdb.weightFunc, hdb.resolver)
	filteredDomains := newFilteredDomains(netAddresses)

	// Create filteredHosts map.
	filteredHosts := make(map[string]types.SiaPublicKey)
	for _, h := range hosts {
		// Add host to filtered host map.
		if _, ok := filteredHosts[h.String()]; ok {
			continue
		}
		filteredHosts[h.String()] = h

		// Update host in unfiltered hosttree.
		err := hdb.staticHostTree.SetFiltered(h, true)
		if err != nil {
			hdb.staticLog.Println("Unable to mark entry as filtered:", err)
		}
	}

	var allErrs error
	allHosts := hdb.staticHostTree.All()
	for _, host := range allHosts {
		if !filteredDomains.managedIsFiltered(host.NetAddress) {
			continue
		}
		filteredHosts[host.PublicKey.String()] = host.PublicKey

		// Update host in unfiltered hosttree.
		err := hdb.staticHostTree.SetFiltered(host.PublicKey, true)
		if err != nil {
			hdb.staticLog.Println("Unable to mark entry as filtered:", err)
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
			allErrs = errors.Compose(allErrs, err)
		}
	}
	hdb.filteredHosts = filteredHosts
	hdb.filterMode = fm
	hdb.filteredDomains = filteredDomains

	return errors.Compose(allErrs, hdb.saveSync())
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (hdb *HostDB) InitialScanComplete() (complete bool, height types.BlockHeight, err error) {
	if err = hdb.tg.Add(); err != nil {
		return false, types.BlockHeight(0), errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	complete = hdb.initialScanComplete
	height = hdb.blockHeight
	return
}

// IPViolationsCheck returns a boolean indicating if the IP violation check is
// enabled or not.
func (hdb *HostDB) IPViolationsCheck() (bool, error) {
	if err := hdb.tg.Add(); err != nil {
		return false, errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	return !hdb.disableIPViolationCheck, nil
}

// SetAllowance updates the allowance used by the hostdb for weighing hosts by
// updating the host weight function. It will completely rebuild the hosttree so
// it should be used with care.
func (hdb *HostDB) SetAllowance(allowance modules.Allowance) error {
	if err := hdb.tg.Add(); err != nil {
		return errors.AddContext(err, "error adding hostdb threadgroup:")
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
	wf := hdb.managedCalculateHostWeightFn(allowance)
	return hdb.managedSetWeightFunction(wf)
}

// SetIPViolationCheck enables or disables the IP violation check. If disabled,
// CheckForIPViolations won't return bad hosts and RandomHosts will return the
// address blacklist.
func (hdb *HostDB) SetIPViolationCheck(enabled bool) error {
	if err := hdb.tg.Add(); err != nil {
		return errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()

	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.disableIPViolationCheck = !enabled
	return nil
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
		return errors.AddContext(err, "error adding hostdb threadgroup:")
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.updateContracts(contracts)
	return nil
}
