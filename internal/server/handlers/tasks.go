package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
)

// Tasks serves /api/v1/tasks: the long-running operations the UI's drawer
// shows, and the only way to stop one from outside the tab that started it.
type Tasks struct {
	registry *service.TaskRegistry
}

// NewTasks builds the task handler set.
func NewTasks(registry *service.TaskRegistry) *Tasks { return &Tasks{registry: registry} }

// List handles GET /tasks.
func (h *Tasks) List(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, r, http.StatusOK, newList(h.registry.List()))
	return nil
}

// Get handles GET /tasks/{id}.
func (h *Tasks) Get(w http.ResponseWriter, r *http.Request) error {
	task, err := h.registry.Get(chi.URLParam(r, "id"))
	if err != nil {
		return taskError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, task)
	return nil
}

// Cancel handles POST /tasks/{id}/cancel.
func (h *Tasks) Cancel(w http.ResponseWriter, r *http.Request) error {
	if err := h.registry.Cancel(chi.URLParam(r, "id")); err != nil {
		return taskError(err)
	}

	task, err := h.registry.Get(chi.URLParam(r, "id"))
	if err != nil {
		return taskError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, task)
	return nil
}

// taskError maps the registry's sentinels onto HTTP.
func taskError(err error) error {
	switch {
	case errors.Is(err, service.ErrTaskNotFound):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"no such task; finished tasks are kept for ten minutes").WithCause(err)
	case errors.Is(err, service.ErrTaskFinished):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"%s", err.Error()).WithCause(err)
	default:
		return err
	}
}
