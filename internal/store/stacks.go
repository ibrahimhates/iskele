package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// StackSource says where a stack's compose file comes from.
type StackSource string

// Stack sources.
const (
	// StackSourceEditor is a compose file typed into the panel. It has no
	// canonical copy anywhere else.
	StackSourceEditor StackSource = "editor"
	// StackSourceFile reads a compose file from this host, inside allowed_paths.
	StackSourceFile StackSource = "file"
	// StackSourceGit clones a repository and reads the compose file from it.
	StackSourceGit StackSource = "git"
)

// Valid reports whether a source is one Iskele knows.
func (s StackSource) Valid() bool {
	switch s {
	case StackSourceEditor, StackSourceFile, StackSourceGit:
		return true
	default:
		return false
	}
}

// StackStatus is what the last deploy did.
//
// It is not what the containers are doing now: the engine is the authority on
// that, and is asked directly whenever a stack is read.
type StackStatus string

// Stack statuses.
const (
	StackCreated   StackStatus = "created"
	StackDeploying StackStatus = "deploying"
	StackDeployed  StackStatus = "deployed"
	StackFailed    StackStatus = "failed"
	StackStopped   StackStatus = "stopped"
)

// Stack is one compose project Iskele manages.
type Stack struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Source StackSource `json:"source"`

	// Path is the compose file's location for a file-backed stack.
	Path string `json:"path,omitempty"`
	// GitURL, GitRef and GitCommit describe a git-backed stack's working copy.
	GitURL    string `json:"git_url,omitempty"`
	GitRef    string `json:"git_ref,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`

	// Compose and Env are the content that was last deployed, whatever the
	// source. For a file or git stack this is the copy that actually ran, which
	// is the copy worth having once the working tree has moved on.
	Compose string `json:"compose"`
	// Env is withheld from listings — it routinely holds passwords — and sent
	// only when a single stack is read.
	Env string `json:"env,omitempty"`

	// WorkingDir is what relative paths in the compose file resolve against.
	WorkingDir string `json:"working_dir,omitempty"`

	Status         StackStatus `json:"status"`
	LastError      string      `json:"last_error,omitempty"`
	LastDeployedAt time.Time   `json:"last_deployed_at,omitempty"`

	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedByID string    `json:"created_by_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MarshalJSON omits a never-deployed stack's zero timestamp, which would
// otherwise go out as a date two thousand years ago.
func (s Stack) MarshalJSON() ([]byte, error) {
	type plain Stack

	payload := struct {
		plain
		LastDeployedAt *time.Time `json:"last_deployed_at,omitempty"`
	}{plain: plain(s)}

	if !s.LastDeployedAt.IsZero() {
		deployed := s.LastDeployedAt
		payload.LastDeployedAt = &deployed
	}
	return json.Marshal(payload)
}

// StackRepo stores stacks.
type StackRepo struct {
	db *sql.DB
}

const stackColumns = `id, name, source, path, git_url, git_ref, git_commit,
	compose_content, env_content, working_dir, status, last_error, last_deployed_at,
	created_by, created_by_id, created_at, updated_at`

// Create inserts a stack, stamping the timestamps onto the caller's copy.
func (r *StackRepo) Create(ctx context.Context, s *Stack) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.Status == "" {
		s.Status = StackCreated
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO stacks (`+stackColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, string(s.Source), s.Path, s.GitURL, s.GitRef, s.GitCommit,
		s.Compose, s.Env, s.WorkingDir, string(s.Status), s.LastError,
		formatOptional(s.LastDeployedAt), s.CreatedBy, s.CreatedByID,
		s.CreatedAt.Format(time.RFC3339Nano), s.UpdatedAt.Format(time.RFC3339Nano),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("create stack: %w", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create stack: %w", err)
	}
	return nil
}

// Update replaces a stack's editable fields.
//
// The name is not among them: it labels every container the stack created, so
// changing it would orphan them all.
func (r *StackRepo) Update(ctx context.Context, s *Stack) error {
	s.UpdatedAt = time.Now().UTC()

	result, err := r.db.ExecContext(ctx, `
		UPDATE stacks
		SET source = ?, path = ?, git_url = ?, git_ref = ?, git_commit = ?,
		    compose_content = ?, env_content = ?, working_dir = ?, updated_at = ?
		WHERE id = ?`,
		string(s.Source), s.Path, s.GitURL, s.GitRef, s.GitCommit,
		s.Compose, s.Env, s.WorkingDir, s.UpdatedAt.Format(time.RFC3339Nano), s.ID)
	if err != nil {
		return fmt.Errorf("update stack: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetStatus records what a deploy did.
func (r *StackRepo) SetStatus(ctx context.Context, id string, status StackStatus, failure string) error {
	query := `UPDATE stacks SET status = ?, last_error = ?, updated_at = ?`
	args := []any{string(status), failure, time.Now().UTC().Format(time.RFC3339Nano)}

	if status == StackDeployed {
		query += `, last_deployed_at = ?`
		args = append(args, time.Now().UTC().Format(time.RFC3339Nano))
	}
	query += ` WHERE id = ?`
	args = append(args, id)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("set stack status: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ByID looks a stack up by primary key.
func (r *StackRepo) ByID(ctx context.Context, id string) (Stack, error) {
	s, err := scanStack(r.db.QueryRowContext(ctx,
		`SELECT `+stackColumns+` FROM stacks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Stack{}, ErrNotFound
	}
	return s, err
}

// ByName looks a stack up by its unique name, which is what a container's
// labels carry.
func (r *StackRepo) ByName(ctx context.Context, name string) (Stack, error) {
	s, err := scanStack(r.db.QueryRowContext(ctx,
		`SELECT `+stackColumns+` FROM stacks WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Stack{}, ErrNotFound
	}
	return s, err
}

// List returns every stack, most recently touched first.
func (r *StackRepo) List(ctx context.Context) ([]Stack, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+stackColumns+` FROM stacks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list stacks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Stack{}
	for rows.Next() {
		s, scanErr := scanStack(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stacks: %w", err)
	}
	return out, nil
}

// Delete removes a stack record.
//
// The containers it created are not touched here: removing them is `down`, and
// an operator who deletes the record after stopping the containers by hand
// should not have them come back.
func (r *StackRepo) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM stacks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete stack: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Deploying returns the stacks a deploy was in the middle of.
//
// A deploy is bound to the process running it, so a row still marked deploying
// after a restart can never finish on its own.
func (r *StackRepo) Deploying(ctx context.Context) ([]Stack, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+stackColumns+` FROM stacks WHERE status = 'deploying'`)
	if err != nil {
		return nil, fmt.Errorf("list deploying stacks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Stack{}
	for rows.Next() {
		s, scanErr := scanStack(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// scanStack reads one row.
func scanStack(row interface{ Scan(...any) error }) (Stack, error) {
	var (
		s          Stack
		source     string
		status     string
		deployedAt sql.NullString
		createdAt  string
		updatedAt  string
		err        error
	)

	err = row.Scan(&s.ID, &s.Name, &source, &s.Path, &s.GitURL, &s.GitRef, &s.GitCommit,
		&s.Compose, &s.Env, &s.WorkingDir, &status, &s.LastError, &deployedAt,
		&s.CreatedBy, &s.CreatedByID, &createdAt, &updatedAt)
	if err != nil {
		return Stack{}, err
	}

	s.Source = StackSource(source)
	s.Status = StackStatus(status)

	if s.CreatedAt, err = parseTime(createdAt); err != nil {
		return Stack{}, err
	}
	if s.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Stack{}, err
	}
	if deployedAt.Valid {
		if s.LastDeployedAt, err = parseTime(deployedAt.String); err != nil {
			return Stack{}, err
		}
	}
	return s, nil
}
