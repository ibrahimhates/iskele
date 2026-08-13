package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BuildStatus is where a build got to.
type BuildStatus string

// Build statuses.
const (
	BuildRunning  BuildStatus = "running"
	BuildSuccess  BuildStatus = "success"
	BuildFailed   BuildStatus = "failed"
	BuildCanceled BuildStatus = "canceled"
)

// Terminal reports whether a status can still change.
func (s BuildStatus) Terminal() bool { return s != BuildRunning }

// Build is one image build started from the panel.
type Build struct {
	ID         string      `json:"id"`
	UserID     string      `json:"user_id,omitempty"`
	Username   string      `json:"username,omitempty"`
	ContextDir string      `json:"context_dir"`
	Dockerfile string      `json:"dockerfile"`
	Tags       []string    `json:"tags"`
	Target     string      `json:"target,omitempty"`
	Platform   string      `json:"platform,omitempty"`
	NoCache    bool        `json:"no_cache"`
	Pull       bool        `json:"pull"`
	Status     BuildStatus `json:"status"`
	ImageID    string      `json:"image_id,omitempty"`
	Error      string      `json:"error,omitempty"`

	ContextFiles int   `json:"context_files"`
	ContextBytes int64 `json:"context_bytes"`

	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
	LogArchived bool      `json:"log_archived"`
}

// Duration is how long the build ran, or how long it has been running.
func (b Build) Duration() time.Duration {
	if b.FinishedAt.IsZero() {
		return time.Since(b.StartedAt)
	}
	return b.FinishedAt.Sub(b.StartedAt)
}

// MarshalJSON omits an unfinished build's zero finish time, which would
// otherwise go out as a date two thousand years ago.
func (b Build) MarshalJSON() ([]byte, error) {
	type plain Build

	payload := struct {
		plain
		FinishedAt *time.Time `json:"finished_at,omitempty"`
		// DurationMS saves every client from recomputing it, and from getting
		// a running build's elapsed time wrong.
		DurationMS int64 `json:"duration_ms"`
	}{plain: plain(b), DurationMS: b.Duration().Milliseconds()}

	if !b.FinishedAt.IsZero() {
		finished := b.FinishedAt
		payload.FinishedAt = &finished
	}
	return json.Marshal(payload)
}

// BuildRepo stores build records.
type BuildRepo struct {
	db *sql.DB
}

const buildColumns = `id, user_id, username, context_dir, dockerfile, tags, target,
	platform, no_cache, pull, status, image_id, error, context_files, context_bytes,
	started_at, finished_at, log_archived`

// Create inserts a build, stamping StartedAt onto the caller's copy.
func (r *BuildRepo) Create(ctx context.Context, b *Build) error {
	if b.StartedAt.IsZero() {
		b.StartedAt = time.Now().UTC()
	}
	if b.Status == "" {
		b.Status = BuildRunning
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO builds (`+buildColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.UserID, b.Username, b.ContextDir, b.Dockerfile, strings.Join(b.Tags, ","),
		b.Target, b.Platform, b.NoCache, b.Pull, string(b.Status), b.ImageID, b.Error,
		b.ContextFiles, b.ContextBytes, b.StartedAt.Format(time.RFC3339Nano),
		formatOptional(b.FinishedAt), b.LogArchived,
	)
	if err != nil {
		return fmt.Errorf("create build: %w", err)
	}
	return nil
}

// Finish records a build's outcome. It is a no-op on a build that already has
// one: the first terminal state is the real one, and a late writer must not
// overwrite it.
func (r *BuildRepo) Finish(ctx context.Context, id string, status BuildStatus, imageID, buildErr string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE builds
		SET status = ?, image_id = ?, error = ?, finished_at = ?, log_archived = 1
		WHERE id = ? AND status = 'running'`,
		string(status), imageID, buildErr, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("finish build: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		// Either the build is gone or it already finished; neither is worth
		// failing the caller over, but the caller may want to know.
		return ErrNotFound
	}
	return nil
}

// SetContextStats records what the context turned out to hold.
func (r *BuildRepo) SetContextStats(ctx context.Context, id string, files int, bytes int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE builds SET context_files = ?, context_bytes = ? WHERE id = ?`, files, bytes, id)
	if err != nil {
		return fmt.Errorf("record build context stats: %w", err)
	}
	return nil
}

// ByID looks a build up by primary key.
func (r *BuildRepo) ByID(ctx context.Context, id string) (Build, error) {
	b, err := scanBuild(r.db.QueryRowContext(ctx,
		`SELECT `+buildColumns+` FROM builds WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Build{}, ErrNotFound
	}
	return b, err
}

// BuildFilter narrows a listing.
type BuildFilter struct {
	Status BuildStatus
	Limit  int
}

// List returns builds newest first.
func (r *BuildRepo) List(ctx context.Context, filter BuildFilter) ([]Build, error) {
	query := `SELECT ` + buildColumns + ` FROM builds`
	var args []any

	if filter.Status != "" {
		query += ` WHERE status = ?`
		args = append(args, string(filter.Status))
	}

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Build{}
	for rows.Next() {
		b, scanErr := scanBuild(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate builds: %w", err)
	}
	return out, nil
}

// Running returns the builds still in flight, which is what a restart needs to
// reconcile: a build recorded as running by a process that is gone can never
// finish on its own.
func (r *BuildRepo) Running(ctx context.Context) ([]Build, error) {
	return r.List(ctx, BuildFilter{Status: BuildRunning, Limit: 500})
}

// MarkLogRemoved records that retention deleted a build's archived log.
func (r *BuildRepo) MarkLogRemoved(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE builds SET log_archived = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark build log removed: %w", err)
	}
	return nil
}

// DeleteOlderThan removes build rows past their retention and reports how many
// went, so the caller can log it.
func (r *BuildRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM builds WHERE status != 'running' AND started_at < ?`,
		cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune builds: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

// scanBuild reads one row.
func scanBuild(row interface{ Scan(...any) error }) (Build, error) {
	var (
		b          Build
		tags       string
		status     string
		startedAt  string
		finishedAt sql.NullString
		err        error
	)

	err = row.Scan(&b.ID, &b.UserID, &b.Username, &b.ContextDir, &b.Dockerfile, &tags,
		&b.Target, &b.Platform, &b.NoCache, &b.Pull, &status, &b.ImageID, &b.Error,
		&b.ContextFiles, &b.ContextBytes, &startedAt, &finishedAt, &b.LogArchived)
	if err != nil {
		return Build{}, err
	}

	b.Status = BuildStatus(status)
	b.Tags = splitTags(tags)
	if b.StartedAt, err = parseTime(startedAt); err != nil {
		return Build{}, err
	}
	if finishedAt.Valid {
		if b.FinishedAt, err = parseTime(finishedAt.String); err != nil {
			return Build{}, err
		}
	}
	return b, nil
}

// splitTags reverses the comma-joined storage, dropping the empty string an
// unset column would otherwise produce.
func splitTags(joined string) []string {
	if strings.TrimSpace(joined) == "" {
		return []string{}
	}
	parts := strings.Split(joined, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
