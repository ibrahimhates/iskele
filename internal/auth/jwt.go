package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ibrahimhates/iskele/internal/store"
)

// JWTPurpose is the key-derivation label for the token signing key. Deriving
// it separately keeps a compromise of the secret-encryption key from also
// letting an attacker mint tokens.
const JWTPurpose = "jwt-signing"

// issuer identifies tokens minted by this daemon.
const issuer = "iskele"

// Token errors.
var (
	// ErrTokenInvalid covers a bad signature, a wrong algorithm, a wrong
	// issuer and malformed input. They are one error on purpose: telling a
	// caller which check failed helps only an attacker.
	ErrTokenInvalid = errors.New("invalid token")
	// ErrTokenExpired is separate because the frontend uses it to decide
	// whether to attempt a refresh.
	ErrTokenExpired = errors.New("token expired")
)

// Claims is the payload of an access token.
type Claims struct {
	UserID   string     `json:"sub"`
	Username string     `json:"username"`
	Role     store.Role `json:"role"`
	jwt.RegisteredClaims
}

// TokenIssuer mints and validates access tokens.
type TokenIssuer struct {
	key       []byte
	accessTTL time.Duration
	now       func() time.Time
}

// NewTokenIssuer builds an issuer from the master key material.
func NewTokenIssuer(signingKey []byte, accessTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{
		key:       signingKey,
		accessTTL: accessTTL,
		now:       time.Now,
	}
}

// SetClock replaces the time source. Tests use it to reach expiry without
// sleeping.
func (t *TokenIssuer) SetClock(now func() time.Time) { t.now = now }

// AccessTTL reports the configured access-token lifetime.
func (t *TokenIssuer) AccessTTL() time.Duration { return t.accessTTL }

// Issue mints a signed access token for a user.
func (t *TokenIssuer) Issue(user store.User) (string, time.Time, error) {
	now := t.now().UTC()
	expiresAt := now.Add(t.accessTTL)

	jti, err := randomID()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate token id: %w", err)
	}

	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			Issuer:    issuer,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse validates a token and returns its claims.
func (t *TokenIssuer) Parse(tokenString string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(tokenString, claims,
		func(token *jwt.Token) (any, error) {
			// Pinning the algorithm blocks the "alg: none" and
			// HMAC-with-the-public-key substitution attacks.
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method %v", token.Header["alg"])
			}
			return t.key, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithTimeFunc(func() time.Time { return t.now() }),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	if claims.UserID == "" || !store.ValidRole(claims.Role) {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}
