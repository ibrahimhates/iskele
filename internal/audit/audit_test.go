package audit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/store"
)

func TestIsSecretKey(t *testing.T) {
	secret := []string{
		"password", "PASSWORD", "db_password", "POSTGRES_PASSWORD",
		"token", "CF_TUNNEL_TOKEN", "api_key", "APIKEY", "apikey",
		"secret", "MYSQL_ROOT_PASSWORD", "credential", "AUTH_TOKEN",
		"private_key", "key", "SALT", "signature", "SESSION_SECRET",
	}
	for _, k := range secret {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false, want it masked", k)
		}
	}

	safe := []string{"image", "name", "port", "TZ", "PATH", "restart_policy", "monkey"}
	for _, k := range safe {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true, want it kept", k)
		}
	}
}

func TestMaskEnv(t *testing.T) {
	env := []string{
		"TZ=Europe/Istanbul",
		"POSTGRES_PASSWORD=hunter2",
		"CF_TUNNEL_TOKEN=eyJhIjoiMTIz",
		"NO_EQUALS_SIGN",
		"EMPTY_PASSWORD=",
	}

	got := MaskEnv(env)

	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, "eyJhIjoiMTIz") {
		t.Fatalf("secrets survived masking: %v", got)
	}
	if !strings.Contains(joined, "TZ=Europe/Istanbul") {
		t.Error("a non-secret variable was masked")
	}
	if !strings.Contains(joined, "POSTGRES_PASSWORD="+Masked) {
		t.Errorf("masked entry = %v, want the key preserved", got)
	}
	if !strings.Contains(joined, "NO_EQUALS_SIGN") {
		t.Error("an entry without a value was dropped")
	}
	// An unset secret stays visibly unset rather than becoming "***".
	if !strings.Contains(joined, "EMPTY_PASSWORD=") || strings.Contains(joined, "EMPTY_PASSWORD="+Masked) {
		t.Errorf("empty secret = %v, want it left empty", got)
	}
}

func TestMaskEnvPreservesNil(t *testing.T) {
	if MaskEnv(nil) != nil {
		t.Error("MaskEnv(nil) allocated a slice")
	}
}

func TestMaskMap(t *testing.T) {
	got := MaskMap(map[string]string{
		"image":         "nginx",
		"registry_pass": "hunter2",
	})

	if got["image"] != "nginx" {
		t.Errorf("image = %q", got["image"])
	}
	if got["registry_pass"] != Masked {
		t.Errorf("registry_pass = %q, want it masked", got["registry_pass"])
	}
}

func TestMaskAnyWalksNestedStructures(t *testing.T) {
	payload := map[string]any{
		"image": "postgres:16",
		"env":   []string{"POSTGRES_PASSWORD=hunter2", "TZ=UTC"},
		"registry": map[string]any{
			"url":      "registry.example",
			"password": "hunter2",
		},
		"labels": map[string]string{"app": "db", "api_key": "abc123"},
		"list": []any{
			map[string]any{"token": "abc123"},
		},
	}

	masked, ok := MaskAny(payload).(map[string]any)
	if !ok {
		t.Fatalf("MaskAny returned %T", MaskAny(payload))
	}

	encoded, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// A credential must not survive at any depth.
	if strings.Contains(string(encoded), "hunter2") || strings.Contains(string(encoded), "abc123") {
		t.Fatalf("a secret survived masking: %s", encoded)
	}
	if !strings.Contains(string(encoded), "postgres:16") {
		t.Error("a non-secret value was lost")
	}
	if !strings.Contains(string(encoded), "registry.example") {
		t.Error("a non-secret nested value was lost")
	}
}

func TestMaskAnyLeavesScalarsAlone(t *testing.T) {
	if got := MaskAny(42); got != 42 {
		t.Errorf("MaskAny(42) = %v", got)
	}
	if got := MaskAny("plain"); got != "plain" {
		t.Errorf("MaskAny(%q) = %v", "plain", got)
	}
}

func newRecorder(t *testing.T) (*Recorder, *store.DB) {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{
		Path: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return New(db.Audit, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

func TestRecordWritesAnEntry(t *testing.T) {
	rec, db := newRecorder(t)
	ctx := context.Background()

	rec.Record(ctx, Event{
		Actor:        Actor{UserID: "u1", Username: "alice", Role: store.RoleAdmin},
		Action:       ActionContainerStart,
		ResourceType: "container",
		ResourceID:   "abc",
		IP:           "192.0.2.1",
		UserAgent:    "curl/8",
	})

	entries, err := db.Audit.List(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	e := entries[0]
	if e.Username != "alice" || e.Action != ActionContainerStart || e.ResourceID != "abc" {
		t.Errorf("entry = %+v", e)
	}
	if e.Result != store.ResultOK {
		t.Errorf("result = %q, want ok", e.Result)
	}
	if e.IP != "192.0.2.1" {
		t.Errorf("ip = %q", e.IP)
	}
}

func TestRecordMasksDetail(t *testing.T) {
	rec, db := newRecorder(t)
	ctx := context.Background()

	rec.Record(ctx, Event{
		Actor:  Actor{Username: "alice"},
		Action: "container.create",
		Detail: map[string]any{
			"image": "postgres:16",
			"env":   []string{"POSTGRES_PASSWORD=hunter2"},
		},
	})

	entries, _ := db.Audit.List(ctx, store.AuditFilter{})
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	// The audit trail is exported and read by humans; a credential in it is a
	// credential leak.
	if strings.Contains(entries[0].Detail, "hunter2") {
		t.Fatalf("detail leaked a secret: %s", entries[0].Detail)
	}
	if !strings.Contains(entries[0].Detail, "postgres:16") {
		t.Errorf("detail lost its useful content: %s", entries[0].Detail)
	}
}

func TestRecordMarksFailures(t *testing.T) {
	rec, db := newRecorder(t)
	ctx := context.Background()

	rec.Record(ctx, Event{
		Actor:  Actor{Username: "alice"},
		Action: ActionContainerStop,
		Err:    errors.New("container is already stopped"),
	})

	entries, _ := db.Audit.List(ctx, store.AuditFilter{})
	if entries[0].Result != store.ResultError {
		t.Errorf("result = %q, want error", entries[0].Result)
	}
	if !strings.Contains(entries[0].Detail, "already stopped") {
		t.Errorf("detail = %q, want the failure reason", entries[0].Detail)
	}
}

func TestRecordNotesTheAPIToken(t *testing.T) {
	rec, db := newRecorder(t)
	ctx := context.Background()

	rec.Record(ctx, Event{
		Actor:  Actor{UserID: "u1", Username: "alice", TokenID: "tok-1"},
		Action: ActionContainerStart,
	})

	entries, _ := db.Audit.List(ctx, store.AuditFilter{})
	// Tracing a leaked token back to what it did depends on this.
	if !strings.Contains(entries[0].Detail, "tok-1") {
		t.Errorf("detail = %q, want the api token id recorded", entries[0].Detail)
	}
}

func TestRecordSurvivesACanceledRequest(t *testing.T) {
	rec, db := newRecorder(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The action already happened; losing its record because the client hung
	// up would leave a gap in the trail.
	rec.Record(ctx, Event{Actor: Actor{Username: "alice"}, Action: ActionContainerStart})

	entries, err := db.Audit.List(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want the record to survive cancellation", len(entries))
	}
}

func TestRecordOnNilRecorderIsSafe(t *testing.T) {
	var rec *Recorder
	// Handlers should not need a nil check at each call site.
	rec.Record(context.Background(), Event{Action: "x"})
}

func TestEmptyDetailIsAnEmptyObject(t *testing.T) {
	rec, db := newRecorder(t)
	ctx := context.Background()

	rec.Record(ctx, Event{Actor: Actor{Username: "alice"}, Action: "x"})

	entries, _ := db.Audit.List(ctx, store.AuditFilter{})
	if entries[0].Detail != "{}" {
		t.Errorf("detail = %q, want {} so clients can always parse it", entries[0].Detail)
	}
}
