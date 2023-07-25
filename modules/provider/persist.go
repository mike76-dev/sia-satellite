package provider

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"golang.org/x/crypto/ed25519"

	"lukechampine.com/frand"
)

// establishDefaults configures the default settings for the Provider,
// overwriting any existing settings.
func (p *Provider) establishDefaults() {
	// Generate the provider's key pair.
	epk, esk, _ := ed25519.GenerateKey(frand.Reader)

	p.secretKey = types.PrivateKey(esk)
	copy(p.publicKey[:], epk)

	// The generated keys are important, save them.
	err := p.save()
	if err != nil {
		p.log.Println("ERROR: failed to save provider persistence:", err)
	}
}

// load loads the Provider's persistent data from disk.
func (p *Provider) load() error {
	var c int
	err := p.db.QueryRow("SELECT COUNT(*) FROM pr_info WHERE id = 1").Scan(&c)
	if err != nil {
		return modules.AddContext(err, "failed to load provider persistence")
	}

	if c == 0 {
		p.establishDefaults()
		return nil
	}

	pk := make([]byte, 32)
	sk := make([]byte, 64)
	var addr string
	err = p.db.QueryRow(`
		SELECT public_key, secret_key, address
		FROM pr_info
		WHERE id = 1
	`).Scan(&pk, &sk, &addr)
	if err != nil {
		return modules.AddContext(err, "failed to load provider persistence")
	}

	copy(p.publicKey[:], pk)
	p.secretKey = types.PrivateKey(sk)
	p.autoAddress = modules.NetAddress(addr)

	return nil
}

// save saves the Provider's persistent data to disk.
func (p *Provider) save() error {
	_, err := p.db.Exec(`
		REPLACE INTO pr_info (id, public_key, secret_key, address)
		VALUES (1, ?, ?, ?)
	`, p.publicKey[:], p.secretKey, p.autoAddress)
	
	return err
}
