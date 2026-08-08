package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SessionRepo stores refresh-token grants.
type SessionRepo struct {
	db *sql.DB
}

const sessionColumns = `id, user_id, refresh_hash, ip, user_agent,
	created_at, expires_at, revoked_at, last_used_at`

// Create inserts a session.
func (r *SessionRepo) Create(ctx context.Context, s Session) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (`+sessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.RefreshHash, s.IP, s.UserAgent,
		s.CreatedAt.Format(time.RFC3339Nano), s.ExpiresAt.UTC().Format(time.RFC3339Nano),
		formatOptional(s.RevokedAt), formatOptional(s.LastUsedAt),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("create session: %w", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// ByRefreshHash looks a session up by the hash of its refresh token.
func (r *SessionRepo) ByRefreshHash(ctx context.Context, hash string) (Session, error) {
	s, err := scanSession(r.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE refresh_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return s, err
}

// Revoke marks one session as no longer usable. Revoking an already-revoked
// session is a no-op, so a repeated logout is not an error.
func (r *SessionRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at = ''`,
		nowString(), id)
	if err != nil {
		return fmt.Errorf("revoke session %s: %w", id, err)
	}
	return nil
}

// RevokeAllForUser invalidates every session of a user, used when an account
// is disabled, deleted, or has its password reset.
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at = ''`,
		nowString(), userID)
	if err != nil {
		return fmt.Errorf("revoke sessions of user %s: %w", userID, err)
	}
	return nil
}

// Touch records that a session was just exchanged.
func (r *SessionRepo) Touch(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET last_used_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("touch session %s: %w", id, err)
	}
	return nil
}

// ListForUser returns a user's sessions, newest first.
func (r *SessionRepo) ListForUser(ctx context.Context, userID string) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return out, nil
}

// DeleteExpired removes sessions that expired before cutoff, along with
// revoked ones. Called periodically so the table does not grow forever.
func (r *SessionRepo) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ? OR (revoked_at != '' AND revoked_at < ?)`,
		cutoff.UTC().Format(time.RFC3339Nano), cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return n, nil
}

func scanSession(s scanner) (Session, error) {
	var (
		out                                         Session
		createdAt, expiresAt, revokedAt, lastUsedAt string
	)
	err := s.Scan(&out.ID, &out.UserID, &out.RefreshHash, &out.IP, &out.UserAgent,
		&createdAt, &expiresAt, &revokedAt, &lastUsedAt)
	if err != nil {
		return Session{}, err
	}
	if out.CreatedAt, err = parseTime(createdAt); err != nil {
		return Session{}, err
	}
	if out.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return Session{}, err
	}
	if out.RevokedAt, err = parseTime(revokedAt); err != nil {
		return Session{}, err
	}
	if out.LastUsedAt, err = parseTime(lastUsedAt); err != nil {
		return Session{}, err
	}
	return out, nil
}
