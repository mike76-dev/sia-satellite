package modules

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

// MinMessageSize is the minimum size of an RPC message. If an encoded message
// would be smaller than minMessageSize, the sender MAY pad it with random data.
// This hinders traffic analysis by obscuring the true sizes of messages.
const MinMessageSize = 4096

// RequestBody is the common interface type for the renter requests.
type RequestBody interface {
	DecodeFrom(d *types.Decoder)
	EncodeTo(e *types.Encoder)
}

// An RPCSession contains the state of an RPC session with a renter.
type RPCSession struct {
	Conn      net.Conn
	Aead      cipher.AEAD
	Challenge [16]byte
}

// readMessage reads an encrypted message from the renter.
func (s *RPCSession) readMessage(message RequestBody, maxLen uint64) error {
	if maxLen < MinMessageSize {
		maxLen = MinMessageSize
	}
	d := types.NewDecoder(io.LimitedReader{R: s.Conn, N: int64(8 + maxLen)})
	msgSize := d.ReadUint64()
	if d.Err() != nil {
		return d.Err()
	} else if msgSize > maxLen {
		return fmt.Errorf("message size (%v bytes) exceeds maxLen of %v bytes", msgSize, maxLen)
	} else if msgSize < uint64(s.Aead.NonceSize() + s.Aead.Overhead()) {
		return fmt.Errorf("message size (%v bytes) is too small (nonce + MAC is %v bytes)", msgSize, s.Aead.NonceSize() + s.Aead.Overhead())
	}
	ciphertext := make([]byte, msgSize)
	d.Read(ciphertext)
	if err := d.Err(); err != nil {
		return err
	}
	nonce := ciphertext[:s.Aead.NonceSize()]
	paddedPayload := ciphertext[s.Aead.NonceSize():]
	plaintext, err := s.Aead.Open(paddedPayload[:0], nonce, paddedPayload, nil)
	if err != nil {
		return err
	}
	b := types.NewBufDecoder(plaintext)
	message.DecodeFrom(b)

	return err
}

// writeMessage sends an encrypted message to the renter.
func (s *RPCSession) writeMessage(message RequestBody) error {
	nonce := make([]byte, 32)[:s.Aead.NonceSize()]
	frand.Read(nonce)

	var buf bytes.Buffer
	buf.Grow(MinMessageSize)
	e := types.NewEncoder(&buf)
	e.WritePrefix(0) // Placeholder.
	e.Write(nonce)
	message.EncodeTo(e)
	e.Flush()

	// Overwrite message length.
	msgSize := buf.Len() + s.Aead.Overhead()
	if msgSize < MinMessageSize {
		msgSize = MinMessageSize
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
	rr := RPCResponse{nil, resp}
	if err := s.readMessage(&rr, maxLen); err != nil {
		return err
	} else if rr.err != nil {
		return rr.err
	}
	return nil
}

// ReadRequest reads an encrypted RPC request from the renter.
func (s *RPCSession) ReadRequest(req RequestBody, maxLen uint64) (types.Hash256, error) {
	err := s.readMessage(req, maxLen)
	if err != nil {
		return types.Hash256{}, err
	}

	// Calculate the hash.
	h := types.NewHasher()
	req.EncodeTo(h.E)

	return h.Sum(), nil
}

// RPCResponse if a helper type for encoding and decoding RPC response
// messages, which can represent either valid data or an error.
type RPCResponse struct {
	err  *RPCError
	data RequestBody
}

// EncodeTo implements types.EncoderTo.
func (resp *RPCResponse) EncodeTo(e *types.Encoder) {
	e.WriteBool(resp.err != nil)
	if resp.err != nil {
		resp.err.EncodeTo(e)
		return
	}
	if resp.data != nil {
		resp.data.EncodeTo(e)
	}
}

// DecodeFrom implements types.DecoderFrom.
func (resp *RPCResponse) DecodeFrom(d *types.Decoder) {
	if d.ReadBool() {
		resp.err = new(RPCError)
		resp.err.DecodeFrom(d)
		return
	}
	resp.data.DecodeFrom(d)
}

// RPCError is the generic error transferred in an RPC.
type RPCError struct {
	Type        types.Specifier
	Data        []byte
	Description string
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return e.Description
}

// EncodeTo implements types.EncoderTo.
func (re *RPCError) EncodeTo(e *types.Encoder) {
	e.Write(re.Type[:])
	e.WriteBytes(re.Data)
	e.WriteString(re.Description)
}

// DecodeFrom implements types.DecoderFrom.
func (re *RPCError) DecodeFrom(d *types.Decoder) {
	re.Type.DecodeFrom(d)
	re.Data = d.ReadBytes()
	re.Description = d.ReadString()
}
