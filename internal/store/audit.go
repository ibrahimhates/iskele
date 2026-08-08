package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AuditRepo stores the audit trail.
type AuditRepo struct {
	db *sql.DB
}

const auditColumns = `id, user_id, username, action, resource_type, resource_id,
	result, detail, ip, user_agent, created_at`

// Append writes one audit record and returns its ID.
func (r *AuditRepo) Append(ctx context.Context, e AuditEntry) (int64, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Result == "" {
		e.Result = ResultOK
	}
	if e.Detail == "" {
		e.Detail = "{}"
	}

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO audit_logs (user_id, username, action, resource_type, resource_id,
			result, detail, ip, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.Username, e.Action, e.ResourceType, e.ResourceID,
		e.Result, e.Detail, e.IP, e.UserAgent, e.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("append audit entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("append audit entry: %w", err)
	}
	return id, nil
}

// AuditFilter narrows an audit query. Zero values mean "no restriction".
type AuditFilter struct {
	UserID       string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	From         time.Time
	To           time.Time
	Limit        int
	Offset       int
}

// defaultAuditLimit bounds an unfiltered query so a UI mistake cannot pull the
// whole table into memory.
const defaultAuditLimit = 200

// maxAuditLimit is the largest page the API will serve.
const maxAuditLimit = 1000

// List returns matching entries, newest first.
func (r *AuditRepo) List(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	var (
		where []string
		args  []any
	)
	add := func(clause string, value any) {
		where = append(where, clause)
		args = append(args, value)
	}

	if f.UserID != "" {
		add("user_id = ?", f.UserID)
	}
	if f.Action != "" {
		add("action = ?", f.Action)
	}
	if f.ResourceType != "" {
		add("resource_type = ?", f.ResourceType)
	}
	if f.ResourceID != "" {
		add("resource_id = ?", f.ResourceID)
	}
	if f.Result != "" {
		add("result = ?", f.Result)
	}
	if !f.From.IsZero() {
		add("created_at >= ?", f.From.UTC().Format(time.RFC3339Nano))
	}
	if !f.To.IsZero() {
		add("created_at <= ?", f.To.UTC().Format(time.RFC3339Nano))
	}

	query := `SELECT ` + auditColumns + ` FROM audit_logs`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ? OFFSET ?"

	limit := f.Limit
	switch {
	case limit <= 0:
		limit = defaultAuditLimit
	case limit > maxAuditLimit:
		limit = maxAuditLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AuditEntry
	for rows.Next() {
		e, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit entries: %w", err)
	}
	return out, nil
}

// DeleteBefore prunes entries older than cutoff and reports how many went.
func (r *AuditRepo) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at < ?`,
		cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune audit entries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune audit entries: %w", err)
	}
	return n, nil
}

func scanAudit(s scanner) (AuditEntry, error) {
	var (
		e         AuditEntry
		createdAt string
	)
	err := s.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.ResourceType, &e.ResourceID,
		&e.Result, &e.Detail, &e.IP, &e.UserAgent, &createdAt)
	if err != nil {
		return AuditEntry{}, err
	}
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return AuditEntry{}, err
	}
	return e, nil
}
