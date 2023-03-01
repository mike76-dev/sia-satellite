package modules

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
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
	copy(buf[16 - len(b):], b[:])
	return core.NewCurrency(binary.BigEndian.Uint64(buf[8:]), binary.BigEndian.Uint64(buf[:8]))
}

// CurrencyUnits converts a types.Currency to a string with human-readable
// units. The unit used will be the largest unit that results in a value
// greater than 1. The value is rounded to 4 significant digits.
func CurrencyUnits(c types.Currency) string {
	pico := types.SiacoinPrecision.Div64(1e12)
	if c.Cmp(pico) < 0 {
		return c.String() + " H"
	}

	// Iterate until we find a unit greater than c.
	mag := pico
	unit := ""
	for _, unit = range []string{"pS", "nS", "uS", "mS", "SC", "KS", "MS", "GS", "TS"} {
		if c.Cmp(mag.Mul64(1e3)) < 0 {
			break
		} else if unit != "TS" {
			// Don't want to perform this multiply on the last iter; that
			// would give us 1.235 TS instead of 1235 TS.
			mag = mag.Mul64(1e3)
		}
	}

	num := new(big.Rat).SetInt(c.Big())
	denom := new(big.Rat).SetInt(mag.Big())
	res, _ := new(big.Rat).Mul(num, denom.Inv(denom)).Float64()

	return fmt.Sprintf("%.4g %s", res, unit)
}
