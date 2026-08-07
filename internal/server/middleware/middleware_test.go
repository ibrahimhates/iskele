package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestRequestIDGeneratesWhenAbsent(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seen == "" {
		t.Fatal("no request ID was placed in the context")
	}
	if got := rec.Header().Get(RequestIDHeader); got != seen {
		t.Errorf("response header = %q, want it to match the context value %q", got, seen)
	}
}

func TestRequestIDReusesInboundHeader(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(RequestIDHeader, "proxy-generated-id")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "proxy-generated-id" {
		t.Errorf("request ID = %q, want the inbound header to be reused", seen)
	}
}

func TestRequestIDRejectsHostileInboundHeaders(t *testing.T) {
	tests := map[string]string{
		"too long":         strings.Repeat("a", maxInboundRequestIDLen+1),
		"control chars":    "abc\ndef",
		"non ascii":        "kimlik-çğü",
		"embedded newline": "id\r\nX-Injected: 1",
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			var seen string
			h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = RequestIDFrom(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set(RequestIDHeader, value)

			h.ServeHTTP(httptest.NewRecorder(), req)

			if seen == value {
				t.Errorf("request ID = %q, want the hostile value to be discarded", seen)
			}
			if seen == "" {
				t.Error("a replacement request ID should still have been generated")
			}
		})
	}
}

func TestRecoverTurnsPanicIntoJSON500(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(WithLogger(req.Context(), discardLogger()))

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body["error"]["code"] != "INTERNAL" {
		t.Errorf("code = %q, want INTERNAL", body["error"]["code"])
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("panic value leaked into the response body")
	}
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("http.ErrAbortHandler was swallowed, want it re-panicked")
		}
	}()

	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(WithLogger(req.Context(), discardLogger()))
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestRecoverPassesThroughNormalResponses(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the handler's own status", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want it omitted on plaintext requests", got)
	}
}

func TestSecurityHeadersAddsHSTSOverTLS(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "https://example.test/x", nil)
	req.TLS = &tlsConnectionState

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("Strict-Transport-Security missing on a TLS request")
	}
}

func TestLoggerRecordsStatusAndAttachesLogger(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	var hadLogger bool
	h := RequestID(Logger(base)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hadLogger = LoggerFrom(r.Context()) != slog.Default()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/x", nil))

	if !hadLogger {
		t.Error("no request-scoped logger was attached to the context")
	}

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	if record["status"] != float64(http.StatusCreated) {
		t.Errorf("logged status = %v, want 201", record["status"])
	}
	if record["method"] != http.MethodPost {
		t.Errorf("logged method = %v", record["method"])
	}
	if record["request_id"] == "" || record["request_id"] == nil {
		t.Error("log record is missing the request ID")
	}
}

func TestLoggerLevelsFollowStatus(t *testing.T) {
	tests := []struct {
		status    int
		wantLevel string
	}{
		{http.StatusOK, "INFO"},
		{http.StatusNotFound, "WARN"},
		{http.StatusInternalServerError, "ERROR"},
	}

	for _, tt := range tests {
		var buf bytes.Buffer
		base := slog.New(slog.NewJSONHandler(&buf, nil))
		status := tt.status

		h := Logger(base)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

		var record map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
			t.Fatalf("log line is not JSON: %v", err)
		}
		if record["level"] != tt.wantLevel {
			t.Errorf("status %d logged at %v, want %s", tt.status, record["level"], tt.wantLevel)
		}
	}
}

func TestLoggerFromFallsBackToDefault(t *testing.T) {
	if LoggerFrom(httptest.NewRequest(http.MethodGet, "/x", nil).Context()) == nil {
		t.Error("LoggerFrom returned nil outside the middleware chain")
	}
}

func TestClientIPStripsPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "192.0.2.5:54321"

	if got := ClientIP(req); got != "192.0.2.5" {
		t.Errorf("ClientIP() = %q, want 192.0.2.5", got)
	}
}

func TestClientIPIgnoresProxyHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "192.0.2.5:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(req); got != "192.0.2.5" {
		t.Errorf("ClientIP() = %q, want the peer address, not the spoofable header", got)
	}
}
