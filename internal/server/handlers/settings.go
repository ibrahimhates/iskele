package handlers

import (
	"errors"
	"net/http"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
)

// Settings serves /api/v1/settings.
type Settings struct {
	svc *service.Settings
}

// NewSettings builds the settings handler set.
func NewSettings(svc *service.Settings) *Settings { return &Settings{svc: svc} }

// Get handles GET /settings.
func (h *Settings) Get(w http.ResponseWriter, r *http.Request) error {
	view, err := h.svc.Get(r.Context())
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, view)
	return nil
}

// Update handles PUT /settings.
func (h *Settings) Update(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.Update](r)
	if err != nil {
		return err
	}

	view, err := h.svc.Set(r.Context(), req, identityOf(r), metaOf(r))
	if err != nil {
		if errors.Is(err, service.ErrRetentionRange) {
			return httpx.ErrValidation("%s", err.Error())
		}
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, view)
	return nil
}
