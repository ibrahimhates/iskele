package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
)

// Containers serves /api/v1/containers.
type Containers struct {
	svc *service.Container
}

// NewContainers builds the container handler set.
func NewContainers(svc *service.Container) *Containers {
	return &Containers{svc: svc}
}

// List handles GET /containers.
//
// Query parameters: all, size, label (repeatable), status (repeatable), name.
func (h *Containers) List(w http.ResponseWriter, r *http.Request) error {
	all, err := boolParam(r, "all")
	if err != nil {
		return err
	}
	size, err := boolParam(r, "size")
	if err != nil {
		return err
	}

	containers, err := h.svc.List(r.Context(), service.ListOptions{
		All:    all,
		Size:   size,
		Label:  listParam(r, "label"),
		Status: listParam(r, "status"),
		Name:   r.URL.Query().Get("name"),
	})
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(containers))
	return nil
}

// Get handles GET /containers/{id}.
func (h *Containers) Get(w http.ResponseWriter, r *http.Request) error {
	detail, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, detail)
	return nil
}

// Inspect handles GET /containers/{id}/inspect and returns the engine's own
// payload untouched, which is what the UI's Inspect tab shows.
func (h *Containers) Inspect(w http.ResponseWriter, r *http.Request) error {
	raw, err := h.svc.Inspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return engineError(err)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(raw); err != nil {
		return err
	}
	return nil
}

// Start handles POST /containers/{id}/start.
func (h *Containers) Start(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Start(r.Context(), chi.URLParam(r, "id")); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "start")
}

// Stop handles POST /containers/{id}/stop. Optional query parameter: timeout.
func (h *Containers) Stop(w http.ResponseWriter, r *http.Request) error {
	timeout, err := intParam(r, "timeout")
	if err != nil {
		return err
	}
	if err := h.svc.Stop(r.Context(), chi.URLParam(r, "id"), timeout); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "stop")
}

// Restart handles POST /containers/{id}/restart. Optional: timeout.
func (h *Containers) Restart(w http.ResponseWriter, r *http.Request) error {
	timeout, err := intParam(r, "timeout")
	if err != nil {
		return err
	}
	if err := h.svc.Restart(r.Context(), chi.URLParam(r, "id"), timeout); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "restart")
}

// Remove handles DELETE /containers/{id}. Query parameters: force, volumes.
func (h *Containers) Remove(w http.ResponseWriter, r *http.Request) error {
	force, err := boolParam(r, "force")
	if err != nil {
		return err
	}
	volumes, err := boolParam(r, "volumes")
	if err != nil {
		return err
	}

	err = h.svc.Remove(r.Context(), chi.URLParam(r, "id"), service.RemoveOptions{
		Force:         force,
		RemoveVolumes: volumes,
	})
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusNoContent, nil)
	return nil
}

// actionResult is the body of a successful lifecycle action.
type actionResult struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Status string `json:"status"`
}

// accepted writes the uniform response every lifecycle action returns.
func (h *Containers) accepted(w http.ResponseWriter, r *http.Request, action string) error {
	httpx.WriteJSON(w, r, http.StatusOK, actionResult{
		ID:     chi.URLParam(r, "id"),
		Action: action,
		Status: "ok",
	})
	return nil
}
