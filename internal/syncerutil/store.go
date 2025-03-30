package syncerutil

import (
	"encoding/json"
	"net"
	"os"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/coreutils/syncer"
)

type peerBan struct {
	Expiry time.Time `json:"expiry"`
	Reason string    `json:"reason"`
}

// EphemeralPeerStore implements PeerStore with an in-memory map.
type EphemeralPeerStore struct {
	peers map[string]syncer.PeerInfo
	bans  map[string]peerBan
	mu    sync.Mutex
}

func (eps *EphemeralPeerStore) banned(peer string) bool {
	host, _, err := net.SplitHostPort(peer)
	if err != nil {
		return false // shouldn't happen
	}
	for _, s := range []string{
		peer,                       //  1.2.3.4:5678
		syncer.Subnet(host, "/32"), //  1.2.3.4:*
		syncer.Subnet(host, "/24"), //  1.2.3.*
		syncer.Subnet(host, "/16"), //  1.2.*
		syncer.Subnet(host, "/8"),  //  1.*
	} {
		if b, ok := eps.bans[s]; ok {
			if time.Until(b.Expiry) <= 0 {
				delete(eps.bans, s)
			} else {
				return true
			}
		}
	}
	return false
}

// AddPeer implements PeerStore.
func (eps *EphemeralPeerStore) AddPeer(addr string) error {
	eps.mu.Lock()
	defer eps.mu.Unlock()
	if _, ok := eps.peers[addr]; !ok {
		eps.peers[addr] = syncer.PeerInfo{
			Address:   addr,
			FirstSeen: time.Now(),
		}
	}
	return nil
}

// Peers implements PeerStore.
func (eps *EphemeralPeerStore) Peers() ([]syncer.PeerInfo, error) {
	eps.mu.Lock()
	defer eps.mu.Unlock()
	var peers []syncer.PeerInfo
	for addr, p := range eps.peers {
		if !eps.banned(addr) {
			peers = append(peers, p)
		}
	}
	return peers, nil
}

// UpdatePeerInfo implements PeerStore.
func (eps *EphemeralPeerStore) UpdatePeerInfo(addr string, fn func(*syncer.PeerInfo)) error {
	eps.mu.Lock()
	defer eps.mu.Unlock()
	info, ok := eps.peers[addr]
	if !ok {
		return syncer.ErrPeerNotFound
	}
	fn(&info)
	eps.peers[addr] = info
	return nil
}

// PeerInfo implements PeerStore.
func (eps *EphemeralPeerStore) PeerInfo(peer string) (syncer.PeerInfo, error) {
	eps.mu.Lock()
	defer eps.mu.Unlock()
	info, ok := eps.peers[peer]
	if !ok {
		return syncer.PeerInfo{}, syncer.ErrPeerNotFound
	}
	return info, nil
}

// Ban implements PeerStore.
func (eps *EphemeralPeerStore) Ban(addr string, duration time.Duration, reason string) error {
	eps.mu.Lock()
	defer eps.mu.Unlock()
	// Canonicalize.
	if _, ipnet, err := net.ParseCIDR(addr); err == nil {
		addr = ipnet.String()
	}
	eps.bans[addr] = peerBan{Expiry: time.Now().Add(duration), Reason: reason}
	return nil
}

// Banned implements PeerStore.
func (eps *EphemeralPeerStore) Banned(addr string) (bool, error) {
	eps.mu.Lock()
	defer eps.mu.Unlock()
	return eps.banned(addr), nil
}

// NewEphemeralPeerStore initializes an EphemeralPeerStore.
func NewEphemeralPeerStore() *EphemeralPeerStore {
	return &EphemeralPeerStore{
		peers: make(map[string]syncer.PeerInfo),
		bans:  make(map[string]peerBan),
	}
}

type jsonPersist struct {
	Peers map[string]syncer.PeerInfo `json:"peers"`
	Bans  map[string]peerBan         `json:"bans"`
}

// JSONPeerStore implements PeerStore with a JSON file on disk.
type JSONPeerStore struct {
	*EphemeralPeerStore
	path     string
	lastSave time.Time
}

func (jps *JSONPeerStore) load() error {
	f, err := os.Open(jps.path)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	defer f.Close()
	var p jsonPersist
	if err := json.NewDecoder(f).Decode(&p); err != nil {
		return err
	}
	jps.EphemeralPeerStore.peers = p.Peers
	jps.EphemeralPeerStore.bans = p.Bans
	return nil
}

func (jps *JSONPeerStore) save() error {
	jps.EphemeralPeerStore.mu.Lock()
	defer jps.EphemeralPeerStore.mu.Unlock()
	if time.Since(jps.lastSave) < 5*time.Second {
		return nil
	}
	defer func() { jps.lastSave = time.Now() }()
	// clear out expired bans
	for peer, b := range jps.EphemeralPeerStore.bans {
		if time.Until(b.Expiry) <= 0 {
			delete(jps.EphemeralPeerStore.bans, peer)
		}
	}
	p := jsonPersist{
		Peers: jps.EphemeralPeerStore.peers,
		Bans:  jps.EphemeralPeerStore.bans,
	}
	js, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(jps.path+"_tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(js); err != nil {
		return err
	} else if err := f.Sync(); err != nil {
		return err
	} else if err := f.Close(); err != nil {
		return err
	} else if err := os.Rename(jps.path+"_tmp", jps.path); err != nil {
		return err
	}
	return nil
}

// AddPeer implements PeerStore.
func (jps *JSONPeerStore) AddPeer(addr string) error {
	return utils.ComposeErrors(jps.EphemeralPeerStore.AddPeer(addr), jps.save())
}

// UpdatePeerInfo implements PeerStore.
func (jps *JSONPeerStore) UpdatePeerInfo(addr string, fn func(*syncer.PeerInfo)) error {
	return utils.ComposeErrors(jps.EphemeralPeerStore.UpdatePeerInfo(addr, fn), jps.save())
}

// Ban implements PeerStore.
func (jps *JSONPeerStore) Ban(addr string, duration time.Duration, reason string) error {
	return utils.ComposeErrors(jps.EphemeralPeerStore.Ban(addr, duration, reason), jps.save())
}

// NewJSONPeerStore returns a JSONPeerStore backed by the specified file.
func NewJSONPeerStore(path string) (*JSONPeerStore, error) {
	jps := &JSONPeerStore{
		EphemeralPeerStore: NewEphemeralPeerStore(),
		path:               path,
	}
	return jps, jps.load()
}
