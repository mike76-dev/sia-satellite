package hosttree

import (
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

var (
	// ErrHostExists is returned if an Insert is called with a public key that
	// already exists in the tree.
	ErrHostExists = errors.New("host already exists in the tree")

	// ErrNoSuchHost is returned if Remove is called with a public key that does
	// not exist in the tree.
	ErrNoSuchHost = errors.New("no host with specified public key")
)

type (
	// ScoreFunc is a function used to score a given HostDBEntry in the tree.
	ScoreFunc func(modules.HostDBEntry) ScoreBreakdown

	// HostTree is used to store and select host database entries. Each HostTree
	// is initialized with a scoring func that is able to assign a score to
	// each entry. The entries can then be selected at random, scored by the
	// score func.
	HostTree struct {
		root *node

		// hosts is a map of public keys to nodes.
		hosts map[string]*node

		// scoreFn calculates the score of a hostEntry.
		scoreFn ScoreFunc

		log *persist.Logger
		mu  sync.Mutex
	}

	// hostEntry is an entry in the host tree.
	hostEntry struct {
		modules.HostDBEntry
		score types.Currency
	}

	// node is a node in the tree.
	node struct {
		parent *node
		left   *node
		right  *node

		count int  // Cumulative count of this node and all children.
		taken bool // `taken` indicates whether there is an active host at this node or not.

		score types.Currency
		entry *hostEntry

		log *persist.Logger
	}
)

// createNode creates a new node using the provided `parent` and `entry`.
func createNode(parent *node, entry *hostEntry, log *persist.Logger) *node {
	return &node{
		parent: parent,
		score:  entry.score,
		count:  1,

		taken: true,
		entry: entry,

		log: log,
	}
}

// New creates a new HostTree given a score function.
func New(sf ScoreFunc, log *persist.Logger) *HostTree {
	return &HostTree{
		hosts: make(map[string]*node),
		root: &node{
			count: 1,
		},
		scoreFn: sf,
		log:     log,
	}
}

// recursiveInsert inserts an entry into the appropriate place in the tree. The
// running time of recursiveInsert is log(n) in the maximum number of elements
// that have ever been in the tree.
func (n *node) recursiveInsert(entry *hostEntry) (nodesAdded int, newnode *node) {
	// If there is no parent and no children, and the node is not taken, assign
	// this entry to this node.
	if n.parent == nil && n.left == nil && n.right == nil && !n.taken {
		n.entry = entry
		n.taken = true
		n.score = entry.score
		newnode = n
		return
	}

	n.score = n.score.Add(entry.score)

	// If the current node is empty, add the entry but don't increase the
	// count.
	if !n.taken {
		n.taken = true
		n.entry = entry
		newnode = n
		return
	}

	// Insert the element into the least populated side.
	if n.left == nil {
		n.left = createNode(n, entry, n.log)
		nodesAdded = 1
		newnode = n.left
	} else if n.right == nil {
		n.right = createNode(n, entry, n.log)
		nodesAdded = 1
		newnode = n.right
	} else if n.left.count <= n.right.count {
		nodesAdded, newnode = n.left.recursiveInsert(entry)
	} else {
		nodesAdded, newnode = n.right.recursiveInsert(entry)
	}

	n.count += nodesAdded
	return
}

// nodeAtScore grabs an element in the tree that appears at the given score.
// Though the tree has an arbitrary sorting, a sufficiently random score will
// pull a random element. The tree is searched through in a post-ordered way.
func (n *node) nodeAtScore(score types.Currency) *node {
	// Sanity check - score must be less than the total score of the tree.
	if score.Cmp(n.score) > 0 {
		n.log.Println("CRITICAL: node score corruption")
		return nil
	}

	// Check if the left or right child should be returned.
	if n.left != nil {
		if score.Cmp(n.left.score) < 0 {
			return n.left.nodeAtScore(score)
		}
		score = score.Sub(n.left.score) // Search from the 0th index of the right side.
	}
	if n.right != nil && score.Cmp(n.right.score) < 0 {
		return n.right.nodeAtScore(score)
	}

	if !n.taken {
		n.log.Println("CRITICAL: node tree structure corruption")
		return nil
	}

	// Return the root entry.
	return n
}

// remove takes a node and removes it from the tree by climbing through the
// list of parents. remove does not delete nodes.
func (n *node) remove() {
	n.score = n.score.Sub(n.entry.score)
	n.taken = false
	current := n.parent
	for current != nil {
		current.score = current.score.Sub(n.entry.score)
		current = current.parent
	}
}

// Host returns the address of the HostEntry.
func (he *hostEntry) Host() string {
	return modules.NetAddress(he.Settings.NetAddress).Host()
}

// All returns all of the hosts in the host tree, sorted by score.
func (ht *HostTree) All() []modules.HostDBEntry {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	return ht.all()
}

// Insert inserts the entry provided to `entry` into the host tree. Insert will
// return an error if the input host already exists.
func (ht *HostTree) Insert(hdbe modules.HostDBEntry) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	return ht.insert(hdbe)
}

// Remove removes the host with the public key provided by `pk`.
func (ht *HostTree) Remove(pk types.PublicKey) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	node, exists := ht.hosts[pk.String()]
	if !exists {
		return ErrNoSuchHost
	}
	node.remove()
	delete(ht.hosts, pk.String())

	return nil
}

// Modify updates a host entry at the given public key, replacing the old entry
// with the entry provided by `newEntry`.
func (ht *HostTree) Modify(hdbe modules.HostDBEntry) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	node, exists := ht.hosts[hdbe.PublicKey.String()]
	if !exists {
		return ErrNoSuchHost
	}

	node.remove()

	entry := &hostEntry{
		HostDBEntry: hdbe,
		score:       ht.scoreFn(hdbe).Score(),
	}

	_, node = ht.root.recursiveInsert(entry)

	ht.hosts[entry.PublicKey.String()] = node
	return nil
}

// SetFiltered updates a host entry filtered field.
func (ht *HostTree) SetFiltered(pubKey types.PublicKey, filtered bool) error {
	entry, ok := ht.Select(pubKey)
	if !ok {
		return ErrNoSuchHost
	}
	entry.Filtered = filtered
	return ht.Modify(entry)
}

// SetScoreFunction resets the HostTree and assigns it a new score
// function. This resets the tree and reinserts all the hosts.
func (ht *HostTree) SetScoreFunction(sf ScoreFunc) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	// Get all the hosts.
	allHosts := ht.all()

	// Reset the tree.
	ht.hosts = make(map[string]*node)
	ht.root = &node{
		count: 1,
	}

	// Assign the new score function.
	ht.scoreFn = sf

	// Reinsert all the hosts. To prevent the host tree from having a
	// catastrophic failure in the event of an error early on, we tally up all
	// of the insertion errors and return them all at the end.
	var insertErrs error
	for _, hdbe := range allHosts {
		if err := ht.insert(hdbe); err != nil {
			insertErrs = modules.ComposeErrors(err, insertErrs)
		}
	}
	return insertErrs
}

// Select returns the host with the provided public key, should the host exist.
func (ht *HostTree) Select(pk types.PublicKey) (modules.HostDBEntry, bool) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	node, exists := ht.hosts[pk.String()]
	if !exists {
		return modules.HostDBEntry{}, false
	}
	return node.entry.HostDBEntry, true
}

// SelectRandom grabs a random n hosts from the tree. There will be no repeats,
// but the length of the slice returned may be less than n, and may even be
// zero. The hosts that are returned first have the higher priority.
//
// Hosts passed to 'blacklist' will not be considered; pass `nil` if no
// blacklist is desired. 'addressBlacklist' is similar to 'blacklist' but
// instead of not considering the hosts in the list, hosts that use the same IP
// subnet as those hosts will be ignored. In most cases those blacklists contain
// the same elements but sometimes it is useful to block a host without blocking
// its IP range.
//
// Hosts with a score of 1 will be ignored. 1 is the lowest score possible, at
// which point it's impossible to distinguish between hosts. Any sane scoring
// system should always have scores greater than 1 unless the host is
// intentionally being given a low score to indicate that the host should not be
// used.
func (ht *HostTree) SelectRandom(n int, blacklist, addressBlacklist []types.PublicKey) []modules.HostDBEntry {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	var removedEntries []*hostEntry

	// Create a filter.
	filter := NewFilter()

	// Add the hosts from the addressBlacklist to the filter.
	for _, pubkey := range addressBlacklist {
		node, exists := ht.hosts[pubkey.String()]
		if !exists {
			continue
		}
		// Add the node to the addressFilter.
		filter.Add(modules.NetAddress(node.entry.Settings.NetAddress))
	}

	// Remove hosts we want to blacklist from the tree but remember them to make
	// sure we can insert them later.
	for _, pubkey := range blacklist {
		node, exists := ht.hosts[pubkey.String()]
		if !exists {
			continue
		}

		// Remove the host from the tree.
		node.remove()
		delete(ht.hosts, pubkey.String())

		// Remember the host to insert it again later.
		removedEntries = append(removedEntries, node.entry)
	}

	var hosts []modules.HostDBEntry

	for len(hosts) < n && len(ht.hosts) > 0 {
		randScoreBig := frand.BigIntn(ht.root.score.Big())
		b := randScoreBig.Bytes()
		buf := make([]byte, 16)
		copy(buf[16 - len(b):], b[:])
		randScore := types.NewCurrency(binary.BigEndian.Uint64(buf[8:]), binary.BigEndian.Uint64(buf[:8]))
		node := ht.root.nodeAtScore(randScore)
		scoreOne := types.NewCurrency64(1)

		if node.entry.Settings.AcceptingContracts &&
			len(node.entry.ScanHistory) > 0 &&
			node.entry.ScanHistory[len(node.entry.ScanHistory) - 1].Success &&
			!filter.Filtered(modules.NetAddress(node.entry.Settings.NetAddress)) &&
			node.entry.score.Cmp(scoreOne) > 0 {
			// The host must be online and accepting contracts to be returned
			// by the random function. It also has to pass the addressFilter
			// check.
			hosts = append(hosts, node.entry.HostDBEntry)

			// If the host passed the filter, we add it to the filter.
			filter.Add(modules.NetAddress(node.entry.Settings.NetAddress))
		}

		removedEntries = append(removedEntries, node.entry)
		node.remove()
		delete(ht.hosts, node.entry.PublicKey.String())
	}

	for _, entry := range removedEntries {
		_, node := ht.root.recursiveInsert(entry)
		ht.hosts[entry.PublicKey.String()] = node
	}

	return hosts
}

// all returns all of the hosts in the host tree, sorted by score.
func (ht *HostTree) all() []modules.HostDBEntry {
	he := make([]hostEntry, 0, len(ht.hosts))
	for _, node := range ht.hosts {
		he = append(he, *node.entry)
	}
	sort.Sort(byScore(he))

	entries := make([]modules.HostDBEntry, 0, len(he))
	for _, entry := range he {
		entries = append(entries, entry.HostDBEntry)
	}
	return entries
}

// insert inserts the entry provided to `entry` into the host tree. Insert will
// return an error if the input host already exists.
func (ht *HostTree) insert(hdbe modules.HostDBEntry) error {
	entry := &hostEntry{
		HostDBEntry: hdbe,
		score:       ht.scoreFn(hdbe).Score(),
	}

	if _, exists := ht.hosts[entry.PublicKey.String()]; exists {
		return ErrHostExists
	}

	_, node := ht.root.recursiveInsert(entry)

	ht.hosts[entry.PublicKey.String()] = node
	return nil
}
