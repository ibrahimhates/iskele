package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

// pruneReport is the body every prune endpoint returns.
type pruneReport struct {
	Deleted        []string `json:"deleted"`
	SpaceReclaimed int64    `json:"space_reclaimed"`
}

func pruneOf(t *testing.T, h http.Handler, path string) pruneReport {
	t.Helper()

	rec := request(t, h, http.MethodPost, APIPrefix+path)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body)
	}

	var report pruneReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	// A null array breaks every client that iterates it, and "nothing to
	// reclaim" is the common case for a prune.
	if report.Deleted == nil {
		t.Error("deleted is null rather than []")
	}
	return report
}

func TestPruneContainersRemovesOnlyStoppedOnes(t *testing.T) {
	env := newEnv(t, fake.New())

	before, _ := listOf(t, request(t, env.as(store.RoleAdmin), http.MethodGet,
		APIPrefix+"/containers?all=true"))
	if len(before) < 2 {
		t.Fatalf("the fixture has %d containers, want a running and a stopped one", len(before))
	}

	report := pruneOf(t, env.as(store.RoleAdmin), "/containers/prune")
	if len(report.Deleted) == 0 {
		t.Fatal("nothing was pruned, though the fixture has a stopped container")
	}

	after, _ := listOf(t, request(t, env.as(store.RoleAdmin), http.MethodGet,
		APIPrefix+"/containers?all=true"))
	for _, container := range after {
		if container["state"] != "running" && container["state"] != "paused" {
			t.Errorf("container %v survived the prune in state %v",
				container["name"], container["state"])
		}
	}
	// The running one is still there: a prune is not a stop.
	if len(after) == 0 {
		t.Error("the prune took the running container too")
	}
}

func TestPruneEndpointsNeedThePrunePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	paths := []string{
		"/containers/prune", "/images/prune", "/volumes/prune", "/networks/prune",
	}

	// Prune is admin-only: it reclaims space by deleting things nobody asked
	// about by name, which is not an operator's call to make.
	for _, role := range []store.Role{store.RoleOperator, store.RoleViewer} {
		for _, path := range paths {
			rec := request(t, env.as(role), http.MethodPost, APIPrefix+path)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s reached %s: %d", role, path, rec.Code)
			}
		}
	}

	for _, path := range paths {
		if rec := request(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+path); rec.Code != http.StatusOK {
			t.Errorf("admin was refused %s: %d", path, rec.Code)
		}
	}
}

func TestPruneReportsAnUnreachableEngine(t *testing.T) {
	f := fake.New()
	boom := docker.NewError(docker.KindUnavailable, "container.prune", "container", "",
		"cannot reach the Docker daemon")
	f.Fail(fake.OpPruneContainers, boom)

	env := newEnv(t, f)
	rec := request(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/containers/prune")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeDockerUnavailable) {
		t.Errorf("code = %q, want %q", code, httpx.CodeDockerUnavailable)
	}
}

// A prune deletes things nobody named, so the trail has to say who ran it and
// how much went.
func TestPruneIsAudited(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	pruneOf(t, admin, "/containers/prune")

	page := auditOf(t, admin, "?action=container.prune")
	if page.Total != 1 {
		t.Fatalf("the prune produced %d audit entries, want 1", page.Total)
	}
	entry := page.Items[0]
	if entry.Username != "admin" {
		t.Errorf("actor = %q", entry.Username)
	}
	if entry.Result != "ok" {
		t.Errorf("result = %q", entry.Result)
	}
	// The count is what makes the record useful a week later.
	if entry.Detail == "" || entry.Detail == "{}" {
		t.Errorf("detail = %q, want the number of objects removed", entry.Detail)
	}
}
