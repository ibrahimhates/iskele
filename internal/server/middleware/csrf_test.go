package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// csrfHandler builds the guard with a recording deny function.
func csrfHandler(allowedOrigin string, denied *bool) http.Handler {
	return CSRFGuard(allowedOrigin, func(w http.ResponseWriter, _ *http.Request, _ string) {
		*denied = true
		w.WriteHeader(http.StatusForbidden)
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestCSRFGuardAllowsSafeMethods(t *testing.T) {
	var denied bool
	h := csrfHandler("", &denied)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/x", nil)
		req.Header.Set("Origin", "https://evil.example")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if denied || rec.Code != http.StatusOK {
			t.Errorf("%s was refused; safe methods change nothing", method)
		}
	}
}

func TestCSRFGuardRejectsForeignOrigin(t *testing.T) {
	var denied bool
	h := csrfHandler("", &denied)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Authorization", "Bearer token")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !denied || rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, denied = %v, want the cross-origin write refused", rec.Code, denied)
	}
}

func TestCSRFGuardRequiresABearerToken(t *testing.T) {
	var denied bool
	h := csrfHandler("", &denied)

	// A cross-site form or image can trigger this shape of request, but it
	// cannot set an Authorization header.
	req := httptest.NewRequest(http.MethodPost, "/x", nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !denied || rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want an ambient-credential write refused", rec.Code)
	}
}

func TestCSRFGuardAllowsSameOriginWithToken(t *testing.T) {
	var denied bool
	h := csrfHandler("", &denied)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Origin", "http://"+req.Host)
	req.Header.Set("Authorization", "Bearer token")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if denied || rec.Code != http.StatusOK {
		t.Errorf("status = %d, denied = %v, want the same-origin write allowed", rec.Code, denied)
	}
}

func TestCSRFGuardAllowsAConfiguredOrigin(t *testing.T) {
	var denied bool
	h := csrfHandler("https://panel.example", &denied)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Origin", "https://panel.example")
	req.Header.Set("Authorization", "Bearer token")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if denied || rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the configured origin allowed", rec.Code)
	}
}

func TestCSRFGuardAllowsHeaderlessClients(t *testing.T) {
	var denied bool
	h := csrfHandler("", &denied)

	// curl and CI runners send no Origin and are not subject to CSRF, but they
	// must still present a token.
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer token")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if denied || rec.Code != http.StatusOK {
		t.Errorf("status = %d, want an Origin-less authenticated write allowed", rec.Code)
	}
}

func TestOriginAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://panel.local/x", nil)
	req.Host = "panel.local"

	tests := []struct {
		origin  string
		allowed string
		want    bool
	}{
		{"", "", true},
		{"http://panel.local", "", true},
		{"https://panel.local", "", true},
		{"http://PANEL.LOCAL", "", true},
		{"https://evil.example", "", false},
		{"https://panel.local.evil.example", "", false},
		{"https://other.example", "https://other.example", true},
	}

	for _, tt := range tests {
		if got := OriginAllowed(req, tt.origin, tt.allowed); got != tt.want {
			t.Errorf("OriginAllowed(%q, allowed=%q) = %v, want %v",
				tt.origin, tt.allowed, got, tt.want)
		}
	}
}

func TestBearerToken(t *testing.T) {
	tests := map[string]string{
		"Bearer abc123":  "abc123",
		"bearer abc123":  "abc123",
		"BEARER abc123":  "abc123",
		"Basic abc123":   "",
		"abc123":         "",
		"":               "",
		"Bearer  spaced": "spaced",
	}

	for header, want := range tests {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		if got := BearerToken(req); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}
