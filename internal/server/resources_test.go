package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

func TestListImages(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/images")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items, total := listOf(t, rec)
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	tags, ok := items[0]["repo_tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "nginx:1.27" {
		t.Errorf("repo_tags = %v", items[0]["repo_tags"])
	}
}

func TestListImagesDanglingIsTriState(t *testing.T) {
	f := fake.New()
	h := routerWith(t, f)

	// Absent: no filter at all.
	request(t, h, http.MethodGet, APIPrefix+"/images")
	opts, ok := f.CallsFor(fake.OpListImages)[0].Opts.(docker.ListImagesOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpListImages)[0].Opts)
	}
	if opts.Dangling != nil {
		t.Errorf("Dangling = %v, want nil when the parameter is absent", *opts.Dangling)
	}

	// Present and false: filter to non-dangling, which is not the same as absent.
	f.Reset()
	request(t, h, http.MethodGet, APIPrefix+"/images?dangling=false")
	opts, ok = f.CallsFor(fake.OpListImages)[0].Opts.(docker.ListImagesOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpListImages)[0].Opts)
	}
	if opts.Dangling == nil || *opts.Dangling {
		t.Errorf("Dangling = %v, want an explicit false", opts.Dangling)
	}
}

func TestListVolumes(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/volumes")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items, total := listOf(t, rec)
	if total != 1 || items[0]["name"] != "web-data" {
		t.Errorf("items = %v", items)
	}
}

func TestListNetworks(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/networks")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items, total := listOf(t, rec)
	if total != 1 || items[0]["name"] != "bridge" {
		t.Errorf("items = %v", items)
	}
}

func TestEmptyListsSerializeAsArrays(t *testing.T) {
	f := fake.New()
	f.Containers = nil
	f.Images = nil
	f.Volumes = nil
	f.Networks = nil
	h := routerWith(t, f)

	for _, path := range []string{"/containers", "/images", "/volumes", "/networks"} {
		rec := request(t, h, http.MethodGet, APIPrefix+path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		// A null items array would break every client that iterates it.
		if !strings.Contains(rec.Body.String(), `"items":[]`) {
			t.Errorf("%s: body = %q, want an empty array", path, rec.Body.String())
		}
	}
}

func TestSystemInfo(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/system/info")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var info map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if info["server_version"] != "28.5.2" {
		t.Errorf("server_version = %v", info["server_version"])
	}
}

func TestSystemDiskUsage(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/system/df")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var usage map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &usage); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	images, ok := usage["images"].(map[string]any)
	if !ok || images["count"] != float64(1) {
		t.Errorf("images = %v", usage["images"])
	}
}

func TestSystemHostReportsTheMachine(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/system/host")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var report map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	memory, ok := report["memory"].(map[string]any)
	if !ok || memory["total"] == float64(0) {
		t.Errorf("memory = %v", report["memory"])
	}
	daemon, ok := report["daemon"].(map[string]any)
	if !ok || daemon["go_version"] == "" {
		t.Errorf("daemon = %v", report["daemon"])
	}
	engine, ok := report["engine"].(map[string]any)
	if !ok || engine["version"] != "28.5.2" {
		t.Errorf("engine = %v", report["engine"])
	}
	// The data directory's filesystem is measured: it is the one the daemon
	// can run out of.
	disks, ok := report["disks"].([]any)
	if !ok || len(disks) == 0 {
		t.Errorf("disks = %v", report["disks"])
	}
}

func TestSystemHostSurvivesAnUnreachableEngine(t *testing.T) {
	f := fake.New()
	f.Fail(fake.OpInfo, docker.NewError(docker.KindUnavailable, "docker.info", "system", "",
		"cannot reach the Docker daemon"))

	rec := request(t, routerWith(t, f), http.MethodGet, APIPrefix+"/system/host")

	// Host metrics do not come from Docker, and this panel is most useful
	// precisely when Docker is down.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when the daemon is down", rec.Code)
	}
	var report map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, present := report["engine"]; present {
		t.Errorf("engine = %v, want it omitted", report["engine"])
	}
	if msg, _ := report["engine_error"].(string); !strings.Contains(msg, "cannot reach") {
		t.Errorf("engine_error = %v, want the engine's explanation", report["engine_error"])
	}
	if memory, _ := report["memory"].(map[string]any); memory == nil || memory["total"] == float64(0) {
		t.Errorf("memory = %v, want a reading", report["memory"])
	}
}

func TestSystemPingReportsReachableEngine(t *testing.T) {
	rec := request(t, routerWith(t, fake.New()), http.MethodGet, APIPrefix+"/system/ping")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if status["reachable"] != true {
		t.Errorf("reachable = %v", status["reachable"])
	}
}

func TestSystemPingReportsUnreachableEngineWithout5xx(t *testing.T) {
	f := fake.New()
	f.Fail(fake.OpPing, docker.NewError(docker.KindUnavailable, "docker.ping", "system", "",
		"cannot reach the Docker daemon"))

	rec := request(t, routerWith(t, f), http.MethodGet, APIPrefix+"/system/ping")

	// The UI polls this to drive its connection banner: an unreachable daemon
	// is the answer, not a failed request.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when the daemon is down", rec.Code)
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if status["reachable"] != false {
		t.Errorf("reachable = %v, want false", status["reachable"])
	}
	if msg, _ := status["error"].(string); !strings.Contains(msg, "cannot reach") {
		t.Errorf("error = %v, want the engine's explanation", status["error"])
	}
}

func TestDockerUnavailableIsReportedOnEveryEngineRoute(t *testing.T) {
	f := fake.New()
	boom := docker.NewError(docker.KindUnavailable, "op", "container", "",
		"cannot reach the Docker daemon at unix:///var/run/docker.sock")
	for _, op := range []string{
		fake.OpListContainers, fake.OpInspectContainer, fake.OpListImages,
		fake.OpListVolumes, fake.OpListNetworks, fake.OpInfo, fake.OpDiskUsage,
	} {
		f.Fail(op, boom)
	}
	h := routerWith(t, f)

	paths := []string{
		"/containers", "/containers/" + runningID, "/images",
		"/volumes", "/networks", "/system/info", "/system/df",
	}
	for _, path := range paths {
		rec := request(t, h, http.MethodGet, APIPrefix+path)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", path, rec.Code)
			continue
		}
		code, msg := errorOf(t, rec)
		if code != string(httpx.CodeDockerUnavailable) {
			t.Errorf("%s: code = %q, want %q", path, code, httpx.CodeDockerUnavailable)
		}
		if !strings.Contains(msg, "docker.sock") {
			t.Errorf("%s: message = %q, want the endpoint named", path, msg)
		}
	}
}

func TestRouterWithoutDockerServesUnavailable(t *testing.T) {
	// This is the startup path when the daemon was down: iskeled still serves,
	// and every Docker route explains why it cannot answer.
	h := newEnv(t, nil).as(store.RoleAdmin)

	if rec := request(t, h, http.MethodGet, APIPrefix+"/health"); rec.Code != http.StatusOK {
		t.Errorf("health status = %d, want the daemon to keep serving", rec.Code)
	}

	rec := request(t, h, http.MethodGet, APIPrefix+"/containers")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("containers status = %d, want 503", rec.Code)
	}
	code, msg := errorOf(t, rec)
	if code != string(httpx.CodeDockerUnavailable) {
		t.Errorf("code = %q", code)
	}
	if !strings.Contains(msg, "docker") || !strings.Contains(msg, "group") {
		t.Errorf("message = %q, want the actionable docker-group hint", msg)
	}
}

func TestUnknownEngineFailureBecomes502(t *testing.T) {
	f := fake.New()
	f.Fail(fake.OpListVolumes, docker.NewError(docker.KindUnknown, "volume.list", "volume", "",
		"the engine exploded"))

	rec := request(t, routerWith(t, f), http.MethodGet, APIPrefix+"/volumes")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeDockerError) {
		t.Errorf("code = %q, want %q", code, httpx.CodeDockerError)
	}
}

func TestNonEngineErrorsStayOpaque(t *testing.T) {
	f := fake.New()
	f.Fail(fake.OpListNetworks, errors.New("dial tcp 10.0.0.1: connection refused"))

	rec := request(t, routerWith(t, f), http.MethodGet, APIPrefix+"/networks")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	code, msg := errorOf(t, rec)
	if code != string(httpx.CodeInternal) {
		t.Errorf("code = %q", code)
	}
	if strings.Contains(msg, "10.0.0.1") {
		t.Error("internal error text leaked into the response")
	}
}
