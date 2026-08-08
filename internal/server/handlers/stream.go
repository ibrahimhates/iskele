package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
)

const (
	// wsWriteTimeout bounds a single frame write, so one stalled client cannot
	// pin a goroutine and an engine stream forever.
	wsWriteTimeout = 10 * time.Second
	// sseHeartbeat keeps proxies from closing an idle event stream.
	sseHeartbeat = 25 * time.Second
	// maxExecFrame bounds one stdin frame from the browser.
	maxExecFrame = 64 << 10
)

// Stream serves the WebSocket and SSE endpoints.
type Stream struct {
	containers *service.Container
	system     *service.System
	images     *service.Image
	tickets    *auth.TicketStore
	tasks      *service.TaskRegistry
}

// NewStream builds the streaming handler set.
func NewStream(containers *service.Container, system *service.System, images *service.Image,
	tickets *auth.TicketStore, tasks *service.TaskRegistry,
) *Stream {
	return &Stream{
		containers: containers,
		system:     system,
		images:     images,
		tickets:    tickets,
		tasks:      tasks,
	}
}

// Ticket handles POST /auth/ws-ticket.
//
// Browsers cannot set an Authorization header on a WebSocket handshake or an
// EventSource request, so streaming endpoints authenticate with a ticket the
// caller fetches here first (D-008).
func (h *Stream) Ticket(w http.ResponseWriter, r *http.Request) error {
	identity := middleware.IdentityFrom(r.Context())

	value, err := h.tickets.Issue(auth.Ticket{
		UserID:   identity.UserID,
		Username: identity.Username,
		Role:     identity.Role,
		TokenID:  identity.TokenID,
		Scopes:   identity.Scopes,
	})
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"ticket":     value,
		"expires_in": int(auth.TicketTTL.Seconds()),
	})
	return nil
}

// redeemTicket authenticates a streaming request and checks its permission.
//
// The ticket is consumed whether or not the permission check passes: a ticket
// is single-use by definition, and leaving a rejected one alive would let it
// be retried against another endpoint.
func (h *Stream) redeemTicket(r *http.Request, perm middleware.Permission) (auth.Ticket, error) {
	ticket, err := h.tickets.Redeem(r.URL.Query().Get("ticket"))
	if err != nil {
		return auth.Ticket{}, httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthorized,
			"a valid streaming ticket is required; request one from POST /api/v1/auth/ws-ticket")
	}
	if !middleware.RoleHas(ticket.Role, perm) {
		return auth.Ticket{}, httpx.NewError(http.StatusForbidden, httpx.CodeForbidden,
			"role %q may not %s", ticket.Role, perm)
	}
	return ticket, nil
}

// acceptWebSocket completes the handshake.
//
// The library rejects a cross-origin handshake by default (it requires Origin
// to match Host), which is the only defense available here: a WebSocket is not
// subject to the same-origin policy the way fetch is.
func acceptWebSocket(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	return conn, nil
}

// wsMessage is the envelope the log and build streams use.
type wsMessage struct {
	Type      string `json:"t"`
	Stream    string `json:"s,omitempty"`
	Timestamp string `json:"ts,omitempty"`
	Message   string `json:"m,omitempty"`
	Code      string `json:"code,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

// Logs handles WS /containers/{id}/logs.
func (h *Stream) Logs(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.redeemTicket(r, middleware.PermRead); err != nil {
		return err
	}

	id := chi.URLParam(r, "id")
	opts, err := logOptions(r)
	if err != nil {
		return err
	}

	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// A client that closes the tab should stop the engine stream; reading
	// until failure is how a WebSocket server notices that.
	go func() {
		for {
			if _, _, readErr := conn.Read(ctx); readErr != nil {
				cancel()
				return
			}
		}
	}()

	lines, errs := h.containers.Logs(ctx, id, opts)

	for {
		select {
		case <-ctx.Done():
			return nil

		case line, ok := <-lines:
			if !ok {
				sendWS(ctx, conn, wsMessage{Type: "eof"})
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return nil
			}
			msg := wsMessage{Type: "log", Stream: line.Stream, Message: line.Message}
			if !line.Timestamp.IsZero() {
				msg.Timestamp = line.Timestamp.Format(time.RFC3339Nano)
			}
			if !sendWS(ctx, conn, msg) {
				return nil
			}

		case streamErr, ok := <-errs:
			if !ok || streamErr == nil {
				continue
			}
			sendWS(ctx, conn, wsMessage{
				Type:    "err",
				Code:    kindCodeOf(streamErr),
				Message: docker.Message(streamErr),
			})
			_ = conn.Close(websocket.StatusInternalError, "stream failed")
			return nil
		}
	}
}

// logOptions reads the log query parameters.
func logOptions(r *http.Request) (docker.LogOptions, error) {
	q := r.URL.Query()

	tail, err := intParam(r, "tail")
	if err != nil {
		return docker.LogOptions{}, err
	}
	timestamps, err := boolParam(r, "timestamps")
	if err != nil {
		return docker.LogOptions{}, err
	}
	stdout, err := boolParam(r, "stdout")
	if err != nil {
		return docker.LogOptions{}, err
	}
	stderr, err := boolParam(r, "stderr")
	if err != nil {
		return docker.LogOptions{}, err
	}

	// Following is the point of a WebSocket log stream, so it is the default;
	// ?follow=false asks for the backlog only.
	follow := true
	if q.Has("follow") {
		if follow, err = boolParam(r, "follow"); err != nil {
			return docker.LogOptions{}, err
		}
	}

	opts := docker.LogOptions{
		Follow:     follow,
		Timestamps: timestamps,
		Stdout:     stdout,
		Stderr:     stderr,
	}
	if tail != nil {
		opts.Tail = *tail
	} else {
		// Replaying an entire multi-gigabyte log by default would hang the
		// browser; the UI can ask for more explicitly.
		opts.Tail = 500
	}
	return opts, nil
}

// execControl is the client-to-server control message on the exec socket.
type execControl struct {
	Type string `json:"t"`
	Cols uint   `json:"cols,omitempty"`
	Rows uint   `json:"rows,omitempty"`
}

// Exec handles WS /containers/{id}/exec.
func (h *Stream) Exec(w http.ResponseWriter, r *http.Request) error {
	ticket, err := h.redeemTicket(r, middleware.PermOperate)
	if err != nil {
		return err
	}

	id := chi.URLParam(r, "id")
	opts, err := execOptions(r)
	if err != nil {
		return err
	}

	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	session, err := h.containers.Exec(ctx, id, opts, ticketActor(ticket), metaOf(r))
	if err != nil {
		sendWS(ctx, conn, wsMessage{Type: "err", Code: kindCodeOf(err), Message: docker.Message(err)})
		_ = conn.Close(websocket.StatusInternalError, "exec failed")
		return nil
	}
	defer session.Close()

	// Output: engine to browser.
	go func() {
		defer cancel()
		buf := make([]byte, 32<<10)
		for {
			n, readErr := session.Reader.Read(buf)
			if n > 0 {
				if writeErr := writeWSBinary(ctx, conn, buf[:n]); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Input: browser to engine. Binary frames are stdin; text frames are
	// control messages, which is how resize arrives without escaping.
	for {
		msgType, data, readErr := conn.Read(ctx)
		if readErr != nil {
			break
		}
		if len(data) > maxExecFrame {
			data = data[:maxExecFrame]
		}

		if msgType == websocket.MessageText {
			var control execControl
			if json.Unmarshal(data, &control) == nil && control.Type == "resize" {
				_ = h.containers.ResizeExec(ctx, session.ID, control.Rows, control.Cols)
			}
			continue
		}

		if _, writeErr := session.Conn.Write(data); writeErr != nil {
			break
		}
	}

	cancel()

	// Report the command's exit status, which is the whole point of running it.
	exitCtx, exitCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer exitCancel()
	if code, codeErr := h.containers.ExecExitCode(exitCtx, session.ID); codeErr == nil {
		sendWS(exitCtx, conn, wsMessage{Type: "exit", ExitCode: &code})
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return nil
}

// execOptions reads the exec query parameters.
func execOptions(r *http.Request) (docker.ExecOptions, error) {
	q := r.URL.Query()

	cmd := q["cmd"]
	if len(cmd) == 0 {
		// The UI offers a shell picker; /bin/sh is the one every image has.
		cmd = []string{"/bin/sh"}
	}

	tty := true
	if q.Has("tty") {
		var err error
		if tty, err = boolParam(r, "tty"); err != nil {
			return docker.ExecOptions{}, err
		}
	}

	rows, err := intParam(r, "rows")
	if err != nil {
		return docker.ExecOptions{}, err
	}
	cols, err := intParam(r, "cols")
	if err != nil {
		return docker.ExecOptions{}, err
	}

	opts := docker.ExecOptions{
		Cmd:        cmd,
		TTY:        tty,
		User:       q.Get("user"),
		WorkingDir: q.Get("workdir"),
	}
	if rows != nil && *rows > 0 {
		opts.Rows = uint(*rows)
	}
	if cols != nil && *cols > 0 {
		opts.Cols = uint(*cols)
	}
	return opts, nil
}

// Stats handles SSE /containers/{id}/stats.
func (h *Stream) Stats(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.redeemTicket(r, middleware.PermRead); err != nil {
		return err
	}

	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpx.ErrInternal("this connection does not support streaming")
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	samples, errs := h.containers.Stats(ctx, id)

	writeSSEHeaders(w)
	flusher.Flush()

	// The buffer means a client that connects to a long-running container
	// sees a populated chart immediately rather than one point.
	history := docker.NewRingBuffer(service.StatsHistorySize)
	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			flusher.Flush()

		case sample, ok := <-samples:
			if !ok {
				writeSSEEvent(w, "eof", json.RawMessage(`{}`))
				flusher.Flush()
				return nil
			}
			history.Add(sample)
			payload, err := json.Marshal(sample)
			if err != nil {
				continue
			}
			if !writeSSEEvent(w, "stats", payload) {
				return nil
			}
			flusher.Flush()

		case streamErr, ok := <-errs:
			if !ok || streamErr == nil {
				continue
			}
			payload, _ := json.Marshal(map[string]string{
				"code":    kindCodeOf(streamErr),
				"message": docker.Message(streamErr),
			})
			writeSSEEvent(w, "error", payload)
			flusher.Flush()
			return nil
		}
	}
}

// StatsAll handles SSE /containers/stats.
//
// One connection covers every running container, because the list view needs a
// figure per row and a browser will not open more than six connections to one
// origin.
func (h *Stream) StatsAll(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.redeemTicket(r, middleware.PermRead); err != nil {
		return err
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpx.ErrInternal("this connection does not support streaming")
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	samples, errs := h.containers.StatsAll(ctx)

	writeSSEHeaders(w)
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			flusher.Flush()

		case sample, ok := <-samples:
			if !ok {
				writeSSEEvent(w, "eof", json.RawMessage(`{}`))
				flusher.Flush()
				return nil
			}
			payload, err := json.Marshal(sample)
			if err != nil {
				continue
			}
			if !writeSSEEvent(w, "stats", payload) {
				return nil
			}
			flusher.Flush()

		case streamErr, ok := <-errs:
			if !ok || streamErr == nil {
				continue
			}
			payload, _ := json.Marshal(map[string]string{
				"code":    kindCodeOf(streamErr),
				"message": docker.Message(streamErr),
			})
			writeSSEEvent(w, "error", payload)
			flusher.Flush()
			return nil
		}
	}
}

// Events handles SSE /system/events.
func (h *Stream) Events(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.redeemTicket(r, middleware.PermRead); err != nil {
		return err
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpx.ErrInternal("this connection does not support streaming")
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	events, errs := h.system.Events(ctx)

	writeSSEHeaders(w)
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			flusher.Flush()

		case event, ok := <-events:
			if !ok {
				return nil
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if !writeSSEEvent(w, "docker", payload) {
				return nil
			}
			flusher.Flush()

		case streamErr, ok := <-errs:
			if !ok || streamErr == nil {
				continue
			}
			payload, _ := json.Marshal(map[string]string{
				"code":    kindCodeOf(streamErr),
				"message": docker.Message(streamErr),
			})
			writeSSEEvent(w, "error", payload)
			flusher.Flush()
			return nil
		}
	}
}

func writeSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Tells nginx not to buffer, which would otherwise hold events back until
	// the response ended.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// writeSSEEvent writes one event, reporting whether the client is still there.
func writeSSEEvent(w http.ResponseWriter, event string, data []byte) bool {
	_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err == nil
}

// sendWS writes one JSON frame, reporting whether the client is still there.
func sendWS(ctx context.Context, conn *websocket.Conn, msg wsMessage) bool {
	payload, err := json.Marshal(msg)
	if err != nil {
		return true
	}

	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()

	return conn.Write(writeCtx, websocket.MessageText, payload) == nil
}

// writeWSBinary sends raw process output.
func writeWSBinary(ctx context.Context, conn *websocket.Conn, data []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageBinary, data)
}

// ticketActor converts a redeemed ticket into an audit actor.
func ticketActor(t auth.Ticket) audit.Actor {
	return audit.Actor{UserID: t.UserID, Username: t.Username, Role: t.Role, TokenID: t.TokenID}
}

// kindCodeOf names the engine failure class for a stream error.
func kindCodeOf(err error) string {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return string(apiErr.Code)
	}
	switch docker.KindOf(err) {
	case docker.KindNotFound:
		return string(httpx.CodeContainerNotFound)
	case docker.KindConflict:
		return string(httpx.CodeConflict)
	case docker.KindUnavailable:
		return string(httpx.CodeDockerUnavailable)
	default:
		return string(httpx.CodeDockerError)
	}
}
