package provider

import (
	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
)

var (
	// Handshake specifier.
	loopEnterSpecifier = types.NewSpecifier("LoopEnter")

	// RPC ciphers.
	cipherChaCha20Poly1305 = types.NewSpecifier("ChaCha20Poly1305")
	cipherNoOverlap        = types.NewSpecifier("NoOverlap")
)

// Handshake objects.
type (
	loopKeyExchangeRequest struct {
		Specifier types.Specifier
		PublicKey [32]byte
		Ciphers   []types.Specifier
	}

	loopKeyExchangeResponse struct {
		PublicKey [32]byte
		Signature types.Signature
		Cipher    types.Specifier
	}
)

// EncodeTo implements types.ProtocolObject.
func (r *loopKeyExchangeRequest) EncodeTo(e *types.Encoder) {
	// Nothing to do here.
}

// DecodeFrom implements types.ProtocolObject.
func (r *loopKeyExchangeRequest) DecodeFrom(d *types.Decoder) {
	r.Specifier.DecodeFrom(d)
	d.Read(r.PublicKey[:])
	r.Ciphers = make([]types.Specifier, d.ReadPrefix())
	for i := range r.Ciphers {
		r.Ciphers[i].DecodeFrom(d)
	}
}

// EncodeTo implements types.ProtocolObject.
func (r *loopKeyExchangeResponse) EncodeTo(e *types.Encoder) {
	e.Write(r.PublicKey[:])
	e.WriteBytes(r.Signature[:])
	r.Cipher.EncodeTo(e)
}

// DecodeFrom implements types.ProtocolObject.
func (r *loopKeyExchangeResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// requestRequest is used when the renter requests the list of their
// active contracts.
type requestRequest struct {
	PubKey    crypto.PublicKey
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rr *requestRequest) DecodeFrom(d *types.Decoder) {
	copy(rr.PubKey[:], d.ReadBytes())
	rr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rr *requestRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(rr.PubKey[:])
}

// formRequest is used when the renter requests forming contracts with
// the hosts.
type formRequest struct {
	PubKey      crypto.PublicKey
	SecretKey   crypto.SecretKey
	Hosts       uint64
	Period      uint64
	RenewWindow uint64

	Storage  uint64
	Upload   uint64
	Download uint64

	MinShards   uint64
	TotalShards uint64

	MaxRPCPrice          types.Currency
	MaxContractPrice     types.Currency
	MaxDownloadPrice     types.Currency
	MaxUploadPrice       types.Currency
	MaxStoragePrice      types.Currency
	MaxSectorAccessPrice types.Currency
	MinMaxCollateral     types.Currency
	BlockHeightLeeway    uint64

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (fr *formRequest) DecodeFrom(d *types.Decoder) {
	copy(fr.PubKey[:], d.ReadBytes())
	copy(fr.SecretKey[:], d.ReadBytes())
	fr.Hosts = d.ReadUint64()
	fr.Period = d.ReadUint64()
	fr.RenewWindow = d.ReadUint64()
	fr.Storage = d.ReadUint64()
	fr.Upload = d.ReadUint64()
	fr.Download = d.ReadUint64()
	fr.MinShards = d.ReadUint64()
	fr.TotalShards = d.ReadUint64()
	fr.MaxRPCPrice.DecodeFrom(d)
	fr.MaxContractPrice.DecodeFrom(d)
	fr.MaxDownloadPrice.DecodeFrom(d)
	fr.MaxUploadPrice.DecodeFrom(d)
	fr.MaxStoragePrice.DecodeFrom(d)
	fr.MaxSectorAccessPrice.DecodeFrom(d)
	fr.MinMaxCollateral.DecodeFrom(d)
	fr.BlockHeightLeeway = d.ReadUint64()
	fr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (fr *formRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(fr.PubKey[:])
	e.WriteBytes(fr.SecretKey[:])
	e.WriteUint64(fr.Hosts)
	e.WriteUint64(fr.Period)
	e.WriteUint64(fr.RenewWindow)
	e.WriteUint64(fr.Storage)
	e.WriteUint64(fr.Upload)
	e.WriteUint64(fr.Download)
	e.WriteUint64(fr.MinShards)
	e.WriteUint64(fr.TotalShards)
	fr.MaxRPCPrice.EncodeTo(e)
	fr.MaxContractPrice.EncodeTo(e)
	fr.MaxDownloadPrice.EncodeTo(e)
	fr.MaxUploadPrice.EncodeTo(e)
	fr.MaxStoragePrice.EncodeTo(e)
	fr.MaxSectorAccessPrice.EncodeTo(e)
	fr.MinMaxCollateral.EncodeTo(e)
	e.WriteUint64(fr.BlockHeightLeeway)
}

// renewRequest is used when the renter requests contract renewals.
type renewRequest struct {
	PubKey      crypto.PublicKey
	SecretKey   crypto.SecretKey
	Contracts   []types.FileContractID
	Period      uint64
	RenewWindow uint64

	Storage  uint64
	Upload   uint64
	Download uint64

	MinShards   uint64
	TotalShards uint64

	MaxRPCPrice          types.Currency
	MaxContractPrice     types.Currency
	MaxDownloadPrice     types.Currency
	MaxUploadPrice       types.Currency
	MaxStoragePrice      types.Currency
	MaxSectorAccessPrice types.Currency
	MinMaxCollateral     types.Currency
	BlockHeightLeeway    uint64

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rr *renewRequest) DecodeFrom(d *types.Decoder) {
	copy(rr.PubKey[:], d.ReadBytes())
	copy(rr.SecretKey[:], d.ReadBytes())
	numContracts := int(d.ReadUint64())
	rr.Contracts = make([]types.FileContractID, numContracts)
	for i := 0; i < numContracts; i++ {
		copy(rr.Contracts[i][:], d.ReadBytes())
	}
	rr.Period = d.ReadUint64()
	rr.RenewWindow = d.ReadUint64()
	rr.Storage = d.ReadUint64()
	rr.Upload = d.ReadUint64()
	rr.Download = d.ReadUint64()
	rr.MinShards = d.ReadUint64()
	rr.TotalShards = d.ReadUint64()
	rr.MaxRPCPrice.DecodeFrom(d)
	rr.MaxContractPrice.DecodeFrom(d)
	rr.MaxDownloadPrice.DecodeFrom(d)
	rr.MaxUploadPrice.DecodeFrom(d)
	rr.MaxStoragePrice.DecodeFrom(d)
	rr.MaxSectorAccessPrice.DecodeFrom(d)
	rr.MinMaxCollateral.DecodeFrom(d)
	rr.BlockHeightLeeway = d.ReadUint64()
	rr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rr *renewRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(rr.PubKey[:])
	e.WriteBytes(rr.SecretKey[:])
	e.WriteUint64(uint64(len(rr.Contracts)))
	for _, id := range rr.Contracts {
		e.WriteBytes(id[:])
	}
	e.WriteUint64(rr.Period)
	e.WriteUint64(rr.RenewWindow)
	e.WriteUint64(rr.Storage)
	e.WriteUint64(rr.Upload)
	e.WriteUint64(rr.Download)
	e.WriteUint64(rr.MinShards)
	e.WriteUint64(rr.TotalShards)
	rr.MaxRPCPrice.EncodeTo(e)
	rr.MaxContractPrice.EncodeTo(e)
	rr.MaxDownloadPrice.EncodeTo(e)
	rr.MaxUploadPrice.EncodeTo(e)
	rr.MaxStoragePrice.EncodeTo(e)
	rr.MaxSectorAccessPrice.EncodeTo(e)
	rr.MinMaxCollateral.EncodeTo(e)
	e.WriteUint64(rr.BlockHeightLeeway)
}

// updateRequest is used when the renter submits a new revision.
type updateRequest struct {
	PubKey      crypto.PublicKey
	Contract    rhpv2.ContractRevision
	Uploads     types.Currency
	Downloads   types.Currency
	FundAccount types.Currency

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (ur *updateRequest) DecodeFrom(d *types.Decoder) {
	copy(ur.PubKey[:], d.ReadBytes())
	ur.Contract.Revision.DecodeFrom(d)
	ur.Contract.Signatures[0].DecodeFrom(d)
	ur.Contract.Signatures[1].DecodeFrom(d)
	ur.Uploads.DecodeFrom(d)
	ur.Downloads.DecodeFrom(d)
	ur.FundAccount.DecodeFrom(d)
	ur.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (ur *updateRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(ur.PubKey[:])
	ur.Contract.Revision.EncodeTo(e)
	ur.Contract.Signatures[0].EncodeTo(e)
	ur.Contract.Signatures[1].EncodeTo(e)
	ur.Uploads.EncodeTo(e)
	ur.Downloads.EncodeTo(e)
	ur.FundAccount.EncodeTo(e)
}

// extendedContract contains the contract and its metadata.
type extendedContract struct {
	contract            rhpv2.ContractRevision
	startHeight         uint64
	totalCost           types.Currency
	uploadSpending      types.Currency
	downloadSpending    types.Currency
	fundAccountSpending types.Currency
	renewedFrom         types.FileContractID
}

// EncodeTo implements requestBody.
func (ec extendedContract) EncodeTo(e *types.Encoder) {
	ec.contract.Revision.EncodeTo(e)
	ec.contract.Signatures[0].EncodeTo(e)
	ec.contract.Signatures[1].EncodeTo(e)
	e.WriteUint64(ec.startHeight)
	ec.totalCost.EncodeTo(e)
	ec.uploadSpending.EncodeTo(e)
	ec.downloadSpending.EncodeTo(e)
	ec.fundAccountSpending.EncodeTo(e)
	ec.renewedFrom.EncodeTo(e)
}

// DecodeFrom implements requestBody.
func (ec extendedContract) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// extendedContractSet is a collection of extendedContracts.
type extendedContractSet struct {
	contracts []extendedContract
}

// EncodeTo implements requestBody.
func (ecs extendedContractSet) EncodeTo(e *types.Encoder) {
	e.WriteUint64(uint64(len(ecs.contracts)))
	for _, ec := range ecs.contracts {
		ec.EncodeTo(e)
	}
}

// DecodeFrom implements requestBody.
func (ecs extendedContractSet) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// formContractRequest is used when forming a contract with a single
// host using the new Renter-Satellite protocol.
type formContractRequest struct {
	PubKey          crypto.PublicKey
	RenterPublicKey crypto.PublicKey
	HostPublicKey   crypto.PublicKey

	EndHeight   uint64
	Storage     uint64
	Upload      uint64
	Download    uint64
	MinShards   uint64
	TotalShards uint64

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (fcr *formContractRequest) DecodeFrom(d *types.Decoder) {
	copy(fcr.PubKey[:], d.ReadBytes())
	copy(fcr.RenterPublicKey[:], d.ReadBytes())
	copy(fcr.HostPublicKey[:], d.ReadBytes())
	fcr.EndHeight = d.ReadUint64()
	fcr.Storage = d.ReadUint64()
	fcr.Upload = d.ReadUint64()
	fcr.Download = d.ReadUint64()
	fcr.MinShards = d.ReadUint64()
	fcr.TotalShards = d.ReadUint64()
	fcr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (fcr *formContractRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(fcr.PubKey[:])
	e.WriteBytes(fcr.RenterPublicKey[:])
	e.WriteBytes(fcr.HostPublicKey[:])
	e.WriteUint64(fcr.EndHeight)
	e.WriteUint64(fcr.Storage)
	e.WriteUint64(fcr.Upload)
	e.WriteUint64(fcr.Download)
	e.WriteUint64(fcr.MinShards)
	e.WriteUint64(fcr.TotalShards)
}

// renewContractRequest is used when renewing a contract using
// the new Renter-Satellite protocol.
type renewContractRequest struct {
	PubKey          crypto.PublicKey
	Contract        types.FileContractID
	EndHeight       uint64

	Storage  uint64
	Upload   uint64
	Download uint64

	MinShards   uint64
	TotalShards uint64

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rcr *renewContractRequest) DecodeFrom(d *types.Decoder) {
	copy(rcr.PubKey[:], d.ReadBytes())
	copy(rcr.Contract[:], d.ReadBytes())
	rcr.EndHeight = d.ReadUint64()
	rcr.Storage = d.ReadUint64()
	rcr.Upload = d.ReadUint64()
	rcr.Download = d.ReadUint64()
	rcr.MinShards = d.ReadUint64()
	rcr.TotalShards = d.ReadUint64()
	rcr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rcr *renewContractRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(rcr.PubKey[:])
	e.WriteBytes(rcr.Contract[:])
	e.WriteUint64(rcr.EndHeight)
	e.WriteUint64(rcr.Storage)
	e.WriteUint64(rcr.Upload)
	e.WriteUint64(rcr.Download)
	e.WriteUint64(rcr.MinShards)
	e.WriteUint64(rcr.TotalShards)
}

// getSettingsRequest is used to retrieve the renter's opt-in
// settings.
type getSettingsRequest struct {
	PubKey    crypto.PublicKey
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (gsr *getSettingsRequest) DecodeFrom(d *types.Decoder) {
	copy(gsr.PubKey[:], d.ReadBytes())
	gsr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (gsr *getSettingsRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(gsr.PubKey[:])
}

// getSettingsResponse is used to send the opt-in settings
// to the renter.
type getSettingsResponse struct {
	AutoRenewContracts bool
}

// DecodeFrom implements requestBody.
func (gsr *getSettingsResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements requestBody.
func (gsr *getSettingsResponse) EncodeTo(e *types.Encoder) {
	e.WriteBool(gsr.AutoRenewContracts)
}

// updateSettingsRequest is used to update the renter's opt-in
// settings.
type updateSettingsRequest struct {
	PubKey             crypto.PublicKey
	AutoRenewContracts bool
	PrivateKey         crypto.SecretKey

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (usr *updateSettingsRequest) DecodeFrom(d *types.Decoder) {
	copy(usr.PubKey[:], d.ReadBytes())
	usr.AutoRenewContracts = d.ReadBool()
	if usr.AutoRenewContracts {
		copy(usr.PrivateKey[:], d.ReadBytes())
	}
	usr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (usr *updateSettingsRequest) EncodeTo(e *types.Encoder) {
	e.WriteBytes(usr.PubKey[:])
	e.WriteBool(usr.AutoRenewContracts)
	if usr.AutoRenewContracts {
		e.WriteBytes(usr.PrivateKey[:])
	}
}
