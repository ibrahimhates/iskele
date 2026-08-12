package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Audit serves /api/v1/audit: reading the trail, and exporting it.
type Audit struct {
	svc *service.Audit
}

// NewAudit builds the audit handler set.
func NewAudit(svc *service.Audit) *Audit { return &Audit{svc: svc} }

// List handles GET /audit.
func (h *Audit) List(w http.ResponseWriter, r *http.Request) error {
	filter, err := auditFilterOf(r)
	if err != nil {
		return err
	}

	page, err := h.svc.List(r.Context(), filter)
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, page)
	return nil
}

// Facets handles GET /audit/facets.
func (h *Audit) Facets(w http.ResponseWriter, r *http.Request) error {
	facets, err := h.svc.Facets(r.Context())
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"actions":        facets.Actions,
		"resource_types": facets.ResourceTypes,
		"actors":         facets.Actors,
	})
	return nil
}

// Export handles GET /audit/export.
//
// The response is a file download rather than an API payload: the operator
// asking for it is taking the trail somewhere else — a ticket, a spreadsheet,
// another system's ingest.
func (h *Audit) Export(w http.ResponseWriter, r *http.Request) error {
	filter, err := auditFilterOf(r)
	if err != nil {
		return err
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	contentType, ok := exportContentTypes[format]
	if !ok {
		return httpx.ErrValidation("%s", service.ErrUnknownExportFormat.Error())
	}

	filename := fmt.Sprintf("iskele-audit-%s.%s", time.Now().UTC().Format("20060102-150405"), format)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	// The body is written as the rows arrive, so its length is not known in
	// advance and the response must not be buffered to find out.
	w.WriteHeader(http.StatusOK)

	if err := h.svc.Export(r.Context(), filter, format, w); err != nil {
		if errors.Is(err, service.ErrUnknownExportFormat) {
			return httpx.ErrValidation("%s", err.Error())
		}
		// The status line is already sent, so there is no way left to report
		// this to the client as an error. Returning it puts it in the log,
		// where the operator can find out why their file is truncated.
		return err
	}
	return nil
}

// exportContentTypes are the formats Export can write.
var exportContentTypes = map[string]string{
	"csv":  "text/csv; charset=utf-8",
	"json": "application/json; charset=utf-8",
}

// auditFilterOf reads the filter every audit endpoint accepts.
func auditFilterOf(r *http.Request) (store.AuditFilter, error) {
	q := r.URL.Query()

	filter := store.AuditFilter{
		UserID:       strings.TrimSpace(q.Get("user_id")),
		Action:       strings.TrimSpace(q.Get("action")),
		ResourceType: strings.TrimSpace(q.Get("resource_type")),
		ResourceID:   strings.TrimSpace(q.Get("resource_id")),
		Result:       strings.TrimSpace(q.Get("result")),
	}

	if filter.Result != "" && filter.Result != store.ResultOK && filter.Result != store.ResultError {
		return store.AuditFilter{}, httpx.ErrValidation(
			"result must be %q or %q, got %q", store.ResultOK, store.ResultError, filter.Result)
	}

	from, err := timeParam(r, "from")
	if err != nil {
		return store.AuditFilter{}, err
	}
	to, err := timeParam(r, "to")
	if err != nil {
		return store.AuditFilter{}, err
	}
	if from != nil && to != nil && to.Before(*from) {
		return store.AuditFilter{}, httpx.ErrValidation("'to' is before 'from'")
	}
	if from != nil {
		filter.From = *from
	}
	if to != nil {
		filter.To = *to
	}

	if limit, limitErr := intParam(r, "limit"); limitErr != nil {
		return store.AuditFilter{}, limitErr
	} else if limit != nil {
		if *limit < 1 {
			return store.AuditFilter{}, httpx.ErrValidation("limit must be at least 1")
		}
		filter.Limit = *limit
	}

	if offset, offsetErr := intParam(r, "offset"); offsetErr != nil {
		return store.AuditFilter{}, offsetErr
	} else if offset != nil {
		if *offset < 0 {
			return store.AuditFilter{}, httpx.ErrValidation("offset cannot be negative")
		}
		filter.Offset = *offset
	}

	return filter, nil
}

// timeParam reads an RFC 3339 timestamp, or a bare date.
//
// A date is accepted because that is what a date picker sends, and reading
// "2026-08-11" as midnight UTC is the only interpretation that does not
// silently depend on the server's timezone.
func timeParam(r *http.Request, name string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}

	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return &utc, nil
		}
	}
	return nil, httpx.ErrValidation(
		"query parameter %s must be an RFC 3339 timestamp or a YYYY-MM-DD date, got %q", name, raw)
}
