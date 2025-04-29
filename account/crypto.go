package account

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"runtime"
	"time"

	"github.com/dchest/threefish"
	"github.com/mike76-dev/sia-satellite/internal/utils"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"lukechampine.com/frand"
)

const (
	// argon2Salt is the salt for the password hashing algorithm.
	argon2Salt = "SiaSatellitePasswordHashingSalt."
)

var (
	// ResetPrefix is used for generating a password reset token.
	ResetPrefix = AuthPrefix{'P', 'W', 'R', 'e', 's', 'e', 't'}

	// ChangePrefix is used for generating a password change token.
	ChangePrefix = AuthPrefix{'P', 'W', 'C', 'h', 'a', 'n', 'g', 'e'}

	// CookiePrefix is used for generating client-side cookies.
	CookiePrefix = AuthPrefix{'C', 'o', 'o', 'k', 'i', 'e'}

	// APIPrefix is used for generating an API token.
	APIPrefix = AuthPrefix{'A', 'P', 'I', 't', 'o', 'k', 'e', 'n'}

	// threeFishTweak is the tweak for the ThreeFish block cipher.
	threeFishTweak = [16]byte{'S', 'i', 'a', '-', 'S', 'a', 't', 'e', 'l', 'l', 'i', 't', 'e'}
)

type (
	// AuthPrefix is the same as [8]byte.
	AuthPrefix [8]byte

	// authToken contains the fields required to authorize a user.
	authToken struct {
		Prefix  AuthPrefix
		Email   []byte
		Expires int64
	}
)

// GenerateToken generates an authorization token.
func (am *AccountManager) GenerateToken(prefix AuthPrefix, email string, expires time.Time) (string, error) {
	// Read the nonce or generate a new one if it doesn't exist.
	nonce, err := am.readNonce(email)
	if err != nil {
		return "", utils.AddContext(err, "couldn't read nonce")
	}

	if bytes.Equal(nonce, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		frand.Read(nonce)
		if err := am.saveNonce(email, nonce); err != nil {
			return "", utils.AddContext(err, "couldn't update nonce")
		}
	}

	// Generate a new Threefish cipher.
	cipher, err := threefish.NewCipher(am.key, threeFishTweak[:])
	if err != nil {
		return "", err
	}

	// Encrypt the data.
	src := make([]byte, 128)
	dst := make([]byte, 128)
	copy(src[:8], prefix[:])
	copy(src[8:24], nonce[:])
	copy(src[24:88], email[:])
	binary.BigEndian.PutUint64(src[88:96], uint64(expires.Unix()))
	checksum := blake2b.Sum256(src[:96])
	copy(src[96:], checksum[:])
	cipher.Encrypt(dst[:64], src[:64])
	cipher.Encrypt(dst[64:], src[64:])

	return hex.EncodeToString(dst), nil
}

// DecodeToken decodes the provided token.
func (am *AccountManager) DecodeToken(token string) (AuthPrefix, string, time.Time, error) {
	// Convert hex to bytes.
	b, err := hex.DecodeString(token)
	if err != nil {
		return AuthPrefix{}, "", time.Unix(0, 0), err
	}
	if len(b) != 128 {
		return AuthPrefix{}, "", time.Unix(0, 0), errors.New("wrong token length")
	}

	// Generate a new Threefish cipher.
	cipher, err := threefish.NewCipher(am.key, threeFishTweak[:])
	if err != nil {
		return AuthPrefix{}, "", time.Unix(0, 0), errors.New("wrong key length")
	}

	// Decrypt the data.
	src := make([]byte, 128)
	dst := make([]byte, 128)
	copy(src[:], b[:])
	cipher.Decrypt(dst[:64], src[:64])
	cipher.Decrypt(dst[64:], src[64:])
	at := authToken{
		Email: make([]byte, 64),
	}
	copy(at.Prefix[:], dst[:8])
	nonce := make([]byte, 16)
	copy(nonce[:], dst[8:24])
	copy(at.Email[:], dst[24:88])
	at.Expires = int64(binary.BigEndian.Uint64(dst[88:96]))

	// Verify the checksum.
	checksum := blake2b.Sum256(dst[:96])
	if !bytes.Equal(checksum[:], dst[96:]) {
		return AuthPrefix{}, "", time.Unix(0, 0), errors.New("invalid checksum")
	}

	// Find the length of email.
	l := bytes.IndexByte(at.Email[:], 0)
	email := string(at.Email[:l])

	// Verify the nonce.
	ok, err := am.verifyNonce(email, nonce)
	if err != nil {
		return AuthPrefix{}, "", time.Unix(0, 0), utils.AddContext(err, "couldn't verify nonce")
	} else if !ok {
		return AuthPrefix{}, "", time.Unix(0, 0), errors.New("invalid nonce")
	}

	return at.Prefix, email, time.Unix(at.Expires, 0), nil
}

// passwordHash implements the Argon2id hashing mechanism.
func passwordHash(password string) (pwh []byte) {
	t := uint8(runtime.NumCPU())
	return argon2.IDKey([]byte(password), []byte(argon2Salt), 1, 64*1024, t, 32)
}
