package provider

import (
	"errors"
	"fmt"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
)

const bytesInTerabyte = 1024 * 1024 * 1024 * 1024

// managedRequestContracts returns a slice containing the list of the
// renter's active contracts.
/*func (p *Provider) managedRequestContracts(s *modules.RPCSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.Conn.SetDeadline(time.Now().Add(requestContractsTime))

	// Read the request.
	var rr requestRequest
	hash, err := s.ReadRequest(&rr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), rr.PubKey, crypto.Signature(rr.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(rr.PubKey)
	_, err = p.satellite.GetRenter(rpk)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Get the contracts.
	contracts := p.satellite.ContractsByRenter(rpk)
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

	return s.WriteResponse(&ecs)
}*/

// managedFormContracts forms the specified number of contracts with the hosts
// on behalf of the renter.
func (p *Provider) managedFormContracts(s *modules.RPCSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.Conn.SetDeadline(time.Now().Add(formContractsTime))

	// Read the request.
	var fr formRequest
	hash, err := s.ReadRequest(&fr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !fr.PubKey.VerifyHash(hash, fr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(fr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Sanity checks.
	if fr.Hosts == 0 {
		err := errors.New("can't form contracts with zero hosts")
		s.WriteError(err)
		return err
	}
	if fr.Period == 0 {
		err := errors.New("can't form contracts with zero period")
		s.WriteError(err)
		return err
	}
	if fr.RenewWindow == 0 {
		err := errors.New("can't form contracts with zero renew window")
		s.WriteError(err)
		return err
	}
	if fr.Storage == 0 {
		err := errors.New("can't form contracts with zero expected storage")
		s.WriteError(err)
		return err
	}
	if fr.MinShards == 0 || fr.TotalShards == 0 {
		err := errors.New("can't form contracts with such redundancy params")
		s.WriteError(err)
		return err
	}

	ecs := extendedContractSet{
		contracts: make([]extendedContract, 0, fr.Hosts),
	}

	// Create an allowance.
	a := modules.Allowance{
		Hosts:       fr.Hosts,
		Period:      fr.Period,
		RenewWindow: fr.RenewWindow,

		ExpectedStorage:    fr.Storage,
		ExpectedUpload:     fr.Upload,
		ExpectedDownload:   fr.Download,
		MinShards:          fr.MinShards,
		TotalShards:        fr.TotalShards,

		MaxRPCPrice:               fr.MaxRPCPrice,
		MaxContractPrice:          fr.MaxContractPrice,
		MaxDownloadBandwidthPrice: fr.MaxDownloadPrice.Div64(bytesInTerabyte),
		MaxSectorAccessPrice:      fr.MaxSectorAccessPrice,
		MaxStoragePrice:           fr.MaxStoragePrice.Div64(bytesInTerabyte),
		MaxUploadBandwidthPrice:   fr.MaxUploadPrice.Div64(bytesInTerabyte),
		MinMaxCollateral:          fr.MinMaxCollateral,
		BlockHeightLeeway:         fr.BlockHeightLeeway,
	}

	// Form the contracts.
	contracts, err := p.m.FormContracts(fr.PubKey, fr.SecretKey, a)
	if err != nil {
		err = fmt.Errorf("could not form contracts: %v", err)
		s.WriteError(err)
		return err
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.contracts = append(ecs.contracts, extendedContract{
			contract:    cr,
			startHeight: contract.StartHeight,
			totalCost:   contract.TotalCost,
		})
	}

	return s.WriteResponse(&ecs)
}

// managedRenewContracts tries to renew the given set of contracts.
func (p *Provider) managedRenewContracts(s *modules.RPCSession) error {
	// Extend the deadline to meet the renewal of multiple contracts.
	s.Conn.SetDeadline(time.Now().Add(renewContractsTime))

	// Read the request.
	var rr renewRequest
	hash, err := s.ReadRequest(&rr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !rr.PubKey.VerifyHash(hash, rr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(rr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Sanity checks.
	if len(rr.Contracts) == 0 {
		err := errors.New("can't renew an empty set of contracts")
		s.WriteError(err)
		return err
	}
	if rr.Period == 0 {
		err := errors.New("can't renew contracts with zero period")
		s.WriteError(err)
		return err
	}
	if rr.RenewWindow == 0 {
		err := errors.New("can't renew contracts with zero renew window")
		s.WriteError(err)
		return err
	}
	if rr.Storage == 0 {
		err := errors.New("can't renew contracts with zero expected storage")
		s.WriteError(err)
		return err
	}
	if rr.MinShards == 0 || rr.TotalShards == 0 {
		err := errors.New("can't renew contracts with such redundancy params")
		s.WriteError(err)
		return err
	}

	ecs := extendedContractSet{
		contracts: make([]extendedContract, 0, len(rr.Contracts)),
	}

	// Create an allowance.
	a := modules.Allowance{
		Hosts:       uint64(len(rr.Contracts)),
		Period:      rr.Period,
		RenewWindow: rr.RenewWindow,

		ExpectedStorage:    rr.Storage,
		ExpectedUpload:     rr.Upload,
		ExpectedDownload:   rr.Download,
		MinShards:          rr.MinShards,
		TotalShards:        rr.TotalShards,

		MaxRPCPrice:               rr.MaxRPCPrice,
		MaxContractPrice:          rr.MaxContractPrice,
		MaxDownloadBandwidthPrice: rr.MaxDownloadPrice.Div64(bytesInTerabyte),
		MaxSectorAccessPrice:      rr.MaxSectorAccessPrice,
		MaxStoragePrice:           rr.MaxStoragePrice.Div64(bytesInTerabyte),
		MaxUploadBandwidthPrice:   rr.MaxUploadPrice.Div64(bytesInTerabyte),
		MinMaxCollateral:          rr.MinMaxCollateral,
		BlockHeightLeeway:         rr.BlockHeightLeeway,
	}

	// Renew the contracts.
	contracts, err := p.m.RenewContracts(rr.PubKey, rr.SecretKey, a, rr.Contracts)
	if err != nil {
		err = fmt.Errorf("could not renew contracts: %v", err)
		s.WriteError(err)
		return err
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.contracts = append(ecs.contracts, extendedContract{
			contract:    cr,
			startHeight: contract.StartHeight,
			totalCost:   contract.TotalCost,
		})
	}

	return s.WriteResponse(&ecs)
}

// convertContract converts the contract metadata into `core` style.
func convertContract(c modules.RenterContract) rhpv2.ContractRevision {
	txn := modules.CopyTransaction(c.Transaction)
	return rhpv2.ContractRevision{
		Revision:   txn.FileContractRevisions[0],
		Signatures: [2]types.TransactionSignature{
			txn.Signatures[0],
			txn.Signatures[1],
		},
	}
}

// managedUpdateRevision updates the contract with a new revision.
/*func (p *Provider) managedUpdateRevision(s *modules.RPCSession) error {
	// Extend the deadline to meet the revision update.
	s.Conn.SetDeadline(time.Now().Add(updateRevisionTime))

	// Read the request.
	var ur updateRequest
	hash, err := s.ReadRequest(&ur, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), ur.PubKey, crypto.Signature(ur.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(ur.PubKey)
	exists, err := p.satellite.UserExists(rpk)
	if !exists || err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
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
		s.WriteError(err)
		return err
	}

	return s.WriteResponse(nil)
}*/

// convertRevision converts a `core`-style revision into the `siad`-style.
/*func convertRevision(rev rhpv2.ContractRevision) (types.FileContractRevision, []types.TransactionSignature) {
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
}*/

// managedFormContract forms a single contract using the new Renter-Satellite
// protocol.
func (p *Provider) managedFormContract(s *modules.RPCSession) error {
	// Extend the deadline to meet the contract formation.
	s.Conn.SetDeadline(time.Now().Add(formContractTime))

	// Read the request.
	var fcr formContractRequest
	hash, err := s.ReadRequest(&fcr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !fcr.PubKey.VerifyHash(hash, fcr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(fcr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Sanity checks.
	if (fcr.RenterPublicKey == types.PublicKey{}) {
		err := errors.New("can't form contract with no renter specified")
		s.WriteError(err)
		return err
	}
	if (fcr.HostPublicKey == types.PublicKey{}) {
		err := errors.New("can't form contract with no host specified")
		s.WriteError(err)
		return err
	}
	if fcr.EndHeight <= uint64(p.m.BlockHeight()) {
		err := errors.New("can't form contract with end height in the past")
		s.WriteError(err)
		return err
	}
	if fcr.Storage == 0 {
		err := errors.New("can't form contract with zero expected storage")
		s.WriteError(err)
		return err
	}
	if fcr.MinShards == 0 || fcr.TotalShards == 0 {
		err := errors.New("can't form contract with such redundancy params")
		s.WriteError(err)
		return err
	}

	// Form the contract.
	contract, err := p.m.FormContract(s, fcr.PubKey, fcr.RenterPublicKey, fcr.HostPublicKey, fcr.EndHeight, fcr.Storage, fcr.Upload, fcr.Download, fcr.MinShards, fcr.TotalShards)
	if err != nil {
		err = fmt.Errorf("could not form contract: %v", err)
		s.WriteError(err)
		return err
	}

	ec := extendedContract{
		contract:    convertContract(contract),
		startHeight: contract.StartHeight,
		totalCost:   contract.TotalCost,
	}

	return s.WriteResponse(&ec)
}

// managedRenewContract renews a contract using the new Renter-Satellite
// protocol.
/*func (p *Provider) managedRenewContract(s *modules.RPCSession) error {
	// Extend the deadline to meet the contract renewal.
	s.Conn.SetDeadline(time.Now().Add(renewContractTime))

	// Read the request.
	var rcr renewContractRequest
	hash, err := s.ReadRequest(&rcr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	err = crypto.VerifyHash(crypto.Hash(hash), rcr.PubKey, crypto.Signature(rcr.Signature))
	if err != nil {
		err = fmt.Errorf("could not verify renter signature: %v", err)
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	pk := types.Ed25519PublicKey(rcr.PubKey)
	exists, err := p.satellite.UserExists(pk)
	if !exists || err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Sanity checks.
	if (rcr.Contract == core.FileContractID{}) {
		err := errors.New("can't renew contract with no contract ID")
		s.WriteError(err)
		return err
	}
	if rcr.EndHeight <= uint64(p.satellite.BlockHeight()) {
		err := errors.New("can't renew contract with end height in the past")
		s.WriteError(err)
		return err
	}
	if rcr.Storage == 0 {
		err := errors.New("can't renew contract with zero expected storage")
		s.WriteError(err)
		return err
	}
	if rcr.MinShards == 0 || rcr.TotalShards == 0 {
		err := errors.New("can't renew contract with such redundancy params")
		s.WriteError(err)
		return err
	}

	// Renew the contract.
	contract, err := p.satellite.RenewContract(s, pk, types.FileContractID(rcr.Contract), types.BlockHeight(rcr.EndHeight), rcr.Storage, rcr.Upload, rcr.Download, rcr.MinShards, rcr.TotalShards)
	if err != nil {
		err = fmt.Errorf("could not renew contract: %v", err)
		s.WriteError(err)
		return err
	}

	ec := extendedContract{
		contract:    convertContract(contract),
		startHeight: uint64(contract.StartHeight),
		totalCost:   modules.ConvertCurrency(contract.TotalCost),
	}

	return s.WriteResponse(&ec)
}*/

// managedGetSettings returns the renter's opt-in settings.
func (p *Provider) managedGetSettings(s *modules.RPCSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.Conn.SetDeadline(time.Now().Add(settingsTime))

	// Read the request.
	var gsr getSettingsRequest
	hash, err := s.ReadRequest(&gsr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !gsr.PubKey.VerifyHash(hash, gsr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	renter, err := p.m.GetRenter(gsr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	resp := getSettingsResponse{
		AutoRenewContracts: renter.Settings.AutoRenewContracts,
	}

	return s.WriteResponse(&resp)
}

// managedUpdateSettings updates the renter's opt-in settings.
func (p *Provider) managedUpdateSettings(s *modules.RPCSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.Conn.SetDeadline(time.Now().Add(settingsTime))

	// Read the request.
	var usr updateSettingsRequest
	hash, err := s.ReadRequest(&usr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !usr.PubKey.VerifyHash(hash, usr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(usr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Sanity checks.
	if usr.AutoRenewContracts {
		if usr.Hosts == 0 {
			err := errors.New("can't set zero hosts")
			s.WriteError(err)
			return err
		}
		if usr.Period == 0 {
			err := errors.New("can't set zero period")
			s.WriteError(err)
			return err
		}
		if usr.RenewWindow == 0 {
			err := errors.New("can't set zero renew window")
			s.WriteError(err)
			return err
		}
		if usr.Storage == 0 {
			err := errors.New("can't set zero expected storage")
			s.WriteError(err)
			return err
		}
		if usr.MinShards == 0 || usr.TotalShards == 0 {
			err := errors.New("can't set such redundancy params")
			s.WriteError(err)
			return err
		}
	}

	// Update the settings.
	err = p.m.UpdateRenterSettings(usr.PubKey, modules.RenterSettings{
		AutoRenewContracts: usr.AutoRenewContracts,
	}, usr.PrivateKey)
	if err != nil {
		err = fmt.Errorf("couldn't update settings: %v", err)
		s.WriteError(err)
		return err
	}

	// If not opted in, return.
	if !usr.AutoRenewContracts {
		return s.WriteResponse(nil)
	}

	// Create an allowance.
	a := modules.Allowance{
		Hosts:       usr.Hosts,
		Period:      usr.Period,
		RenewWindow: usr.RenewWindow,

		ExpectedStorage:    usr.Storage,
		ExpectedUpload:     usr.Upload,
		ExpectedDownload:   usr.Download,
		MinShards:          usr.MinShards,
		TotalShards:        usr.TotalShards,

		MaxRPCPrice:               usr.MaxRPCPrice,
		MaxContractPrice:          usr.MaxContractPrice,
		MaxDownloadBandwidthPrice: usr.MaxDownloadPrice.Div64(bytesInTerabyte),
		MaxSectorAccessPrice:      usr.MaxSectorAccessPrice,
		MaxStoragePrice:           usr.MaxStoragePrice.Div64(bytesInTerabyte),
		MaxUploadBandwidthPrice:   usr.MaxUploadPrice.Div64(bytesInTerabyte),
		MinMaxCollateral:          usr.MinMaxCollateral,
		BlockHeightLeeway:         usr.BlockHeightLeeway,
	}

	// Set the allowance.
	err = p.m.SetAllowance(usr.PubKey, a)
	if err != nil {
		err = fmt.Errorf("could not set allowance: %v", err)
		s.WriteError(err)
		return err
	}

	return s.WriteResponse(nil)
}
