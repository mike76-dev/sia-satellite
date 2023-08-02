package provider

import (
	"errors"
	"fmt"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
)

// managedRequestContracts returns a slice containing the list of the
// renter's active contracts.
func (p *Provider) managedRequestContracts(s *modules.RPCSession) error {
	// Extend the deadline.
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
	if ok := rr.PubKey.VerifyHash(hash, rr.Signature); !ok {
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

	// Get the contracts.
	contracts := p.m.ContractsByRenter(rr.PubKey)
	ecs := extendedContractSet{
		contracts: make([]extendedContract, 0, len(contracts)),
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.contracts = append(ecs.contracts, extendedContract{
			contract:            cr,
			startHeight:         contract.StartHeight,
			totalCost:           contract.TotalCost,
			uploadSpending:      contract.UploadSpending,
			downloadSpending:    contract.DownloadSpending,
			fundAccountSpending: contract.FundAccountSpending,
			renewedFrom:         p.m.RenewedFrom(contract.ID),
		})
	}

	return s.WriteResponse(&ecs)
}

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

		ExpectedStorage:  fr.Storage,
		ExpectedUpload:   fr.Upload,
		ExpectedDownload: fr.Download,
		MinShards:        fr.MinShards,
		TotalShards:      fr.TotalShards,

		MaxRPCPrice:               fr.MaxRPCPrice,
		MaxContractPrice:          fr.MaxContractPrice,
		MaxDownloadBandwidthPrice: fr.MaxDownloadPrice,
		MaxSectorAccessPrice:      fr.MaxSectorAccessPrice,
		MaxStoragePrice:           fr.MaxStoragePrice.Mul64(modules.BlocksPerMonth).Mul64(modules.BytesPerTerabyte),
		MaxUploadBandwidthPrice:   fr.MaxUploadPrice,
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

		ExpectedStorage:  rr.Storage,
		ExpectedUpload:   rr.Upload,
		ExpectedDownload: rr.Download,
		MinShards:        rr.MinShards,
		TotalShards:      rr.TotalShards,

		MaxRPCPrice:               rr.MaxRPCPrice,
		MaxContractPrice:          rr.MaxContractPrice,
		MaxDownloadBandwidthPrice: rr.MaxDownloadPrice,
		MaxSectorAccessPrice:      rr.MaxSectorAccessPrice,
		MaxStoragePrice:           rr.MaxStoragePrice.Mul64(modules.BlocksPerMonth).Mul64(modules.BytesPerTerabyte),
		MaxUploadBandwidthPrice:   rr.MaxUploadPrice,
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
		Revision: txn.FileContractRevisions[0],
		Signatures: [2]types.TransactionSignature{
			txn.Signatures[0],
			txn.Signatures[1],
		},
	}
}

// managedUpdateRevision updates the contract with a new revision.
func (p *Provider) managedUpdateRevision(s *modules.RPCSession) error {
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
	if ok := ur.PubKey.VerifyHash(hash, ur.Signature); !ok {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(ur.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	uploads := ur.Uploads
	downloads := ur.Downloads
	fundAccount := ur.FundAccount
	rev, sigs := ur.Contract.Revision, ur.Contract.Signatures

	// Update the contract.
	err = p.m.UpdateContract(rev, sigs[:], uploads, downloads, fundAccount)

	// Send a response.
	if err != nil {
		err = fmt.Errorf("couldn't update contract: %v", err)
		s.WriteError(err)
		return err
	}

	return s.WriteResponse(nil)
}

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
func (p *Provider) managedRenewContract(s *modules.RPCSession) error {
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
	if !rcr.PubKey.VerifyHash(hash, rcr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(rcr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Sanity checks.
	if (rcr.Contract == types.FileContractID{}) {
		err := errors.New("can't renew contract with no contract ID")
		s.WriteError(err)
		return err
	}
	if rcr.EndHeight <= p.m.BlockHeight() {
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
	contract, err := p.m.RenewContract(s, rcr.PubKey, rcr.Contract, rcr.EndHeight, rcr.Storage, rcr.Upload, rcr.Download, rcr.MinShards, rcr.TotalShards)
	if err != nil {
		err = fmt.Errorf("could not renew contract: %v", err)
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
		BackupFileMetadata: renter.Settings.BackupFileMetadata,
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
	renter, err := p.m.GetRenter(usr.PubKey)
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
		BackupFileMetadata: usr.BackupFileMetadata,
	}, usr.PrivateKey)
	if err != nil {
		err = fmt.Errorf("couldn't update settings: %v", err)
		s.WriteError(err)
		return err
	}

	// Delete file metadata if opted out.
	if !usr.BackupFileMetadata {
		p.m.DeleteMetadata(usr.PubKey)
	}

	// If not opted in, return.
	if !usr.AutoRenewContracts {
		return s.WriteResponse(nil)
	}

	// Create an allowance.
	a := modules.Allowance{
		Funds:       renter.Allowance.Funds,
		Hosts:       usr.Hosts,
		Period:      usr.Period,
		RenewWindow: usr.RenewWindow,

		ExpectedStorage:  usr.Storage,
		ExpectedUpload:   usr.Upload,
		ExpectedDownload: usr.Download,
		MinShards:        usr.MinShards,
		TotalShards:      usr.TotalShards,

		MaxRPCPrice:               usr.MaxRPCPrice,
		MaxContractPrice:          usr.MaxContractPrice,
		MaxDownloadBandwidthPrice: usr.MaxDownloadPrice,
		MaxSectorAccessPrice:      usr.MaxSectorAccessPrice,
		MaxStoragePrice:           usr.MaxStoragePrice.Mul64(modules.BlocksPerMonth).Mul64(modules.BytesPerTerabyte),
		MaxUploadBandwidthPrice:   usr.MaxUploadPrice,
		MinMaxCollateral:          usr.MinMaxCollateral,
		BlockHeightLeeway:         usr.BlockHeightLeeway,
	}
	if a.Funds.IsZero() {
		a.Funds = types.HastingsPerSiacoin.Mul64(1000) // 1 KS
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

// managedSaveMetadata reads the file metadata and saves it.
func (p *Provider) managedSaveMetadata(s *modules.RPCSession) error {
	// Extend the deadline to meet the formation of multiple contracts.
	s.Conn.SetDeadline(time.Now().Add(saveMetadataTime))

	// Read the request.
	var smr saveMetadataRequest
	hash, err := s.ReadRequest(&smr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !smr.PubKey.VerifyHash(hash, smr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	renter, err := p.m.GetRenter(smr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Check if the renter has opted in.
	if !renter.Settings.BackupFileMetadata {
		err := errors.New("metadata backups disabled")
		s.WriteError(err)
		return err
	}

	// Save the metadata.
	err = p.m.UpdateMetadata(smr.PubKey, smr.Metadata)
	if err != nil {
		err = fmt.Errorf("couldn't save metadata: %v", err)
		s.WriteError(err)
		return err
	}

	return s.WriteResponse(nil)
}

// managedRequestMetadata returns a slice containing the saved file
// metadata belonging top the renter.
func (p *Provider) managedRequestMetadata(s *modules.RPCSession) error {
	// Extend the deadline.
	s.Conn.SetDeadline(time.Now().Add(requestMetadataTime))

	// Read the request.
	var rmr requestMetadataRequest
	hash, err := s.ReadRequest(&rmr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if ok := rmr.PubKey.VerifyHash(hash, rmr.Signature); !ok {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(rmr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Get the metadata.
	fm, err := p.m.RetrieveMetadata(rmr.PubKey, rmr.PresentObjects)
	if err != nil {
		err = fmt.Errorf("could not retrieve metadata: %v", err)
		s.WriteError(err)
		return err
	}

	resp := requestMetadataResponse{
		metadata: fm,
	}

	return s.WriteResponse(&resp)
}
