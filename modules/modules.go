package modules

import (
	"crypto/ed25519"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
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
	xsk := types.NewPrivateKeyFromSeed(seed[:])
	defer frand.Read(seed[:])
	copy(sk, xsk)
	xpk := sk.PublicKey()
	copy(pk[:], xpk[:])
	return
}
