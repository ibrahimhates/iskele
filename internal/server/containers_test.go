package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
)

const (
	runningID = "c1000000000000000000000000000000000000000000000000000000000000a"
	stoppedID = "c2000000000000000000000000000000000000000000000000000000000000b"
)

// routerWith builds a router backed by the supplied fake engine.
func routerWith(t *testing.T, f *fake.Client) http.Handler {
	t.Helper()
	cfg := config.Default()
	return NewRouter(Deps{
		Config: &cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Docker: f,
	})
}

func request(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// errorOf decodes the standard error envelope.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not an error envelope: %v (%q)", err, rec.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

// listOf decodes the {items,total} envelope.
func listOf(t *testing.T, rec *httptest.ResponseRecorder) (items []map[string]any, total int) {
	t.Helper()
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a list envelope: %v (%q)", err, rec.Body.String())
	}
	return body.Items, body.Total
}

func TestListContainers(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/containers")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items, total := listOf(t, rec)
	if total != 1 || len(items) != 1 {
		t.Fatalf("total = %d, want only the running container by default", total)
	}
	if items[0]["name"] != "web" {
		t.Errorf("name = %v", items[0]["name"])
	}
	if items[0]["health"] != "healthy" {
		t.Errorf("health = %v", items[0]["health"])
	}
}

func TestListContainersAllFlag(t *testing.T) {
	h := routerWith(t, fake.New())

	for _, query := range []string{"?all=true", "?all=1", "?all"} {
		rec := request(t, h, http.MethodGet, APIPrefix+"/containers"+query)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", query, rec.Code)
		}
		if _, total := listOf(t, rec); total != 2 {
			t.Errorf("%s: total = %d, want 2", query, total)
		}
	}
}

func TestListContainersRejectsBadBoolean(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/containers?all=maybe")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	code, msg := errorOf(t, rec)
	if code != string(httpx.CodeBadRequest) {
		t.Errorf("code = %q", code)
	}
	if !strings.Contains(msg, "all") {
		t.Errorf("message = %q, want it to name the parameter", msg)
	}
}

func TestListContainersForwardsFilters(t *testing.T) {
	f := fake.New()
	h := routerWith(t, f)

	rec := request(t, h, http.MethodGet,
		APIPrefix+"/containers?label=app%3Dweb&label=tier%3Dfront&status=running&name=web&size=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	opts, ok := f.CallsFor(fake.OpListContainers)[0].Opts.(docker.ListContainersOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpListContainers)[0].Opts)
	}
	if len(opts.Label) != 2 || opts.Label[0] != "app=web" {
		t.Errorf("Label = %v", opts.Label)
	}
	if len(opts.Status) != 1 || opts.Status[0] != "running" {
		t.Errorf("Status = %v", opts.Status)
	}
	if opts.Name != "web" || !opts.Size {
		t.Errorf("Name/Size = %q/%v", opts.Name, opts.Size)
	}
}

func TestListParamAcceptsCommaSeparatedValues(t *testing.T) {
	f := fake.New()

	rec := request(t, routerWith(t, f), http.MethodGet, APIPrefix+"/containers?label=a%3D1,b%3D2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	opts, ok := f.CallsFor(fake.OpListContainers)[0].Opts.(docker.ListContainersOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpListContainers)[0].Opts)
	}
	if len(opts.Label) != 2 {
		t.Errorf("Label = %v, want the comma-separated value split", opts.Label)
	}
}

func TestGetContainer(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/containers/"+runningID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["name"] != "web" {
		t.Errorf("name = %v", body["name"])
	}
	if body["restart_count"] != float64(0) {
		t.Errorf("restart_count = %v", body["restart_count"])
	}
}

func TestGetUnknownContainerReturns404WithSpecificCode(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/containers/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	code, msg := errorOf(t, rec)
	if code != string(httpx.CodeContainerNotFound) {
		t.Errorf("code = %q, want %q", code, httpx.CodeContainerNotFound)
	}
	if !strings.Contains(msg, "nope") {
		t.Errorf("message = %q, want the engine text naming the container", msg)
	}
}

func TestInspectReturnsEnginePayloadVerbatim(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet,
		APIPrefix+"/containers/"+runningID+"/inspect")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if raw["Id"] != runningID {
		t.Errorf("Id = %v, want the engine's own field names preserved", raw["Id"])
	}
}

func TestLifecycleActions(t *testing.T) {
	tests := []struct {
		action string
		op     string
	}{
		{"start", fake.OpStartContainer},
		{"stop", fake.OpStopContainer},
		{"restart", fake.OpRestartContainer},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			f := fake.New()
			rec := request(t, routerWith(t, f), http.MethodPost,
				APIPrefix+"/containers/"+runningID+"/"+tt.action)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if body["action"] != tt.action || body["status"] != "ok" || body["id"] != runningID {
				t.Errorf("body = %v", body)
			}
			if len(f.CallsFor(tt.op)) != 1 {
				t.Errorf("engine op %s called %d times", tt.op, len(f.CallsFor(tt.op)))
			}
		})
	}
}

func TestStopForwardsTimeout(t *testing.T) {
	f := fake.New()

	rec := request(t, routerWith(t, f), http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/stop?timeout=30")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	opts, ok := f.CallsFor(fake.OpStopContainer)[0].Opts.(docker.StopOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpStopContainer)[0].Opts)
	}
	if opts.Timeout == nil || *opts.Timeout != 30 {
		t.Errorf("timeout = %v, want 30", opts.Timeout)
	}
}

func TestStopRejectsNonNumericTimeout(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/stop?timeout=soon")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestActionOnUnknownContainerReturns404(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		rec := request(t, routerWith(t, fake.New()), http.MethodPost,
			APIPrefix+"/containers/ghost/"+action)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", action, rec.Code)
		}
		if code, _ := errorOf(t, rec); code != string(httpx.CodeContainerNotFound) {
			t.Errorf("%s: code = %q", action, code)
		}
	}
}

func TestRemoveContainer(t *testing.T) {
	f := fake.New()
	h := routerWith(t, f)

	rec := request(t, h, http.MethodDelete, APIPrefix+"/containers/"+stoppedID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty for 204", rec.Body.String())
	}

	if rec := request(t, h, http.MethodGet, APIPrefix+"/containers/"+stoppedID); rec.Code != http.StatusNotFound {
		t.Errorf("container still present after remove: status = %d", rec.Code)
	}
}

func TestRemoveRunningContainerConflicts(t *testing.T) {
	h := routerWith(t, fake.New())

	rec := request(t, h, http.MethodDelete, APIPrefix+"/containers/"+runningID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	code, msg := errorOf(t, rec)
	if code != string(httpx.CodeConflict) {
		t.Errorf("code = %q", code)
	}
	// The engine's advice ("stop the container ... or force remove") is what
	// tells the operator how to proceed, so it must survive.
	if !strings.Contains(msg, "force") {
		t.Errorf("message = %q, want the engine's own guidance", msg)
	}
}

func TestRemoveForwardsForceAndVolumes(t *testing.T) {
	f := fake.New()

	rec := request(t, routerWith(t, f), http.MethodDelete,
		APIPrefix+"/containers/"+runningID+"?force=true&volumes=true")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	opts, ok := f.CallsFor(fake.OpRemoveContainer)[0].Opts.(docker.RemoveContainerOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpRemoveContainer)[0].Opts)
	}
	if !opts.Force || !opts.RemoveVolumes {
		t.Errorf("options = %+v, want both flags forwarded", opts)
	}
}
