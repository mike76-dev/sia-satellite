package utils

import (
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/wallet"
)

// NewPrivateKey generates a new 64-byte private key.
func NewPrivateKey() types.PrivateKey {
	phrase := wallet.NewSeedPhrase()
	var seed [32]byte
	if err := wallet.SeedFromPhrase(&seed, phrase); err != nil {
		panic(err)
	}

	return wallet.KeyFromSeed(&seed, 0)
}
