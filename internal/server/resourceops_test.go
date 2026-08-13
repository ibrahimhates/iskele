package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
)

const fakeImageID = "sha256:img1"

func TestImageRemoveReportsWhatWent(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleAdmin), http.MethodDelete,
		APIPrefix+"/images/"+fakeImageID+"?force=true")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Deleted []map[string]string `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(body.Deleted) == 0 {
		t.Error("nothing was reported as deleted")
	}
}

func TestImageRemoveNeedsTheDeletePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodDelete, APIPrefix+"/images/"+fakeImageID)
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want 403", rec.Code)
	}
}

func TestImagePruneNeedsThePrunePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	if rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/images/prune", nil); rec.Code != http.StatusForbidden {
		t.Errorf("operator status = %d, want 403: pruning is admin-only", rec.Code)
	}
	if rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/images/prune", nil); rec.Code != http.StatusOK {
		t.Errorf("admin status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestImageTagAddsAReference(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost,
		APIPrefix+"/images/"+fakeImageID+"/tag", map[string]string{"tag": "nginx:pinned"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	list := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/images")
	if !strings.Contains(list.Body.String(), "nginx:pinned") {
		t.Error("the new tag is not in the listing")
	}
}

func TestImageHistoryAndInspect(t *testing.T) {
	env := newEnv(t, fake.New())

	history := request(t, env.as(store.RoleViewer), http.MethodGet,
		APIPrefix+"/images/"+fakeImageID+"/history")
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d (%s)", history.Code, history.Body.String())
	}
	if _, total := listOf(t, history); total == 0 {
		t.Error("history is empty")
	}

	inspect := request(t, env.as(store.RoleViewer), http.MethodGet,
		APIPrefix+"/images/"+fakeImageID+"/inspect")
	if inspect.Code != http.StatusOK {
		t.Fatalf("inspect status = %d", inspect.Code)
	}
	if !strings.Contains(inspect.Body.String(), fakeImageID) {
		t.Errorf("inspect body = %q", inspect.Body.String())
	}
}

func TestUnknownImageIsNotFound(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/images/missing/history")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeImageNotFound) {
		t.Errorf("code = %q, want %q", code, httpx.CodeImageNotFound)
	}
}

func TestVolumeLifecycle(t *testing.T) {
	env := newEnv(t, fake.New())

	created := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/volumes",
		map[string]any{"name": "pgdata", "labels": map[string]string{"app": "db"}})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", created.Code, created.Body.String())
	}

	got := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/volumes/pgdata")
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d (%s)", got.Code, got.Body.String())
	}

	removed := request(t, env.as(store.RoleAdmin), http.MethodDelete, APIPrefix+"/volumes/pgdata")
	if removed.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d (%s)", removed.Code, removed.Body.String())
	}

	gone := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/volumes/pgdata")
	if gone.Code != http.StatusNotFound {
		t.Errorf("status after delete = %d, want 404", gone.Code)
	}
}

func TestVolumeCreateNeedsTheCreatePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleViewer), http.MethodPost, APIPrefix+"/volumes",
		map[string]any{"name": "nope"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want 403", rec.Code)
	}
}

func TestNetworkLifecycle(t *testing.T) {
	env := newEnv(t, fake.New())

	created := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/networks",
		map[string]any{
			"name":   "backend",
			"driver": "bridge",
			"ipam":   []map[string]string{{"subnet": "172.30.0.0/16", "gateway": "172.30.0.1"}},
		})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", created.Code, created.Body.String())
	}

	var network struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IPAM []struct {
			Subnet string `json:"subnet"`
		} `json:"ipam"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &network); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if network.Name != "backend" {
		t.Errorf("name = %q", network.Name)
	}
	if len(network.IPAM) != 1 || network.IPAM[0].Subnet != "172.30.0.0/16" {
		t.Errorf("ipam = %+v, want the subnet carried through", network.IPAM)
	}

	connect := send(t, env.as(store.RoleOperator), http.MethodPost,
		APIPrefix+"/networks/backend/connect",
		map[string]any{"container": runningID, "aliases": []string{"api"}})
	if connect.Code != http.StatusOK {
		t.Fatalf("connect status = %d (%s)", connect.Code, connect.Body.String())
	}

	// A network with a container on it must not be removable.
	blocked := request(t, env.as(store.RoleAdmin), http.MethodDelete, APIPrefix+"/networks/backend")
	if blocked.Code != http.StatusConflict {
		t.Errorf("delete-while-attached status = %d, want 409 (%s)", blocked.Code, blocked.Body.String())
	}

	disconnect := send(t, env.as(store.RoleOperator), http.MethodPost,
		APIPrefix+"/networks/backend/disconnect", map[string]any{"container": runningID})
	if disconnect.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d (%s)", disconnect.Code, disconnect.Body.String())
	}

	removed := request(t, env.as(store.RoleAdmin), http.MethodDelete, APIPrefix+"/networks/backend")
	if removed.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204 (%s)", removed.Code, removed.Body.String())
	}
}

func TestDuplicateNetworkNameIsAConflict(t *testing.T) {
	env := newEnv(t, fake.New())

	body := map[string]any{"name": "backend"}
	if rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/networks", body); rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", rec.Code)
	}

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/networks", body)
	if rec.Code != http.StatusConflict {
		t.Errorf("second create status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

// Prune never touches the engine's own predefined networks.
func TestNetworkPruneKeepsThePredefinedNetworks(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/networks/prune", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	list := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/networks")
	if !strings.Contains(list.Body.String(), `"bridge"`) {
		t.Error("the default bridge network was pruned")
	}
}

func TestResourceMutationsAreAudited(t *testing.T) {
	env := newEnv(t, fake.New())

	send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/volumes",
		map[string]any{"name": "audited"})
	send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/networks",
		map[string]any{"name": "audited-net"})

	entries, err := env.db.Audit.List(context.Background(), store.AuditFilter{Limit: 50})
	if err != nil {
		t.Fatalf("Audit.List() error = %v", err)
	}

	want := map[string]bool{"volume.create": false, "network.create": false}
	for _, e := range entries {
		if _, tracked := want[e.Action]; tracked {
			want[e.Action] = true
		}
	}
	for action, found := range want {
		if !found {
			t.Errorf("no audit entry for %s", action)
		}
	}
}

func TestImagePullStreamsProgressAndRegistersATask(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleOperator)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/images/pull?ref=nginx:1.27&ticket="+ticket, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(body, "event: task") {
		t.Error("the stream did not announce its task id")
	}
	if !strings.Contains(body, "event: progress") {
		t.Errorf("body = %q, want progress events", body)
	}
	if !strings.Contains(body, `"percent"`) {
		t.Error("progress events carry no overall percentage")
	}
	if !strings.Contains(body, "event: done") {
		t.Error("the stream did not finish cleanly")
	}
}

func TestPullNeedsTheOperatePermission(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleViewer)

	code := requestStream(t, env.raw, APIPrefix+"/images/pull?ref=nginx&ticket="+ticket)
	if code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want 403", code)
	}
}

func TestPullWithoutAReferenceIsABadRequest(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleOperator)

	code := requestStream(t, env.raw, APIPrefix+"/images/pull?ticket="+ticket)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// A failure the engine reports inside a 200 response has to reach the client
// as an error event, because the status line already said everything was fine.
func TestPullFailureBecomesAnErrorEvent(t *testing.T) {
	f := fake.New()
	f.SetPullEvents([]docker.PullEvent{
		{Status: "Pulling from library/nope"},
		{Error: "manifest for nope:latest not found"},
	})
	env := newEnv(t, f)
	ticket := env.issueTicket(t, store.RoleOperator)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/images/pull?ref=nope&ticket="+ticket, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "event: error") {
		t.Fatalf("body = %q, want an error event", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "manifest for nope") {
		t.Error("the engine's own message was not passed through")
	}
}

func TestTasksAreListedAfterAPull(t *testing.T) {
	env := newEnv(t, fake.New())
	ticket := env.issueTicket(t, store.RoleOperator)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/images/pull?ref=nginx:1.27&ticket="+ticket, nil).WithContext(ctx)
	env.raw.ServeHTTP(httptest.NewRecorder(), req)

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/tasks")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	items, total := listOf(t, rec)
	if total == 0 {
		t.Fatal("the pull left no task behind")
	}
	if items[0]["kind"] != "image.pull" || items[0]["target"] != "nginx:1.27" {
		t.Errorf("task = %+v", items[0])
	}
	if items[0]["state"] != string(service.TaskSucceeded) {
		t.Errorf("state = %v, want the finished pull to have succeeded", items[0]["state"])
	}
}

func TestCancelingAnUnknownTaskIs404(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/tasks/nope/cancel", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}
