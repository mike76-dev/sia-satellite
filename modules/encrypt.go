package modules

import (
	"crypto/cipher"
	"errors"

	"golang.org/x/crypto/twofish"

	"lukechampine.com/frand"
)

// ErrInsufficientLen is an error when supplied ciphertext is not
// long enough to contain a nonce.
var ErrInsufficientLen = errors.New("supplied ciphertext is not long enough to contain a nonce")

// createCipher returns an initialized TwoFish cipher.
func createCipher(key WalletKey) (cipher.AEAD, error) {
	c, err := twofish.NewCipher(key[:])
	if err != nil {
		return nil, errors.New("NewCipher only returns an error if len(key) != 16, 24, or 32.")
	}
	return cipher.NewGCM(c)
}

// Encrypt encrypts the plaintext with the provided key.
func Encrypt(key WalletKey, plaintext []byte) ([]byte, error) {
	aead, err := createCipher(key)
	if err != nil {
		return nil, errors.New("NewGCM only returns an error if twofishCipher.BlockSize != 16")
	}
	nonce := frand.Bytes(aead.NonceSize())
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts the ciphertext using the provided key.
func Decrypt(key WalletKey, ciphertext []byte) ([]byte, error) {
	aead, err := createCipher(key)
	if err != nil {
		return nil, errors.New("NewGCM only returns an error if twofishCipher.BlockSize != 16")
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, ErrInsufficientLen
	}
	nonce, ciphertext := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}
