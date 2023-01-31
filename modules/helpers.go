package modules

import (
	"math/big"

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
