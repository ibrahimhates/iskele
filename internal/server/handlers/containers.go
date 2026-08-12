package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
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
	if err := h.svc.Start(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r)); err != nil {
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
	if err := h.svc.Stop(r.Context(), chi.URLParam(r, "id"), timeout, actorOf(r), metaOf(r)); err != nil {
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
	if err := h.svc.Restart(r.Context(), chi.URLParam(r, "id"), timeout, actorOf(r), metaOf(r)); err != nil {
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
	}, actorOf(r), metaOf(r))
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

// Pause handles POST /containers/{id}/pause.
func (h *Containers) Pause(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Pause(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "pause")
}

// Unpause handles POST /containers/{id}/unpause.
func (h *Containers) Unpause(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Unpause(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "unpause")
}

// Kill handles POST /containers/{id}/kill. Optional query parameter: signal.
func (h *Containers) Kill(w http.ResponseWriter, r *http.Request) error {
	signal := strings.TrimSpace(r.URL.Query().Get("signal"))
	if err := h.svc.Kill(r.Context(), chi.URLParam(r, "id"), signal, actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "kill")
}

// renameRequest is the body of POST /containers/{id}/rename.
type renameRequest struct {
	Name string `json:"name"`
}

// Rename handles POST /containers/{id}/rename.
func (h *Containers) Rename(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[renameRequest](r)
	if err != nil {
		return err
	}
	if err := h.svc.Rename(r.Context(), chi.URLParam(r, "id"), req.Name, actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}
	return h.accepted(w, r, "rename")
}

// Redeploy handles POST /containers/{id}/redeploy.
func (h *Containers) Redeploy(w http.ResponseWriter, r *http.Request) error {
	result, err := h.svc.Redeploy(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r))
	if err != nil {
		// A rollback means the operator still has a working container, which
		// is worth saying alongside the failure.
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, result)
	return nil
}

// batchRequest is the body of POST /containers/batch.
type batchRequest struct {
	IDs    []string `json:"ids"`
	Action string   `json:"action"`
}

// Prune handles POST /containers/prune: removing every stopped container.
func (h *Containers) Prune(w http.ResponseWriter, r *http.Request) error {
	report, err := h.svc.Prune(r.Context(), actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, report)
	return nil
}

// Batch handles POST /containers/batch.
//
// It answers 207 when some containers failed, so a client can tell "all done"
// from "mostly done" without inspecting every result.
func (h *Containers) Batch(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[batchRequest](r)
	if err != nil {
		return err
	}

	results, err := h.svc.Batch(r.Context(), req.IDs, req.Action, actorOf(r), metaOf(r))
	if err != nil {
		if errors.Is(err, service.ErrEmptyID) {
			return httpx.ErrBadRequest("%s", err.Error())
		}
		return httpx.ErrBadRequest("%s", err.Error())
	}

	failed := 0
	for _, res := range results {
		if !res.OK {
			failed++
		}
	}

	status := http.StatusOK
	if failed > 0 {
		status = http.StatusMultiStatus
	}

	httpx.WriteJSON(w, r, status, map[string]any{
		"action":    req.Action,
		"total":     len(results),
		"succeeded": len(results) - failed,
		"failed":    failed,
		"results":   results,
	})
	return nil
}

// actorOf builds the audit actor for the authenticated caller.
func actorOf(r *http.Request) audit.Actor {
	id := middleware.IdentityFrom(r.Context())
	return audit.Actor{UserID: id.UserID, Username: id.Username, Role: id.Role, TokenID: id.TokenID}
}
