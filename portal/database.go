package portal

import (
	"encoding/hex"
	"runtime"

	"golang.org/x/crypto/argon2"
)

const (
	// argon2Salt is the salt for the password hashing algorithm.
	argon2Salt = "SiaSatellitePasswordHashingSalt."
)

// countEmails counts all accounts with the given email
// address. There should be at most one per address.
func (p *Portal) countEmails(email string) (count int, err error) {
	err = p.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE email = ?", email).Scan(&count)
	return
}

// createAccount creates a new user account in the database.
func (p *Portal) createAccount(email, password string) error {
	pwHash := passwordHash(password)
	_, err := p.db.Exec("INSERT INTO accounts (email, password_hash, verified) VALUES (?, ?, ?)", email, pwHash, false)
	return err
}

// passwordHash implements the Argon2id hashing mechanism.
func passwordHash(password string) string {
	t := uint8(runtime.NumCPU())
	hash := argon2.IDKey([]byte(password), []byte(argon2Salt), 1, 64 * 1024, t, 32)
	return hex.EncodeToString(hash)
}
