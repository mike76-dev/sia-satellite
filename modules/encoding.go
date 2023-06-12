package modules

import (
	"go.sia.tech/core/types"
)

// WriteUint64 writes an uint64 to the underlying stream.
func WriteUint64(i uint64, e *types.Encoder) {
	e.WriteUint64(8)
	e.WriteUint64(i)
	e.Flush()
}

// WriteBool writes a bool to the underlying stream.
func WriteBool(b bool, e *types.Encoder) {
	e.WriteUint64(1)
	e.WriteBool(b)
	e.Flush()
}

// WriteString writes a string to the underlying stream.
func WriteString(s string, e *types.Encoder) {
	e.WritePrefix(len(s) + 8)
	e.WriteString(s)
	e.Flush()
}

// WriteBytes writes a byte slice to the underlying stream.
func WriteBytes(b []byte, e *types.Encoder) {
	e.WritePrefix(len(b) + 8)
	e.WriteBytes(b)
	e.Flush()
}

// ReadUint64 reads an uint64 from the underlying stream.
func ReadUint64(d *types.Decoder) uint64 {
	_ = d.ReadUint64()
	if err := d.Err(); err != nil {
		return 0
	}
	return d.ReadUint64()
}

// ReadBool reads a bool from the underlying stream.
func ReadBool(d *types.Decoder) bool {
	_ = d.ReadUint64()
	if err := d.Err(); err != nil {
		return false
	}
	return d.ReadBool()
}

// ReadString reads a string from the underlying stream.
func ReadString(d *types.Decoder) string {
	_ = d.ReadUint64()
	if err := d.Err(); err != nil {
		return ""
	}
	return d.ReadString()
}

// ReadBytes reads a byte slice from the underlying stream.
func ReadBytes(d *types.Decoder) []byte {
	_ = d.ReadUint64()
	if err := d.Err(); err != nil {
		return make([]byte, 0)
	}
	return d.ReadBytes()
}
