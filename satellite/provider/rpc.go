package provider

import (
	"net"
	"time"

	"gitlab.com/NebulousLabs/encoding"
	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
)

type keyRequest struct {
	Specifier types.Specifier
	PubKey    crypto.PublicKey
	Signature crypto.Signature
}

// managedRequestKey verifies the renter's public key and sends the satellite's
// public key in exchange.
func (p *Provider) managedRequestKey(conn net.Conn) error {
	// Extend the deadline to meet the exchange.
	conn.SetDeadline(time.Now().Add(requestKeyTime))

	// Read the data.
	var renterPK crypto.PublicKey
	var signature crypto.Signature
	err := encoding.ReadObject(conn, &renterPK, 256)
	if err != nil {
		return errors.Extend(errors.New("could not read renter public key"), err)
	}
	err = encoding.ReadObject(conn, &signature, 256)
	if err != nil {
		return errors.Extend(errors.New("could not read renter signature"), err)
	}

	// Verify the renter's signature.
	s := publicKeySpecifier
	err = crypto.VerifyHash(crypto.HashBytes(append(s[:], renterPK[:]...)), renterPK, signature)
	if err != nil {
		return errors.Extend(errors.New("could not verify renter signature"), err)
	}

	// Check if we know this renter.
	rpk := types.Ed25519PublicKey(renterPK)
	exists, err := p.Satellite.UserExists(rpk)
	if !exists || err != nil {
		return errors.Extend(errors.New("could not find renter in the database"), err)
	}

	// Respond with the specifier, satellite's public key and signature.
	spk := p.Satellite.PublicKey().Key
	kr := keyRequest{
		Specifier: s,
		Signature: crypto.SignHash(crypto.HashBytes(append(s[:], spk...)), p.Satellite.SecretKey()),
	}
	copy(kr.PubKey[:], spk)
	err = encoding.WriteObject(conn, kr)
	if err != nil {
		return errors.Extend(errors.New("could not write signature"), err)
	}

	return nil
}
