package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// secretPurpose separates the encryption subkey from the signing subkey.
const secretPurpose = "secretbox"

// ErrDecrypt is returned when a ciphertext cannot be authenticated, whether
// because it was tampered with or because the key changed.
var ErrDecrypt = errors.New("cannot decrypt: wrong key or corrupted data")

// SecretBox encrypts short secrets with AES-256-GCM.
type SecretBox struct {
	aead cipher.AEAD
}

// NewSecretBox builds a SecretBox from the master key.
func NewSecretBox(key Key) (*SecretBox, error) {
	block, err := aes.NewCipher(key.Derive(secretPurpose))
	if err != nil {
		return nil, fmt.Errorf("build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

// Encrypt seals plaintext and returns a base64 string safe to store in a text
// column. The random nonce is prefixed to the ciphertext.
//
// An empty plaintext round-trips as an empty string, so an unset secret does
// not become a decryptable blob.
func (s *SecretBox) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := s.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt.
func (s *SecretBox) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: not valid base64", ErrDecrypt)
	}

	nonceSize := s.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("%w: ciphertext is too short", ErrDecrypt)
	}

	plaintext, err := s.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		// The underlying error is deliberately not wrapped: distinguishing
		// "wrong key" from "tampered" would tell an attacker which it was.
		return "", ErrDecrypt
	}
	return string(plaintext), nil
}
