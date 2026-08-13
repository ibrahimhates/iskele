package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/compose"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Stacks serves /api/v1/stacks.
type Stacks struct {
	svc     *service.StackService
	tickets *auth.TicketStore
}

// NewStacks builds the stack handler set.
func NewStacks(svc *service.StackService, tickets *auth.TicketStore) *Stacks {
	return &Stacks{svc: svc, tickets: tickets}
}

// List handles GET /stacks.
func (h *Stacks) List(w http.ResponseWriter, r *http.Request) error {
	stacks, err := h.svc.List(r.Context())
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(stacks))
	return nil
}

// Get handles GET /stacks/{id}.
func (h *Stacks) Get(w http.ResponseWriter, r *http.Request) error {
	detail, err := h.svc.Detail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, detail)
	return nil
}

// Create handles POST /stacks.
func (h *Stacks) Create(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeJSON[service.StackInput](r)
	if err != nil {
		return err
	}

	stack, err := h.svc.Create(r.Context(), in, actorOf(r), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, stack)
	return nil
}

// Update handles PUT /stacks/{id}.
func (h *Stacks) Update(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeJSON[service.StackInput](r)
	if err != nil {
		return err
	}

	stack, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), in, actorOf(r), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, stack)
	return nil
}

// Delete handles DELETE /stacks/{id}.
//
// It removes the record, not the containers: an operator who already stopped
// them by hand should not have this bring anything back, and `down` is how a
// stack is taken away.
func (h *Stacks) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r)); err != nil {
		return stackError(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Validate handles POST /stacks/validate.
//
// It answers 200 with a report rather than 422 with an error: an invalid file
// is the normal state of one being edited, and the editor needs the whole list
// of problems, not the first.
func (h *Stacks) Validate(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeJSON[service.StackInput](r)
	if err != nil {
		return err
	}

	report, err := h.svc.Validate(r.Context(), in, middleware.RoleHas(middleware.IdentityFrom(r.Context()).Role, middleware.PermPrivileged))
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, report)
	return nil
}

// Diff handles POST /stacks/{id}/diff.
//
// It reports what saving and deploying the submitted content would do to the
// stack as it stands, so "this restarts your database" is knowable before the
// edit is saved rather than after.
func (h *Stacks) Diff(w http.ResponseWriter, r *http.Request) error {
	in, err := decodeJSON[service.StackInput](r)
	if err != nil {
		return err
	}

	diff, err := h.svc.Diff(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, diff)
	return nil
}

// Down handles POST /stacks/{id}/down.
func (h *Stacks) Down(w http.ResponseWriter, r *http.Request) error {
	stack, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return stackError(err)
	}

	// Named volumes are the one part of a stack that cannot be recreated, so
	// removing them is opt-in. Networks are the opposite: one with nothing
	// attached is only clutter, so they go unless the caller says otherwise.
	volumes, err := boolParam(r, "volumes")
	if err != nil {
		return err
	}
	networks := true
	if r.URL.Query().Has("networks") {
		networks, err = boolParam(r, "networks")
		if err != nil {
			return err
		}
	}

	result, err := h.svc.Down(r.Context(), stack,
		service.DownOptions{Volumes: volumes, Networks: networks}, actorOf(r), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, result)
	return nil
}

// Act handles POST /stacks/{id}/{stop,start,restart}.
func (h *Stacks) Act(action service.StackAction) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		stack, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			return stackError(err)
		}

		result, err := h.svc.Act(r.Context(), stack, action, actorOf(r), metaOf(r))
		if err != nil {
			return stackError(err)
		}

		httpx.WriteJSON(w, r, http.StatusOK, result)
		return nil
	}
}

// Discovered handles GET /stacks/discovered.
func (h *Stacks) Discovered(w http.ResponseWriter, r *http.Request) error {
	found, err := h.svc.Discover(r.Context())
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(found))
	return nil
}

// Import handles POST /stacks/import.
func (h *Stacks) Import(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeJSON[struct {
		Name string `json:"name"`
	}](r)
	if err != nil {
		return err
	}
	if body.Name == "" {
		return httpx.ErrBadRequest("a stack name is required")
	}

	stack, err := h.svc.Import(r.Context(), body.Name, actorOf(r), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, stack)
	return nil
}

// Up handles SSE GET /stacks/{id}/up.
//
// A stream rather than a request/response because deploying a stack pulls
// images and starts containers one after another, and an operator watching a
// blank page for two minutes cannot tell it from a hang. The deploy outlives
// this request: closing the tab stops the frames, not the work.
func (h *Stacks) Up(w http.ResponseWriter, r *http.Request) error {
	ticket, stack, err := h.streamTarget(r, middleware.PermOperate)
	if err != nil {
		return err
	}

	pull, err := boolParam(r, "pull")
	if err != nil {
		return err
	}
	recreate, err := boolParam(r, "recreate")
	if err != nil {
		return err
	}

	opts := service.UpOptions{
		Privileged: middleware.RoleHas(ticket.Role, middleware.PermPrivileged),
		Services:   listParam(r, "service"),
		Pull:       pull,
		Recreate:   recreate,
	}

	events, errs, err := h.svc.Up(context.WithoutCancel(r.Context()), stack, opts,
		ticketActor(ticket), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	return streamStackEvents(w, r, events, errs)
}

// PullImages handles SSE GET /stacks/{id}/pull.
func (h *Stacks) PullImages(w http.ResponseWriter, r *http.Request) error {
	ticket, stack, err := h.streamTarget(r, middleware.PermOperate)
	if err != nil {
		return err
	}

	events, errs, err := h.svc.Pull(context.WithoutCancel(r.Context()), stack,
		ticketActor(ticket), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	return streamStackEvents(w, r, events, errs)
}

// Scale handles SSE GET /stacks/{id}/scale.
func (h *Stacks) Scale(w http.ResponseWriter, r *http.Request) error {
	ticket, stack, err := h.streamTarget(r, middleware.PermOperate)
	if err != nil {
		return err
	}

	name := r.URL.Query().Get("service")
	if name == "" {
		return httpx.ErrBadRequest("a ?service name is required")
	}
	replicas, err := strconv.Atoi(r.URL.Query().Get("replicas"))
	if err != nil {
		return httpx.ErrBadRequest("?replicas must be a whole number")
	}

	events, errs, err := h.svc.Scale(context.WithoutCancel(r.Context()), stack, name, replicas,
		service.UpOptions{Privileged: middleware.RoleHas(ticket.Role, middleware.PermPrivileged)},
		ticketActor(ticket), metaOf(r))
	if err != nil {
		return stackError(err)
	}

	return streamStackEvents(w, r, events, errs)
}

// streamTarget authenticates a streaming stack request and loads its stack.
func (h *Stacks) streamTarget(r *http.Request, perm middleware.Permission) (auth.Ticket, store.Stack, error) {
	ticket, err := redeemStreamTicket(h.tickets, r, perm)
	if err != nil {
		return auth.Ticket{}, store.Stack{}, err
	}

	stack, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return auth.Ticket{}, store.Stack{}, stackError(err)
	}
	return ticket, stack, nil
}

// streamStackEvents writes a stack operation's progress as SSE.
func streamStackEvents(w http.ResponseWriter, r *http.Request,
	events <-chan service.StackEvent, errs <-chan error,
) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpx.ErrInternal("this connection does not support streaming")
	}

	writeSSEHeaders(w)
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	// The client going away stops the frames, not the work: the deploy runs on
	// its own context and the stack's status is how it is picked up again.
	gone := r.Context().Done()

	for events != nil || errs != nil {
		select {
		case <-gone:
			return nil

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			flusher.Flush()

		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if !writeSSEEvent(w, string(event.Kind), mustJSON(event)) {
				return nil
			}
			flusher.Flush()

		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err == nil {
				continue
			}
			writeSSEEvent(w, "error", mustJSON(stackErrorPayload(err)))
			flusher.Flush()
			return nil
		}
	}

	return nil
}

// stackErrorPayload renders a failed stack operation for the stream.
//
// A refusal carries its per-service problems: "this stack cannot be deployed"
// on its own tells an operator nothing about which service to fix.
func stackErrorPayload(err error) any {
	payload := map[string]any{
		"code":    kindCodeOf(err),
		"message": err.Error(),
	}

	var refused *service.StackRefused
	if errors.As(err, &refused) {
		payload["code"] = string(httpx.CodeValidationFailed)
		payload["problems"] = refused.Problems
	}
	return payload
}

// Logs handles WS /stacks/{id}/logs.
//
// One socket for the whole stack: reading a deploy means reading what every
// service said, interleaved, and opening one connection per service would make
// the interleaving the browser's problem.
func (h *Stacks) Logs(w http.ResponseWriter, r *http.Request) error {
	_, stack, err := h.streamTarget(r, middleware.PermRead)
	if err != nil {
		return err
	}

	opts, err := logOptions(r)
	if err != nil {
		return err
	}

	services := listParam(r, "service")
	frames, streamErrs, err := h.svc.Logs(context.WithoutCancel(r.Context()), stack, services, opts)
	if err != nil {
		return stackError(err)
	}

	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	go func() {
		for {
			if _, _, readErr := conn.Read(ctx); readErr != nil {
				stop()
				return
			}
		}
	}()

	for frames != nil || streamErrs != nil {
		select {
		case <-ctx.Done():
			return nil

		case frame, ok := <-frames:
			if !ok {
				frames = nil
				continue
			}
			if !sendWSJSON(ctx, conn, frame) {
				return nil
			}

		case streamErr, ok := <-streamErrs:
			if !ok {
				streamErrs = nil
				continue
			}
			if streamErr == nil {
				continue
			}
			sendWSJSON(ctx, conn, map[string]string{
				"t": "err", "code": kindCodeOf(streamErr), "m": streamErr.Error(),
			})
			return nil
		}
	}

	sendWSJSON(ctx, conn, map[string]string{"t": "eof"})
	return nil
}

// stackError maps the stack service's sentinels onto HTTP.
func stackError(err error) error {
	var refused *service.StackRefused
	switch {
	case errors.Is(err, service.ErrStackNotFound):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"no such stack").WithCause(err)
	case errors.Is(err, service.ErrNoSuchService):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"%s", err.Error()).WithCause(err)
	case errors.Is(err, service.ErrStackBusy):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"%s", err.Error()).WithCause(err)
	case errors.Is(err, store.ErrConflict):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"%s", err.Error()).WithCause(err)
	case errors.As(err, &refused):
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", err.Error()).WithDetails(map[string]any{"problems": refused.Problems}).WithCause(err)
	case errors.Is(err, service.ErrComposeSource):
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", err.Error()).WithCause(err)
	case errors.Is(err, compose.ErrGitMissing), errors.Is(err, compose.ErrGitURL):
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", err.Error()).WithCause(err)
	}

	var composeErr *compose.Error
	if errors.As(err, &composeErr) {
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", composeErr.Message).WithCause(err)
	}

	return engineError(err)
}
