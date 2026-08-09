// Package store is the SQLite persistence layer.
//
// It owns the schema, the migrations and every query. Nothing above it writes
// SQL, and nothing in it knows about Docker or HTTP.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	// Registers the "sqlite" driver. modernc's implementation is pure Go, so
	// the binary stays cgo-free and cross-compilable.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned by lookups that matched no row. Callers use
// errors.Is rather than comparing against sql.ErrNoRows, which stays inside
// this package.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write violates a uniqueness constraint.
var ErrConflict = errors.New("conflict")

// DB wraps the SQLite handle and exposes the repositories.
type DB struct {
	sql *sql.DB

	Users    *UserRepo
	Sessions *SessionRepo
	Tokens   *TokenRepo
	Audit    *AuditRepo
	Logins   *LoginAttemptRepo
	Settings *SettingsRepo

	Registries *RegistryRepo
	Builds     *BuildRepo
}

// Options configures Open.
type Options struct {
	// Path is the database file. Use ":memory:" in tests.
	Path string
	// BusyTimeout is how long a writer waits for a lock before failing.
	BusyTimeout time.Duration
}

// Open opens (creating if needed) the database at path and applies every
// pending migration.
func Open(ctx context.Context, opts Options) (*DB, error) {
	if opts.Path == "" {
		return nil, errors.New("store: database path is required")
	}
	if opts.BusyTimeout <= 0 {
		opts.BusyTimeout = 5 * time.Second
	}

	handle, err := sql.Open("sqlite", dsn(opts))
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", opts.Path, err)
	}

	// SQLite allows exactly one writer. Serializing through a single
	// connection avoids SQLITE_BUSY entirely on the write path; reads are
	// fast enough at single-host scale that the simplicity is worth it.
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	handle.SetConnMaxLifetime(0)

	if err := handle.PingContext(ctx); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("connect to database %s: %w", opts.Path, err)
	}

	db := &DB{sql: handle}
	db.Users = &UserRepo{db: handle}
	db.Sessions = &SessionRepo{db: handle}
	db.Tokens = &TokenRepo{db: handle}
	db.Audit = &AuditRepo{db: handle}
	db.Logins = &LoginAttemptRepo{db: handle}
	db.Settings = &SettingsRepo{db: handle}
	db.Registries = &RegistryRepo{db: handle}
	db.Builds = &BuildRepo{db: handle}

	if err := db.Migrate(ctx); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return db, nil
}

// dsn builds the connection string, including the pragmas that must be set
// before any query runs.
func dsn(opts Options) string {
	params := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
		// Balances durability against fsync cost; WAL makes this safe against
		// process crashes, losing at most the last transaction on power loss.
		"_pragma=synchronous(NORMAL)",
		"_pragma=busy_timeout(" + strconv.FormatInt(opts.BusyTimeout.Milliseconds(), 10) + ")",
	}
	return opts.Path + "?" + strings.Join(params, "&")
}

// SQL exposes the raw handle for the repositories and tests.
func (db *DB) SQL() *sql.DB { return db.sql }

// Close releases the database handle.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

// migration is one numbered SQL file.
type migration struct {
	version int
	name    string
	body    string
}

// Migrate applies every migration not yet recorded, in order and each in its
// own transaction. It is safe to call repeatedly.
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := db.appliedVersions(ctx)
	if err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if _, done := applied[m.version]; done {
			continue
		}
		if err := db.apply(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// AppliedMigrations returns the versions recorded as applied, for diagnostics.
func (db *DB) AppliedMigrations(ctx context.Context) ([]int, error) {
	applied, err := db.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(applied))
	for v := range applied {
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

func (db *DB) appliedVersions(ctx context.Context) (map[int]struct{}, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := map[int]struct{}{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return applied, nil
}

func (db *DB) apply(ctx context.Context, m migration) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", m.version, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, execErr := tx.ExecContext(ctx, m.body); execErr != nil {
		return fmt.Errorf("apply migration %04d_%s: %w", m.version, m.name, execErr)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, nowString())
	if err != nil {
		return fmt.Errorf("record migration %d: %w", m.version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", m.version, err)
	}
	return nil
}

// loadMigrations reads the embedded files, sorted by version.
//
// File names must be NNNN_name.sql; anything else is a packaging mistake and
// fails loudly rather than being silently skipped.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}

		base := strings.TrimSuffix(e.Name(), ".sql")
		numPart, namePart, found := strings.Cut(base, "_")
		if !found {
			return nil, fmt.Errorf("migration %q: want NNNN_name.sql", e.Name())
		}
		version, err := strconv.Atoi(numPart)
		if err != nil {
			return nil, fmt.Errorf("migration %q: %q is not a version number", e.Name(), numPart)
		}

		body, err := migrationFS.ReadFile(filepath.Join("migrations", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}

		out = append(out, migration{version: version, name: namePart, body: string(body)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })

	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

// nowString renders a timestamp in the single format the schema uses, so
// lexical ordering matches chronological ordering.
func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// parseTime reads a timestamp written by nowString.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure.
//
// modernc's driver reports these as text; matching on it keeps the driver's
// error type out of the repositories.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed: unique")
}
