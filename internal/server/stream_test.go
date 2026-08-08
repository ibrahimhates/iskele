package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

// dialWS opens a WebSocket, failing the test if the handshake does not.
//
// The handshake response body belongs to the connection once the upgrade
// succeeds; closing the connection closes it.
func dialWS(t *testing.T, ctx context.Context, url string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.Dial(ctx, url, nil) //nolint:bodyclose // owned by conn
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("dial %s: %v", url, err)
	}
	return conn
}

// issueTicket asks the server for a streaming ticket as the given role.
func (e *testEnv) issueTicket(t *testing.T, role store.Role) string {
	t.Helper()

	rec := postJSON(t, e.raw, http.MethodPost, APIPrefix+"/auth/ws-ticket", `{}`, e.tokens[role])
	if rec.Code != http.StatusCreated {
		t.Fatalf("ws-ticket status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ticket response is not JSON: %v", err)
	}
	if body.Ticket == "" || body.ExpiresIn <= 0 {
		t.Fatalf("ticket response = %+v", body)
	}
	return body.Ticket
}

func TestTicketRequiresAuthentication(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/ws-ticket", `{}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestTicketIsSingleUse(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	// SSE is the easiest endpoint to redeem against without a real socket.
	first := requestStream(t, env.raw, APIPrefix+"/containers/"+runningID+"/stats?ticket="+ticket)
	if first != http.StatusOK {
		t.Fatalf("first use = %d, want 200", first)
	}

	second := requestStream(t, env.raw, APIPrefix+"/containers/"+runningID+"/stats?ticket="+ticket)
	if second != http.StatusUnauthorized {
		t.Errorf("second use = %d, want 401 — a ticket must not be replayable", second)
	}
}

func TestStreamRejectsMissingOrUnknownTicket(t *testing.T) {
	env := newEnv(t, fake.New())

	paths := []string{
		APIPrefix + "/containers/" + runningID + "/stats",
		APIPrefix + "/containers/" + runningID + "/logs",
		APIPrefix + "/containers/" + runningID + "/exec",
		APIPrefix + "/system/events",
	}

	for _, path := range paths {
		for _, query := range []string{"", "?ticket=made-up"} {
			if code := requestStream(t, env.raw, path+query); code != http.StatusUnauthorized {
				t.Errorf("%s%s: status = %d, want 401", path, query, code)
			}
		}
	}
}

func TestStreamTicketCarriesThePermissionCheck(t *testing.T) {
	env := newEnv(t, fake.New())

	// A viewer may read logs and stats.
	viewerTicket := env.issueTicket(t, store.RoleViewer)
	if code := requestStream(t, env.raw, APIPrefix+"/containers/"+runningID+"/stats?ticket="+viewerTicket); code != http.StatusOK {
		t.Errorf("viewer stats = %d, want 200", code)
	}

	// But not open a shell.
	viewerTicket = env.issueTicket(t, store.RoleViewer)
	code := requestStream(t, env.raw, APIPrefix+"/containers/"+runningID+"/exec?ticket="+viewerTicket)
	if code != http.StatusForbidden {
		t.Errorf("viewer exec = %d, want 403", code)
	}
}

// requestStream issues a streaming request with a short deadline and returns
// the status code. SSE handlers never return on their own, so the context
// deadline is what ends them.
func requestStream(t *testing.T, h http.Handler, path string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream handler did not return")
	}
	return rec.Code
}

func TestStatsStreamEmitsSamples(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/containers/"+runningID+"/stats?ticket="+ticket, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: stats") {
		t.Fatalf("body = %q, want stats events", body)
	}
	if !strings.Contains(body, `"cpu_percent"`) {
		t.Errorf("body = %q, want the sample payload", body)
	}
}

// One connection covers every running container, so the payload has to say
// which container each sample belongs to.
func TestAllStatsStreamTagsSamplesWithTheirContainer(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/containers/stats?ticket="+ticket, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: stats") {
		t.Fatalf("body = %q, want stats events", body)
	}
	if !strings.Contains(body, `"id":"`+runningID+`"`) {
		t.Errorf("body = %q, want the container id on the sample", body)
	}
	if !strings.Contains(body, `"cpu_percent"`) {
		t.Errorf("body = %q, want the sample payload", body)
	}
}

// The static path must win over the /containers/{id} pattern; otherwise this
// would be read as a request for a container literally named "stats".
func TestAllStatsPathIsNotMistakenForAContainerID(t *testing.T) {
	env := newEnv(t, fake.New())

	code := requestStream(t, env.raw, APIPrefix+"/containers/stats?ticket=nonsense")
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 from the ticket check", code)
	}
}

func TestEventsStreamEmitsDockerEvents(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/system/events?ticket="+ticket, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "event: docker") {
		t.Errorf("body = %q, want docker events", rec.Body.String())
	}
}

func TestLogWebSocketStreamsLines(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	srv := httptest.NewServer(env.raw)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") +
		APIPrefix + "/containers/" + runningID + "/logs?ticket=" + ticket + "&follow=false"

	conn := dialWS(t, ctx, url)
	defer func() { _ = conn.CloseNow() }()

	var got []map[string]any
	for i := 0; i < 4; i++ {
		_, data, readErr := conn.Read(ctx)
		if readErr != nil {
			break
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("frame is not JSON: %v (%q)", err, data)
		}
		got = append(got, msg)
		if msg["t"] == "eof" {
			break
		}
	}

	if len(got) < 2 {
		t.Fatalf("received %d frames, want the log backlog", len(got))
	}
	if got[0]["t"] != "log" || got[0]["m"] != "starting" {
		t.Errorf("first frame = %v", got[0])
	}
	// stderr must be labeled, so the viewer can color it.
	var sawStderr bool
	for _, msg := range got {
		if msg["s"] == "stderr" {
			sawStderr = true
		}
	}
	if !sawStderr {
		t.Errorf("frames = %v, want the stderr line labeled", got)
	}
	if got[len(got)-1]["t"] != "eof" {
		t.Errorf("last frame = %v, want eof", got[len(got)-1])
	}
}

func TestExecWebSocketEchoesStdin(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	srv := httptest.NewServer(env.raw)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") +
		APIPrefix + "/containers/" + runningID + "/exec?ticket=" + ticket + "&cmd=/bin/sh"

	conn := dialWS(t, ctx, url)
	defer func() { _ = conn.CloseNow() }()

	// The fake pipes stdin back as output, which proves the plumbing without
	// needing a container.
	if writeErr := conn.Write(ctx, websocket.MessageBinary, []byte("echo hi\n")); writeErr != nil {
		t.Fatalf("write stdin: %v", writeErr)
	}

	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msgType != websocket.MessageBinary {
		t.Errorf("message type = %v, want binary for process output", msgType)
	}
	if string(data) != "echo hi\n" {
		t.Errorf("output = %q", data)
	}
}

func TestExecResizeIsForwarded(t *testing.T) {
	f := fake.New()
	env := newEnv(t, f)
	ticket := env.issueTicket(t, store.RoleAdmin)

	srv := httptest.NewServer(env.raw)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") +
		APIPrefix + "/containers/" + runningID + "/exec?ticket=" + ticket

	conn := dialWS(t, ctx, url)
	defer func() { _ = conn.CloseNow() }()

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"t":"resize","rows":40,"cols":120}`)); err != nil {
		t.Fatalf("write control: %v", err)
	}

	// The resize is asynchronous; poll briefly rather than sleeping a fixed time.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if calls := f.CallsFor(fake.OpResizeExec); len(calls) > 0 {
			size, ok := calls[0].Opts.([2]uint)
			if !ok || size[0] != 40 || size[1] != 120 {
				t.Fatalf("resize forwarded as %v, want 40x120", calls[0].Opts)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the resize control message never reached the engine")
}

func TestExecIsAudited(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleAdmin)

	srv := httptest.NewServer(env.raw)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") +
		APIPrefix + "/containers/" + runningID + "/exec?ticket=" + ticket + "&cmd=/bin/bash"

	conn := dialWS(t, ctx, url)
	_ = conn.CloseNow()

	// Opening a shell in a container is the most sensitive action in the
	// panel; it must leave a trace regardless of what the session did.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := env.db.Audit.List(context.Background(), store.AuditFilter{Action: "container.exec"})
		if err != nil {
			t.Fatalf("audit List() error = %v", err)
		}
		if len(entries) > 0 {
			if !strings.Contains(entries[0].Detail, "/bin/bash") {
				t.Errorf("audit detail = %q, want the command recorded", entries[0].Detail)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the exec was not audited")
}

func TestBatchAppliesToEveryContainer(t *testing.T) {
	f := fake.New()
	env := newEnv(t, f)

	body := `{"ids":["` + runningID + `","` + stoppedID + `"],"action":"stop"}`
	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/containers/batch", body, env.tokens[store.RoleAdmin])

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Total     int `json:"total"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
		Results   []struct {
			ID string `json:"id"`
			OK bool   `json:"ok"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if result.Total != 2 || result.Succeeded != 2 || result.Failed != 0 {
		t.Errorf("result = %+v", result)
	}
	if len(f.CallsFor(fake.OpStopContainer)) != 2 {
		t.Errorf("engine stop called %d times", len(f.CallsFor(fake.OpStopContainer)))
	}
}

func TestBatchReportsPartialFailure(t *testing.T) {
	env := newEnv(t, fake.New())

	// One real container and one that does not exist: the real one must still
	// be acted on, and the failure named precisely.
	body := `{"ids":["` + runningID + `","ghost"],"action":"stop"}`
	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/containers/batch", body, env.tokens[store.RoleAdmin])

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207 for a partial failure", rec.Code)
	}

	var result struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
		Results   []struct {
			ID    string `json:"id"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
			Code  string `json:"code"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	for _, r := range result.Results {
		if r.ID == "ghost" {
			if r.OK || r.Code != "NOT_FOUND" || !strings.Contains(r.Error, "ghost") {
				t.Errorf("failed entry = %+v, want a precise reason", r)
			}
		}
	}
}

func TestBatchRejectsUnknownAction(t *testing.T) {
	env := newEnv(t, fake.New())

	body := `{"ids":["` + runningID + `"],"action":"detonate"}`
	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/containers/batch", body, env.tokens[store.RoleAdmin])

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeBadRequest) {
		t.Errorf("code = %q", code)
	}
}

func TestBatchRejectsEmptySelection(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/containers/batch",
		`{"ids":[],"action":"stop"}`, env.tokens[store.RoleAdmin])

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestBatchNeedsOperatePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	body := `{"ids":["` + runningID + `"],"action":"stop"}`
	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/containers/batch", body, env.tokens[store.RoleViewer])

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a viewer", rec.Code)
	}
}

func TestPauseUnpauseKillRename(t *testing.T) {
	f := fake.New()
	env := newEnv(t, f)
	token := env.tokens[store.RoleAdmin]

	for _, action := range []string{"pause", "unpause", "kill"} {
		rec := postJSON(t, env.raw, http.MethodPost,
			APIPrefix+"/containers/"+runningID+"/"+action, ``, token)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200: %s", action, rec.Code, rec.Body.String())
		}
	}

	rec := postJSON(t, env.raw, http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/rename", `{"name":"web-renamed"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d: %s", rec.Code, rec.Body.String())
	}

	calls := f.CallsFor(fake.OpRenameContainer)
	if len(calls) != 1 || calls[0].Opts != "web-renamed" {
		t.Errorf("rename forwarded as %+v", calls)
	}
}

func TestRenameRequiresAName(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := postJSON(t, env.raw, http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/rename", `{"name":"  "}`, env.tokens[store.RoleAdmin])

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRedeployRecreatesTheContainer(t *testing.T) {
	f := fake.New()
	env := newEnv(t, f)

	rec := postJSON(t, env.raw, http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/redeploy", ``, env.tokens[store.RoleAdmin])

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		OldID      string `json:"old_id"`
		NewID      string `json:"new_id"`
		RolledBack bool   `json:"rolled_back"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if result.NewID == "" || result.RolledBack {
		t.Errorf("result = %+v", result)
	}

	// The original must have been parked under a temporary name before the
	// replacement claimed the original one.
	renames := f.CallsFor(fake.OpRenameContainer)
	if len(renames) == 0 {
		t.Fatal("the original container was never renamed out of the way")
	}
	parked, ok := renames[0].Opts.(string)
	if !ok || !strings.Contains(parked, "_old_") {
		t.Errorf("parked name = %v, want a temporary name", renames[0].Opts)
	}

	if len(f.CallsFor(fake.OpCreateContainer)) != 1 {
		t.Error("no replacement container was created")
	}
}

func TestRedeployNeedsOperatePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := postJSON(t, env.raw, http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/redeploy", ``, env.tokens[store.RoleViewer])

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
