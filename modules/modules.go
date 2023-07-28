package modules

import (
	"crypto/ed25519"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

var (
	// MaxRPCPrice is how much the Satellite is willing to pay
	// for a single RPC call.
	MaxRPCPrice = types.HastingsPerSiacoin.Div64(1e6)

	// MaxSectorAccessPrice is how much the Satellite is willing
	// to pay to download a single sector.
	MaxSectorAccessPrice = types.HastingsPerSiacoin.Div64(1e5)
)

// DeriveRenterSeed derives a seed to be used by the renter for accessing the
// file contracts.
// NOTE: The seed returned by this function should be wiped once it's no longer
// in use.
func DeriveRenterSeed(walletSeed Seed, email string) []byte {
	renterSeed := make([]byte, ed25519.SeedSize)
	rs := types.HashBytes(append(walletSeed[:], []byte(email)...))
	defer frand.Read(rs[:])
	copy(renterSeed, rs[:])
	return renterSeed
}

// DeriveEphemeralKey derives a secret key to be used by the renter for the
// exchange with the hosts.
func DeriveEphemeralKey(rsk types.PrivateKey, hpk types.PublicKey) types.PrivateKey {
	ers := types.HashBytes(append(rsk, hpk[:]...))
	defer frand.Read(ers[:])
	esk, _ := GenerateKeyPair(ers)
	return esk
}

// GenerateKeyPair generates a private/public keypair from a seed.
func GenerateKeyPair(seed types.Hash256) (sk types.PrivateKey, pk types.PublicKey) {
	sk = types.NewPrivateKeyFromSeed(seed[:])
	pk = sk.PublicKey()
	return
}
