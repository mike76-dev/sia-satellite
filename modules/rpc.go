package modules

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"io"
	"net"

	"gitlab.com/NebulousLabs/fastrand"

	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
)

// RequestBody is the common interface type for the renter requests.
type RequestBody interface {
	DecodeFrom(d *core.Decoder)
	EncodeTo(e *core.Encoder)
}

// An RPCSession contains the state of an RPC session with a renter.
type RPCSession struct {
	Conn      net.Conn
	Aead      cipher.AEAD
	Challenge [16]byte
}

// ReadRequest reads an encrypted RPC request from the renter.
func (s *RPCSession) ReadRequest(req RequestBody, maxLen uint64) (core.Hash256, error) {
	d := core.NewDecoder(io.LimitedReader{R: s.Conn, N: int64(maxLen)})
	ciphertext := d.ReadBytes()
	if err := d.Err(); err != nil {
		return core.Hash256{}, err
	}
	plaintext, err := crypto.DecryptWithNonce(ciphertext, s.Aead)
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

// WriteMessage sends an encrypted message to the renter.
func (s *RPCSession) writeMessage(message RequestBody) error {
	nonce := make([]byte, 32)[:s.Aead.NonceSize()]
	fastrand.Read(nonce)

	var buf bytes.Buffer
	buf.Grow(4096)
	e := core.NewEncoder(&buf)
	e.WritePrefix(0) // Placeholder.
	e.Write(nonce)
	message.EncodeTo(e)
	e.Flush()

	// Overwrite message length.
	msgSize := buf.Len() + s.Aead.Overhead()
	if msgSize < 4096 {
		msgSize = 4096
	}
	buf.Grow(s.Aead.Overhead())
	msg := buf.Bytes()[:msgSize]
	binary.LittleEndian.PutUint64(msg[:8], uint64(msgSize - 8))

	// Encrypt the response in-place.
	msgNonce := msg[8:][:len(nonce)]
	payload := msg[8 + len(nonce) : msgSize - s.Aead.Overhead()]
	s.Aead.Seal(payload[:0], msgNonce, payload, nil)

	_, err := s.Conn.Write(msg)

	return err
}

// WriteResponse sends an encrypted RPC responce to the renter.
func (s *RPCSession) WriteResponse(resp RequestBody) error {
	return s.writeMessage(&RPCResponse{nil, resp})
}

// WriteError sends an error message to the renter.
func (s *RPCSession) WriteError(err error) error {
	var re *RPCError
	if err != nil {
		re = &RPCError{Description: err.Error()}
	}
	return s.writeMessage(&RPCResponse{re, nil})
}

// ReadResponse reads an encrypted RPC response from the renter.
func (s *RPCSession) ReadResponse(resp RequestBody, maxLen uint64) error {
	d := core.NewDecoder(io.LimitedReader{R: s.Conn, N: int64(maxLen)})
	ciphertext := d.ReadBytes()
	if err := d.Err(); err != nil {
		return err
	}
	plaintext, err := crypto.DecryptWithNonce(ciphertext, s.Aead)
	if err != nil {
		return err
	}
	b := core.NewBufDecoder(plaintext)
	rr := RPCResponse{nil, resp}
	rr.DecodeFrom(b)
	if err != nil {
		return err
	} else if rr.err != nil {
		return rr.err
	}

	return nil
}

// RPCResponse if a helper type for encoding and decoding RPC response
// messages, which can represent either valid data or an error.
type RPCResponse struct {
	err  *RPCError
	data RequestBody
}

// EncodeTo implements core.ProtocolObject.
func (resp *RPCResponse) EncodeTo(e *core.Encoder) {
	e.WriteBool(resp.err != nil)
	if resp.err != nil {
		resp.err.EncodeTo(e)
		return
	}
	if resp.data != nil {
		resp.data.EncodeTo(e)
	}
}

// DecodeFrom implements core.ProtocolObject.
func (resp *RPCResponse) DecodeFrom(d *core.Decoder) {
	if d.ReadBool() {
		resp.err = new(RPCError)
		resp.err.DecodeFrom(d)
		return
	}
	resp.data.DecodeFrom(d)
}

// RPCError is the generic error transferred in an RPC.
type RPCError struct {
	Type        core.Specifier
	Data        []byte
	Description string
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return e.Description
}

// EncodeTo implements core.ProtocolObject.
func (re *RPCError) EncodeTo(e *core.Encoder) {
	e.Write(re.Type[:])
	e.WriteBytes(re.Data)
	e.WriteString(re.Description)
}

// DecodeFrom implements core.ProtocolObject.
func (re *RPCError) DecodeFrom(d *core.Decoder) {
	re.Type.DecodeFrom(d)
	re.Data = d.ReadBytes()
	re.Description = d.ReadString()
}
