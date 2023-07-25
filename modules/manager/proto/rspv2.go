package proto

import (
	"go.sia.tech/core/types"
)

// signRequest is used to send the revision hash to the renter.
type signRequest struct {
	RevisionHash types.Hash256
}

// DecodeFrom implements rhpv2.ProtocolObject.
func (sr *signRequest) DecodeFrom(d *types.Decoder) {
	// Nothing to do here.
}

// EncodeTo implements rhpv2.ProtocolObject.
func (sr *signRequest) EncodeTo(e *types.Encoder) {
	sr.RevisionHash.EncodeTo(e)
}

// signResponse is used to receive the revision signature from the renter.
type signResponse struct {
	Signature types.Signature
}

// DecodeFrom implements rhpv2.ProtocolObject.
func (sr *signResponse) DecodeFrom(d *types.Decoder) {
	sr.Signature.DecodeFrom(d)
}

// EncodeTo implements rhpv2.ProtocolObject.
func (sr *signResponse) EncodeTo(e *types.Encoder) {
	// Nothing to do here.
}
