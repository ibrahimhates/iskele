package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/version"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	return NewRouter(Deps{
		Config: &cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthEndpoint(t *testing.T) {
	rec := do(t, testRouter(t), http.MethodGet, APIPrefix+"/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
		Uptime string `json:"uptime"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
	if body.Uptime == "" {
		t.Error("uptime is empty")
	}
}

func TestVersionEndpoint(t *testing.T) {
	rec := do(t, testRouter(t), http.MethodGet, APIPrefix+"/version")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body version.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Version == "" || body.GoVersion == "" || body.Platform == "" {
		t.Errorf("version info is incomplete: %+v", body)
	}
}

func TestUnknownRouteReturnsStandardEnvelope(t *testing.T) {
	rec := do(t, testRouter(t), http.MethodGet, APIPrefix+"/does-not-exist")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body.Error.Code != string(httpx.CodeNotFound) {
		t.Errorf("code = %q, want %q", body.Error.Code, httpx.CodeNotFound)
	}
	if body.Error.Message == "" {
		t.Error("message is empty")
	}
}

func TestWrongMethodReturnsMethodNotAllowed(t *testing.T) {
	rec := do(t, testRouter(t), http.MethodPost, APIPrefix+"/health")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body.Error.Code != string(httpx.CodeMethodNotAllowed) {
		t.Errorf("code = %q, want %q", body.Error.Code, httpx.CodeMethodNotAllowed)
	}
}

func TestEveryResponseCarriesSecurityHeadersAndRequestID(t *testing.T) {
	h := testRouter(t)

	for _, path := range []string{APIPrefix + "/health", APIPrefix + "/nope"} {
		rec := do(t, h, http.MethodGet, path)

		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", path, got)
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Errorf("%s: X-Request-Id header missing", path)
		}
	}
}

func TestRouterToleratesNilLogger(t *testing.T) {
	cfg := config.Default()
	h := NewRouter(Deps{Config: &cfg})

	if rec := do(t, h, http.MethodGet, APIPrefix+"/health"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the router to work without an explicit logger", rec.Code)
	}
}
