package portal

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/dchest/threefish"
)

var (
	// verifyPrefix is used for generating a verification token.
	verifyPrefix = authPrefix{'V', 'e', 'r', 'i', 'f', 'y', 0, 0}

	// resetPrefix is used for generating a password reset token.
	resetPrefix = authPrefix{'P', 'W', 'R', 'e', 's', 'e', 't', 0}

	// changePrefix is used to authenticate a password change.
	changePrefix = authPrefix{'P', 'W', 'C', 'h', 'a', 'n', 'g', 'e'}

	// cookiePrefix is used for generating client-side cookies.
	cookiePrefix = authPrefix{'C', 'o', 'o', 'k', 'i', 'e', 0, 0}
)

type (
	// authPrefix is the same as [8]byte.
	authPrefix [8]byte

	// authToken contains the fields required to authorize a user.
	authToken struct {
		Prefix  authPrefix
		Email   []byte
		Expires int64
	}
)

// generateToken generates an authorization token.
func (p *Portal) generateToken(prefix authPrefix, email string, expires time.Time) string {
	// Generate a new Threefish cipher.
	key := p.satellite.SecretKey()
	cipher, err := threefish.NewCipher(key[:], make([]byte, threefish.TweakSize))
	if err != nil {
		log.Fatalln("Wrong key length for a Threefish cipher")
	}

	// Encrypt the data.
	src := make([]byte, 64)
	dst := make([]byte, 64)
	copy(src[:8], prefix[:])
	copy(src[8:56], email[:])
	binary.BigEndian.PutUint64(src[56:], uint64(expires.Unix()))
	cipher.Encrypt(dst, src)

	return hex.EncodeToString(dst)
}

func (p *Portal) decodeToken(token string) (prefix authPrefix, email string, expires time.Time, err error) {
	// Convert hex to bytes.
	b, err := hex.DecodeString(token)
	if err != nil {
		return authPrefix{}, "", time.Unix(0, 0), err
	}
	if len(b) != 64 {
		return authPrefix{}, "", time.Unix(0, 0), errors.New("Wrong token length")
	}

	// Generate a new Threefish cipher.
	key := p.satellite.SecretKey()
	cipher, err := threefish.NewCipher(key[:], make([]byte, threefish.TweakSize))
	if err != nil {
		log.Fatalln("Wrong key length for a Threefish cipher")
	}

	// Decrypt the data.
	src := make([]byte, 64)
	dst := make([]byte, 64)
	copy(src[:], b[:])
	cipher.Decrypt(dst, src)
	at := authToken{
		Email: make([]byte, 48),
	}
	copy(at.Prefix[:], dst[:8])
	copy(at.Email[:], dst[8:56])
	at.Expires = int64(binary.BigEndian.Uint64(dst[56:]))

	// Find the length of email.
	l := bytes.IndexByte(at.Email[:], 0)

	return at.Prefix, string(at.Email[:l]), time.Unix(at.Expires, 0), nil
}
