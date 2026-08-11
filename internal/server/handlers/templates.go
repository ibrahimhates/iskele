package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/templates"
)

// Templates serves /api/v1/templates: the app catalog.
type Templates struct {
	svc *service.Catalog
}

// NewTemplates builds the catalog handler set.
func NewTemplates(svc *service.Catalog) *Templates {
	return &Templates{svc: svc}
}

// List handles GET /templates.
func (h *Templates) List(w http.ResponseWriter, r *http.Request) error {
	entries, problems := h.svc.List(r.Context())

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items": entries,
		"total": len(entries),
		// Categories come with the listing so a client can build the filter
		// without deriving it and disagreeing about the order.
		"categories": h.svc.Categories(),
		// A custom template that would not load is reported rather than
		// swallowed: the operator who wrote it needs to know why it is missing.
		"problems": problems,
	})
	return nil
}

// Get handles GET /templates/{id}.
func (h *Templates) Get(w http.ResponseWriter, r *http.Request) error {
	template, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return templateError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, template)
	return nil
}

// Deploy handles POST /templates/{id}/deploy.
func (h *Templates) Deploy(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.DeployRequest](r)
	if err != nil {
		return err
	}

	identity := middleware.IdentityFrom(r.Context())
	result, err := h.svc.Deploy(r.Context(), chi.URLParam(r, "id"), req, service.CreateOptions{
		Privileged: middleware.RoleHas(identity.Role, middleware.PermPrivileged),
	}, actorOf(r), metaOf(r))
	if err != nil {
		return templateError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, result)
	return nil
}

// GenerateSecret handles POST /templates/secret.
//
// The value is made here rather than in the browser: the browser is where an
// extension, or a machine with a poor entropy source, would quietly produce
// something guessable.
func (h *Templates) GenerateSecret(w http.ResponseWriter, r *http.Request) error {
	length, err := intParam(r, "length")
	if err != nil {
		return err
	}

	size := 0
	if length != nil {
		size = *length
	}

	secret, err := service.GenerateSecret(size)
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusCreated, map[string]string{"secret": secret})
	return nil
}

// templateError maps the catalog's failures onto HTTP.
func templateError(err error) error {
	if errors.Is(err, templates.ErrNotFound) {
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"no such template").WithCause(err)
	}

	// A rejected answer is the operator's to fix, and every problem is
	// reported at once so a nine-field form is not submitted nine times.
	if values, ok := service.IsTemplateValueError(err); ok {
		fields := make([]map[string]string, 0, len(values.Errors))
		for _, item := range values.Errors {
			fields = append(fields, map[string]string{
				"field": item.Field, "message": item.Message,
			})
		}
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", err.Error()).WithDetails(map[string]any{"fields": fields}).WithCause(err)
	}

	var schema *templates.SchemaError
	if errors.As(err, &schema) {
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", err.Error()).WithCause(err)
	}

	return engineError(err)
}
