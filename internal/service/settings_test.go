package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/store"
)

func newSettings(t *testing.T) (*Settings, *store.DB) {
	t.Helper()

	db, err := store.Open(context.Background(), store.Options{
		Path: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.Default()
	return NewSettings(db.Settings, &cfg, nil), db
}

func TestSettingsDefaultToKeepingEverything(t *testing.T) {
	svc, _ := newSettings(t)

	view, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Deleting somebody's audit history because they never opened the settings
	// page would be the wrong default to pick for them.
	if view.AuditRetentionDays != 0 {
		t.Errorf("AuditRetentionDays = %d, want 0", view.AuditRetentionDays)
	}
	if !view.BindMountWarning {
		t.Error("the bind mount warning is off by default")
	}
	if view.Installation.DockerHost == "" {
		t.Error("the installation facts are missing")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	svc, _ := newSettings(t)
	ctx := context.Background()

	days := 30
	view, err := svc.Set(ctx, Update{AuditRetentionDays: &days}, Identity{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if view.AuditRetentionDays != 30 {
		t.Fatalf("AuditRetentionDays = %d, want 30", view.AuditRetentionDays)
	}
	// The field that was not in the update keeps its value.
	if !view.BindMountWarning {
		t.Error("an unrelated setting was reset")
	}

	off := false
	view, err = svc.Set(ctx, Update{BindMountWarning: &off}, Identity{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if view.BindMountWarning {
		t.Error("BindMountWarning did not change")
	}
	if view.AuditRetentionDays != 30 {
		t.Errorf("AuditRetentionDays = %d after an unrelated update", view.AuditRetentionDays)
	}
}

func TestSettingsRefuseAnImpossibleRetention(t *testing.T) {
	svc, _ := newSettings(t)
	ctx := context.Background()

	for _, days := range []int{-1, MaxRetentionDays + 1} {
		d := days
		if _, err := svc.Set(ctx, Update{AuditRetentionDays: &d}, Identity{}, RequestMeta{}); !errors.Is(err, ErrRetentionRange) {
			t.Errorf("Set(%d) error = %v, want ErrRetentionRange", days, err)
		}
	}
}

func TestAuditRetentionAsADuration(t *testing.T) {
	svc, _ := newSettings(t)
	ctx := context.Background()

	// Zero means forever, and the sweep reads that as "do nothing".
	got, err := svc.AuditRetention(ctx)
	if err != nil {
		t.Fatalf("AuditRetention() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("AuditRetention() = %v, want 0", got)
	}

	days := 7
	if _, setErr := svc.Set(ctx, Update{AuditRetentionDays: &days}, Identity{}, RequestMeta{}); setErr != nil {
		t.Fatalf("Set() error = %v", setErr)
	}

	got, err = svc.AuditRetention(ctx)
	if err != nil {
		t.Fatalf("AuditRetention() error = %v", err)
	}
	if got != 7*24*time.Hour {
		t.Fatalf("AuditRetention() = %v, want 168h", got)
	}
}

// The whole retention path, end to end: set a window, and the sweep the daemon
// runs removes what falls outside it and keeps what does not.
func TestRetentionSweepRemovesOnlyExpiredEntries(t *testing.T) {
	svc, db := newSettings(t)
	ctx := context.Background()

	now := time.Now().UTC()
	ages := map[string]time.Time{
		"ancient": now.Add(-30 * 24 * time.Hour),
		"old":     now.Add(-8 * 24 * time.Hour),
		"recent":  now.Add(-1 * time.Hour),
	}
	for action, at := range ages {
		if _, err := db.Audit.Append(ctx, store.AuditEntry{
			Action: action, Result: store.ResultOK, CreatedAt: at,
		}); err != nil {
			t.Fatalf("Append(%s) error = %v", action, err)
		}
	}

	days := 7
	if _, err := svc.Set(ctx, Update{AuditRetentionDays: &days}, Identity{}, RequestMeta{}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	retention, err := svc.AuditRetention(ctx)
	if err != nil {
		t.Fatalf("AuditRetention() error = %v", err)
	}
	removed, err := db.Audit.DeleteBefore(ctx, time.Now().Add(-retention))
	if err != nil {
		t.Fatalf("DeleteBefore() error = %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed %d entries, want the two older than a week", removed)
	}

	left, err := db.Audit.List(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(left) != 1 || left[0].Action != "recent" {
		t.Fatalf("the trail holds %+v, want only the recent entry", left)
	}
}

// A retention of zero must not be turned into "delete everything before now",
// which is the way this goes wrong.
func TestRetentionOfZeroDeletesNothing(t *testing.T) {
	svc, db := newSettings(t)
	ctx := context.Background()

	if _, err := db.Audit.Append(ctx, store.AuditEntry{
		Action: "container.stop", Result: store.ResultOK,
		CreatedAt: time.Now().Add(-10 * 365 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	retention, err := svc.AuditRetention(ctx)
	if err != nil {
		t.Fatalf("AuditRetention() error = %v", err)
	}
	if retention != 0 {
		t.Fatalf("AuditRetention() = %v, want 0", retention)
	}

	// The sweep is guarded by `retention > 0`; this asserts the value that
	// guard depends on, and that a decade-old entry is still there.
	left, err := db.Audit.List(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("the trail holds %d entries, want the untouched one", len(left))
	}
}

// A settings row edited by hand into nonsense must not take the screen down.
func TestSettingsTolerateAMalformedStoredValue(t *testing.T) {
	svc, db := newSettings(t)
	ctx := context.Background()

	if err := db.Settings.Set(ctx, SettingAuditRetentionDays, "not a number"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := db.Settings.Set(ctx, SettingBindMountWarning, "maybe"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	view, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if view.AuditRetentionDays != DefaultAuditRetentionDays || !view.BindMountWarning {
		t.Fatalf("view = %+v, want the defaults", view.Editable)
	}
}
