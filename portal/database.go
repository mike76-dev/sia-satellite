package portal

import (
	"encoding/hex"
	"errors"
	"runtime"
	"time"

	"gitlab.com/NebulousLabs/fastrand"

	"golang.org/x/crypto/argon2"
)

const (
	// argon2Salt is the salt for the password hashing algorithm.
	argon2Salt = "SiaSatellitePasswordHashingSalt."

	// pruneUnverifiedAccountsFrequency is how often unverified
	// accounts are pruned from the database.
	pruneUnverifiedAccountsFrequency = 2 * time.Hour

	// pruneUnverifiedAccountsThreshold is how old an unverified
	// account needs to be to get pruned.
	pruneUnverifiedAccountsThreshold = 7 * 24 * time.Hour
)

// countEmails counts all accounts with the given email
// address. There should be at most one per address.
func (p *Portal) countEmails(email string) (count int, err error) {
	err = p.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE email = ?", email).Scan(&count)
	return
}

// isVerified checks if the user account is verified. If password
// is not empty, it also checks if the password matches the one
// in the database.
func (p *Portal) isVerified(email, password string) (verified bool, ok bool, err error) {
	pwHash := ""
	if password != "" {
		pwHash = passwordHash(password)
	}
	var ph string
	var v bool
	err = p.db.QueryRow("SELECT password_hash, verified FROM accounts WHERE email = ?", email).Scan(&ph, &v)
	return v, (ph == pwHash), err
}

// updateAccount updates the user account in the database.
// If the account does not exist yet, it is created.
func (p *Portal) updateAccount(email, password string, verified bool) error {
	c, err := p.countEmails(email)
	if err != nil {
		return err
	}

	// No entries, create a new account.
	if c == 0 {
		if password == "" {
			return errors.New("password may not be empty")
		}
		pwHash := passwordHash(password)
		_, err := p.db.Exec("INSERT INTO accounts (email, password_hash, verified, created) VALUES (?, ?, ?, ?)", email, pwHash, false, time.Now().Unix())
		return err
	}

	// An entry found, update it.
	if password == "" {
		_, err := p.db.Exec("UPDATE accounts SET verified = ? WHERE email = ?", verified, email)
		return err
	}
	pwHash := passwordHash(password)
	_, err = p.db.Exec("UPDATE accounts SET password_hash = ?, verified = ? WHERE email = ?", pwHash, verified, email)
	return err
}

// passwordHash implements the Argon2id hashing mechanism.
func passwordHash(password string) string {
	t := uint8(runtime.NumCPU())
	hash := argon2.IDKey([]byte(password), []byte(argon2Salt), 1, 64 * 1024, t, 32)
	defer fastrand.Read(hash[:])
	return hex.EncodeToString(hash)
}

// threadedPruneUnverifiedAccounts deletes unverified user accounts
// from the database.
func (p *Portal) threadedPruneUnverifiedAccounts() {
	for {
		select {
		case <-p.threads.StopChan():
			return
		case <-time.After(pruneUnverifiedAccountsFrequency):
		}

		func() {
			err := p.threads.Add()
			if err != nil {
				return
			}
			defer p.threads.Done()

			p.mu.Lock()
			defer p.mu.Unlock()

			now := time.Now().Unix()
			_, err = p.db.Exec("DELETE FROM accounts WHERE verified = FALSE AND created < ?", now - pruneUnverifiedAccountsThreshold.Milliseconds() / 1000)
			if err != nil {
				p.log.Printf("ERROR: error querying database: %v\n", err)
			}
		}()
	}
}

// deleteAccount deletes the user account from the database.
func (p *Portal) deleteAccount(email string) error {
	_, err := p.db.Exec("DELETE FROM accounts WHERE email = ?", email)
	return err
}
