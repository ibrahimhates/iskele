// Package crypto owns the master key and the symmetric encryption used for
// secrets that must be stored (registry passwords, TOTP secrets, tunnel
// tokens) and for deriving the JWT signing key.
package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// KeySize is the length of the master key in bytes (AES-256).
const KeySize = 32

// keyFileMode is the only permission the key file may carry. Anything wider
// would let another local account read every stored secret.
const keyFileMode fs.FileMode = 0o600

// ErrKeyPermissions is returned when the key file is readable by anyone but
// its owner.
var ErrKeyPermissions = errors.New("key file permissions are too permissive")

// Key is the master key. It is never logged or serialized.
type Key [KeySize]byte

// String hides the key from accidental formatting.
func (k Key) String() string { return "crypto.Key(redacted)" }

// LoadOrCreateKey reads the master key from path, generating one on first run.
//
// The file is created with 0600 and its permissions are verified on every
// start: a key another local user can read is a key that no longer protects
// anything, so that is a startup failure rather than a warning.
func LoadOrCreateKey(path string) (Key, error) {
	var key Key

	data, err := os.ReadFile(path) //nolint:gosec // operator-configured path
	switch {
	case err == nil:
		if permErr := checkPermissions(path); permErr != nil {
			return key, permErr
		}
		return parseKey(path, data)

	case os.IsNotExist(err):
		return createKey(path)

	default:
		return key, fmt.Errorf("read key file %s: %w", path, err)
	}
}

func parseKey(path string, data []byte) (Key, error) {
	var key Key

	decoded, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return key, fmt.Errorf("key file %s is not valid hex: %w", path, err)
	}
	if len(decoded) != KeySize {
		return key, fmt.Errorf("key file %s holds %d bytes, want %d", path, len(decoded), KeySize)
	}
	copy(key[:], decoded)
	return key, nil
}

func createKey(path string) (Key, error) {
	var key Key

	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("generate key: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return key, fmt.Errorf("create key directory %s: %w", dir, err)
		}
	}

	// O_EXCL so a key created by a concurrent start is never overwritten:
	// silently replacing it would make every stored secret undecryptable.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, keyFileMode) //nolint:gosec // operator-configured path
	if err != nil {
		if os.IsExist(err) {
			return LoadOrCreateKey(path)
		}
		return key, fmt.Errorf("create key file %s: %w", path, err)
	}

	if _, err := f.WriteString(hex.EncodeToString(key[:]) + "\n"); err != nil {
		_ = f.Close()
		return key, fmt.Errorf("write key file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return key, fmt.Errorf("close key file %s: %w", path, err)
	}
	return key, nil
}

// checkPermissions rejects a key file that group or others can read.
func checkPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat key file %s: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%w: %s is %#o, want %#o (chmod 600 %s)",
			ErrKeyPermissions, path, perm, keyFileMode, path)
	}
	return nil
}

// Derive produces a purpose-specific subkey from the master key.
//
// Different uses (JWT signing, secret encryption) get independent keys, so
// compromising one does not compromise the other.
func (k Key) Derive(purpose string) []byte {
	mac := hmac.New(sha256.New, k[:])
	mac.Write([]byte("iskele/v1/" + purpose))
	return mac.Sum(nil)
}
