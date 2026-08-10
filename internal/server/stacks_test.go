package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

const stackCompose = "services:\n  app:\n    image: alpine:3.20\n    command: sleep infinity\n"

// createStack posts a stack and returns its id.
func createStack(t *testing.T, env *testEnv, name, composeYAML string) string {
	t.Helper()

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/stacks", map[string]any{
		"name":    name,
		"source":  "editor",
		"compose": composeYAML,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create stack: status = %d (%s)", rec.Code, rec.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}
	return created.ID
}

func TestStackCRUD(t *testing.T) {
	env := newEnv(t, fake.New())

	id := createStack(t, env, "shop", stackCompose)

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/stacks")
	items, total := listOf(t, rec)
	if total != 1 || len(items) != 1 {
		t.Fatalf("list = %d items, want 1 (%s)", total, rec.Body.String())
	}

	rec = request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/stacks/"+id)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d (%s)", rec.Code, rec.Body.String())
	}
	var detail struct {
		Name     string `json:"name"`
		Services []struct {
			Name     string `json:"name"`
			Replicas int    `json:"replicas"`
		} `json:"services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("detail is not JSON: %v", err)
	}
	if detail.Name != "shop" || len(detail.Services) != 1 || detail.Services[0].Name != "app" {
		t.Errorf("detail = %+v, want the stack's one service", detail)
	}

	rec = send(t, env.as(store.RoleAdmin), http.MethodPut, APIPrefix+"/stacks/"+id, map[string]any{
		"source":  "editor",
		"compose": strings.Replace(stackCompose, "alpine:3.20", "alpine:3.21", 1),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d (%s)", rec.Code, rec.Body.String())
	}

	rec = request(t, env.as(store.RoleAdmin), http.MethodDelete, APIPrefix+"/stacks/"+id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d (%s)", rec.Code, rec.Body.String())
	}

	rec = request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/stacks/"+id)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: status = %d, want 404", rec.Code)
	}
}

// The name labels every container a stack creates, so two stacks cannot share
// one.
func TestStackCreateRefusesADuplicateName(t *testing.T) {
	env := newEnv(t, fake.New())
	createStack(t, env, "shop", stackCompose)

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/stacks", map[string]any{
		"name": "shop", "source": "editor", "compose": stackCompose,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeConflict) {
		t.Errorf("code = %q, want CONFLICT", code)
	}
}

// An invalid file is the normal state of one being edited, so validation
// answers 200 with a report rather than an error status.
func TestStackValidateReportsProblemsWithA200(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleViewer), http.MethodPost, APIPrefix+"/stacks/validate", map[string]any{
		"name":    "broken",
		"source":  "editor",
		"compose": "services:\n  app:\n  image: [",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var report struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if report.Valid || report.Error == "" {
		t.Errorf("report = %+v, want invalid with an explanation", report)
	}
}

func TestStackValidateReportsWhitelistProblems(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleViewer), http.MethodPost, APIPrefix+"/stacks/validate", map[string]any{
		"name":    "leak",
		"source":  "editor",
		"compose": "services:\n  app:\n    image: alpine\n    volumes:\n      - /etc:/x\n",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var report struct {
		Valid    bool `json:"valid"`
		Problems []struct {
			Service string `json:"service"`
			Field   string `json:"field"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if report.Valid || len(report.Problems) == 0 {
		t.Fatalf("report = %+v, want the bind refused", report)
	}
	if report.Problems[0].Field != "volumes" {
		t.Errorf("problem = %+v, want it to name the field", report.Problems[0])
	}
}

func TestStackDiffReportsWhatAnEditWouldDo(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)

	rec := send(t, env.as(store.RoleViewer), http.MethodPost, APIPrefix+"/stacks/"+id+"/diff",
		map[string]any{
			"source":  "editor",
			"compose": strings.Replace(stackCompose, "alpine:3.20", "alpine:3.21", 1),
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var diff struct {
		Services []struct {
			Service string   `json:"service"`
			Kind    string   `json:"kind"`
			Fields  []string `json:"fields"`
		} `json:"services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
		t.Fatalf("diff is not JSON: %v", err)
	}
	if len(diff.Services) != 1 || diff.Services[0].Kind != "modified" {
		t.Fatalf("diff = %+v, want one modified service", diff.Services)
	}
	if len(diff.Services[0].Fields) == 0 || diff.Services[0].Fields[0] != "image" {
		t.Errorf("fields = %v, want image", diff.Services[0].Fields)
	}
}

// Reading a stack is one permission; changing one is another. A viewer that
// could deploy would make the whole role pointless.
func TestStackWritesNeedThePermission(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, APIPrefix + "/stacks", map[string]any{"name": "x", "source": "editor", "compose": stackCompose}},
		{http.MethodPut, APIPrefix + "/stacks/" + id, map[string]any{"source": "editor", "compose": stackCompose}},
		{http.MethodDelete, APIPrefix + "/stacks/" + id, nil},
		{http.MethodPost, APIPrefix + "/stacks/" + id + "/down", nil},
		{http.MethodPost, APIPrefix + "/stacks/import", map[string]any{"name": "x"}},
	}

	for _, tc := range cases {
		rec := send(t, env.as(store.RoleViewer), tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as viewer: status = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}

	// An operator runs and removes stacks — that is what the role is for.
	// Taking a stack down removes containers, so it takes the delete
	// permission, which an operator has and a viewer does not.
	for _, action := range []string{"stop", "down"} {
		rec := send(t, env.as(store.RoleOperator), http.MethodPost,
			APIPrefix+"/stacks/"+id+"/"+action, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s as operator: status = %d, want 200 (%s)", action, rec.Code, rec.Body.String())
		}
	}
}

func TestStackActionsReportWhatTheyTouched(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)

	// Deploy through the service so there is something to act on.
	deployStack(t, env, id)

	for _, action := range []string{"stop", "start", "restart"} {
		rec := send(t, env.as(store.RoleOperator), http.MethodPost,
			APIPrefix+"/stacks/"+id+"/"+action, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d (%s)", action, rec.Code, rec.Body.String())
		}

		var result struct {
			Containers []string `json:"containers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("%s response is not JSON: %v", action, err)
		}
		if len(result.Containers) != 1 || result.Containers[0] != "shop-app-1" {
			t.Errorf("%s touched %v, want the stack's container", action, result.Containers)
		}
	}
}

func TestStackDownRemovesTheContainers(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)
	deployStack(t, env, id)

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/stacks/"+id+"/down", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var result struct {
		Containers []string `json:"containers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(result.Containers) != 1 {
		t.Errorf("removed = %v, want the stack's container", result.Containers)
	}
}

func TestStackUpStreamsItsProgress(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)

	body := readStream(t, env, APIPrefix+"/stacks/"+id+"/up?ticket="+env.issueTicket(t, store.RoleAdmin))
	if !strings.Contains(body, "shop-app-1 started") {
		t.Errorf("stream =\n%s\nwant the container's start", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("stream =\n%s\nwant a done event", body)
	}
}

// A deploy that cannot be attempted must say which service and which field,
// not just that it failed.
func TestStackUpStreamsARefusalWithItsProblems(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "leak",
		"services:\n  app:\n    image: alpine\n    volumes:\n      - /etc:/x\n")

	body := readStream(t, env, APIPrefix+"/stacks/"+id+"/up?ticket="+env.issueTicket(t, store.RoleAdmin))
	if !strings.Contains(body, "event: error") {
		t.Fatalf("stream =\n%s\nwant an error event", body)
	}
	if !strings.Contains(body, "problems") || !strings.Contains(body, "volumes") {
		t.Errorf("stream =\n%s\nwant the problem's service and field", body)
	}
}

func TestStackStreamsNeedATicket(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)

	for _, path := range []string{
		APIPrefix + "/stacks/" + id + "/up",
		APIPrefix + "/stacks/" + id + "/pull",
		APIPrefix + "/stacks/" + id + "/scale?service=app&replicas=2",
	} {
		rec := request(t, env.anonymous(), http.MethodGet, path)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a ticket: status = %d, want 401", path, rec.Code)
		}
	}
}

func TestStackScaleNeedsItsParameters(t *testing.T) {
	env := newEnv(t, fake.New())
	id := createStack(t, env, "shop", stackCompose)

	rec := request(t, env.anonymous(), http.MethodGet,
		APIPrefix+"/stacks/"+id+"/scale?ticket="+env.issueTicket(t, store.RoleAdmin))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a missing ?service (%s)", rec.Code, rec.Body.String())
	}
}

func TestStackNotFoundIsA404(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, path := range []string{
		APIPrefix + "/stacks/nope",
		APIPrefix + "/stacks/nope/up?ticket=" + env.issueTicket(t, store.RoleAdmin),
	} {
		rec := request(t, env.as(store.RoleAdmin), http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestStackDiscoveryListsProjectsStartedElsewhere(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/stacks/discovered")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if _, total := listOf(t, rec); total != 0 {
		t.Errorf("discovered = %d, want none from the fixture", total)
	}
}

// deployStack runs a stack's SSE deploy to completion.
func deployStack(t *testing.T, env *testEnv, id string) {
	t.Helper()

	body := readStream(t, env, APIPrefix+"/stacks/"+id+"/up?ticket="+env.issueTicket(t, store.RoleAdmin))
	if !strings.Contains(body, "event: done") {
		t.Fatalf("deploy did not finish:\n%s", body)
	}
}

// readStream runs an SSE handler to completion and returns everything it
// wrote. The stack streams end when the work does, so no cancellation is
// needed — a hang here is a bug worth failing on rather than papering over.
func readStream(t *testing.T, env *testEnv, path string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.raw.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the stream handler did not return")
	}
	return rec.Body.String()
}
