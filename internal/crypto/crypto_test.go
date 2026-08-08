package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateKeyGeneratesOnFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	if key == (Key{}) {
		t.Fatal("generated key is all zeroes")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != keyFileMode {
		t.Errorf("permissions = %#o, want %#o", perm, keyFileMode)
	}
}

func TestLoadOrCreateKeyIsStableAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	first, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}
	second, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}

	if first != second {
		t.Error("the key changed between calls; every stored secret would be lost")
	}
}

func TestLoadOrCreateKeyCreatesMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "iskele", "secret.key")

	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("key file was not created: %v", err)
	}
}

func TestLoadOrCreateKeyRejectsLoosePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatalf("setup error = %v", err)
	}

	// A key another local account can read protects nothing.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, err := LoadOrCreateKey(path)
	if !errors.Is(err, ErrKeyPermissions) {
		t.Fatalf("error = %v, want ErrKeyPermissions", err)
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error = %q, want it to say how to fix it", err)
	}
}

func TestLoadOrCreateKeyRejectsGroupReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatalf("setup error = %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := LoadOrCreateKey(path); !errors.Is(err, ErrKeyPermissions) {
		t.Errorf("error = %v, want group-readable to be rejected", err)
	}
}

func TestLoadOrCreateKeyRejectsMalformedContent(t *testing.T) {
	tests := map[string]string{
		"not hex":   "zzzz",
		"too short": hex.EncodeToString([]byte("short")),
		"empty":     "",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret.key")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}

			if _, err := LoadOrCreateKey(path); err == nil {
				t.Fatal("LoadOrCreateKey() error = nil, want a malformed-key error")
			}
		})
	}
}

func TestKeyFileToleratesTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	raw := bytes.Repeat([]byte{0xAB}, KeySize)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(raw)+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	if !bytes.Equal(key[:], raw) {
		t.Error("key was not parsed correctly")
	}
}

func TestKeyStringDoesNotLeakMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	key, _ := LoadOrCreateKey(path)

	if s := key.String(); strings.Contains(s, hex.EncodeToString(key[:8])) {
		t.Errorf("String() = %q, want the key redacted", s)
	}
}

func TestDeriveIsDeterministicAndPurposeSeparated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	key, _ := LoadOrCreateKey(path)

	a1 := key.Derive("jwt")
	a2 := key.Derive("jwt")
	b := key.Derive("secretbox")

	if !bytes.Equal(a1, a2) {
		t.Error("Derive is not deterministic")
	}
	if bytes.Equal(a1, b) {
		t.Error("different purposes produced the same subkey")
	}
	if bytes.Equal(a1, key[:]) {
		t.Error("the derived key equals the master key")
	}
	if len(a1) != 32 {
		t.Errorf("derived key is %d bytes, want 32", len(a1))
	}
}

func newBox(t *testing.T) *SecretBox {
	t.Helper()
	key, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatalf("key error = %v", err)
	}
	box, err := NewSecretBox(key)
	if err != nil {
		t.Fatalf("NewSecretBox() error = %v", err)
	}
	return box
}

func TestSecretBoxRoundTrip(t *testing.T) {
	box := newBox(t)

	secrets := []string{
		"hunter2",
		"a very long registry password with spaces and ünïcödé",
		"{\"json\":\"value\"}",
	}
	for _, secret := range secrets {
		sealed, err := box.Encrypt(secret)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if strings.Contains(sealed, secret) {
			t.Fatal("the ciphertext contains the plaintext")
		}

		opened, err := box.Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		if opened != secret {
			t.Errorf("Decrypt() = %q, want %q", opened, secret)
		}
	}
}

func TestSecretBoxEmptyStringStaysEmpty(t *testing.T) {
	box := newBox(t)

	sealed, err := box.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if sealed != "" {
		t.Errorf("Encrypt(\"\") = %q, want an empty string so an unset secret stays unset", sealed)
	}

	opened, err := box.Decrypt("")
	if err != nil || opened != "" {
		t.Errorf("Decrypt(\"\") = %q, %v", opened, err)
	}
}

func TestSecretBoxUsesAFreshNonce(t *testing.T) {
	box := newBox(t)

	first, _ := box.Encrypt("same secret")
	second, _ := box.Encrypt("same secret")

	if first == second {
		t.Error("encrypting the same value twice produced identical ciphertext")
	}
}

func TestSecretBoxRejectsTamperedCiphertext(t *testing.T) {
	box := newBox(t)
	sealed, _ := box.Encrypt("hunter2")

	// Flip a character in the middle of the payload.
	tampered := []byte(sealed)
	mid := len(tampered) / 2
	if tampered[mid] == 'A' {
		tampered[mid] = 'B'
	} else {
		tampered[mid] = 'A'
	}

	if _, err := box.Decrypt(string(tampered)); !errors.Is(err, ErrDecrypt) {
		t.Errorf("error = %v, want ErrDecrypt for tampered data", err)
	}
}

func TestSecretBoxRejectsGarbage(t *testing.T) {
	box := newBox(t)

	for _, bad := range []string{"not base64!!", "AAAA"} {
		if _, err := box.Decrypt(bad); !errors.Is(err, ErrDecrypt) {
			t.Errorf("Decrypt(%q) error = %v, want ErrDecrypt", bad, err)
		}
	}
}

func TestSecretBoxCannotOpenAnotherKeysCiphertext(t *testing.T) {
	first := newBox(t)
	second := newBox(t)

	sealed, _ := first.Encrypt("hunter2")

	if _, err := second.Decrypt(sealed); !errors.Is(err, ErrDecrypt) {
		t.Errorf("error = %v, want a foreign key to be rejected", err)
	}
}
