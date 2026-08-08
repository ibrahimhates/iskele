package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// refreshTokenBytes is the entropy of a refresh token. 32 bytes is well past
// what a remote attacker can search.
const refreshTokenBytes = 32

// apiTokenSecretBytes is the entropy of the secret half of an API token.
const apiTokenSecretBytes = 32

// apiTokenPrefixBytes is the entropy of the public identifying half.
const apiTokenPrefixBytes = 6

// APITokenPrefix marks Iskele tokens so secret scanners can recognize them.
const APITokenPrefix = "isk"

// randomToken returns a URL-safe random string with n bytes of entropy.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// randomID returns a short random identifier for database rows and JWT IDs.
func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// NewID exposes the identifier generator to the service layer, so IDs are
// produced the same way everywhere.
func NewID() (string, error) { return randomID() }

// HashToken returns the hex SHA-256 of a token.
//
// Refresh and API tokens are high-entropy random strings, so a plain hash is
// enough: there is nothing to brute-force, unlike a password. Storing only the
// hash means a database leak does not hand over live credentials.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewRefreshToken returns a refresh token and its storable hash.
func NewRefreshToken() (token, hash string, err error) {
	token, err = randomToken(refreshTokenBytes)
	if err != nil {
		return "", "", err
	}
	return token, HashToken(token), nil
}

// NewAPIToken returns a token in the form isk_<prefix>_<secret>, along with
// the public prefix and the storable hash.
//
// The prefix lets the UI identify a token in a list, and lets an operator who
// finds one in a log know which entry to revoke, without the secret.
func NewAPIToken() (token, prefix, hash string, err error) {
	prefixPart, err := randomToken(apiTokenPrefixBytes)
	if err != nil {
		return "", "", "", err
	}
	secret, err := randomToken(apiTokenSecretBytes)
	if err != nil {
		return "", "", "", err
	}

	prefix = APITokenPrefix + "_" + prefixPart
	token = prefix + "_" + secret
	return token, prefix, HashToken(token), nil
}

// IsAPIToken reports whether a bearer credential looks like an Iskele API
// token rather than a JWT, so the middleware knows which path to take.
func IsAPIToken(value string) bool {
	return len(value) > len(APITokenPrefix)+1 && value[:len(APITokenPrefix)+1] == APITokenPrefix+"_"
}
