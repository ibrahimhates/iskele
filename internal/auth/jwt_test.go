package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ibrahimhates/iskele/internal/store"
)

var testKey = []byte("test-signing-key-at-least-32-byte")

func testUser() store.User {
	return store.User{ID: "u1", Username: "alice", Role: store.RoleAdmin}
}

func TestIssueAndParse(t *testing.T) {
	issuer := NewTokenIssuer(testKey, 15*time.Minute)

	token, expiresAt, err := issuer.Issue(testUser())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if token == "" {
		t.Fatal("Issue() returned an empty token")
	}
	if time.Until(expiresAt) > 15*time.Minute+time.Second {
		t.Errorf("expiry = %v, want about 15 minutes out", expiresAt)
	}

	claims, err := issuer.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != "u1" || claims.Username != "alice" || claims.Role != store.RoleAdmin {
		t.Errorf("claims = %+v", claims)
	}
}

func TestEachTokenHasAUniqueID(t *testing.T) {
	issuer := NewTokenIssuer(testKey, time.Minute)

	first, _, _ := issuer.Issue(testUser())
	second, _, _ := issuer.Issue(testUser())

	c1, _ := issuer.Parse(first)
	c2, _ := issuer.Parse(second)

	if c1.ID == "" || c1.ID == c2.ID {
		t.Errorf("token IDs = %q / %q, want distinct non-empty values", c1.ID, c2.ID)
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	issuer := NewTokenIssuer(testKey, time.Minute)

	token, _, err := issuer.Issue(testUser())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	// Move the clock past the expiry rather than sleeping.
	issuer.SetClock(func() time.Time { return time.Now().Add(2 * time.Minute) })

	_, err = issuer.Parse(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("error = %v, want ErrTokenExpired", err)
	}
}

func TestParseRejectsAForeignSignature(t *testing.T) {
	issuer := NewTokenIssuer(testKey, time.Minute)
	token, _, _ := issuer.Issue(testUser())

	other := NewTokenIssuer([]byte("a completely different signing key!"), time.Minute)

	if _, err := other.Parse(token); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want ErrTokenInvalid", err)
	}
}

func TestParseRejectsAlgNone(t *testing.T) {
	// The classic JWT attack: strip the signature and claim the algorithm is
	// "none". Pinning HS256 must defeat it.
	claims := Claims{
		UserID:   "attacker",
		Username: "mallory",
		Role:     store.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "attacker",
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build unsigned token: %v", err)
	}

	if _, err := NewTokenIssuer(testKey, time.Minute).Parse(unsigned); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want an alg:none token to be rejected", err)
	}
}

func TestParseRejectsTamperedClaims(t *testing.T) {
	tokenIssuer := NewTokenIssuer(testKey, time.Minute)
	token, _, _ := tokenIssuer.Issue(store.User{ID: "u1", Username: "alice", Role: store.RoleViewer})

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	// Promote the viewer to admin without touching the signature.
	tampered := strings.Replace(string(payload), `"viewer"`, `"admin"`, 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(tampered))

	if _, err := tokenIssuer.Parse(strings.Join(parts, ".")); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want a tampered payload to be rejected", err)
	}
}

func TestParseRejectsAForeignIssuer(t *testing.T) {
	claims := Claims{
		UserID: "u1",
		Role:   store.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			Issuer:    "somebody-else",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := NewTokenIssuer(testKey, time.Minute).Parse(token); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want a foreign issuer to be rejected", err)
	}
}

func TestParseRejectsAnUnknownRole(t *testing.T) {
	// A validly signed token whose role is not in the matrix must fail closed
	// rather than being handed to the RBAC check.
	claims := Claims{
		UserID: "u1",
		Role:   "superuser",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := NewTokenIssuer(testKey, time.Minute).Parse(token); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want an unknown role to be rejected", err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	tokenIssuer := NewTokenIssuer(testKey, time.Minute)

	for _, bad := range []string{"", "not.a.token", "a.b", strings.Repeat("x", 100)} {
		if _, err := tokenIssuer.Parse(bad); err == nil {
			t.Errorf("Parse(%q) error = nil, want a rejection", bad)
		}
	}
}
