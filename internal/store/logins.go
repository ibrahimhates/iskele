package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LoginAttemptRepo records authentication attempts for brute-force limiting.
type LoginAttemptRepo struct {
	db *sql.DB
}

// Record stores one attempt.
func (r *LoginAttemptRepo) Record(ctx context.Context, ip, username string, success bool) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO login_attempts (ip, username, success, created_at) VALUES (?, ?, ?, ?)`,
		ip, strings.ToLower(strings.TrimSpace(username)), boolToInt(success), nowString())
	if err != nil {
		return fmt.Errorf("record login attempt: %w", err)
	}
	return nil
}

// FailuresSince counts failed attempts from one IP after the given time.
//
// A successful login clears the count: only failures recorded after the most
// recent success are considered, so a legitimate user who mistypes twice and
// then succeeds does not stay one attempt away from a lockout.
func (r *LoginAttemptRepo) FailuresSince(ctx context.Context, ip string, since time.Time) (int, error) {
	sinceStr := since.UTC().Format(time.RFC3339Nano)

	var lastSuccess sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(created_at) FROM login_attempts
		WHERE ip = ? AND success = 1 AND created_at >= ?`, ip, sinceStr).Scan(&lastSuccess)
	if err != nil {
		return 0, fmt.Errorf("count login failures: %w", err)
	}

	from := sinceStr
	if lastSuccess.Valid && lastSuccess.String > from {
		from = lastSuccess.String
	}

	var n int
	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM login_attempts
		WHERE ip = ? AND success = 0 AND created_at > ?`, ip, from).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count login failures: %w", err)
	}
	return n, nil
}

// LastFailureAt returns when the most recent failure from an IP happened, or
// the zero time when there is none. The lockout window is measured from it.
func (r *LoginAttemptRepo) LastFailureAt(ctx context.Context, ip string) (time.Time, error) {
	var at sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM login_attempts WHERE ip = ? AND success = 0`, ip).Scan(&at)
	if err != nil {
		return time.Time{}, fmt.Errorf("read last login failure: %w", err)
	}
	if !at.Valid {
		return time.Time{}, nil
	}
	return parseTime(at.String)
}

// DeleteBefore prunes attempts older than cutoff.
func (r *LoginAttemptRepo) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM login_attempts WHERE created_at < ?`,
		cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune login attempts: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune login attempts: %w", err)
	}
	return n, nil
}
