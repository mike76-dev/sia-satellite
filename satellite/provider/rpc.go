package provider

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/fastrand"

	rhpv2 "go.sia.tech/core/rhp/v2"
	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
)

const bytesInTerabyte = 1024 * 1024 * 1024 * 1024

// An rpcSession contains the state of an RPC session with a renter.
type rpcSession struct {
	conn      net.Conn
	aead      cipher.AEAD
	challenge [16]byte
}

// readRequest reads an encrypted RPC request from the renter.
func (s *rpcSession) readRequest(req requestBody, maxLen uint64) (core.Hash256, error) {
	d := core.NewDecoder(io.LimitedReader{R: s.conn, N: int64(maxLen)})
	ciphertext := d.ReadBytes()
	if err := d.Err(); err != nil {
		return core.Hash256{}, err
	}
	plaintext, err := crypto.DecryptWithNonce(ciphertext, s.aead)
	if err != nil {
		return core.Hash256{}, err
	}
	b := core.NewBufDecoder(plaintext)
	req.DecodeFrom(b)

	// Calculate the hash.
	h := core.NewHasher()
	req.EncodeTo(h.E)

	return h.Sum(), err
}

// writeMessage sends an encrypted message to the renter.
func (s *rpcSession) writeMessage(message requestBody) error {
	nonce := make([]byte, 32)[:s.aead.NonceSize()]
	fastrand.Read(nonce)

	var buf bytes.Buffer
	buf.Grow(4096)
	e := core.NewEncoder(&buf)
	e.WritePrefix(0) // Placeholder.
	e.Write(nonce)
	message.EncodeTo(e)
	e.Flush()

	// Overwrite message length.
	msgSize := buf.Len() + s.aead.Overhead()
	if msgSize < 4096 {
		msgSize = 4096
	}
	buf.Grow(s.aead.Overhead())
	msg := buf.Bytes()[:msgSize]
	binary.LittleEndian.PutUint64(msg[:8], uint64(msgSize - 8))

	// Encrypt the response in-place.
	msgNonce := msg[8:][:len(nonce)]
	payload := msg[8 + len(nonce) : msgSize - s.aead.Overhead()]
	s.aead.Seal(payload[:0], msgNonce, payload, nil)

	_, err := s.conn.Write(msg)

	return err
}

// writeResponse sends an encrypted RPC responce to the renter.
func (s *rpcSession) writeResponse(resp requestBody) error {
	return s.writeMessage(&rpcResponse{nil, resp})
}

// writeError sends an error message to the renter.
func (s *rpcSession) writeError(err error) error {
	var re *rpcError
	if err != nil {
		re = &rpcError{Description: err.Error()}
	}
	return s.writeMessage(&rpcResponse{re, nil})
}

// managedRequestContracts returns a slice containing the list of the
// renter's active contracts.
func (p *Provider) managedRequestContracts(s *rpcSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.conn.SetDeadline(time.Now().Add(requestContractsTime))

	// Read the request.
	var rr requestRequest
	hash, err := s.readRequest(&rr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.writeError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), rr.PubKey, crypto.Signature(rr.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.writeError(err)
		return err
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(crypto.PublicKey(rr.PubKey))
	renter, err := p.satellite.GetRenter(rpk)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.writeError(err)
		return err
	}

	seed, err := p.satellite.WalletSeed()
	if err != nil {
		err = fmt.Errorf("couldn't retrieve contracts: %v", err)
		s.writeError(err)
		return err
	}
	rs := modules.DeriveRenterSeed(seed, renter.Email)
	defer fastrand.Read(rs[:])

	// Get the contracts.
	contracts := p.satellite.ContractsByRenter(rs)
	ecs := extendedContractSet{
		contracts: make([]extendedContract, 0, len(contracts)),
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.contracts = append(ecs.contracts, extendedContract{
			contract:            cr,
			startHeight:         uint64(contract.StartHeight),
			totalCost:           modules.ConvertCurrency(contract.TotalCost),
			uploadSpending:      modules.ConvertCurrency(contract.UploadSpending),
			downloadSpending:    modules.ConvertCurrency(contract.DownloadSpending),
			fundAccountSpending: modules.ConvertCurrency(contract.FundAccountSpending),
			renewedFrom:         core.FileContractID(p.satellite.RenewedFrom(contract.ID)),
		})
	}

	return s.writeResponse(&ecs)
}

// managedFormContracts forms the specified number of contracts with the hosts
// on behalf of the renter.
func (p *Provider) managedFormContracts(s *rpcSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.conn.SetDeadline(time.Now().Add(formContractsTime))

	// Read the request.
	var fr formRequest
	hash, err := s.readRequest(&fr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.writeError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), fr.PubKey, crypto.Signature(fr.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.writeError(err)
		return err
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(crypto.PublicKey(fr.PubKey))
	exists, err := p.satellite.UserExists(rpk)
	if !exists || err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.writeError(err)
		return err
	}

	// Sanity checks.
	if fr.Hosts == 0 {
		err := errors.New("can't form contracts with zero hosts")
		s.writeError(err)
		return err
	}
	if fr.Period == 0 {
		err := errors.New("can't form contracts with zero period")
		s.writeError(err)
		return err
	}
	if fr.RenewWindow == 0 {
		err := errors.New("can't form contracts with zero renew window")
		s.writeError(err)
		return err
	}
	if fr.Storage == 0 {
		err := errors.New("can't form contracts with zero expected storage")
		s.writeError(err)
		return err
	}
	if fr.MinShards == 0 || fr.TotalShards == 0 {
		err := errors.New("can't form contracts with such redundancy params")
		s.writeError(err)
		return err
	}

	ecs := extendedContractSet{
		contracts: make([]extendedContract, 0, fr.Hosts),
	}

	// Create an allowance.
	a := modules.Allowance{
		Hosts:       fr.Hosts,
		Period:      types.BlockHeight(fr.Period),
		RenewWindow: types.BlockHeight(fr.RenewWindow),

		ExpectedStorage:    fr.Storage,
		ExpectedUpload:     fr.Upload,
		ExpectedDownload:   fr.Download,
		MinShards:          fr.MinShards,
		TotalShards:        fr.TotalShards,

		MaxRPCPrice:               types.NewCurrency(fr.MaxRPCPrice.Big()),
		MaxContractPrice:          types.NewCurrency(fr.MaxContractPrice.Big()),
		MaxDownloadBandwidthPrice: types.NewCurrency(fr.MaxDownloadPrice.Big()).Div64(bytesInTerabyte),
		MaxSectorAccessPrice:      types.NewCurrency(fr.MaxSectorAccessPrice.Big()),
		MaxStoragePrice:           types.NewCurrency(fr.MaxStoragePrice.Big()).Div64(bytesInTerabyte),
		MaxUploadBandwidthPrice:   types.NewCurrency(fr.MaxUploadPrice.Big()).Div64(bytesInTerabyte),
		MinMaxCollateral:          types.NewCurrency(fr.MinMaxCollateral.Big()),
		BlockHeightLeeway:         types.BlockHeight(fr.BlockHeightLeeway),
	}

	// Form the contracts.
	contracts, err := p.satellite.FormContracts(rpk, a)
	if err != nil {
		err = fmt.Errorf("could not form contracts: %v", err)
		s.writeError(err)
		return err
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.contracts = append(ecs.contracts, extendedContract{
			contract:    cr,
			startHeight: uint64(contract.StartHeight),
			totalCost:   modules.ConvertCurrency(contract.TotalCost),
		})
	}

	return s.writeResponse(&ecs)
}

// managedRenewContracts tries to renew the given set of contracts.
func (p *Provider) managedRenewContracts(s *rpcSession) error {
	// Extend the deadline to meet the renewal of multiple contracts.
	s.conn.SetDeadline(time.Now().Add(renewContractsTime))

	// Read the request.
	var rr renewRequest
	hash, err := s.readRequest(&rr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.writeError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), rr.PubKey, crypto.Signature(rr.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.writeError(err)
		return err
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(crypto.PublicKey(rr.PubKey))
	exists, err := p.satellite.UserExists(rpk)
	if !exists || err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.writeError(err)
		return err
	}

	// Sanity checks.
	if len(rr.Contracts) == 0 {
		err := errors.New("can't renew an empty set of contracts")
		s.writeError(err)
		return err
	}
	if rr.Period == 0 {
		err := errors.New("can't renew contracts with zero period")
		s.writeError(err)
		return err
	}
	if rr.RenewWindow == 0 {
		err := errors.New("can't renew contracts with zero renew window")
		s.writeError(err)
		return err
	}
	if rr.Storage == 0 {
		err := errors.New("can't renew contracts with zero expected storage")
		s.writeError(err)
		return err
	}
	if rr.MinShards == 0 || rr.TotalShards == 0 {
		err := errors.New("can't renew contracts with such redundancy params")
		s.writeError(err)
		return err
	}

	ecs := extendedContractSet{
		contracts: make([]extendedContract, 0, len(rr.Contracts)),
	}

	// Create an allowance.
	a := modules.Allowance{
		Hosts:       uint64(len(rr.Contracts)),
		Period:      types.BlockHeight(rr.Period),
		RenewWindow: types.BlockHeight(rr.RenewWindow),

		ExpectedStorage:    rr.Storage,
		ExpectedUpload:     rr.Upload,
		ExpectedDownload:   rr.Download,
		MinShards:          rr.MinShards,
		TotalShards:        rr.TotalShards,

		MaxRPCPrice:               types.NewCurrency(rr.MaxRPCPrice.Big()),
		MaxContractPrice:          types.NewCurrency(rr.MaxContractPrice.Big()),
		MaxDownloadBandwidthPrice: types.NewCurrency(rr.MaxDownloadPrice.Big()).Div64(bytesInTerabyte),
		MaxSectorAccessPrice:      types.NewCurrency(rr.MaxSectorAccessPrice.Big()),
		MaxStoragePrice:           types.NewCurrency(rr.MaxStoragePrice.Big()).Div64(bytesInTerabyte),
		MaxUploadBandwidthPrice:   types.NewCurrency(rr.MaxUploadPrice.Big()).Div64(bytesInTerabyte),
		MinMaxCollateral:          types.NewCurrency(rr.MinMaxCollateral.Big()),
		BlockHeightLeeway:         types.BlockHeight(rr.BlockHeightLeeway),
	}

	// Renew the contracts.
	fcids := make([]types.FileContractID, len(rr.Contracts))
	for i, fcid := range rr.Contracts {
		copy(fcids[i][:], fcid[:])
	}
	contracts, err := p.satellite.RenewContracts(rpk, a, fcids)
	if err != nil {
		err = fmt.Errorf("could not renew contracts: %v", err)
		s.writeError(err)
		return err
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.contracts = append(ecs.contracts, extendedContract{
			contract:    cr,
			startHeight: uint64(contract.StartHeight),
			totalCost:   modules.ConvertCurrency(contract.TotalCost),
		})
	}

	return s.writeResponse(&ecs)
}

// convertContract converts the contract metadata from `siad`-style
// into `core`-style.
func convertContract(c modules.RenterContract) rhpv2.ContractRevision {
	fcr := c.Transaction.FileContractRevisions[0]
	ts0 := c.Transaction.TransactionSignatures[0]
	ts1 := c.Transaction.TransactionSignatures[1]
	renterSig := make([]byte, len(ts0.Signature))
	hostSig := make([]byte, len(ts1.Signature))
	copy(renterSig, ts0.Signature)
	copy(hostSig, ts1.Signature)
	cr := rhpv2.ContractRevision{
		Revision: core.FileContractRevision{
			ParentID:         core.FileContractID(c.ID),
			UnlockConditions: core.UnlockConditions{
				Timelock:           uint64(fcr.UnlockConditions.Timelock),
				PublicKeys:         []core.UnlockKey{
					core.PublicKey(c.RenterPublicKey.ToPublicKey()).UnlockKey(),
					core.PublicKey(c.HostPublicKey.ToPublicKey()).UnlockKey(),
				},
				SignaturesRequired: 2,
			},
			FileContract: core.FileContract{
				Filesize:       fcr.NewFileSize,
				FileMerkleRoot: core.Hash256(fcr.NewFileMerkleRoot),
				WindowStart:    uint64(fcr.NewWindowStart),
				WindowEnd:      uint64(fcr.NewWindowEnd),
				Payout:         core.ZeroCurrency,

				ValidProofOutputs:  []core.SiacoinOutput{{
					Value:   modules.ConvertCurrency(fcr.NewValidProofOutputs[0].Value),
					Address: core.Address(fcr.NewValidProofOutputs[0].UnlockHash),
				}, {
					Value:   modules.ConvertCurrency(fcr.NewValidProofOutputs[1].Value),
					Address: core.Address(fcr.NewValidProofOutputs[1].UnlockHash),
				}},
				MissedProofOutputs: []core.SiacoinOutput{{
					Value:   modules.ConvertCurrency(fcr.NewMissedProofOutputs[0].Value),
					Address: core.Address(fcr.NewMissedProofOutputs[0].UnlockHash),
				}, {
					Value:   modules.ConvertCurrency(fcr.NewMissedProofOutputs[1].Value),
					Address: core.Address(fcr.NewMissedProofOutputs[1].UnlockHash),
				}, {
					Value:   modules.ConvertCurrency(fcr.NewMissedProofOutputs[2].Value),
					Address: core.Address(fcr.NewMissedProofOutputs[2].UnlockHash),
				}},

				UnlockHash:     core.Hash256(fcr.NewUnlockHash),
				RevisionNumber: fcr.NewRevisionNumber,
			},
		},
		Signatures: [2]core.TransactionSignature{{
			ParentID:       core.Hash256(ts0.ParentID),
			PublicKeyIndex: ts0.PublicKeyIndex,
			Timelock:       uint64(ts0.Timelock),
			CoveredFields:  core.CoveredFields{FileContractRevisions: []uint64{0},},
			Signature:      renterSig,
		}, {
			ParentID:       core.Hash256(ts1.ParentID),
			PublicKeyIndex: ts1.PublicKeyIndex,
			Timelock:       uint64(ts1.Timelock),
			CoveredFields:  core.CoveredFields{FileContractRevisions: []uint64{0},},
			Signature:      hostSig,
		}},
	}

	return cr
}

// managedUpdateRevision updates the contract with a new revision.
func (p *Provider) managedUpdateRevision(s *rpcSession) error {
	// Extend the deadline to meet the renewal of multiple contracts.
	s.conn.SetDeadline(time.Now().Add(updateRevisionTime))

	// Read the request.
	var ur updateRequest
	hash, err := s.readRequest(&ur, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.writeError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), ur.PubKey, crypto.Signature(ur.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.writeError(err)
		return err
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(crypto.PublicKey(ur.PubKey))
	exists, err := p.satellite.UserExists(rpk)
	if !exists || err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.writeError(err)
		return err
	}

	uploads := types.NewCurrency(ur.Uploads.Big())
	downloads := types.NewCurrency(ur.Downloads.Big())
	fundAccount := types.NewCurrency(ur.FundAccount.Big())
	rev, sigs := convertRevision(ur.Contract)

	// Update the contract.
	err = p.satellite.UpdateContract(rev, sigs, uploads, downloads, fundAccount)
	
	// Send a response.
	if err != nil {
		err = fmt.Errorf("couldn't update contract: %v", err)
		s.writeError(err)
		return err
	}

	return s.writeResponse(nil)
}

// convertRevision converts a `core`-style revision into the `siad`-style.
func convertRevision(rev rhpv2.ContractRevision) (types.FileContractRevision, []types.TransactionSignature) {
	var rpk, hpk crypto.PublicKey
	copy(rpk[:], rev.Revision.UnlockConditions.PublicKeys[0].Key)
	copy(hpk[:], rev.Revision.UnlockConditions.PublicKeys[1].Key)
	fcr := types.FileContractRevision{
		ParentID:         types.FileContractID(rev.Revision.ParentID),
		UnlockConditions: types.UnlockConditions{
			Timelock:   types.BlockHeight(rev.Revision.UnlockConditions.Timelock),
			PublicKeys: []types.SiaPublicKey{
				types.Ed25519PublicKey(rpk),
				types.Ed25519PublicKey(hpk),
			},
			SignaturesRequired: rev.Revision.UnlockConditions.SignaturesRequired,
		},
		NewRevisionNumber: rev.Revision.RevisionNumber,
		NewFileSize:       rev.Revision.Filesize,
		NewFileMerkleRoot: crypto.Hash(rev.Revision.FileMerkleRoot),
		NewWindowStart:    types.BlockHeight(rev.Revision.WindowStart),
		NewWindowEnd:      types.BlockHeight(rev.Revision.WindowEnd),

		NewValidProofOutputs: []types.SiacoinOutput{{
			Value:      types.NewCurrency(rev.Revision.ValidProofOutputs[0].Value.Big()),
			UnlockHash: types.UnlockHash(rev.Revision.ValidProofOutputs[0].Address),
		}, {
			Value:      types.NewCurrency(rev.Revision.ValidProofOutputs[1].Value.Big()),
			UnlockHash: types.UnlockHash(rev.Revision.ValidProofOutputs[1].Address),
		}},
		NewMissedProofOutputs: []types.SiacoinOutput{{
			Value:      types.NewCurrency(rev.Revision.MissedProofOutputs[0].Value.Big()),
			UnlockHash: types.UnlockHash(rev.Revision.MissedProofOutputs[0].Address),
		}, {
			Value:      types.NewCurrency(rev.Revision.MissedProofOutputs[1].Value.Big()),
			UnlockHash: types.UnlockHash(rev.Revision.MissedProofOutputs[1].Address),
		}, {
			Value:      types.NewCurrency(rev.Revision.MissedProofOutputs[1].Value.Big()),
			UnlockHash: types.UnlockHash(rev.Revision.MissedProofOutputs[1].Address),
		}},
		NewUnlockHash: types.UnlockHash(rev.Revision.UnlockHash),
	}

	renterSig := make([]byte, len(rev.Signatures[0].Signature))
	copy(renterSig, rev.Signatures[0].Signature)
	hostSig := make([]byte, len(rev.Signatures[1].Signature))
	copy(hostSig, rev.Signatures[1].Signature)
	sigs := []types.TransactionSignature{{
		ParentID:       crypto.Hash(rev.Signatures[0].ParentID),
		PublicKeyIndex: rev.Signatures[0].PublicKeyIndex,
		Timelock:       types.BlockHeight(rev.Signatures[0].Timelock),
		CoveredFields:  types.CoveredFields{FileContractRevisions: []uint64{0},},
		Signature:      renterSig,
	}, {
		ParentID:       crypto.Hash(rev.Signatures[1].ParentID),
		PublicKeyIndex: rev.Signatures[1].PublicKeyIndex,
		Timelock:       types.BlockHeight(rev.Signatures[1].Timelock),
		CoveredFields:  types.CoveredFields{FileContractRevisions: []uint64{0},},
		Signature:      hostSig,
	}}

	return fcr, sigs
}
