package proto

import (
	"bytes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"net"
	"sort"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	"go.sia.tech/siad/types"
)

// sessionDialTimeout determines how long a Session will try to dial a host
// before aborting.
var sessionDialTimeout = 45 * time.Second

// A Session is an ongoing exchange of RPCs via the renter-host protocol.
type Session struct {
	aead        cipher.AEAD
	challenge   [16]byte
	closeChan   chan struct{}
	conn        net.Conn
	contractID  types.FileContractID
	contractSet *ContractSet
	hdb         hostDB
	height      types.BlockHeight
	host        modules.HostDBEntry
	once        sync.Once
	logger      *persist.Logger
}

// writeRequest sends an encrypted RPC request to the host.
func (s *Session) writeRequest(rpcID types.Specifier, req interface{}) error {
	return modules.WriteRPCRequest(s.conn, s.aead, rpcID, req)
}

// writeResponse writes an encrypted RPC response to the host.
func (s *Session) writeResponse(resp interface{}, err error) error {
	return modules.WriteRPCResponse(s.conn, s.aead, resp, err)
}

// readResponse reads an encrypted RPC response from the host.
func (s *Session) readResponse(resp interface{}, maxLen uint64) error {
	return modules.ReadRPCResponse(s.conn, s.aead, resp, maxLen)
}

// call is a helper method that calls writeRequest followed by readResponse.
func (s *Session) call(rpcID types.Specifier, req, resp interface{}, maxLen uint64) error {
	if err := s.writeRequest(rpcID, req); err != nil {
		return err
	}
	return s.readResponse(resp, maxLen)
}

// Lock calls the Lock RPC, locking the supplied contract and returning its
// most recent revision.
func (s *Session) Lock(id types.FileContractID, secretKey crypto.SecretKey) (types.FileContractRevision, []types.TransactionSignature, error) {
	sig := crypto.SignHash(crypto.HashAll(modules.RPCChallengePrefix, s.challenge), secretKey)
	req := modules.LoopLockRequest{
		ContractID: id,
		Signature:  sig[:],
		Timeout:    defaultContractLockTimeout,
	}

	timeoutDur := time.Duration(defaultContractLockTimeout) * time.Millisecond
	extendDeadline(s.conn, modules.NegotiateSettingsTime + timeoutDur)
	var resp modules.LoopLockResponse
	if err := s.call(modules.RPCLoopLock, req, &resp, modules.RPCMinLen); err != nil {
		return types.FileContractRevision{}, nil, errors.AddContext(err, "lock request on host session has failed")
	}
	// Unconditionally update the challenge.
	s.challenge = resp.NewChallenge

	if !resp.Acquired {
		return resp.Revision, resp.Signatures, errors.New("contract is locked by another party")
	}
	// Set the new Session contract.
	s.contractID = id
	// Verify the public keys in the claimed revision.
	expectedUnlockConditions := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(secretKey.PublicKey()),
			s.host.PublicKey,
		},
		SignaturesRequired: 2,
	}
	if resp.Revision.UnlockConditions.UnlockHash() != expectedUnlockConditions.UnlockHash() {
		return resp.Revision, resp.Signatures, errors.New("host's claimed revision has wrong unlock conditions")
	}
	// Verify the claimed signatures.
	if err := modules.VerifyFileContractRevisionTransactionSignatures(resp.Revision, resp.Signatures, s.height); err != nil {
		return resp.Revision, resp.Signatures, errors.AddContext(err, "unable to verify signatures on contract revision")
	}
	return resp.Revision, resp.Signatures, nil
}

// Unlock calls the Unlock RPC, unlocking the currently-locked contract.
func (s *Session) Unlock() error {
	if s.contractID == (types.FileContractID{}) {
		return errors.New("no contract locked")
	}
	extendDeadline(s.conn, modules.NegotiateSettingsTime)
	return s.writeRequest(modules.RPCLoopUnlock, nil)
}

// HostSettings returns the currently active host settings of the session.
func (s *Session) HostSettings() modules.HostExternalSettings {
	return s.host.HostExternalSettings
}

// Settings calls the Settings RPC, returning the host's reported settings.
func (s *Session) Settings() (modules.HostExternalSettings, error) {
	extendDeadline(s.conn, modules.NegotiateSettingsTime)
	var resp modules.LoopSettingsResponse
	if err := s.call(modules.RPCLoopSettings, nil, &resp, modules.RPCMinLen); err != nil {
		return modules.HostExternalSettings{}, err
	}
	var hes modules.HostExternalSettings
	if err := json.Unmarshal(resp.Settings, &hes); err != nil {
		return modules.HostExternalSettings{}, err
	}
	s.host.HostExternalSettings = hes
	return s.host.HostExternalSettings, nil
}

// shutdown terminates the revision loop and signals the goroutine spawned in
// NewSession to return.
func (s *Session) shutdown() {
	extendDeadline(s.conn, modules.NegotiateSettingsTime)
	// Don't care about this error.
	_ = s.writeRequest(modules.RPCLoopExit, nil)
	close(s.closeChan)
}

// Close cleanly terminates the protocol session with the host and closes the
// connection.
func (s *Session) Close() error {
	// Using once ensures that Close is idempotent.
	s.once.Do(s.shutdown)
	return s.conn.Close()
}

// NewSession initiates the RPC loop with a host and returns a Session.
func (cs *ContractSet) NewSession(host modules.HostDBEntry, id types.FileContractID, currentHeight types.BlockHeight, hdb hostDB, logger *persist.Logger, cancel <-chan struct{}) (_ *Session, err error) {
	sc, ok := cs.Acquire(id)
	if !ok {
		return nil, errors.New("could not locate contract to create session")
	}
	defer cs.Return(sc)
	s, err := cs.managedNewSession(host, currentHeight, hdb, logger, cancel)
	if err != nil {
		return nil, errors.AddContext(err, "unable to create a new session with the host")
	}
	// Lock the contract.
	rev, sigs, err := s.Lock(id, sc.header.SecretKey)
	if err != nil {
		s.Close()
		return nil, errors.AddContext(err, "unable to get a session lock")
	}

	// Resynchronize.
	err = sc.managedSyncRevision(rev, sigs)
	if err != nil {
		logger.Printf("%v revision resync failed, err: %v\n", host.PublicKey.String(), err)
		err = errors.Compose(err, s.Close())
		return nil, errors.AddContext(err, "unable to sync revisions when creating session")
	}
	logger.Printf("%v revision resync attempted, succeeded: %v\n", host.PublicKey.String(), sc.LastRevision().NewRevisionNumber == rev.NewRevisionNumber)

	return s, nil
}

// NewRawSession creates a new session unassociated with any contract.
func (cs *ContractSet) NewRawSession(host modules.HostDBEntry, currentHeight types.BlockHeight, hdb hostDB, logger *persist.Logger, cancel <-chan struct{}) (_ *Session, err error) {
	return cs.managedNewSession(host, currentHeight, hdb, logger, cancel)
}

// managedNewSession initiates the RPC loop with a host and returns a Session.
func (cs *ContractSet) managedNewSession(host modules.HostDBEntry, currentHeight types.BlockHeight, hdb hostDB, logger *persist.Logger, cancel <-chan struct{}) (_ *Session, err error) {
	// Increase Successful/Failed interactions accordingly.
	defer func() {
		if err != nil {
			hdb.IncrementFailedInteractions(host.PublicKey)
			err = errors.Extend(err, modules.ErrHostFault)
		} else {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	conn, err := (&net.Dialer{
		Cancel:  cancel,
		Timeout: sessionDialTimeout,
	}).Dial("tcp", string(host.NetAddress))
	if err != nil {
		return nil, errors.AddContext(err, "unsuccessful dial when creating a new session")
	}

	closeChan := make(chan struct{})
	go func() {
		select {
		case <-cancel:
			conn.Close()
		case <-closeChan:
			// We don't close the connection here because we want session.Close
			// to be able to return the Close error directly.
		}
	}()

	// Perform the handshake and create the session object.
	aead, challenge, err := performSessionHandshake(conn, host.PublicKey)
	if err != nil {
		conn.Close()
		close(closeChan)
		return nil, errors.AddContext(err, "session handshake failed")
	}
	s := &Session{
		aead:        aead,
		challenge:   challenge.Challenge,
		closeChan:   closeChan,
		conn:        conn,
		contractSet: cs,
		hdb:         hdb,
		height:      currentHeight,
		host:        host,
		logger:      logger
	}

	return s, nil
}
