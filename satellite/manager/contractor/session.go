package contractor

import (
	"sync"

	"github.com/mike76-dev/sia-satellite/satellite/manager/proto"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// ErrContractRenewing is returned by operations that can't be completed due to
// the contract being renewed.
var ErrContractRenewing = errors.New("currently renewing that contract")

var errInvalidSession = errors.New("session has been invalidated because its contract is being renewed")

// A Session modifies a Contract by communicating with a host. It uses the
// renter-host protocol to send modification requests to the host. Among other
// things, Sessions are the means by which the renter transfers file data to
// and from hosts.
type Session interface {
	// Address returns the address of the host.
	Address() modules.NetAddress

	// Close terminates the connection to the host.
	Close() error

	// ContractID returns the FileContractID of the contract.
	ContractID() types.FileContractID

	// EndHeight returns the height at which the contract ends.
	EndHeight() types.BlockHeight

	// HostSettings will return the currently active host settings for a
	// session, which allows the workers to check for price gouging and
	// determine whether or not an operation should continue.
	HostSettings() modules.HostExternalSettings

	// Settings calls the Session RPC and updates the active host settings.
	Settings() (modules.HostExternalSettings, error)
}

// A hostSession modifies a Contract via the renter-host RPC loop. It
// implements the Session interface. hostSessions are safe for use by multiple
// goroutines.
type hostSession struct {
	clients    int // Safe to Close when 0.
	contractor *Contractor
	session    *proto.Session
	endHeight  types.BlockHeight
	id         types.FileContractID
	invalid    bool // True if invalidate has been called.
	netAddress modules.NetAddress

	mu sync.Mutex
}

// invalidate sets the invalid flag and closes the underlying proto.Session.
// Once invalidate returns, the hostSession is guaranteed to not further revise
// its contract. This is used during contract renewal to prevent an Session
// from revising a contract mid-renewal.
func (hs *hostSession) invalidate() {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if hs.invalid {
		return
	}
	hs.session.Close()
	hs.contractor.mu.Lock()
	delete(hs.contractor.sessions, hs.id)
	hs.contractor.mu.Unlock()
	hs.invalid = true
}

// Address returns the NetAddress of the host.
func (hs *hostSession) Address() modules.NetAddress { return hs.netAddress }

// Close cleanly terminates the revision loop with the host and closes the
// connection.
func (hs *hostSession) Close() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.clients--
	// Close is a no-op if invalidate has been called, or if there are other
	// clients still using the hostSession.
	if hs.invalid || hs.clients > 0 {
		return nil
	}
	hs.invalid = true
	hs.contractor.mu.Lock()
	delete(hs.contractor.sessions, hs.id)
	hs.contractor.mu.Unlock()

	return hs.session.Close()
}

// ContractID returns the ID of the contract being revised.
func (hs *hostSession) ContractID() types.FileContractID { return hs.id }

// EndHeight returns the height at which the host is no longer obligated to
// store the file.
func (hs *hostSession) EndHeight() types.BlockHeight { return hs.endHeight }

// HostSettings returns the currently active host settings for the session.
func (hs *hostSession) HostSettings() modules.HostExternalSettings {
	return hs.session.HostSettings()
}

// Settings calls the Session RPC and updates the active host settings.
func (hs *hostSession) Settings() (modules.HostExternalSettings, error) {
	return hs.session.Settings()
}

// Session returns a Session object that can be used to upload, modify, and
// delete sectors on a host.
func (c *Contractor) Session(rpk, hpk types.SiaPublicKey, cancel <-chan struct{}) (_ Session, err error) {
	c.mu.RLock()
	id, gotID := c.pubKeysToContractID[rpk.String() + hpk.String()]
	cachedSession, haveSession := c.sessions[id]
	height := c.blockHeight
	renewing := c.renewing[id]
	c.mu.RUnlock()
	if !gotID {
		return nil, errors.New("failed to get filecontract id from key")
	}
	if renewing {
		// Cannot use the session if the contract is being renewed.
		return nil, ErrContractRenewing
	} else if haveSession {
		// This session already exists. Mark that there are now two routines
		// using the session, and then return the session that already exists.
		cachedSession.mu.Lock()
		cachedSession.clients++
		cachedSession.mu.Unlock()
		return cachedSession, nil
	}

	// Check that the contract and host are both available, and run some brief
	// sanity checks to see that the host is not swindling us.
	contract, haveContract := c.staticContracts.View(id)
	if !haveContract {
		return nil, errors.New("contract not found in the renter contract set")
	}
	host, haveHost, err := c.hdb.Host(contract.HostPublicKey)
	if err != nil {
		return nil, errors.AddContext(err, "error getting host from hostdb:")
	} else if height > contract.EndHeight {
		return nil, errContractEnded
	} else if !haveHost {
		return nil, errHostNotFound
	} else if host.Filtered {
		return nil, errHostBlocked
	} else if host.StoragePrice.Cmp(maxStoragePrice) > 0 {
		return nil, errTooExpensive
	} else if host.UploadBandwidthPrice.Cmp(maxUploadPrice) > 0 {
		return nil, errTooExpensive
	}

	// Create the session.
	s, err := c.staticContracts.NewSession(host, rpk, id, height, c.hdb, c.log, cancel)
	if modules.IsContractNotRecognizedErr(err) {
		err = errors.Compose(err, c.MarkContractBad(id))
	}
	if err != nil {
		return nil, err
	}

	// cache session
	hs := &hostSession{
		clients:    1,
		contractor: c,
		session:    s,
		endHeight:  contract.EndHeight,
		id:         id,
		netAddress: host.NetAddress,
	}
	c.mu.Lock()
	c.sessions[contract.ID] = hs
	c.mu.Unlock()

	return hs, nil
}
