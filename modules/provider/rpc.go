package provider

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

// The maximum allowed total size of temporary files.
const maxBufferSize = uint64(40 * 1024 * 1024 * 1024) // 40 GiB

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
	ecs := modules.ExtendedContractSet{
		Contracts: make([]modules.ExtendedContract, 0, len(contracts)),
	}

	for _, contract := range contracts {
		cr := convertContract(contract)
		ecs.Contracts = append(ecs.Contracts, modules.ExtendedContract{
			Contract:            cr,
			StartHeight:         contract.StartHeight,
			ContractPrice:       contract.ContractFee,
			TotalCost:           contract.TotalCost,
			UploadSpending:      contract.UploadSpending,
			DownloadSpending:    contract.DownloadSpending,
			FundAccountSpending: contract.FundAccountSpending,
			RenewedFrom:         p.m.RenewedFrom(contract.ID),
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

	ecs := modules.ExtendedContractSet{
		Contracts: make([]modules.ExtendedContract, 0, fr.Hosts),
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

		UploadPacking: fr.UploadPacking,
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
		ecs.Contracts = append(ecs.Contracts, modules.ExtendedContract{
			Contract:    cr,
			StartHeight: contract.StartHeight,
			TotalCost:   contract.TotalCost,
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

	ecs := modules.ExtendedContractSet{
		Contracts: make([]modules.ExtendedContract, 0, len(rr.Contracts)),
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

		UploadPacking: rr.UploadPacking,
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
		ecs.Contracts = append(ecs.Contracts, modules.ExtendedContract{
			Contract:    cr,
			StartHeight: contract.StartHeight,
			TotalCost:   contract.TotalCost,
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

	ec := modules.ExtendedContract{
		Contract:    convertContract(contract),
		StartHeight: contract.StartHeight,
		TotalCost:   contract.TotalCost,
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

	ec := modules.ExtendedContract{
		Contract:    convertContract(contract),
		StartHeight: contract.StartHeight,
		TotalCost:   contract.TotalCost,
	}

	return s.WriteResponse(&ec)
}

// managedGetSettings returns the renter's opt-in settings.
func (p *Provider) managedGetSettings(s *modules.RPCSession) error {
	// Extend the deadline.
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
		AutoRepairFiles:    renter.Settings.AutoRepairFiles,
		ProxyUploads:       renter.Settings.ProxyUploads,
	}

	return s.WriteResponse(&resp)
}

// managedUpdateSettings updates the renter's opt-in settings.
func (p *Provider) managedUpdateSettings(s *modules.RPCSession) error {
	// Extend the deadline.
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
	if usr.AutoRenewContracts || usr.AutoRepairFiles {
		if len(usr.PrivateKey) == 0 {
			err := errors.New("private key must be provided with these options")
			s.WriteError(err)
			return err
		}
	}
	if usr.AutoRepairFiles {
		if len(usr.AccountKey) == 0 {
			err := errors.New("account key must be provided with these options")
			s.WriteError(err)
			return err
		}
	}
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
	if usr.AutoRepairFiles && (!usr.BackupFileMetadata || !usr.AutoRenewContracts) {
		err := errors.New("file auto-repairs only work with automatic renewals and metadata backups enabled")
		s.WriteError(err)
		return err
	}

	if usr.ProxyUploads && !usr.BackupFileMetadata {
		err := errors.New("proxying uploads only works with automatic metadata backups enabled")
		s.WriteError(err)
		return err
	}

	// Update the settings.
	err = p.m.UpdateRenterSettings(usr.PubKey, modules.RenterSettings{
		AutoRenewContracts: usr.AutoRenewContracts,
		BackupFileMetadata: usr.BackupFileMetadata,
		AutoRepairFiles:    usr.AutoRepairFiles,
		ProxyUploads:       usr.ProxyUploads,
	}, usr.PrivateKey, usr.AccountKey)
	if err != nil {
		err = fmt.Errorf("couldn't update settings: %v", err)
		s.WriteError(err)
		return err
	}

	// Delete buffered files if opted out.
	if !usr.ProxyUploads {
		p.m.DeleteBufferedFiles(usr.PubKey)
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

		UploadPacking: usr.UploadPacking,
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
func (p *Provider) managedSaveMetadata(s *rhpv3.Stream) error {
	// Extend the deadline.
	s.SetDeadline(time.Now().Add(saveMetadataTime))

	// Read the request.
	var smr saveMetadataRequest
	err := s.ReadResponse(&smr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Verify the signature.
	h := types.NewHasher()
	smr.EncodeTo(h.E)
	if ok := smr.PubKey.VerifyHash(h.Sum(), smr.Signature); !ok {
		err = errors.New("could not verify renter signature")
		s.WriteResponseErr(err)
		return err
	}

	// Check if we know this renter.
	renter, err := p.m.GetRenter(smr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Check if the renter has opted in.
	if !renter.Settings.BackupFileMetadata {
		err := errors.New("metadata backups disabled")
		s.WriteResponseErr(err)
		return err
	}

	// Receive partial slab data if there is any.
	if smr.DataSize > 0 {
		var ud uploadData
		maxLen := uint64(1048576) + 8 + 1
		ud.More = true
		offset := 0
		for ud.More {
			s.SetDeadline(time.Now().Add(30 * time.Second))
			err = s.ReadResponse(&ud, maxLen)
			if err != nil {
				err = fmt.Errorf("could not read data: %v", err)
				s.WriteResponseErr(err)
				return err
			}
			copy(smr.Metadata.Data[offset:], ud.Data)
			offset += len(ud.Data)
			resp := uploadResponse{
				Filesize: uint64(len(ud.Data)),
			}
			if err := s.WriteResponse(&resp); err != nil {
				err = fmt.Errorf("could not write response: %v", err)
				s.WriteResponseErr(err)
				return err
			}
		}
	}

	// Save the metadata.
	err = p.m.UpdateMetadata(smr.PubKey, smr.Metadata)
	if err != nil {
		err = fmt.Errorf("couldn't save metadata: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	return nil
}

// managedRequestMetadata returns a slice containing the saved file
// metadata belonging top the renter.
func (p *Provider) managedRequestMetadata(s *rhpv3.Stream) error {
	// Extend the deadline.
	s.SetDeadline(time.Now().Add(requestMetadataTime))

	// Read the request.
	var rmr requestMetadataRequest
	err := s.ReadResponse(&rmr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Verify the signature.
	h := types.NewHasher()
	rmr.EncodeTo(h.E)
	if ok := rmr.PubKey.VerifyHash(h.Sum(), rmr.Signature); !ok {
		err = errors.New("could not verify renter signature")
		s.WriteResponseErr(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(rmr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Get the metadata.
	fm, err := p.m.RetrieveMetadata(rmr.PubKey, rmr.PresentObjects)
	if err != nil {
		err = fmt.Errorf("could not retrieve metadata: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	resp := requestMetadataResponse{
		metadata: fm,
	}

	if err := s.WriteResponse(&resp); err != nil {
		err = fmt.Errorf("could not write response: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Send the partial slab data one by one.
	for _, md := range fm {
		s.SetDeadline(time.Now().Add(30 * time.Second))
		ur := uploadResponse{
			Filesize: uint64(len(md.Data)),
		}
		if err := s.WriteResponse(&ur); err != nil {
			err = fmt.Errorf("could not write response: %v", err)
			s.WriteResponseErr(err)
			return err
		}

		if len(md.Data) == 0 {
			continue // No partial slab data with this object.
		}

		var ud uploadData
		dataLen := 1048576
		for len(md.Data) > 0 {
			s.SetDeadline(time.Now().Add(30 * time.Second))
			if len(md.Data) > dataLen {
				ud.Data = md.Data[:dataLen]
			} else {
				ud.Data = md.Data
			}
			md.Data = md.Data[len(ud.Data):]
			ud.More = len(md.Data) > 0
			err = s.WriteResponse(&ud)
			if err != nil {
				err = fmt.Errorf("could not write data: %v", err)
				s.WriteResponseErr(err)
				return err
			}
			var ur uploadResponse
			if err := s.ReadResponse(&ur, 1024); err != nil {
				err = fmt.Errorf("could not read response: %v", err)
				s.WriteResponseErr(err)
				return err
			}
			if ur.Filesize != uint64(len(ud.Data)) {
				err = errors.New("wrong data size received")
				s.WriteResponseErr(err)
				return err
			}
		}
	}

	return nil
}

// managedUpdateSlab updates a single slab.
func (p *Provider) managedUpdateSlab(s *modules.RPCSession) error {
	// Extend the deadline.
	s.Conn.SetDeadline(time.Now().Add(updateSlabTime))

	// Read the request.
	var usr updateSlabRequest
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

	// Check if the renter has opted in.
	if !renter.Settings.BackupFileMetadata {
		err := errors.New("metadata backups disabled")
		s.WriteError(err)
		return err
	}

	// Update the slab.
	err = p.m.UpdateSlab(usr.PubKey, usr.Slab, usr.Packed)
	if err != nil {
		err = fmt.Errorf("couldn't update slab: %v", err)
		s.WriteError(err)
		return err
	}

	return s.WriteResponse(nil)
}

// managedRequestSlabs returns a slice of slabs modified
// since the last retrieval.
func (p *Provider) managedRequestSlabs(s *modules.RPCSession) error {
	// Extend the deadline.
	s.Conn.SetDeadline(time.Now().Add(requestSlabsTime))

	// Read the request.
	var rsr requestSlabsRequest
	hash, err := s.ReadRequest(&rsr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if !rsr.PubKey.VerifyHash(hash, rsr.Signature) {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Retrieve the slabs.
	slabs, err := p.m.GetModifiedSlabs(rsr.PubKey)
	if err != nil {
		err = fmt.Errorf("couldn't retrieve slabs: %v", err)
		s.WriteError(err)
		return err
	}

	resp := requestSlabsResponse{
		slabs: slabs,
	}

	return s.WriteResponse(&resp)
}

// managedAcceptContracts accepts a set of contracts from the renter.
func (p *Provider) managedAcceptContracts(s *modules.RPCSession) error {
	// Extend the deadline.
	s.Conn.SetDeadline(time.Now().Add(shareContractsTime))

	// Read the request.
	var sr shareRequest
	hash, err := s.ReadRequest(&sr, 65536)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteError(err)
		return err
	}

	// Verify the signature.
	if ok := sr.PubKey.VerifyHash(hash, sr.Signature); !ok {
		err = errors.New("could not verify renter signature")
		s.WriteError(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(sr.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteError(err)
		return err
	}

	// Accept the contracts.
	p.m.AcceptContracts(sr.PubKey, sr.Contracts)

	return s.WriteResponse(nil)
}

// managedReceiveFile accepts a file from the renter.
func (p *Provider) managedReceiveFile(s *rhpv3.Stream) error {
	// Read the request.
	var ur uploadRequest
	err := s.ReadResponse(&ur, 1024)
	if err != nil {
		err = fmt.Errorf("could not read renter request: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Verify the signature.
	h := types.NewHasher()
	ur.EncodeTo(h.E)
	if ok := ur.PubKey.VerifyHash(h.Sum(), ur.Signature); !ok {
		err = errors.New("could not verify renter signature")
		s.WriteResponseErr(err)
		return err
	}

	// Check if we know this renter.
	_, err = p.m.GetRenter(ur.PubKey)
	if err != nil {
		err = fmt.Errorf("could not find renter in the database: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Get the current size of the buffer.
	currentSize, err := p.m.GetBufferSize()
	if err != nil {
		err = fmt.Errorf("could not calculate the buffer size: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Send the length of the file already uploaded.
	path, length, err := p.m.BytesUploaded(ur.PubKey, ur.Bucket, ur.Path)
	if err != nil {
		err = fmt.Errorf("could not get uploaded length: %v", err)
		s.WriteResponseErr(err)
		return err
	}
	resp := uploadResponse{
		Filesize: length,
	}
	err = s.WriteResponse(&resp)
	if err != nil {
		err = fmt.Errorf("could not write response: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Start receiving data in chunks.
	var ud uploadData
	maxLen := uint64(1048576) + 8 + 1
	err = s.ReadResponse(&ud, maxLen)
	if err != nil {
		err = fmt.Errorf("could not read data: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Open the file and write data to it.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0660)
	if err != nil {
		err = fmt.Errorf("could not open file: %v", err)
		s.WriteResponseErr(err)
		return err
	}
	defer func() {
		if err := file.Sync(); err != nil {
			p.log.Println("ERROR: couldn't sync file:", err)
		} else if err := file.Close(); err != nil {
			p.log.Println("ERROR: couldn't close file:", err)
		} else if err := p.m.RegisterUpload(ur.PubKey, ur.Bucket, ur.Path, ur.MimeType, path, !ud.More); err != nil {
			p.log.Println("ERROR: couldn't register file:", err)
		}
	}()

	if currentSize+uint64(len(ud.Data)) > maxBufferSize {
		err = errors.New("maximum buffer size exceeded")
		s.WriteResponseErr(err)
		return err
	}

	numBytes, err := file.Write(ud.Data)
	if err != nil {
		err = fmt.Errorf("could not write data: %v", err)
		s.WriteResponseErr(err)
		return err
	}
	currentSize += uint64(len(ud.Data))

	// Return the number of bytes written.
	resp.Filesize = uint64(numBytes)
	if err := s.WriteResponse(&resp); err != nil {
		err = fmt.Errorf("could not write response: %v", err)
		s.WriteResponseErr(err)
		return err
	}

	// Continue as long as there is more data available.
	for ud.More {
		s.SetDeadline(time.Now().Add(30 * time.Second))
		err = s.ReadResponse(&ud, maxLen)
		if err != nil {
			err = fmt.Errorf("could not read data: %v", err)
			s.WriteResponseErr(err)
			return err
		}

		if currentSize+uint64(len(ud.Data)) > maxBufferSize {
			err = errors.New("maximum buffer size exceeded")
			s.WriteResponseErr(err)
			return err
		}
		currentSize += uint64(len(ud.Data))

		numBytes, err = file.Write(ud.Data)
		if err != nil {
			err = fmt.Errorf("could not write data: %v", err)
			s.WriteResponseErr(err)
			return err
		}

		resp.Filesize = uint64(numBytes)
		if err := s.WriteResponse(&resp); err != nil {
			err = fmt.Errorf("could not write response: %v", err)
			s.WriteResponseErr(err)
			return err
		}
	}

	return nil
}
