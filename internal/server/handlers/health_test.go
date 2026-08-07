package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/version"
)

func TestHealthReportsOKAndUptime(t *testing.T) {
	s := NewSystem()
	s.startedAt = time.Now().Add(-90 * time.Second)

	rec := httptest.NewRecorder()
	if err := s.Health(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)); err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
	if body.Uptime != "1m30s" {
		t.Errorf("uptime = %q, want %q", body.Uptime, "1m30s")
	}
}

func TestHealthLeaksNoInternalState(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := NewSystem().Health(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)); err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	// The endpoint is unauthenticated, so its body must stay limited to the
	// two documented fields.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	for k := range raw {
		if k != "status" && k != "uptime" {
			t.Errorf("unexpected field %q in the unauthenticated health response", k)
		}
	}
}

func TestVersionReturnsBuildInfo(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := NewSystem().Version(rec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)); err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body version.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.GoVersion == "" || body.Platform == "" {
		t.Errorf("version info is incomplete: %+v", body)
	}
}
