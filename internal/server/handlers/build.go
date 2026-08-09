package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
)

// maxLogDownloadBytes bounds a log replay. A runaway build can produce
// hundreds of megabytes, and handing all of it to a browser helps nobody.
const maxLogDownloadBytes = 16 << 20

// Builds serves /api/v1/builds and the filesystem browser the build form needs.
type Builds struct {
	svc     *service.Builder
	browser *service.Browser
	tasks   *service.TaskRegistry
	tickets *auth.TicketStore
}

// NewBuilds builds the build handler set.
func NewBuilds(svc *service.Builder, browser *service.Browser,
	tasks *service.TaskRegistry, tickets *auth.TicketStore,
) *Builds {
	return &Builds{svc: svc, browser: browser, tasks: tasks, tickets: tickets}
}

// Browse handles GET /fs/browse?path=.
//
// It is the same trust boundary as a bind mount, seen from the other side: a
// path outside allowed_paths does not open, and a symlink pointing out of one
// is not followed.
func (h *Builds) Browse(w http.ResponseWriter, r *http.Request) error {
	listing, err := h.browser.Browse(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		return browseError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, listing)
	return nil
}

// List handles GET /builds. Query parameters: status, limit.
func (h *Builds) List(w http.ResponseWriter, r *http.Request) error {
	limit, err := intParam(r, "limit")
	if err != nil {
		return err
	}

	filter := store.BuildFilter{Status: store.BuildStatus(r.URL.Query().Get("status"))}
	if limit != nil {
		filter.Limit = *limit
	}

	builds, err := h.svc.List(r.Context(), filter)
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(builds))
	return nil
}

// Get handles GET /builds/{id}.
func (h *Builds) Get(w http.ResponseWriter, r *http.Request) error {
	record, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return buildError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, record)
	return nil
}

// Log handles GET /builds/{id}/log.
//
// Plain text rather than JSON: this is the build's output verbatim, and a
// client that wants to save it should get the same bytes an operator would
// have seen on a terminal.
func (h *Builds) Log(w http.ResponseWriter, r *http.Request) error {
	reader, err := h.svc.OpenLog(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return buildError(err)
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, io.LimitReader(reader, maxLogDownloadBytes))
	return copyErr
}

// Cancel handles POST /builds/{id}/cancel.
func (h *Builds) Cancel(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")

	if err := h.svc.Cancel(r.Context(), id, actorOf(r), metaOf(r)); err != nil {
		return buildError(err)
	}

	record, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return buildError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, record)
	return nil
}

// buildMessage is one frame on the build WebSocket.
type buildMessage struct {
	// Type is "build" (the record), "log", "status", "err" or "done".
	Type string `json:"t"`
	// ID is the build's, sent first so the client can cancel it.
	ID string `json:"id,omitempty"`
	// Line is a chunk of build output.
	Line string `json:"line,omitempty"`
	// Step and TotalSteps track progress through the Dockerfile.
	Step       int `json:"step,omitempty"`
	TotalSteps int `json:"total_steps,omitempty"`
	// Status, LayerID, Current and Total carry the base-image pull.
	Status  string `json:"status,omitempty"`
	LayerID string `json:"layer_id,omitempty"`
	Current int64  `json:"current,omitempty"`
	Total   int64  `json:"total,omitempty"`
	// ImageID is set on the final frame of a successful build.
	ImageID string `json:"image_id,omitempty"`
	Message string `json:"m,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Build handles WS /build.
//
// The request is validated and recorded before the socket is accepted, so a
// build that can never run — a directory outside the whitelist, a missing
// Dockerfile — is refused with an ordinary HTTP error rather than as the first
// frame of a stream the client then has to interpret.
func (h *Builds) Build(w http.ResponseWriter, r *http.Request) error {
	ticket, err := h.redeemBuildTicket(r)
	if err != nil {
		return err
	}

	req, err := buildRequestFromQuery(r)
	if err != nil {
		return err
	}

	record, err := h.svc.Start(r.Context(), req, ticketActor(ticket), metaOf(r))
	if err != nil {
		return buildError(err)
	}

	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	// The build outlives the socket: an operator who closes the tab has not
	// asked to stop the build, so the work is rooted in a context the request's
	// cancellation cannot reach. The timeout bounds a build that is stuck
	// rather than slow.
	rooted, stopRoot := context.WithTimeout(context.WithoutCancel(r.Context()), buildTimeout)
	defer stopRoot()
	buildCtx := h.tasks.StartWithID(rooted, record.ID, "image.build",
		buildTarget(record), ticket.Username)

	// Sending is watched separately from building, and deliberately not derived
	// from the task's context: canceling a task ends the build, and the client
	// still has to be told why.
	sendCtx, stopSending := context.WithCancel(rooted)
	defer stopSending()
	go func() {
		for {
			if _, _, readErr := conn.Read(sendCtx); readErr != nil {
				// The client is gone. Only the sending stops; the build runs on
				// to its end, and the history and archived log are how it is
				// picked up again.
				stopSending()
				return
			}
		}
	}()

	sendWSJSON(sendCtx, conn, buildMessage{Type: "build", ID: record.ID})

	events, errs := h.svc.Run(buildCtx, record, req)

	var failure error
	for events != nil || errs != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			sendWSJSON(sendCtx, conn, buildFrame(event))

		case buildErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if buildErr != nil && failure == nil {
				failure = buildErr
			}
		}
	}

	h.tasks.Finish(record.ID, failure)

	if failure != nil {
		sendWSJSON(sendCtx, conn, buildMessage{
			Type:    "err",
			Code:    kindCodeOf(failure),
			Message: docker.Message(failure),
		})
		_ = conn.Close(websocket.StatusInternalError, "build failed")
		return nil
	}

	done := buildMessage{Type: "done", ID: record.ID}
	if final, getErr := h.svc.Get(rooted, record.ID); getErr == nil {
		done.ImageID = final.ImageID
		done.Status = string(final.Status)
	}
	sendWSJSON(sendCtx, conn, done)
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return nil
}

// buildFrame renders one engine event for the socket.
func buildFrame(event docker.BuildEvent) buildMessage {
	switch {
	case event.Stream != "":
		return buildMessage{
			Type:       "log",
			Line:       event.Stream,
			Step:       event.Step,
			TotalSteps: event.TotalSteps,
		}
	case event.ImageID != "":
		return buildMessage{Type: "status", ImageID: event.ImageID}
	default:
		return buildMessage{
			Type:    "status",
			Status:  event.Status,
			LayerID: event.ID,
			Current: event.Current,
			Total:   event.Total,
		}
	}
}

// buildTarget names a build in the task drawer.
func buildTarget(record store.Build) string {
	if len(record.Tags) > 0 {
		return record.Tags[0]
	}
	return record.ContextDir
}

// redeemBuildTicket authenticates the build socket.
//
// Building runs arbitrary commands from a Dockerfile as root inside the
// daemon, which is why it takes the build permission rather than operate.
func (h *Builds) redeemBuildTicket(r *http.Request) (auth.Ticket, error) {
	return redeemStreamTicket(h.tickets, r, middleware.PermBuild)
}

// buildRequestFromQuery reads the build parameters.
//
// They travel in the query string because a WebSocket handshake is a GET: the
// browser cannot send a body with it.
func buildRequestFromQuery(r *http.Request) (service.BuildRequest, error) {
	q := r.URL.Query()

	req := service.BuildRequest{
		ContextDir: q.Get("context"),
		Dockerfile: q.Get("dockerfile"),
		Tags:       listParam(r, "tag"),
		Target:     q.Get("target"),
		Platform:   q.Get("platform"),
	}

	noCache, err := boolParam(r, "nocache")
	if err != nil {
		return req, err
	}
	req.NoCache = noCache

	pull, err := boolParam(r, "pull")
	if err != nil {
		return req, err
	}
	req.Pull = pull

	if raw := q.Get("buildargs"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &req.BuildArgs); err != nil {
			return req, httpx.ErrBadRequest("buildargs must be a JSON object of string values")
		}
	}
	if raw := q.Get("labels"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &req.Labels); err != nil {
			return req, httpx.ErrBadRequest("labels must be a JSON object of string values")
		}
	}

	if req.ContextDir == "" {
		return req, httpx.ErrBadRequest("a ?context build directory is required")
	}
	return req, nil
}

// buildError maps the build service's sentinels onto HTTP.
func buildError(err error) error {
	switch {
	case errors.Is(err, service.ErrBuildNotFound):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"no such build").WithCause(err)
	case errors.Is(err, service.ErrLogUnavailable):
		return httpx.NewError(http.StatusGone, httpx.CodeNotFound,
			"%s", err.Error()).WithCause(err)
	case errors.Is(err, service.ErrTaskFinished):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"%s", err.Error()).WithCause(err)
	case errors.Is(err, service.ErrNoDockerfile):
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"%s", err.Error()).WithDetails(map[string]any{"field": "dockerfile"}).WithCause(err)
	case errors.Is(err, service.ErrContextTooLarge):
		return httpx.NewError(http.StatusRequestEntityTooLarge, httpx.CodePayloadTooLarge,
			"%s", err.Error()).WithCause(err)
	default:
		return engineError(err)
	}
}

// browseError maps the browser's sentinels onto HTTP.
func browseError(err error) error {
	if errors.Is(err, service.ErrNotADirectory) {
		return httpx.ErrBadRequest("%s", err.Error())
	}
	return engineError(err)
}

// buildTimeout bounds how long a single build may run before its task is
// ended. A build that has not finished in this long is stuck, and leaving it
// pinning a goroutine and a daemon connection helps nobody.
const buildTimeout = 2 * time.Hour
