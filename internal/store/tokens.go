package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TokenRepo stores API tokens.
type TokenRepo struct {
	db *sql.DB
}

//nolint:gosec // G101 false positive: this is a column list, not a credential.
const tokenColumns = `id, user_id, name, prefix, token_hash, scopes,
	created_at, expires_at, last_used_at, revoked_at`

// Create inserts an API token. The plaintext token is never stored.
func (r *TokenRepo) Create(ctx context.Context, t APIToken) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_tokens (`+tokenColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, t.Prefix, t.TokenHash, strings.Join(t.Scopes, ","),
		t.CreatedAt.Format(time.RFC3339Nano), formatOptional(t.ExpiresAt),
		formatOptional(t.LastUsedAt), formatOptional(t.RevokedAt),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("create api token: %w", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create api token: %w", err)
	}
	return nil
}

// ByHash looks a token up by the hash of its secret.
func (r *TokenRepo) ByHash(ctx context.Context, hash string) (APIToken, error) {
	t, err := scanToken(r.db.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM api_tokens WHERE token_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	return t, err
}

// ByID looks a token up by primary key.
func (r *TokenRepo) ByID(ctx context.Context, id string) (APIToken, error) {
	t, err := scanToken(r.db.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM api_tokens WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	return t, err
}

// ListForUser returns a user's tokens, newest first.
func (r *TokenRepo) ListForUser(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+tokenColumns+` FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api tokens: %w", err)
	}
	return out, nil
}

// Revoke marks a token as unusable.
func (r *TokenRepo) Revoke(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at = ''`,
		nowString(), id)
	if err != nil {
		return fmt.Errorf("revoke api token %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke api token %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("api token %s: %w", id, ErrNotFound)
	}
	return nil
}

// Touch records that a token was just used to authenticate.
func (r *TokenRepo) Touch(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("touch api token %s: %w", id, err)
	}
	return nil
}

func scanToken(s scanner) (APIToken, error) {
	var (
		out                                         APIToken
		scopes                                      string
		createdAt, expiresAt, lastUsedAt, revokedAt string
	)
	err := s.Scan(&out.ID, &out.UserID, &out.Name, &out.Prefix, &out.TokenHash, &scopes,
		&createdAt, &expiresAt, &lastUsedAt, &revokedAt)
	if err != nil {
		return APIToken{}, err
	}

	out.Scopes = splitScopes(scopes)
	if out.CreatedAt, err = parseTime(createdAt); err != nil {
		return APIToken{}, err
	}
	if out.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return APIToken{}, err
	}
	if out.LastUsedAt, err = parseTime(lastUsedAt); err != nil {
		return APIToken{}, err
	}
	if out.RevokedAt, err = parseTime(revokedAt); err != nil {
		return APIToken{}, err
	}
	return out, nil
}

func splitScopes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
