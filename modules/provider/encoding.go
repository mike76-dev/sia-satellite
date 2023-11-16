package provider

import (
	"github.com/mike76-dev/sia-satellite/modules"
	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
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

	loopChallengeRequest struct {
		// Entropy signed by the renter to prove that it controls the secret key
		// used to sign contract revisions. The actual data signed should be:
		//
		//    blake2b(RPCChallengePrefix | Challenge)
		Challenge [16]byte
	}
)

// EncodeTo implements modules.RequestBody.
func (r *loopKeyExchangeRequest) EncodeTo(e *types.Encoder) {
	// Nothing to do here.
}

// DecodeFrom implements modules.RequestBody.
func (r *loopKeyExchangeRequest) DecodeFrom(d *types.Decoder) {
	r.Specifier.DecodeFrom(d)
	d.Read(r.PublicKey[:])
	r.Ciphers = make([]types.Specifier, d.ReadPrefix())
	for i := range r.Ciphers {
		r.Ciphers[i].DecodeFrom(d)
	}
}

// EncodeTo implements modules.RequestBody.
func (r *loopKeyExchangeResponse) EncodeTo(e *types.Encoder) {
	e.Write(r.PublicKey[:])
	e.WriteBytes(r.Signature[:])
	r.Cipher.EncodeTo(e)
}

// DecodeFrom implements modules.RequestBody.
func (r *loopKeyExchangeResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements modules.RequestBody.
func (r *loopChallengeRequest) EncodeTo(e *types.Encoder) {
	e.Write(r.Challenge[:])
}

// DecodeFrom implements modules.RequestBody.
func (r *loopChallengeRequest) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// requestRequest is used when the renter requests the list of their
// active contracts.
type requestRequest struct {
	PubKey    types.PublicKey
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rr *requestRequest) DecodeFrom(d *types.Decoder) {
	d.Read(rr.PubKey[:])
	rr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rr *requestRequest) EncodeTo(e *types.Encoder) {
	e.Write(rr.PubKey[:])
}

// formRequest is used when the renter requests forming contracts with
// the hosts.
type formRequest struct {
	PubKey      types.PublicKey
	SecretKey   types.PrivateKey
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

	UploadPacking bool

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (fr *formRequest) DecodeFrom(d *types.Decoder) {
	d.Read(fr.PubKey[:])
	sk := d.ReadBytes()
	fr.SecretKey = types.PrivateKey(sk)
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
	fr.UploadPacking = d.ReadBool()
	fr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (fr *formRequest) EncodeTo(e *types.Encoder) {
	e.Write(fr.PubKey[:])
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
	e.WriteBool(fr.UploadPacking)
}

// renewRequest is used when the renter requests contract renewals.
type renewRequest struct {
	PubKey      types.PublicKey
	SecretKey   types.PrivateKey
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

	UploadPacking bool

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rr *renewRequest) DecodeFrom(d *types.Decoder) {
	d.Read(rr.PubKey[:])
	sk := d.ReadBytes()
	rr.SecretKey = types.PrivateKey(sk)
	numContracts := int(d.ReadUint64())
	rr.Contracts = make([]types.FileContractID, numContracts)
	for i := 0; i < numContracts; i++ {
		d.Read(rr.Contracts[i][:])
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
	rr.UploadPacking = d.ReadBool()
	rr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rr *renewRequest) EncodeTo(e *types.Encoder) {
	e.Write(rr.PubKey[:])
	e.WriteBytes(rr.SecretKey[:])
	e.WriteUint64(uint64(len(rr.Contracts)))
	for _, id := range rr.Contracts {
		e.Write(id[:])
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
	e.WriteBool(rr.UploadPacking)
}

// updateRequest is used when the renter submits a new revision.
type updateRequest struct {
	PubKey      types.PublicKey
	Contract    rhpv2.ContractRevision
	Uploads     types.Currency
	Downloads   types.Currency
	FundAccount types.Currency

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (ur *updateRequest) DecodeFrom(d *types.Decoder) {
	d.Read(ur.PubKey[:])
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
	e.Write(ur.PubKey[:])
	ur.Contract.Revision.EncodeTo(e)
	ur.Contract.Signatures[0].EncodeTo(e)
	ur.Contract.Signatures[1].EncodeTo(e)
	ur.Uploads.EncodeTo(e)
	ur.Downloads.EncodeTo(e)
	ur.FundAccount.EncodeTo(e)
}

// formContractRequest is used when forming a contract with a single
// host using the new Renter-Satellite protocol.
type formContractRequest struct {
	PubKey          types.PublicKey
	RenterPublicKey types.PublicKey
	HostPublicKey   types.PublicKey

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
	d.Read(fcr.PubKey[:])
	d.Read(fcr.RenterPublicKey[:])
	d.Read(fcr.HostPublicKey[:])
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
	e.Write(fcr.PubKey[:])
	e.Write(fcr.RenterPublicKey[:])
	e.Write(fcr.HostPublicKey[:])
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
	PubKey    types.PublicKey
	Contract  types.FileContractID
	EndHeight uint64

	Storage  uint64
	Upload   uint64
	Download uint64

	MinShards   uint64
	TotalShards uint64

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rcr *renewContractRequest) DecodeFrom(d *types.Decoder) {
	d.Read(rcr.PubKey[:])
	d.Read(rcr.Contract[:])
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
	e.Write(rcr.PubKey[:])
	e.Write(rcr.Contract[:])
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
	PubKey    types.PublicKey
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (gsr *getSettingsRequest) DecodeFrom(d *types.Decoder) {
	d.Read(gsr.PubKey[:])
	gsr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (gsr *getSettingsRequest) EncodeTo(e *types.Encoder) {
	e.Write(gsr.PubKey[:])
}

// getSettingsResponse is used to send the opt-in settings
// to the renter.
type getSettingsResponse struct {
	AutoRenewContracts bool
	BackupFileMetadata bool
	AutoRepairFiles    bool
	ProxyUploads       bool
}

// DecodeFrom implements requestBody.
func (gsr *getSettingsResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements requestBody.
func (gsr *getSettingsResponse) EncodeTo(e *types.Encoder) {
	e.WriteBool(gsr.AutoRenewContracts)
	e.WriteBool(gsr.BackupFileMetadata)
	e.WriteBool(gsr.AutoRepairFiles)
	e.WriteBool(gsr.ProxyUploads)
}

// updateSettingsRequest is used to update the renter's opt-in
// settings.
type updateSettingsRequest struct {
	PubKey             types.PublicKey
	AutoRenewContracts bool
	BackupFileMetadata bool
	AutoRepairFiles    bool
	ProxyUploads       bool
	PrivateKey         types.PrivateKey
	AccountKey         types.PrivateKey

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

	UploadPacking bool

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (usr *updateSettingsRequest) DecodeFrom(d *types.Decoder) {
	d.Read(usr.PubKey[:])
	usr.AutoRenewContracts = d.ReadBool()
	usr.BackupFileMetadata = d.ReadBool()
	usr.AutoRepairFiles = d.ReadBool()
	usr.ProxyUploads = d.ReadBool()
	if usr.AutoRenewContracts || usr.AutoRepairFiles {
		sk := d.ReadBytes()
		usr.PrivateKey = types.PrivateKey(sk)
	}
	if usr.AutoRepairFiles {
		ak := d.ReadBytes()
		usr.AccountKey = types.PrivateKey(ak)
	}
	if usr.AutoRenewContracts {
		usr.Hosts = d.ReadUint64()
		usr.Period = d.ReadUint64()
		usr.RenewWindow = d.ReadUint64()
		usr.Storage = d.ReadUint64()
		usr.Upload = d.ReadUint64()
		usr.Download = d.ReadUint64()
		usr.MinShards = d.ReadUint64()
		usr.TotalShards = d.ReadUint64()
		usr.MaxRPCPrice.DecodeFrom(d)
		usr.MaxContractPrice.DecodeFrom(d)
		usr.MaxDownloadPrice.DecodeFrom(d)
		usr.MaxUploadPrice.DecodeFrom(d)
		usr.MaxStoragePrice.DecodeFrom(d)
		usr.MaxSectorAccessPrice.DecodeFrom(d)
		usr.MinMaxCollateral.DecodeFrom(d)
		usr.BlockHeightLeeway = d.ReadUint64()
		usr.UploadPacking = d.ReadBool()
	}
	usr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (usr *updateSettingsRequest) EncodeTo(e *types.Encoder) {
	e.Write(usr.PubKey[:])
	e.WriteBool(usr.AutoRenewContracts)
	e.WriteBool(usr.BackupFileMetadata)
	e.WriteBool(usr.AutoRepairFiles)
	e.WriteBool(usr.ProxyUploads)
	if usr.AutoRenewContracts || usr.BackupFileMetadata || usr.AutoRepairFiles || usr.ProxyUploads {
		e.WriteBytes(usr.PrivateKey[:])
	}
	if usr.BackupFileMetadata || usr.AutoRepairFiles || usr.ProxyUploads {
		e.WriteBytes(usr.AccountKey[:])
	}
	if usr.AutoRenewContracts {
		e.WriteUint64(usr.Hosts)
		e.WriteUint64(usr.Period)
		e.WriteUint64(usr.RenewWindow)
		e.WriteUint64(usr.Storage)
		e.WriteUint64(usr.Upload)
		e.WriteUint64(usr.Download)
		e.WriteUint64(usr.MinShards)
		e.WriteUint64(usr.TotalShards)
		usr.MaxRPCPrice.EncodeTo(e)
		usr.MaxContractPrice.EncodeTo(e)
		usr.MaxDownloadPrice.EncodeTo(e)
		usr.MaxUploadPrice.EncodeTo(e)
		usr.MaxStoragePrice.EncodeTo(e)
		usr.MaxSectorAccessPrice.EncodeTo(e)
		usr.MinMaxCollateral.EncodeTo(e)
		e.WriteUint64(usr.BlockHeightLeeway)
		e.WriteBool(usr.UploadPacking)
	}
}

// saveMetadataRequest is used to accept file metadata and save it.
type saveMetadataRequest struct {
	PubKey    types.PublicKey
	Metadata  modules.FileMetadata
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (smr *saveMetadataRequest) DecodeFrom(d *types.Decoder) {
	d.Read(smr.PubKey[:])
	smr.Metadata.DecodeFrom(d)
	smr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (smr *saveMetadataRequest) EncodeTo(e *types.Encoder) {
	e.Write(smr.PubKey[:])
	smr.Metadata.EncodeTo(e)
}

// requestMetadataRequest is used to retrieve file metadata.
type requestMetadataRequest struct {
	PubKey         types.PublicKey
	PresentObjects []modules.BucketFiles
	Signature      types.Signature
}

// DecodeFrom implements requestBody.
func (rmr *requestMetadataRequest) DecodeFrom(d *types.Decoder) {
	d.Read(rmr.PubKey[:])
	rmr.PresentObjects = make([]modules.BucketFiles, d.ReadPrefix())
	for i := 0; i < len(rmr.PresentObjects); i++ {
		d.Read(rmr.PresentObjects[i].Name[:])
		rmr.PresentObjects[i].Paths = make([][255]byte, d.ReadPrefix())
		for j := 0; j < len(rmr.PresentObjects[i].Paths); j++ {
			d.Read(rmr.PresentObjects[i].Paths[j][:])
		}
	}
	rmr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rmr *requestMetadataRequest) EncodeTo(e *types.Encoder) {
	e.Write(rmr.PubKey[:])
	e.WritePrefix(len(rmr.PresentObjects))
	for _, po := range rmr.PresentObjects {
		e.Write(po.Name[:])
		e.WritePrefix(len(po.Paths))
		for _, p := range po.Paths {
			e.Write(p[:])
		}
	}
}

// requestMetadataResponse is a response type for requestMetadataRequest.
type requestMetadataResponse struct {
	metadata []modules.FileMetadata
}

// EncodeTo implements requestBody.
func (rmr requestMetadataResponse) EncodeTo(e *types.Encoder) {
	e.WritePrefix(len(rmr.metadata))
	for _, fm := range rmr.metadata {
		fm.EncodeTo(e)
	}
}

// DecodeFrom implements requestBody.
func (rmr requestMetadataResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// updateSlabRequest is used to update a single slab.
type updateSlabRequest struct {
	PubKey    types.PublicKey
	Slab      modules.Slab
	Packed    bool
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (usr *updateSlabRequest) DecodeFrom(d *types.Decoder) {
	d.Read(usr.PubKey[:])
	usr.Slab.DecodeFrom(d)
	usr.Packed = d.ReadBool()
	usr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (usr *updateSlabRequest) EncodeTo(e *types.Encoder) {
	e.Write(usr.PubKey[:])
	usr.Slab.EncodeTo(e)
	e.WriteBool(usr.Packed)
}

// requestSlabsRequest is used to retrieve modified slabs.
type requestSlabsRequest struct {
	PubKey    types.PublicKey
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (rsr *requestSlabsRequest) DecodeFrom(d *types.Decoder) {
	d.Read(rsr.PubKey[:])
	rsr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (rsr *requestSlabsRequest) EncodeTo(e *types.Encoder) {
	e.Write(rsr.PubKey[:])
}

// requestSlabsResponse is a response type for requestSlabsRequest.
type requestSlabsResponse struct {
	slabs []modules.Slab
}

// EncodeTo implements requestBody.
func (rsr requestSlabsResponse) EncodeTo(e *types.Encoder) {
	e.WritePrefix(len(rsr.slabs))
	for _, s := range rsr.slabs {
		s.EncodeTo(e)
	}
}

// DecodeFrom implements requestBody.
func (rsr requestSlabsResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// shareRequest is used when the renter submits a set of contracts.
type shareRequest struct {
	PubKey    types.PublicKey
	Contracts []modules.ContractMetadata

	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (sr *shareRequest) DecodeFrom(d *types.Decoder) {
	d.Read(sr.PubKey[:])
	sr.Contracts = make([]modules.ContractMetadata, d.ReadPrefix())
	for i := 0; i < len(sr.Contracts); i++ {
		sr.Contracts[i].DecodeFrom(d)
	}
	sr.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (sr *shareRequest) EncodeTo(e *types.Encoder) {
	e.Write(sr.PubKey[:])
	e.WritePrefix(len(sr.Contracts))
	for _, contract := range sr.Contracts {
		contract.EncodeTo(e)
	}
}

// uploadRequest is used when the renter wants to upload a file.
type uploadRequest struct {
	PubKey    types.PublicKey
	Bucket    [255]byte
	Path      [255]byte
	MimeType  [255]byte
	Signature types.Signature
}

// DecodeFrom implements requestBody.
func (ur *uploadRequest) DecodeFrom(d *types.Decoder) {
	d.Read(ur.PubKey[:])
	d.Read(ur.Bucket[:])
	d.Read(ur.Path[:])
	d.Read(ur.MimeType[:])
	ur.Signature.DecodeFrom(d)
}

// EncodeTo implements requestBody.
func (ur *uploadRequest) EncodeTo(e *types.Encoder) {
	e.Write(ur.PubKey[:])
	e.Write(ur.Bucket[:])
	e.Write(ur.Path[:])
	e.Write(ur.MimeType[:])
}

// uploadResponse is used to respond with the filesize already uploaded.
type uploadResponse struct {
	Filesize uint64
}

// DecodeFrom implements requestBody.
func (ur *uploadResponse) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements requestBody.
func (ur *uploadResponse) EncodeTo(e *types.Encoder) {
	e.WriteUint64(ur.Filesize)
}

// uploadData contains a chunk of data and an indicator if there is more.
type uploadData struct {
	Data []byte
	More bool
}

// DecodeFrom implements requestBody.
func (ud *uploadData) DecodeFrom(d *types.Decoder) {
	ud.Data = d.ReadBytes()
	ud.More = d.ReadBool()
}

// EncodeTo implements requestBody.
func (ud *uploadData) EncodeTo(e *types.Encoder) {
	// Nothing to do here.
}
