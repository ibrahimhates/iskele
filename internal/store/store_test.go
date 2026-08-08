package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// newTestDB opens a fresh file-backed database. A file rather than :memory: so
// the WAL pragmas exercised in production are exercised here too.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newUser(t *testing.T, db *DB, username string, role Role) User {
	t.Helper()
	u := User{
		ID:           "u-" + username,
		Username:     username,
		Role:         role,
		PasswordHash: "hash-" + username,
	}
	if err := db.Users.Create(context.Background(), u); err != nil {
		t.Fatalf("Create(%s) error = %v", username, err)
	}
	return u
}

func TestOpenAppliesMigrations(t *testing.T) {
	db := newTestDB(t)

	versions, err := db.AppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("AppliedMigrations() error = %v", err)
	}
	if len(versions) == 0 || versions[0] != 1 {
		t.Fatalf("applied = %v, want migration 1 to be present", versions)
	}

	// Every table the schema promises must exist.
	for _, table := range []string{"users", "sessions", "api_tokens", "login_attempts", "audit_logs", "settings"} {
		var name string
		err := db.SQL().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	db, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	newUser(t, db, "alice", RoleAdmin)

	// Running the migrations again, and reopening, must not fail or wipe data.
	for i := 0; i < 3; i++ {
		if migrateErr := db.Migrate(ctx); migrateErr != nil {
			t.Fatalf("Migrate() run %d error = %v", i, migrateErr)
		}
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

	reopened, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer func() { _ = reopened.Close() }()

	n, err := reopened.Users.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 1 {
		t.Errorf("user count = %d, want the data to survive re-migration", n)
	}
}

func TestOpenRequiresAPath(t *testing.T) {
	if _, err := Open(context.Background(), Options{}); err == nil {
		t.Fatal("Open() error = nil, want a missing-path error")
	}
}

func TestWALAndForeignKeysAreOn(t *testing.T) {
	db := newTestDB(t)

	var journal string
	if err := db.SQL().QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}

	var fk int
	if err := db.SQL().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestUserCRUD(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if n, _ := db.Users.Count(ctx); n != 0 {
		t.Fatalf("count = %d, want an empty installation", n)
	}

	created := newUser(t, db, "Alice", RoleAdmin)

	byID, err := db.Users.ByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if byID.Username != "Alice" || byID.Role != RoleAdmin {
		t.Errorf("user = %+v", byID)
	}
	if byID.CreatedAt.IsZero() || byID.UpdatedAt.IsZero() {
		t.Error("timestamps were not set on create")
	}

	// Lookup is case-insensitive, but the display form is preserved.
	byName, err := db.Users.ByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("ByUsername() error = %v", err)
	}
	if byName.ID != created.ID {
		t.Errorf("ByUsername returned %q", byName.ID)
	}
	if byName.Username != "Alice" {
		t.Errorf("Username = %q, want the original casing preserved", byName.Username)
	}
}

func TestUsernameUniquenessIsCaseInsensitive(t *testing.T) {
	db := newTestDB(t)
	newUser(t, db, "alice", RoleAdmin)

	err := db.Users.Create(context.Background(), User{
		ID: "u-other", Username: "ALICE", Role: RoleViewer, PasswordHash: "x",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}

func TestUserNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.Users.ByID(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByID error = %v, want ErrNotFound", err)
	}
	if _, err := db.Users.ByUsername(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByUsername error = %v, want ErrNotFound", err)
	}
	if err := db.Users.UpdateRole(ctx, "ghost", RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateRole error = %v, want ErrNotFound", err)
	}
}

func TestUserUpdates(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u := newUser(t, db, "alice", RoleViewer)

	if err := db.Users.UpdatePassword(ctx, u.ID, "new-hash"); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}
	if err := db.Users.UpdateRole(ctx, u.ID, RoleOperator); err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	if err := db.Users.SetDisabled(ctx, u.ID, true); err != nil {
		t.Fatalf("SetDisabled() error = %v", err)
	}
	if err := db.Users.SetTOTP(ctx, u.ID, "enc-secret", true); err != nil {
		t.Fatalf("SetTOTP() error = %v", err)
	}
	loginAt := time.Now().UTC().Truncate(time.Second)
	if err := db.Users.TouchLogin(ctx, u.ID, loginAt); err != nil {
		t.Fatalf("TouchLogin() error = %v", err)
	}

	got, err := db.Users.ByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if got.PasswordHash != "new-hash" || got.Role != RoleOperator || !got.Disabled {
		t.Errorf("user = %+v", got)
	}
	if !got.TOTPEnabled || got.TOTPSecretEnc != "enc-secret" {
		t.Errorf("totp = %v / %q", got.TOTPEnabled, got.TOTPSecretEnc)
	}
	if !got.LastLoginAt.Equal(loginAt) {
		t.Errorf("LastLoginAt = %v, want %v", got.LastLoginAt, loginAt)
	}
}

func TestDeletingAUserCascadesSessionsAndTokens(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u := newUser(t, db, "alice", RoleAdmin)

	err := db.Sessions.Create(ctx, Session{
		ID: "s1", UserID: u.ID, RefreshHash: "rh1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("session Create() error = %v", err)
	}
	err = db.Tokens.Create(ctx, APIToken{
		ID: "t1", UserID: u.ID, Name: "ci", Prefix: "isk_abc", TokenHash: "th1",
	})
	if err != nil {
		t.Fatalf("token Create() error = %v", err)
	}

	if err := db.Users.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := db.Sessions.ByRefreshHash(ctx, "rh1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("session survived the user delete: %v", err)
	}
	if _, err := db.Tokens.ByHash(ctx, "th1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token survived the user delete: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u := newUser(t, db, "alice", RoleAdmin)
	now := time.Now().UTC()

	s := Session{
		ID: "s1", UserID: u.ID, RefreshHash: "rh1",
		IP: "192.0.2.1", UserAgent: "curl", ExpiresAt: now.Add(time.Hour),
	}
	if err := db.Sessions.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := db.Sessions.ByRefreshHash(ctx, "rh1")
	if err != nil {
		t.Fatalf("ByRefreshHash() error = %v", err)
	}
	if !got.Active(now) {
		t.Error("freshly created session is not active")
	}
	if got.IP != "192.0.2.1" {
		t.Errorf("IP = %q", got.IP)
	}

	if err := db.Sessions.Revoke(ctx, "s1"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	got, _ = db.Sessions.ByRefreshHash(ctx, "rh1")
	if got.Active(now) {
		t.Error("revoked session still reports active")
	}

	// Revoking twice is a no-op, so a repeated logout is not an error.
	if err := db.Sessions.Revoke(ctx, "s1"); err != nil {
		t.Errorf("second Revoke() error = %v", err)
	}
}

func TestSessionExpiryMakesItInactive(t *testing.T) {
	now := time.Now().UTC()
	s := Session{ExpiresAt: now.Add(-time.Minute)}

	if s.Active(now) {
		t.Error("expired session reports active")
	}
}

func TestRevokeAllForUser(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u := newUser(t, db, "alice", RoleAdmin)
	now := time.Now().UTC()

	for _, id := range []string{"s1", "s2", "s3"} {
		err := db.Sessions.Create(ctx, Session{
			ID: id, UserID: u.ID, RefreshHash: "rh-" + id, ExpiresAt: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
	}

	if err := db.Sessions.RevokeAllForUser(ctx, u.ID); err != nil {
		t.Fatalf("RevokeAllForUser() error = %v", err)
	}

	sessions, err := db.Sessions.ListForUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListForUser() error = %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions", len(sessions))
	}
	for _, s := range sessions {
		if s.Active(now) {
			t.Errorf("session %s is still active", s.ID)
		}
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u := newUser(t, db, "alice", RoleAdmin)
	now := time.Now().UTC()

	_ = db.Sessions.Create(ctx, Session{ID: "old", UserID: u.ID, RefreshHash: "a", ExpiresAt: now.Add(-2 * time.Hour)})
	_ = db.Sessions.Create(ctx, Session{ID: "live", UserID: u.ID, RefreshHash: "b", ExpiresAt: now.Add(2 * time.Hour)})

	n, err := db.Sessions.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired() error = %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d, want 1", n)
	}
	if _, err := db.Sessions.ByRefreshHash(ctx, "b"); err != nil {
		t.Errorf("the live session was deleted: %v", err)
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	u := newUser(t, db, "alice", RoleAdmin)
	now := time.Now().UTC()

	tok := APIToken{
		ID: "t1", UserID: u.ID, Name: "ci", Prefix: "isk_abc", TokenHash: "th1",
		Scopes: []string{"containers:read", "images:read"},
	}
	if err := db.Tokens.Create(ctx, tok); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := db.Tokens.ByHash(ctx, "th1")
	if err != nil {
		t.Fatalf("ByHash() error = %v", err)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "containers:read" {
		t.Errorf("Scopes = %v", got.Scopes)
	}
	if !got.Active(now) {
		t.Error("new token is not active")
	}

	if err := db.Tokens.Revoke(ctx, "t1"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	got, _ = db.Tokens.ByHash(ctx, "th1")
	if got.Active(now) {
		t.Error("revoked token still reports active")
	}

	if err := db.Tokens.Revoke(ctx, "t1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Revoke() error = %v, want ErrNotFound", err)
	}
}

func TestAPITokenExpiry(t *testing.T) {
	now := time.Now().UTC()

	expired := APIToken{ExpiresAt: now.Add(-time.Minute)}
	if expired.Active(now) {
		t.Error("expired token reports active")
	}

	// An empty expiry means "never expires".
	forever := APIToken{}
	if !forever.Active(now) {
		t.Error("token without an expiry reports inactive")
	}
}

func TestAuditAppendAndFilter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	entries := []AuditEntry{
		{UserID: "u1", Username: "alice", Action: "container.start", ResourceType: "container", ResourceID: "c1", Result: ResultOK},
		{UserID: "u1", Username: "alice", Action: "container.stop", ResourceType: "container", ResourceID: "c1", Result: ResultOK},
		{UserID: "u2", Username: "bob", Action: "container.start", ResourceType: "container", ResourceID: "c2", Result: ResultError},
	}
	for _, e := range entries {
		if _, err := db.Audit.Append(ctx, e); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	all, err := db.Audit.List(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d entries, want 3", len(all))
	}
	// Newest first.
	if all[0].Username != "bob" {
		t.Errorf("first entry = %+v, want the newest", all[0])
	}

	byUser, _ := db.Audit.List(ctx, AuditFilter{UserID: "u1"})
	if len(byUser) != 2 {
		t.Errorf("filter by user returned %d", len(byUser))
	}

	byAction, _ := db.Audit.List(ctx, AuditFilter{Action: "container.start"})
	if len(byAction) != 2 {
		t.Errorf("filter by action returned %d", len(byAction))
	}

	byResult, _ := db.Audit.List(ctx, AuditFilter{Result: ResultError})
	if len(byResult) != 1 {
		t.Errorf("filter by result returned %d", len(byResult))
	}

	combined, _ := db.Audit.List(ctx, AuditFilter{UserID: "u1", Action: "container.start"})
	if len(combined) != 1 {
		t.Errorf("combined filter returned %d", len(combined))
	}
}

func TestAuditPaginationAndLimits(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if _, err := db.Audit.Append(ctx, AuditEntry{Action: "test", Result: ResultOK}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	page1, _ := db.Audit.List(ctx, AuditFilter{Limit: 4})
	if len(page1) != 4 {
		t.Fatalf("page1 = %d entries, want 4", len(page1))
	}
	page2, _ := db.Audit.List(ctx, AuditFilter{Limit: 4, Offset: 4})
	if len(page2) != 4 {
		t.Fatalf("page2 = %d entries, want 4", len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Error("offset had no effect")
	}

	// An absurd limit is clamped rather than honored.
	clamped, _ := db.Audit.List(ctx, AuditFilter{Limit: 999_999})
	if len(clamped) != 10 {
		t.Errorf("clamped query returned %d", len(clamped))
	}
}

func TestAuditTimeRangeAndPrune(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, _ = db.Audit.Append(ctx, AuditEntry{Action: "old", Result: ResultOK, CreatedAt: now.Add(-48 * time.Hour)})
	_, _ = db.Audit.Append(ctx, AuditEntry{Action: "new", Result: ResultOK, CreatedAt: now})

	recent, _ := db.Audit.List(ctx, AuditFilter{From: now.Add(-time.Hour)})
	if len(recent) != 1 || recent[0].Action != "new" {
		t.Errorf("time-filtered query = %+v", recent)
	}

	n, err := db.Audit.DeleteBefore(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteBefore() error = %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
}

func TestAuditDefaultsDetailToEmptyObject(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.Audit.Append(ctx, AuditEntry{Action: "x", Result: ResultOK}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries, _ := db.Audit.List(ctx, AuditFilter{})
	if entries[0].Detail != "{}" {
		t.Errorf("Detail = %q, want {} so clients can always parse it", entries[0].Detail)
	}
}

func TestLoginAttemptCounting(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	for i := 0; i < 3; i++ {
		if err := db.Logins.Record(ctx, "192.0.2.1", "alice", false); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	// A different IP must not contribute to this one's count.
	_ = db.Logins.Record(ctx, "192.0.2.99", "alice", false)

	n, err := db.Logins.FailuresSince(ctx, "192.0.2.1", since)
	if err != nil {
		t.Fatalf("FailuresSince() error = %v", err)
	}
	if n != 3 {
		t.Errorf("failures = %d, want 3", n)
	}
}

func TestSuccessfulLoginClearsTheFailureCount(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	_ = db.Logins.Record(ctx, "192.0.2.1", "alice", false)
	_ = db.Logins.Record(ctx, "192.0.2.1", "alice", false)
	// Someone who mistypes and then signs in should not stay near a lockout.
	_ = db.Logins.Record(ctx, "192.0.2.1", "alice", true)
	_ = db.Logins.Record(ctx, "192.0.2.1", "alice", false)

	n, err := db.Logins.FailuresSince(ctx, "192.0.2.1", since)
	if err != nil {
		t.Fatalf("FailuresSince() error = %v", err)
	}
	if n != 1 {
		t.Errorf("failures = %d, want only the one after the success", n)
	}
}

func TestLastFailureAt(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	at, err := db.Logins.LastFailureAt(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("LastFailureAt() error = %v", err)
	}
	if !at.IsZero() {
		t.Errorf("LastFailureAt = %v, want zero when there are no failures", at)
	}

	_ = db.Logins.Record(ctx, "192.0.2.1", "alice", false)

	at, err = db.Logins.LastFailureAt(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("LastFailureAt() error = %v", err)
	}
	if at.IsZero() {
		t.Error("LastFailureAt is zero after a recorded failure")
	}
}

func TestLoginAttemptPrune(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_ = db.Logins.Record(ctx, "192.0.2.1", "alice", false)

	n, err := db.Logins.DeleteBefore(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("DeleteBefore() error = %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.Settings.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}

	if err := db.Settings.Set(ctx, "theme", `"dark"`); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := db.Settings.Set(ctx, "theme", `"light"`); err != nil {
		t.Fatalf("Set() overwrite error = %v", err)
	}

	v, err := db.Settings.Get(ctx, "theme")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if v != `"light"` {
		t.Errorf("value = %q, want the overwritten one", v)
	}

	all, err := db.Settings.All(ctx)
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(all) != 1 {
		t.Errorf("All() = %v", all)
	}

	if err := db.Settings.Delete(ctx, "theme"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	// Deleting a missing key is not an error.
	if err := db.Settings.Delete(ctx, "theme"); err != nil {
		t.Errorf("second Delete() error = %v", err)
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []Role{RoleAdmin, RoleOperator, RoleViewer} {
		if !ValidRole(r) {
			t.Errorf("ValidRole(%q) = false", r)
		}
	}
	for _, r := range []Role{"", "root", "Admin"} {
		if ValidRole(r) {
			t.Errorf("ValidRole(%q) = true", r)
		}
	}
}

func TestRoleCheckConstraintIsEnforced(t *testing.T) {
	db := newTestDB(t)

	err := db.Users.Create(context.Background(), User{
		ID: "u1", Username: "mallory", Role: "superuser", PasswordHash: "x",
	})
	if err == nil {
		t.Fatal("Create() error = nil, want the schema to reject an unknown role")
	}
}
