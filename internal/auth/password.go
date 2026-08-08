// Package auth implements password hashing, tokens, sessions and the
// brute-force limiter.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is the floor PROMPT §4.1 mandates.
const MinPasswordLength = 12

// maxPasswordLength bounds the input so a huge body cannot turn one login
// attempt into a memory-hard denial of service.
const maxPasswordLength = 1024

// Argon2id parameters. Time and memory are chosen so a hash costs roughly
// 50-100ms on a modest single-board server, which is affordable per login and
// expensive per guess.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// Password errors. They are deliberately specific for the caller's validation
// messages, but the login handler must never reveal which one occurred.
var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d characters", maxPasswordLength)
	ErrPasswordTooWeak  = errors.New("password must contain at least two of: lowercase, uppercase, digit, symbol")
	ErrInvalidHash      = errors.New("stored password hash is malformed")
)

// ValidatePassword enforces the password policy.
//
// The rule is length-first with a light character-class requirement: a long
// passphrase is stronger than a short scrambled string, so the floor is 12
// characters and the class check only rules out the most trivial inputs.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	if classesIn(password) < 2 {
		return ErrPasswordTooWeak
	}
	return nil
}

func classesIn(s string) int {
	var lower, upper, digit, symbol bool
	for _, r := range s {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	n := 0
	for _, present := range []bool{lower, upper, digit, symbol} {
		if present {
			n++
		}
	}
	return n
}

// HashPassword derives an argon2id hash in the standard PHC string format, so
// the parameters travel with the hash and can be raised later without
// invalidating existing passwords.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded hash.
//
// The comparison is constant-time, and a malformed stored hash is an error
// rather than a silent mismatch so the operator learns their data is corrupt.
func VerifyPassword(encodedHash, password string) (bool, error) {
	params, salt, want, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	// The stored key length is bounded by decodeHash's base64 parsing of a
	// hash this package produced, so the conversion cannot overflow.
	keyLen := uint32(len(want)) //nolint:gosec // bounded by the stored hash

	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, keyLen)

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type hashParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func decodeHash(encoded string) (hashParams, []byte, []byte, error) {
	var p hashParams

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return p, nil, nil, fmt.Errorf("%w: unsupported argon2 version %d", ErrInvalidHash, version)
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return p, nil, nil, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return p, nil, nil, ErrInvalidHash
	}

	return p, salt, key, nil
}

// NeedsRehash reports whether a stored hash was made with weaker parameters
// than the current ones, so it can be upgraded on the next successful login.
func NeedsRehash(encodedHash string) bool {
	params, _, _, err := decodeHash(encodedHash)
	if err != nil {
		return true
	}
	return params.memory < argonMemory || params.time < argonTime || params.threads < argonThreads
}
