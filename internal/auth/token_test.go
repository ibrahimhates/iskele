package auth

import (
	"strings"
	"testing"
)

func TestNewRefreshTokenIsUniqueAndHashed(t *testing.T) {
	first, firstHash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	second, secondHash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}

	if first == second {
		t.Error("two refresh tokens collided")
	}
	if firstHash == secondHash {
		t.Error("two refresh token hashes collided")
	}
	if strings.Contains(firstHash, first) {
		t.Error("the hash contains the token")
	}
	if firstHash != HashToken(first) {
		t.Error("the returned hash does not match HashToken")
	}
	// 32 random bytes, base64url without padding.
	if len(first) < 40 {
		t.Errorf("token is %d characters, want at least 40 for 32 bytes of entropy", len(first))
	}
}

func TestNewAPITokenFormat(t *testing.T) {
	token, prefix, hash, err := NewAPIToken()
	if err != nil {
		t.Fatalf("NewAPIToken() error = %v", err)
	}

	if !strings.HasPrefix(token, APITokenPrefix+"_") {
		t.Errorf("token = %q, want the isk_ prefix so secret scanners can spot it", token)
	}
	if !strings.HasPrefix(token, prefix+"_") {
		t.Errorf("token %q does not start with its own prefix %q", token, prefix)
	}
	if strings.Count(token, "_") < 2 {
		t.Errorf("token = %q, want the isk_<prefix>_<secret> shape", token)
	}
	if hash != HashToken(token) {
		t.Error("the returned hash does not match HashToken")
	}
	if strings.Contains(hash, token) {
		t.Error("the hash contains the token")
	}

	// The prefix alone must not be enough to authenticate.
	if HashToken(prefix) == hash {
		t.Error("the prefix hashes to the same value as the full token")
	}
}

func TestAPITokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		token, prefix, _, err := NewAPIToken()
		if err != nil {
			t.Fatalf("NewAPIToken() error = %v", err)
		}
		if seen[token] {
			t.Fatal("duplicate API token")
		}
		if seen[prefix] {
			t.Fatal("duplicate API token prefix")
		}
		seen[token] = true
		seen[prefix] = true
	}
}

func TestIsAPIToken(t *testing.T) {
	token, _, _, err := NewAPIToken()
	if err != nil {
		t.Fatalf("NewAPIToken() error = %v", err)
	}

	if !IsAPIToken(token) {
		t.Errorf("IsAPIToken(%q) = false", token)
	}

	// A JWT must not be mistaken for an API token, or it would be looked up in
	// the wrong table and always fail.
	jwtLike := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1MSJ9.signature"
	for _, notToken := range []string{"", "isk", "isk_", jwtLike, "bearer isk_x_y"} {
		if IsAPIToken(notToken) {
			t.Errorf("IsAPIToken(%q) = true", notToken)
		}
	}
}

func TestHashTokenIsStable(t *testing.T) {
	first, second := HashToken("abc"), HashToken("abc")
	if first != second {
		t.Error("HashToken is not deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Error("HashToken collided on different inputs")
	}
	if len(HashToken("abc")) != 64 {
		t.Errorf("HashToken produced %d characters, want 64 hex characters", len(HashToken("abc")))
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if seen[id] {
			t.Fatal("duplicate id")
		}
		seen[id] = true
	}
}
