package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"too short", "Short1!", ErrPasswordTooShort},
		{"exactly at the floor minus one", strings.Repeat("aB1", 3) + "x", ErrPasswordTooShort},
		{"long but single class", strings.Repeat("a", 20), ErrPasswordTooWeak},
		{"two classes", "correct-horse-battery-staple1", nil},
		{"passphrase with punctuation", "correct horse battery staple!", nil},
		{"mixed case only", "CorrectHorseBattery", nil},
		{"too long", strings.Repeat("aB1", 400), ErrPasswordTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidatePassword() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestMinPasswordLengthMatchesTheRequirement(t *testing.T) {
	// PROMPT §4.1 mandates 12 characters; a regression here weakens every
	// account on the installation.
	if MinPasswordLength != 12 {
		t.Errorf("MinPasswordLength = %d, want 12", MinPasswordLength)
	}
	if err := ValidatePassword(strings.Repeat("aB1", 4)); err != nil {
		t.Errorf("a 12-character password was rejected: %v", err)
	}
}

func TestHashAndVerify(t *testing.T) {
	const password = "correct-horse-battery-Staple-1"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if strings.Contains(hash, password) {
		t.Fatal("the hash contains the password")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want the PHC argon2id format", hash)
	}

	ok, err := VerifyPassword(hash, password)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("the correct password did not verify")
	}

	ok, err = VerifyPassword(hash, password+"x")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if ok {
		t.Error("a wrong password verified")
	}
}

func TestHashIsSaltedPerCall(t *testing.T) {
	const password = "correct-horse-battery-Staple-1"

	first, _ := HashPassword(password)
	second, _ := HashPassword(password)

	if first == second {
		t.Error("hashing the same password twice produced identical output")
	}

	// Both must still verify: the salt travels with the hash.
	for _, h := range []string{first, second} {
		if ok, err := VerifyPassword(h, password); err != nil || !ok {
			t.Errorf("hash %q did not verify: ok=%v err=%v", h, ok, err)
		}
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	tests := map[string]string{
		"empty":            "",
		"not phc":          "plaintext",
		"wrong algorithm":  "$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"missing sections": "$argon2id$v=19$m=65536,t=3,p=2",
		"bad base64":       "$argon2id$v=19$m=65536,t=3,p=2$!!!$!!!",
		"bad version":      "$argon2id$v=99$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"empty salt":       "$argon2id$v=19$m=65536,t=3,p=2$$aGFzaA",
	}

	for name, hash := range tests {
		t.Run(name, func(t *testing.T) {
			ok, err := VerifyPassword(hash, "anything")
			if err == nil {
				t.Fatal("VerifyPassword() error = nil, want a malformed-hash error")
			}
			if !errors.Is(err, ErrInvalidHash) {
				t.Errorf("error = %v, want ErrInvalidHash", err)
			}
			if ok {
				t.Error("a malformed hash reported a successful verification")
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := HashPassword("correct-horse-battery-Staple-1")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if NeedsRehash(current) {
		t.Error("a freshly created hash was flagged for rehashing")
	}

	// A hash made with weaker parameters must be upgraded on next login.
	weak := "$argon2id$v=19$m=4096,t=1,p=1$c2FsdHNhbHRzYWx0MTI$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhcw"
	if !NeedsRehash(weak) {
		t.Error("a weak hash was not flagged for rehashing")
	}

	// So must an unparsable one, so corrupt data self-heals on next login.
	if !NeedsRehash("garbage") {
		t.Error("an unparsable hash was not flagged for rehashing")
	}
}
