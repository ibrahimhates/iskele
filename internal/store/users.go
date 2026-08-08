package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UserRepo stores accounts.
type UserRepo struct {
	db *sql.DB
}

const userColumns = `id, username, username_lower, password_hash, role,
	totp_secret_enc, totp_enabled, disabled, created_at, updated_at, last_login_at`

// Count returns the number of accounts. Zero means the installation has not
// been bootstrapped yet.
func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// Create inserts a user. The caller supplies the ID and the already-hashed
// password: this layer never hashes, so the cost parameters stay in one place.
func (r *UserRepo) Create(ctx context.Context, u User) error {
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (`+userColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, strings.ToLower(u.Username), u.PasswordHash, string(u.Role),
		u.TOTPSecretEnc, boolToInt(u.TOTPEnabled), boolToInt(u.Disabled),
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano),
		formatOptional(u.LastLoginAt),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("create user %q: %w", u.Username, ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// ByID looks a user up by primary key.
func (r *UserRepo) ByID(ctx context.Context, id string) (User, error) {
	return r.scanOne(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
}

// ByUsername looks a user up case-insensitively.
func (r *UserRepo) ByUsername(ctx context.Context, username string) (User, error) {
	return r.scanOne(ctx,
		`SELECT `+userColumns+` FROM users WHERE username_lower = ?`,
		strings.ToLower(strings.TrimSpace(username)))
}

// List returns every user, oldest first.
func (r *UserRepo) List(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return out, nil
}

// UpdatePassword replaces the stored hash.
func (r *UserRepo) UpdatePassword(ctx context.Context, id, hash string) error {
	return r.exec(ctx, id, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hash, nowString(), id)
}

// UpdateRole changes a user's role.
func (r *UserRepo) UpdateRole(ctx context.Context, id string, role Role) error {
	return r.exec(ctx, id, `UPDATE users SET role = ?, updated_at = ? WHERE id = ?`,
		string(role), nowString(), id)
}

// SetDisabled enables or disables an account.
func (r *UserRepo) SetDisabled(ctx context.Context, id string, disabled bool) error {
	return r.exec(ctx, id, `UPDATE users SET disabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(disabled), nowString(), id)
}

// SetTOTP stores (or clears) the encrypted TOTP secret.
func (r *UserRepo) SetTOTP(ctx context.Context, id, secretEnc string, enabled bool) error {
	return r.exec(ctx, id,
		`UPDATE users SET totp_secret_enc = ?, totp_enabled = ?, updated_at = ? WHERE id = ?`,
		secretEnc, boolToInt(enabled), nowString(), id)
}

// TouchLogin records a successful sign-in.
func (r *UserRepo) TouchLogin(ctx context.Context, id string, at time.Time) error {
	return r.exec(ctx, id, `UPDATE users SET last_login_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id)
}

// Delete removes a user; sessions and tokens cascade.
func (r *UserRepo) Delete(ctx context.Context, id string) error {
	return r.exec(ctx, id, `DELETE FROM users WHERE id = ?`, id)
}

// exec runs a statement and turns "no rows affected" into ErrNotFound.
func (r *UserRepo) exec(ctx context.Context, id, query string, args ...any) error {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update user %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	return nil
}

func (r *UserRepo) scanOne(ctx context.Context, query string, args ...any) (User, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (User, error) {
	var (
		u                                     User
		role, createdAt, updatedAt, lastLogin string
		usernameLower                         string
		totpEnabled, disabled                 int
	)
	err := s.Scan(&u.ID, &u.Username, &usernameLower, &u.PasswordHash, &role,
		&u.TOTPSecretEnc, &totpEnabled, &disabled, &createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return User{}, err
	}

	u.Role = Role(role)
	u.TOTPEnabled = totpEnabled == 1
	u.Disabled = disabled == 1
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return User{}, err
	}
	if u.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return User{}, err
	}
	if u.LastLoginAt, err = parseTime(lastLogin); err != nil {
		return User{}, err
	}
	return u, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// formatOptional renders a timestamp, using "" for the zero value so the
// column can express "never".
func formatOptional(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
