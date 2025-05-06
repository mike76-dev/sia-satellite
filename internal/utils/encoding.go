package utils

import (
	"bytes"

	"go.sia.tech/core/types"
)

// EncodeCurrency encodes a currency value.
func EncodeCurrency(c types.Currency) []byte {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	types.V2Currency(c).EncodeTo(e)
	e.Flush()
	return buf.Bytes()
}
