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

// Registry is a private image registry and the credential for it.
//
// Password holds ciphertext; it is tagged "-" so it cannot reach a JSON
// response by accident, which is the failure mode that matters here.
type Registry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Server     string    `json:"server"`
	Username   string    `json:"username"`
	Password   string    `json:"-"`
	Email      string    `json:"email,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	// HasPassword tells the UI whether a credential is stored without
	// revealing it.
	HasPassword bool `json:"has_password"`
}

// MarshalJSON omits a never-used timestamp rather than sending the zero time.
//
// encoding/json's omitempty does not apply to a struct, so a zero time.Time
// goes out as "0001-01-01T00:00:00Z" — which a UI renders as a date two
// thousand years ago rather than as "never".
func (r Registry) MarshalJSON() ([]byte, error) {
	type plain Registry // a distinct type, so this method does not recurse

	if r.LastUsedAt.IsZero() {
		return json.Marshal(struct {
			plain
			// Shadows the embedded field at depth 0, so a nil pointer with
			// omitempty drops it from the output.
			LastUsedAt *time.Time `json:"last_used_at,omitempty"`
		}{plain: plain(r)})
	}
	return json.Marshal(plain(r))
}

// RegistryRepo stores registry credentials.
type RegistryRepo struct {
	db *sql.DB
}

const registryColumns = `id, name, server, username, password, email,
	created_at, updated_at, last_used_at`

// Create inserts a registry, stamping the timestamps onto the caller's copy so
// the response carries what was actually written.
func (r *RegistryRepo) Create(ctx context.Context, reg *Registry) error {
	now := time.Now().UTC()
	if reg.CreatedAt.IsZero() {
		reg.CreatedAt = now
	}
	reg.UpdatedAt = now
	reg.HasPassword = reg.Password != ""

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO registries (`+registryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reg.ID, reg.Name, reg.Server, reg.Username, reg.Password, reg.Email,
		reg.CreatedAt.Format(time.RFC3339Nano), reg.UpdatedAt.Format(time.RFC3339Nano),
		formatOptional(reg.LastUsedAt),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("create registry: %w", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create registry: %w", err)
	}
	return nil
}

// Update replaces a registry's mutable fields.
//
// An empty password means "keep the stored one": the UI cannot send back a
// credential it was never given, so a blank field on an edit form must not
// erase it.
func (r *RegistryRepo) Update(ctx context.Context, reg *Registry) error {
	reg.UpdatedAt = time.Now().UTC()

	query := `UPDATE registries SET name = ?, server = ?, username = ?, email = ?, updated_at = ?`
	args := []any{reg.Name, reg.Server, reg.Username, reg.Email, reg.UpdatedAt.Format(time.RFC3339Nano)}

	if reg.Password != "" {
		query += `, password = ?`
		args = append(args, reg.Password)
	}
	query += ` WHERE id = ?`
	args = append(args, reg.ID)

	result, err := r.db.ExecContext(ctx, query, args...)
	if isUniqueViolation(err) {
		return fmt.Errorf("update registry: %w", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("update registry: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ByID looks a registry up by primary key.
func (r *RegistryRepo) ByID(ctx context.Context, id string) (Registry, error) {
	reg, err := scanRegistry(r.db.QueryRowContext(ctx,
		`SELECT `+registryColumns+` FROM registries WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Registry{}, ErrNotFound
	}
	return reg, err
}

// ByServer looks a registry up by its normalized host.
func (r *RegistryRepo) ByServer(ctx context.Context, server string) (Registry, error) {
	reg, err := scanRegistry(r.db.QueryRowContext(ctx,
		`SELECT `+registryColumns+` FROM registries WHERE server = ?`, server))
	if errors.Is(err, sql.ErrNoRows) {
		return Registry{}, ErrNotFound
	}
	return reg, err
}

// List returns every registry, oldest first.
func (r *RegistryRepo) List(ctx context.Context) ([]Registry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+registryColumns+` FROM registries ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list registries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Registry{}
	for rows.Next() {
		reg, scanErr := scanRegistry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, reg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registries: %w", err)
	}
	return out, nil
}

// Delete removes a registry.
func (r *RegistryRepo) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM registries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete registry: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchLastUsed records that a pull authenticated with this credential.
func (r *RegistryRepo) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE registries SET last_used_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("touch registry: %w", err)
	}
	return nil
}

// scanRegistry reads one row.
func scanRegistry(row interface{ Scan(...any) error }) (Registry, error) {
	var (
		reg        Registry
		createdAt  string
		updatedAt  string
		lastUsedAt sql.NullString
		err        error
	)

	err = row.Scan(&reg.ID, &reg.Name, &reg.Server, &reg.Username, &reg.Password,
		&reg.Email, &createdAt, &updatedAt, &lastUsedAt)
	if err != nil {
		return Registry{}, err
	}

	if reg.CreatedAt, err = parseTime(createdAt); err != nil {
		return Registry{}, err
	}
	if reg.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Registry{}, err
	}
	if lastUsedAt.Valid {
		if reg.LastUsedAt, err = parseTime(lastUsedAt.String); err != nil {
			return Registry{}, err
		}
	}
	reg.HasPassword = reg.Password != ""
	return reg, nil
}

// NormalizeRegistryServer reduces a user-typed host to the form a lookup by
// image reference will produce.
//
// Docker Hub is the awkward case: an image called "nginx" resolves to
// registry-1.docker.io, but an operator types "docker.io". Both map to the
// same canonical name so one credential covers both.
func NormalizeRegistryServer(server string) string {
	s := strings.TrimSpace(strings.ToLower(server))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, "/")

	switch s {
	case "", "docker.io", "index.docker.io", "registry-1.docker.io", "registry.hub.docker.com":
		return "docker.io"
	default:
		return s
	}
}

// RegistryServerForImage extracts the registry host from an image reference.
//
// The rule the Docker CLI uses: the part before the first slash is a registry
// only when it contains a dot or a colon, or is exactly "localhost". Anything
// else is a Docker Hub namespace, so "library/nginx" is Hub and
// "ghcr.io/org/app" is not.
func RegistryServerForImage(ref string) string {
	trimmed := strings.TrimSpace(ref)
	head, _, found := strings.Cut(trimmed, "/")
	if !found {
		return "docker.io"
	}
	if head == "localhost" || strings.ContainsAny(head, ".:") {
		return NormalizeRegistryServer(head)
	}
	return "docker.io"
}
