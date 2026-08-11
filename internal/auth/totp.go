package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238's mandated digest; see totpDigits
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters.
//
// These are not choices so much as what authenticator apps implement: Google
// Authenticator, Aegis, 1Password and the rest assume SHA-1, 6 digits and a
// 30-second step, and a server that picks anything else is a server whose
// codes do not work. SHA-1 is fine here — HMAC-SHA1 is not affected by the
// collision attacks that retired SHA-1 for signatures, and each code is valid
// for half a minute.
const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	// totpSecretBytes is 160 bits, the size RFC 4226 recommends.
	totpSecretBytes = 20
	// totpSkew is how many steps either side of now are accepted, so a phone
	// whose clock is half a minute off still works.
	totpSkew = 1
)

// TOTP errors.
var (
	ErrInvalidTOTPSecret = errors.New("the two-factor secret is malformed")
	ErrInvalidTOTPCode   = errors.New("that code is not valid")
)

// totpEncoding is unpadded base32, which is what every authenticator app
// expects in an otpauth:// URI and in a manually typed secret.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh base32 secret.
func NewTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate two-factor secret: %w", err)
	}
	return totpEncoding.EncodeToString(raw), nil
}

// TOTPCode computes the code for one instant.
func TOTPCode(secret string, at time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return codeAt(key, counterFor(at)), nil
}

// VerifyTOTP reports whether code is valid for secret at time now.
//
// One step either side is accepted, which is the usual allowance for clock
// drift between a phone and a server. The comparison is constant-time: a code
// is a six-digit shared secret, and leaking how many leading digits were right
// would cut the search space to sixty guesses.
func VerifyTOTP(secret, code string, now time.Time) error {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return err
	}

	entered := strings.TrimSpace(code)
	// Apps display the code in two groups; operators paste what they see.
	entered = strings.ReplaceAll(entered, " ", "")
	if len(entered) != totpDigits {
		return ErrInvalidTOTPCode
	}

	counter := counterFor(now)
	ok := false
	for step := -totpSkew; step <= totpSkew; step++ {
		expected := codeAt(key, uint64(int64(counter)+int64(step))) //nolint:gosec // counter is seconds/30 since the epoch
		// No early exit: every candidate is compared, so the time this takes
		// does not reveal which step matched.
		if subtle.ConstantTimeCompare([]byte(expected), []byte(entered)) == 1 {
			ok = true
		}
	}
	if !ok {
		return ErrInvalidTOTPCode
	}
	return nil
}

// TOTPURI builds the otpauth:// URI an authenticator app scans.
//
// The issuer appears twice — once as a label prefix and once as a parameter —
// because apps disagree about which one they read, and the pair is what the
// Key URI Format recommends.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)

	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprint(totpDigits))
	params.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))

	return "otpauth://totp/" + label + "?" + params.Encode()
}

// FormatTOTPSecret groups a secret into blocks of four, the way apps show it
// for manual entry.
func FormatTOTPSecret(secret string) string {
	var out strings.Builder
	for i, r := range secret {
		if i > 0 && i%4 == 0 {
			out.WriteByte(' ')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// decodeTOTPSecret accepts a secret as stored, as displayed (spaced), and
// either case: an operator retyping one should not have to think about it.
func decodeTOTPSecret(secret string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	cleaned = strings.TrimRight(cleaned, "=")
	if cleaned == "" {
		return nil, ErrInvalidTOTPSecret
	}

	key, err := totpEncoding.DecodeString(cleaned)
	if err != nil || len(key) == 0 {
		return nil, ErrInvalidTOTPSecret
	}
	return key, nil
}

// counterFor is the RFC 6238 time step: seconds since the Unix epoch divided
// by the period.
func counterFor(at time.Time) uint64 {
	seconds := at.Unix()
	if seconds < 0 {
		return 0
	}
	return uint64(seconds) / uint64(totpPeriod.Seconds()) //nolint:gosec // guarded above
}

// codeAt is HOTP (RFC 4226) over one counter value.
func codeAt(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation: the low nibble of the last byte picks where to read
	// four bytes from, and the top bit is masked off so the result is positive
	// on every implementation, signed or not.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", totpDigits, value%pow10(totpDigits))
}

func pow10(n int) uint32 {
	out := uint32(1)
	for range n {
		out *= 10
	}
	return out
}
