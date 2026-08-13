package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/store"
)

// Audit reads the trail the rest of the daemon writes.
//
// It is read-only by design: nothing in the API edits or deletes a record,
// because an audit log an admin can rewrite is not an audit log. Aging
// records out is the store's business (AuditRepo.DeleteBefore), driven by a
// retention setting that does not exist yet — until it does, the trail only
// grows.
type Audit struct {
	entries *store.AuditRepo
}

// NewAudit builds the audit query service.
func NewAudit(entries *store.AuditRepo) *Audit { return &Audit{entries: entries} }

// ErrUnknownExportFormat is returned for a format the exporter cannot write.
var ErrUnknownExportFormat = errors.New("export format must be csv or json")

// exportBatch is how many rows the exporter pulls at a time.
//
// An export is deliberately not one big query: a year of a busy host is more
// rows than the daemon should hold in memory at once, and the operator asking
// for it is on a small machine by assumption.
const exportBatch = 500

// Page is one screen of the audit trail, with the size of the whole result so
// a client can page through it.
type Page struct {
	Items  []store.AuditEntry `json:"items"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

// List returns one page of matching entries, newest first.
func (s *Audit) List(ctx context.Context, filter store.AuditFilter) (Page, error) {
	items, err := s.entries.List(ctx, filter)
	if err != nil {
		return Page{}, err
	}
	total, err := s.entries.Count(ctx, filter)
	if err != nil {
		return Page{}, err
	}

	if items == nil {
		items = []store.AuditEntry{}
	}
	return Page{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

// Facets lists the distinct values on record, for building the filter
// controls. They come from the trail rather than from a hard-coded list so a
// deleted account and a retired action still appear as long as their records
// do.
func (s *Audit) Facets(ctx context.Context) (store.Facets, error) {
	return s.entries.Facets(ctx)
}

// Export writes every matching entry to w in the requested format.
//
// Limit and Offset on the filter are ignored: an export is the whole result,
// and a client that wanted a page would have called List. Rows are fetched in
// batches and written as they arrive, so memory does not grow with the size of
// the trail.
func (s *Audit) Export(ctx context.Context, filter store.AuditFilter, format string, w io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return s.exportCSV(ctx, filter, w)
	case "json":
		return s.exportJSON(ctx, filter, w)
	default:
		return ErrUnknownExportFormat
	}
}

// exportColumns is the CSV header, and the order every row follows.
var exportColumns = []string{
	"id", "created_at", "username", "user_id", "action",
	"resource_type", "resource_id", "result", "detail", "ip", "user_agent",
}

func (s *Audit) exportCSV(ctx context.Context, filter store.AuditFilter, w io.Writer) error {
	out := csv.NewWriter(w)
	if err := out.Write(exportColumns); err != nil {
		return fmt.Errorf("write audit header: %w", err)
	}

	err := s.each(ctx, filter, func(e store.AuditEntry) error {
		return out.Write([]string{
			fmt.Sprint(e.ID),
			e.CreatedAt.UTC().Format(time.RFC3339),
			e.Username,
			e.UserID,
			e.Action,
			e.ResourceType,
			e.ResourceID,
			e.Result,
			e.Detail,
			e.IP,
			e.UserAgent,
		})
	})
	if err != nil {
		return err
	}

	out.Flush()
	return out.Error()
}

// exportJSON writes a JSON array, one entry per element.
//
// The array is written incrementally rather than marshaled whole, for the
// same reason the rows are fetched in batches.
func (s *Audit) exportJSON(ctx context.Context, filter store.AuditFilter, w io.Writer) error {
	if _, err := io.WriteString(w, "[\n"); err != nil {
		return fmt.Errorf("write audit export: %w", err)
	}

	encoder := json.NewEncoder(w)
	first := true
	err := s.each(ctx, filter, func(e store.AuditEntry) error {
		if !first {
			if _, err := io.WriteString(w, ","); err != nil {
				return fmt.Errorf("write audit export: %w", err)
			}
		}
		first = false
		return encoder.Encode(e)
	})
	if err != nil {
		return err
	}

	if _, err := io.WriteString(w, "]\n"); err != nil {
		return fmt.Errorf("write audit export: %w", err)
	}
	return nil
}

// each walks every matching entry, oldest page first, calling fn per row.
func (s *Audit) each(ctx context.Context, filter store.AuditFilter, fn func(store.AuditEntry) error) error {
	page := filter
	page.Limit = exportBatch
	page.Offset = 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		batch, err := s.entries.List(ctx, page)
		if err != nil {
			return err
		}
		for _, entry := range batch {
			if err := fn(entry); err != nil {
				return err
			}
		}
		// A short batch is the last one: the store caps a page at its own
		// maximum, so asking for exportBatch and getting fewer means the end.
		if len(batch) < exportBatch {
			return nil
		}
		page.Offset += len(batch)
	}
}
