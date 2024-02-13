package modules

import (
	"bytes"
	"errors"
	"io"

	"go.sia.tech/core/types"
)

var (
	// prefixHostAnnouncement is used to indicate that a transaction's
	// Arbitrary Data field contains a host announcement. The encoded
	// announcement will follow this prefix.
	prefixHostAnnouncement = types.NewSpecifier("HostAnnouncement")

	// errAnnNotAnnouncement indicates that the provided host announcement does
	// not use a recognized specifier, indicating that it's either not a host
	// announcement or it's not a recognized version of a host announcement.
	errAnnNotAnnouncement = errors.New("provided data does not form a recognized host announcement")

	// errAnnUnrecognizedSignature is returned when the signature in a host
	// announcement is not a type of signature that is recognized.
	errAnnUnrecognizedSignature = errors.New("the signature provided in the host announcement is not recognized")
)

// hostAnnouncement is an announcement by the host that appears in the
// blockchain. 'Specifier' is always 'prefixHostAnnouncement'. The
// announcement is always followed by a signature from the public key of
// the whole announcement.
type hostAnnouncement struct {
	Specifier  types.Specifier
	NetAddress NetAddress
	PublicKey  types.UnlockKey
}

// EncodeTo implements types.EncoderTo.
func (ha *hostAnnouncement) EncodeTo(e *types.Encoder) {
	ha.Specifier.EncodeTo(e)
	e.WriteString(string(ha.NetAddress))
	ha.PublicKey.EncodeTo(e)
}

// DecodeFrom implements types.DecoderFrom.
func (ha *hostAnnouncement) DecodeFrom(d *types.Decoder) {
	ha.Specifier.DecodeFrom(d)
	ha.NetAddress = NetAddress(d.ReadString())
	ha.PublicKey.DecodeFrom(d)
}

// DecodeAnnouncement decodes announcement bytes into a host announcement,
// verifying the prefix and the signature.
func DecodeAnnouncement(fullAnnouncement []byte) (na NetAddress, pk types.PublicKey, err error) {
	// Read the first part of the announcement to get the intended host
	// announcement.
	var ha hostAnnouncement
	d := types.NewDecoder(io.LimitedReader{R: bytes.NewBuffer(fullAnnouncement), N: int64(len(fullAnnouncement) * 3)})
	ha.DecodeFrom(d)
	if err = d.Err(); err != nil {
		return "", types.PublicKey{}, err
	}

	// Check that the announcement was registered as a host announcement.
	if ha.Specifier != prefixHostAnnouncement {
		return "", types.PublicKey{}, errAnnNotAnnouncement
	}
	// Check that the public key is a recognized type of public key.
	if ha.PublicKey.Algorithm != types.SpecifierEd25519 {
		return "", types.PublicKey{}, errAnnUnrecognizedSignature
	}

	// Read the signature out of the reader.
	var sig types.Signature
	sig.DecodeFrom(d)
	if err = d.Err(); err != nil {
		return "", types.PublicKey{}, err
	}

	// Verify the signature.
	copy(pk[:], ha.PublicKey.Key)
	h := types.NewHasher()
	ha.EncodeTo(h.E)
	annHash := h.Sum()
	if ok := pk.VerifyHash(annHash, sig); !ok {
		return "", types.PublicKey{}, errAnnUnrecognizedSignature
	}

	return ha.NetAddress, pk, nil
}

// DecodeV2Announcement verifies a V2 host announcement against the signature.
func DecodeV2Announcement(at types.Attestation) (na string, pk types.PublicKey, err error) {
	if at.Key != "HostAnnouncement" {
		return "", types.PublicKey{}, errAnnNotAnnouncement
	}

	pk = at.PublicKey
	h := types.NewHasher()
	at.EncodeTo(h.E)
	annHash := h.Sum()
	if ok := pk.VerifyHash(annHash, at.Signature); !ok {
		return "", types.PublicKey{}, errAnnUnrecognizedSignature
	}

	return string(at.Value), pk, nil
}
