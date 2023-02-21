package modules

import (
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"strings"

	core "go.sia.tech/core/types"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
)

// ReadCurrency converts a string to types.Currency.
func ReadCurrency(s string) types.Currency {
	i := new(big.Int)
	i, ok := i.SetString(s, 10)
	if ok {
		return types.NewCurrency(i)
	}
	return types.ZeroCurrency
}

// ReadPublicKey converts a string to types.SiaPublicKey.
func ReadPublicKey(s string) types.SiaPublicKey {
	if !strings.HasPrefix(s, "ed25519:") {
		return types.SiaPublicKey{}
	}
	s = strings.TrimPrefix(s, "ed25519:")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != crypto.PublicKeySize {
		return types.SiaPublicKey{}
	}
	var pk crypto.PublicKey
	copy(pk[:], b)
	return types.Ed25519PublicKey(pk)
}

// ConvertCurrency converts a siad currency to a core currency.
func ConvertCurrency(c types.Currency) core.Currency {
	b := c.Big().Bytes()
	buf := make([]byte, 16)
	copy(buf[:], b[:])
	return core.NewCurrency(binary.BigEndian.Uint64(buf[8:]), binary.BigEndian.Uint64(buf[:8]))
}
