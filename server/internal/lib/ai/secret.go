package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

var ErrSecret = errors.New("invalid encrypted secret")

// SealSecret encrypts a credential using an external 32-byte key. The returned
// envelope starts with a version byte; the caller must store the key separately.
func SealSecret(key, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrSecret
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrSecret
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, ErrSecret
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ErrSecret
	}
	out := append([]byte{1}, nonce...)
	return g.Seal(out, nonce, plaintext, []byte("hsg/provider-secret/v1")), nil
}

// OpenSecret authenticates and decrypts a versioned credential envelope.
func OpenSecret(key, envelope []byte) ([]byte, error) {
	if len(key) != 32 || len(envelope) == 0 || envelope[0] != 1 {
		return nil, ErrSecret
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrSecret
	}
	g, err := cipher.NewGCM(b)
	if err != nil || len(envelope) < 1+g.NonceSize()+g.Overhead() {
		return nil, ErrSecret
	}
	n := g.NonceSize()
	p, err := g.Open(nil, envelope[1:1+n], envelope[1+n:], []byte("hsg/provider-secret/v1"))
	if err != nil {
		return nil, ErrSecret
	}
	return p, nil
}
