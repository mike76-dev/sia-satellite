package wallet

import (
	"bytes"
	"encoding/binary"

	"go.sia.tech/core/types"
)

func encodeCurrency(c types.Currency) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint64(buf, c.Lo)
	binary.LittleEndian.PutUint64(buf[8:], c.Hi)
	return buf
}

func decodeCurrency(buf []byte) types.Currency {
	if len(buf) != 16 {
		panic("wrong currency length")
	}
	var c types.Currency
	c.Lo = binary.LittleEndian.Uint64(buf)
	c.Hi = binary.LittleEndian.Uint64(buf[8:])
	return c
}

func encodeProof(proof []types.Hash256) []byte {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	e.WritePrefix(len(proof))
	for _, hash := range proof {
		hash.EncodeTo(e)
	}
	e.Flush()
	return buf.Bytes()
}

func decodeProof(buf []byte) []types.Hash256 {
	d := types.NewBufDecoder(buf)
	l := d.ReadPrefix()
	if err := d.Err(); err != nil {
		panic(err)
	}
	proof := make([]types.Hash256, l)
	for i := range proof {
		proof[i].DecodeFrom(d)
		if err := d.Err(); err != nil {
			panic(err)
		}
	}
	return proof
}
