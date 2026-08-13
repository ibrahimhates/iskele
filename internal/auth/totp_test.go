package auth

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The RFC 6238 test vectors, for the SHA-1 variant every authenticator app
// implements. The secret is the ASCII string "12345678901234567890".
//
// These are what prove the implementation is TOTP rather than merely
// self-consistent: a wrong-but-stable algorithm would pass every test that
// only compares our own output to itself.
func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	t.Parallel()

	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))

	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1_111_111_109, "081804"},
		{1_111_111_111, "050471"},
		{1_234_567_890, "005924"},
		{2_000_000_000, "279037"},
	}

	for _, tc := range cases {
		got, err := TOTPCode(secret, time.Unix(tc.unix, 0))
		if err != nil {
			t.Fatalf("TOTPCode(%d) error = %v", tc.unix, err)
		}
		if got != tc.want {
			t.Errorf("TOTPCode(%d) = %q, want %q", tc.unix, got, tc.want)
		}
	}
}

func TestVerifyTOTPAcceptsTheCurrentCode(t *testing.T) {
	t.Parallel()

	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret() error = %v", err)
	}

	now := time.Now()
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("TOTPCode() error = %v", err)
	}

	if err := VerifyTOTP(secret, code, now); err != nil {
		t.Fatalf("VerifyTOTP() error = %v", err)
	}
}

// A phone's clock is never exactly the server's, so one step either side is
// accepted — and no more, or a code would outlive its usefulness.
func TestVerifyTOTPToleratesOneStepOfDrift(t *testing.T) {
	t.Parallel()

	secret, _ := NewTOTPSecret()
	now := time.Unix(1_700_000_000, 0)

	for _, drift := range []time.Duration{-totpPeriod, 0, totpPeriod} {
		code, _ := TOTPCode(secret, now.Add(drift))
		if err := VerifyTOTP(secret, code, now); err != nil {
			t.Errorf("a code %v out was refused: %v", drift, err)
		}
	}

	for _, drift := range []time.Duration{-2 * totpPeriod, 2 * totpPeriod} {
		code, _ := TOTPCode(secret, now.Add(drift))
		if err := VerifyTOTP(secret, code, now); !errors.Is(err, ErrInvalidTOTPCode) {
			t.Errorf("a code %v out was accepted", drift)
		}
	}
}

func TestVerifyTOTPRefusesMalformedInput(t *testing.T) {
	t.Parallel()

	secret, _ := NewTOTPSecret()
	now := time.Now()

	cases := map[string]string{
		"empty":      "",
		"too short":  "12345",
		"too long":   "1234567",
		"not digits": "abcdef",
		"wrong code": "000000",
		"with tabs":  "\t123456\t",
	}

	for name, code := range cases {
		if err := VerifyTOTP(secret, code, now); err == nil {
			t.Errorf("%s: VerifyTOTP(%q) accepted it", name, code)
		}
	}
}

// Apps display a code as two groups of three; an operator pastes what is on
// the screen, spaces included.
func TestVerifyTOTPAcceptsASpacedCode(t *testing.T) {
	t.Parallel()

	secret, _ := NewTOTPSecret()
	now := time.Now()
	code, _ := TOTPCode(secret, now)

	spaced := code[:3] + " " + code[3:]
	if err := VerifyTOTP(secret, spaced, now); err != nil {
		t.Fatalf("VerifyTOTP(%q) error = %v", spaced, err)
	}
}

func TestTOTPRefusesAMalformedSecret(t *testing.T) {
	t.Parallel()

	for _, secret := range []string{"", "   ", "not-base32!", "1"} {
		if _, err := TOTPCode(secret, time.Now()); !errors.Is(err, ErrInvalidTOTPSecret) {
			t.Errorf("TOTPCode(%q) error = %v, want ErrInvalidTOTPSecret", secret, err)
		}
		if err := VerifyTOTP(secret, "123456", time.Now()); !errors.Is(err, ErrInvalidTOTPSecret) {
			t.Errorf("VerifyTOTP(%q) error = %v, want ErrInvalidTOTPSecret", secret, err)
		}
	}
}

// A secret typed back in from a phone's screen arrives lower-cased and
// spaced; refusing it would send the operator hunting for a fault that is not
// there.
func TestTOTPAcceptsASecretAsDisplayed(t *testing.T) {
	t.Parallel()

	secret, _ := NewTOTPSecret()
	now := time.Now()
	code, _ := TOTPCode(secret, now)

	displayed := strings.ToLower(FormatTOTPSecret(secret))
	if err := VerifyTOTP(displayed, code, now); err != nil {
		t.Fatalf("VerifyTOTP(%q) error = %v", displayed, err)
	}
}

func TestNewTOTPSecretIsDistinctEveryTime(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 32)
	for range 32 {
		secret, err := NewTOTPSecret()
		if err != nil {
			t.Fatalf("NewTOTPSecret() error = %v", err)
		}
		if _, dup := seen[secret]; dup {
			t.Fatal("NewTOTPSecret() repeated a secret")
		}
		if len(secret) != 32 { // 20 bytes, unpadded base32
			t.Fatalf("secret = %q (%d chars), want 32", secret, len(secret))
		}
		seen[secret] = struct{}{}
	}
}

func TestTOTPURIIsScannable(t *testing.T) {
	t.Parallel()

	uri := TOTPURI("iskele", "admin", "JBSWY3DPEHPK3PXP")

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("the URI does not parse: %v", err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Errorf("uri = %q", uri)
	}
	if got := parsed.Path; got != "/iskele:admin" {
		t.Errorf("label = %q, want /iskele:admin", got)
	}

	q := parsed.Query()
	// Apps disagree about which issuer they read, so both must be present.
	if q.Get("issuer") != "iskele" {
		t.Errorf("issuer = %q", q.Get("issuer"))
	}
	if q.Get("secret") != "JBSWY3DPEHPK3PXP" {
		t.Errorf("secret = %q", q.Get("secret"))
	}
	if q.Get("algorithm") != "SHA1" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Errorf("parameters = %v", q)
	}
}

func TestFormatTOTPSecretGroupsForTyping(t *testing.T) {
	t.Parallel()

	if got := FormatTOTPSecret("JBSWY3DPEHPK3PXP"); got != "JBSW Y3DP EHPK 3PXP" {
		t.Errorf("FormatTOTPSecret() = %q", got)
	}
	if got := FormatTOTPSecret(""); got != "" {
		t.Errorf("FormatTOTPSecret(\"\") = %q", got)
	}
}
