package modules

import (
	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
)

// Provider implements the methods necessary to communicate with the
// renters.
type Provider interface {
	Alerter

	// Close safely shuts down the provider.
	Close() error

	// PublicKey returns the provider's public key.
	PublicKey() types.PublicKey

	// SecretKey returns the provider's secret key.
	SecretKey() types.PrivateKey
}

// ExtendedContract contains the contract and its metadata.
type ExtendedContract struct {
	Contract            rhpv2.ContractRevision
	StartHeight         uint64
	TotalCost           types.Currency
	UploadSpending      types.Currency
	DownloadSpending    types.Currency
	FundAccountSpending types.Currency
	RenewedFrom         types.FileContractID
}

// EncodeTo implements requestBody.
func (ec ExtendedContract) EncodeTo(e *types.Encoder) {
	ec.Contract.Revision.EncodeTo(e)
	ec.Contract.Signatures[0].EncodeTo(e)
	ec.Contract.Signatures[1].EncodeTo(e)
	e.WriteUint64(ec.StartHeight)
	ec.TotalCost.EncodeTo(e)
	ec.UploadSpending.EncodeTo(e)
	ec.DownloadSpending.EncodeTo(e)
	ec.FundAccountSpending.EncodeTo(e)
	ec.RenewedFrom.EncodeTo(e)
}

// DecodeFrom implements requestBody.
func (ec ExtendedContract) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// ExtendedContractSet is a collection of extendedContracts.
type ExtendedContractSet struct {
	Contracts []ExtendedContract
}

// EncodeTo implements requestBody.
func (ecs ExtendedContractSet) EncodeTo(e *types.Encoder) {
	e.WritePrefix(len(ecs.Contracts))
	for _, ec := range ecs.Contracts {
		ec.EncodeTo(e)
	}
}

// DecodeFrom implements requestBody.
func (ecs ExtendedContractSet) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}
