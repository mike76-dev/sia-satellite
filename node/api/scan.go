package api

import (
	"math/big"

	"errors"

	"go.sia.tech/core/types"
)

// scanAmount scans a types.Currency from a string.
func scanAmount(amount string) (types.Currency, bool) {
	// use SetString manually to ensure that amount does not contain
	// multiple values, which would confuse fmt.Scan.
	i, ok := new(big.Int).SetString(amount, 10)
	if !ok {
		return types.ZeroCurrency, false
	} else if i.Sign() < 0 {
		return types.ZeroCurrency, false
	} else if i.BitLen() > 128 {
		return types.ZeroCurrency, false
	}
	return types.NewCurrency(i.Uint64(), new(big.Int).Rsh(i, 64).Uint64()), true
}

// scanAddress scans a types.UnlockHash from a string.
func scanAddress(addrStr string) (addr types.Address, err error) {
	err = addr.UnmarshalText([]byte(addrStr))
	if err != nil {
		return types.Address{}, err
	}
	return addr, nil
}

// scanHash scans a types.Hash256 from a string.
func scanHash(s string) (h types.Hash256, err error) {
	err = h.UnmarshalText([]byte(s))
	if err != nil {
		return types.Hash256{}, err
	}
	return h, nil
}

// scanBool converts "true" and "false" strings to their respective
// boolean value and returns an error if conversion is not possible.
func scanBool(param string) (bool, error) {
	if param == "true" {
		return true, nil
	} else if param == "false" || len(param) == 0 {
		return false, nil
	}
	return false, errors.New("could not decode boolean: value was not true or false")
}
