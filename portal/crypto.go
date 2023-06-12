package portal

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dchest/threefish"

	"lukechampine.com/frand"
)

var (
	// verifyPrefix is used for generating a verification token.
	verifyPrefix = authPrefix{"Verify"}

	// resetPrefix is used for generating a password reset token.
	resetPrefix = authPrefix{"PWReset"}

	// changePrefix is used to authenticate a password change.
	changePrefix = authPrefix{"PWChange"}

	// cookiePrefix is used for generating client-side cookies.
	cookiePrefix = authPrefix{"Cookie"}

	// threeFishTweak is the tweak for the ThreeFish block cipher.
	threeFishTweak = [16]byte{"Sia-Satellite"}
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
func (p *Portal) generateToken(prefix authPrefix, email string, expires time.Time) (string, error) {
	// Generate a new Threefish cipher.
	key := p.satellite.SecretKey()
	cipher, err := threefish.NewCipher(key[:], threeFishTweak[:])
	if err != nil {
		return "", err
	}

	// Encrypt the data.
	src := make([]byte, 128)
	dst := make([]byte, 128)
	copy(src[:8], prefix[:])
	copy(src[8:72], email[:])
	binary.BigEndian.PutUint64(src[72:80], uint64(expires.Unix()))
	nonce := make([]byte, 16)
	frand.Read(nonce)
	copy(src[80:96], nonce[:])
	cipher.Encrypt(dst[:64], src[:64])
	cipher.Encrypt(dst[64:], src[64:])
	err = p.saveNonce(email, nonce)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(dst), nil
}

func (p *Portal) decodeToken(token string) (authPrefix, string, time.Time, error) {
	// Convert hex to bytes.
	b, err := hex.DecodeString(token)
	if err != nil {
		return authPrefix{}, "", time.Unix(0, 0), err
	}
	if len(b) != 128 {
		return authPrefix{}, "", time.Unix(0, 0), errors.New("wrong token length")
	}

	// Generate a new Threefish cipher.
	key := p.satellite.SecretKey()
	cipher, err := threefish.NewCipher(key[:], threeFishTweak[:])
	if err != nil {
		return authPrefix{}, "", time.Unix(0, 0), errors.New("wrong key length")
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
	copy(at.Email[:], dst[8:72])
	at.Expires = int64(binary.BigEndian.Uint64(dst[72:80]))
	nonce := make([]byte, 16)
	copy(nonce[:], dst[80:96])

	// Find the length of email.
	l := bytes.IndexByte(at.Email[:], 0)
	email := string(at.Email[:l])

	// Verify the nonce.
	ok, err := p.verifyNonce(email, nonce)
	if err != nil {
		return authPrefix{}, "", time.Unix(0, 0), errors.New("couldn't verify nonce")
	}
	if !ok {
		return authPrefix{}, "", time.Unix(0, 0), errors.New("invalid nonce")
	}

	return at.Prefix, email, time.Unix(at.Expires, 0), nil
}
