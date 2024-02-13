package modules

import (
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

// DeriveRenterKey derives a key to be used by the renter for accessing the
// file contracts.
// NOTE: The key returned by this function should be wiped once it's no longer
// in use.
func DeriveRenterKey(masterKey types.PrivateKey, email string) []byte {
	rs := types.HashBytes(append(masterKey, []byte(email)...))
	defer frand.Read(rs[:])
	pk := types.NewPrivateKeyFromSeed(rs[:])
	return pk
}

// DeriveEphemeralKey derives a secret key to be used by the renter for the
// exchange with the host.
func DeriveEphemeralKey(rsk types.PrivateKey, hpk types.PublicKey) types.PrivateKey {
	ers := types.HashBytes(append(rsk, hpk[:]...))
	defer frand.Read(ers[:])
	esk, _ := GenerateKeyPair(ers)
	return esk
}

// DeriveAccountKey derives an account key to be used by the renter to
// access the ephemeral account at the host.
func DeriveAccountKey(ak types.PrivateKey, hpk types.PublicKey) types.PrivateKey {
	ers := types.HashBytes(append(append(ak, hpk[:]...), byte(0)))
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
