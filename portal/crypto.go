package portal

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"time"

	"github.com/dchest/threefish"
)

var (
	// verifyPrefix is used for generating a verification token.
	verifyPrefix = authPrefix{'V', 'e', 'r', 'i', 'f', 'y', 0, 0}

	// resetPrefix is used for generating a password reset token.
	resetPrefix = authPrefix{'P', 'W', 'R', 'e', 's', 'e', 't', 0}
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

// generateToken generates an authorization token. Only the first
// 48 bytes of the email address are used.
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
