// Package credentials separates public bearer digests from encrypted upstream credentials.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// NewToken generates a bearer independent of all control and upstream credentials.
func NewToken() string { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }

func randomBytes(size int) []byte {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic("secure randomness unavailable")
	}
	return b
}

// Digest is only appropriate for the high-entropy credentials generated here.
func Digest(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

// Vault encrypts only upstream runtime credentials; third-party OAuth tokens
// remain exclusively in OpenConnector. The deployment key is never persisted.
type Vault struct{ aead cipher.AEAD }

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("a 32-byte encryption key is required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// Seal authenticates the credential's project and record identity as AAD.
func (v *Vault) Seal(secret []byte, identity string) ([]byte, error) {
	if len(secret) == 0 || identity == "" {
		return nil, errors.New("credential and identity are required")
	}
	nonce := randomBytes(v.aead.NonceSize())
	return v.aead.Seal(nonce, nonce, secret, []byte(identity)), nil
}

func (v *Vault) Open(sealed []byte, identity string) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(sealed) < n || identity == "" {
		return nil, errors.New("invalid encrypted credential")
	}
	plain, err := v.aead.Open(nil, sealed[:n], sealed[n:], []byte(identity))
	if err != nil {
		return nil, errors.New("invalid encrypted credential")
	}
	return plain, nil
}
