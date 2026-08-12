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

// auditWhere renders a filter as SQL conditions and their arguments.
//
// List and Count share it on purpose: a page that says "200 of 12,481" is
// wrong the moment the two build their conditions differently.
func auditWhere(f AuditFilter) (where []string, args []any) {
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
	return where, args
}

// List returns matching entries, newest first.
func (r *AuditRepo) List(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	where, args := auditWhere(f)

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

// Count reports how many entries match, ignoring Limit and Offset.
//
// The listing is paged, and a page is meaningless without knowing how many
// there are: "200 of 12,481" and "200 of 200" ask an operator for very
// different next steps.
func (r *AuditRepo) Count(ctx context.Context, f AuditFilter) (int, error) {
	where, args := auditWhere(f)

	query := `SELECT COUNT(*) FROM audit_logs`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count audit entries: %w", err)
	}
	return total, nil
}

// Facets are the distinct values present in the trail, for building filters.
type Facets struct {
	Actions       []string
	ResourceTypes []string
	Actors        []AuditActor
}

// AuditActor is one account that appears in the trail. The username is stored
// on the record itself, so an actor survives the deletion of their account —
// which is the entire point of an audit log.
type AuditActor struct {
	UserID   string `json:"user_id,omitempty"`
	Username string `json:"username"`
}

// Facets lists the distinct actions, resource types and actors on record.
func (r *AuditRepo) Facets(ctx context.Context) (Facets, error) {
	var out Facets

	actions, err := r.distinct(ctx, "action")
	if err != nil {
		return Facets{}, err
	}
	out.Actions = actions

	types, err := r.distinct(ctx, "resource_type")
	if err != nil {
		return Facets{}, err
	}
	out.ResourceTypes = types

	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT user_id, username FROM audit_logs
		WHERE username != '' ORDER BY username`)
	if err != nil {
		return Facets{}, fmt.Errorf("list audit actors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out.Actors = []AuditActor{}
	for rows.Next() {
		var actor AuditActor
		if err := rows.Scan(&actor.UserID, &actor.Username); err != nil {
			return Facets{}, fmt.Errorf("scan audit actor: %w", err)
		}
		out.Actors = append(out.Actors, actor)
	}
	if err := rows.Err(); err != nil {
		return Facets{}, fmt.Errorf("iterate audit actors: %w", err)
	}
	return out, nil
}

// facetQueries are the only distinct-value queries this repository runs.
//
// A column name cannot be bound as a query argument, so the alternative is
// interpolating one — and then the safety of every call depends on the caller.
// Writing the two queries out in full moves that from a rule to a fact.
var facetQueries = map[string]string{
	"action": `SELECT DISTINCT action FROM audit_logs
		WHERE action != '' ORDER BY action`,
	"resource_type": `SELECT DISTINCT resource_type FROM audit_logs
		WHERE resource_type != '' ORDER BY resource_type`,
}

// distinct lists the non-empty values of one facetable column.
func (r *AuditRepo) distinct(ctx context.Context, column string) ([]string, error) {
	query, ok := facetQueries[column]
	if !ok {
		return nil, fmt.Errorf("audit: %q is not a facetable column", column)
	}

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list distinct %s: %w", column, err)
	}
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan distinct %s: %w", column, err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate distinct %s: %w", column, err)
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
