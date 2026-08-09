package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

// writeContext puts a build context inside the test server's whitelist.
func writeContext(t *testing.T, env *testEnv, files map[string]string) string {
	t.Helper()

	root := allowedPathOf(t, env)
	dir := filepath.Join(root, "ctx")
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestBrowseListsTheWhitelistRoots(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleAdmin), http.MethodGet, APIPrefix+"/fs/browse")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var listing struct {
		Entries      []map[string]any `json:"entries"`
		AllowedRoots []string         `json:"allowed_roots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(listing.Entries) != 1 || len(listing.AllowedRoots) != 1 {
		t.Errorf("listing = %+v", listing)
	}
}

func TestBrowseListsADirectoryAndItsDockerfiles(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{
		"Dockerfile":      "FROM alpine\n",
		"Dockerfile.prod": "FROM alpine\n",
		"app/main.go":     "package main\n",
	})

	rec := request(t, env.as(store.RoleAdmin), http.MethodGet,
		APIPrefix+"/fs/browse?path="+dir)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var listing struct {
		Dockerfiles []string `json:"dockerfiles"`
		Parent      string   `json:"parent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(listing.Dockerfiles) != 2 {
		t.Errorf("dockerfiles = %v", listing.Dockerfiles)
	}
	if listing.Parent == "" {
		t.Error("a directory inside the root should offer a parent")
	}
}

// Browsing is the same trust boundary as a bind mount, seen from the other
// side.
func TestBrowseRefusesPathsOutsideTheWhitelist(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, path := range []string{"/etc", "/", "/root"} {
		rec := request(t, env.as(store.RoleAdmin), http.MethodGet,
			APIPrefix+"/fs/browse?path="+path)

		if rec.Code != http.StatusForbidden {
			t.Errorf("browse %q: status = %d, want 403", path, rec.Code)
			continue
		}
		if code, _ := errorOf(t, rec); code != string(httpx.CodePathNotAllowed) {
			t.Errorf("browse %q: code = %q", path, code)
		}
	}
}

func TestBrowseRefusesAFile(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"Dockerfile": "FROM alpine\n"})

	rec := request(t, env.as(store.RoleAdmin), http.MethodGet,
		APIPrefix+"/fs/browse?path="+filepath.Join(dir, "Dockerfile"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

// Browsing enumerates host directories, so it takes the build permission
// rather than read: a viewer has no business learning what is on this disk.
func TestBrowseNeedsTheBuildPermission(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, role := range []store.Role{store.RoleViewer, store.RoleOperator} {
		rec := request(t, env.as(role), http.MethodGet, APIPrefix+"/fs/browse")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403 (%s)", role, rec.Code, rec.Body.String())
		}
	}
}

// buildOverWS runs a build to completion and returns the frames it produced.
func buildOverWS(t *testing.T, env *testEnv, query string, role store.Role) []map[string]any {
	t.Helper()

	srv := httptest.NewServer(env.raw)
	defer srv.Close()

	ticket := env.issueTicket(t, role)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + APIPrefix + "/build?ticket=" + ticket + "&" + query

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, url, nil) //nolint:bodyclose // closed via conn
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed with status %d", resp.StatusCode)
		}
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	var frames []map[string]any
	for {
		_, data, readErr := conn.Read(ctx)
		if readErr != nil {
			return frames
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("frame is not JSON: %v (%q)", err, data)
		}
		frames = append(frames, frame)
		if frame["t"] == "done" || frame["t"] == "err" {
			return frames
		}
	}
}

func TestBuildStreamsItsOutputAndFinishes(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{
		"Dockerfile": "FROM alpine:3.20\nCOPY . /app\n",
		"app.js":     "console.log(1)\n",
	})

	frames := buildOverWS(t, env, "context="+dir+"&tag=app:v1", store.RoleAdmin)

	if len(frames) == 0 {
		t.Fatal("no frames arrived")
	}
	if frames[0]["t"] != "build" || frames[0]["id"] == "" {
		t.Errorf("first frame = %+v, want the build id", frames[0])
	}

	var sawLog, sawDone bool
	for _, frame := range frames {
		switch frame["t"] {
		case "log":
			sawLog = true
		case "done":
			sawDone = true
			if frame["status"] != string(store.BuildSuccess) {
				t.Errorf("done frame = %+v, want a successful status", frame)
			}
			if frame["image_id"] == "" {
				t.Error("the done frame carries no image id")
			}
		}
	}
	if !sawLog || !sawDone {
		t.Errorf("frames = %+v, want log output and a done frame", frames)
	}
}

func TestBuildRecordsItsHistory(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"Dockerfile": "FROM alpine\n"})

	buildOverWS(t, env, "context="+dir+"&tag=app:v1", store.RoleAdmin)

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/builds")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	items, total := listOf(t, rec)
	if total != 1 {
		t.Fatalf("total = %d, want the one build", total)
	}
	if items[0]["status"] != string(store.BuildSuccess) {
		t.Errorf("status = %v", items[0]["status"])
	}
	tags, _ := items[0]["tags"].([]any)
	if len(tags) != 1 || tags[0] != "app:v1" {
		t.Errorf("tags = %v", items[0]["tags"])
	}
	if items[0]["duration_ms"] == nil {
		t.Error("duration_ms is missing, so every client would have to compute it")
	}
}

func TestBuildLogIsReplayable(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"Dockerfile": "FROM alpine\n"})

	frames := buildOverWS(t, env, "context="+dir, store.RoleAdmin)
	id, _ := frames[0]["id"].(string)

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/builds/"+id+"/log")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want the log verbatim", ct)
	}
	if !strings.Contains(rec.Body.String(), "Step 1/3") {
		t.Errorf("body = %q, want the build output", rec.Body.String())
	}
}

// Building runs arbitrary commands from a Dockerfile as root inside the
// daemon, so it takes the build permission rather than operate.
func TestBuildNeedsTheBuildPermission(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"Dockerfile": "FROM alpine\n"})

	ticket := env.issueTicket(t, store.RoleOperator)
	code := requestStream(t, env.raw, APIPrefix+"/build?ticket="+ticket+"&context="+dir)

	if code != http.StatusForbidden {
		t.Errorf("operator status = %d, want 403", code)
	}
}

// A build that can never run is refused before the socket is accepted, so the
// client gets an ordinary HTTP error rather than a stream that dies at once.
func TestBuildRefusesAContextOutsideTheWhitelistBeforeUpgrading(t *testing.T) {
	env := newEnv(t, fake.New())

	ticket := env.issueTicket(t, store.RoleAdmin)
	code := requestStream(t, env.raw, APIPrefix+"/build?ticket="+ticket+"&context=/etc")

	if code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", code)
	}
}

func TestBuildRefusesAMissingDockerfileBeforeUpgrading(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"main.go": "package main\n"})

	ticket := env.issueTicket(t, store.RoleAdmin)
	code := requestStream(t, env.raw, APIPrefix+"/build?ticket="+ticket+"&context="+dir)

	if code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", code)
	}
}

func TestBuildWithoutAContextIsABadRequest(t *testing.T) {
	env := newEnv(t, fake.New())

	ticket := env.issueTicket(t, store.RoleAdmin)
	code := requestStream(t, env.raw, APIPrefix+"/build?ticket="+ticket)

	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestUnknownBuildIsNotFound(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, path := range []string{"/builds/nope", "/builds/nope/log"} {
		rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestCancellingAFinishedBuildIsAConflict(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"Dockerfile": "FROM alpine\n"})

	frames := buildOverWS(t, env, "context="+dir, store.RoleAdmin)
	id, _ := frames[0]["id"].(string)

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/builds/"+id+"/cancel", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCancellingNeedsTheBuildPermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost,
		APIPrefix+"/builds/anything/cancel", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestBuildsAreAudited(t *testing.T) {
	env := newEnv(t, fake.New())
	dir := writeContext(t, env, map[string]string{"Dockerfile": "FROM alpine\n"})

	buildOverWS(t, env, "context="+dir, store.RoleAdmin)

	entries, err := env.db.Audit.List(context.Background(), store.AuditFilter{Limit: 20})
	if err != nil {
		t.Fatalf("Audit.List() error = %v", err)
	}
	for _, e := range entries {
		if e.Action == "build.start" {
			if !strings.Contains(e.Detail, "Dockerfile") {
				t.Errorf("detail = %q, want the build file recorded", e.Detail)
			}
			return
		}
	}
	t.Error("no build.start audit entry")
}
