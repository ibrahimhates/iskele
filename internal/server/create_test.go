package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

// send issues a request with a JSON body.
func send(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// allowedPathOf reads the whitelist the test server was configured with, so a
// bind-mount test does not have to guess the temporary directory.
func allowedPathOf(t *testing.T, env *testEnv) string {
	t.Helper()

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/system/allowed-paths")
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed-paths status = %d", rec.Code)
	}

	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("allowed-paths body: %v", err)
	}
	if len(body.Paths) == 0 {
		t.Fatal("the test server has no allowed paths configured")
	}
	return body.Paths[0]
}

func TestCreateContainerReturnsTheNewID(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{
			"name":  "api",
			"image": "nginx:1.27",
			"env":   []map[string]string{{"key": "PORT", "value": "8080"}},
			"ports": []map[string]any{{"container_port": 80, "host_port": "8080"}},
		})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Image   string `json:"image"`
		Started bool   `json:"started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.ID == "" || body.Name != "api" || body.Image != "nginx:1.27" {
		t.Errorf("body = %+v", body)
	}
	if body.Started {
		t.Error("the container was started without being asked to")
	}
}

func TestCreateContainerCanStartIt(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{"name": "api", "image": "nginx:1.27", "start": true})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Started bool `json:"started"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Started {
		t.Error("start was requested but not reported")
	}
}

// The whitelist is the only thing between an operator account and the host
// filesystem, so this is the single most important test in the milestone.
func TestCreateRefusesABindMountOutsideTheWhitelist(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, path := range []string{"/", "/etc", "/var/run/docker.sock", "/root/.ssh"} {
		t.Run(path, func(t *testing.T) {
			rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
				map[string]any{
					"name":  "escape",
					"image": "alpine",
					"mounts": []map[string]any{
						{"type": "bind", "source": path, "destination": "/host"},
					},
				})

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
			}
			code, message := errorOf(t, rec)
			if code != string(httpx.CodePathNotAllowed) {
				t.Errorf("code = %q, want %q", code, httpx.CodePathNotAllowed)
			}
			if !strings.Contains(message, path) {
				t.Errorf("message %q does not name the refused path", message)
			}
		})
	}
}

// A refused mount must not leave a container behind.
func TestARefusedBindMountCreatesNothing(t *testing.T) {
	f := fake.New()
	env := newEnv(t, f)

	send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{
			"image":  "alpine",
			"mounts": []map[string]any{{"type": "bind", "source": "/etc", "destination": "/host"}},
		})

	if calls := f.CallsFor(fake.OpCreateContainer); len(calls) != 0 {
		t.Errorf("the engine was asked to create %d containers after a refusal", len(calls))
	}
}

func TestCreateAcceptsABindMountInsideTheWhitelist(t *testing.T) {
	env := newEnv(t, fake.New())
	root := allowedPathOf(t, env)

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{
			"name":  "data",
			"image": "alpine",
			"mounts": []map[string]any{
				{"type": "bind", "source": root + "/app", "destination": "/data", "read_only": true},
			},
		})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
}

// Named volumes and tmpfs never touch a host path, so the whitelist does not
// apply to them; refusing them would make the wizard useless.
func TestVolumeAndTmpfsMountsAreNotPathChecked(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{
			"name":  "db",
			"image": "postgres:16",
			"mounts": []map[string]any{
				{"type": "volume", "source": "pgdata", "destination": "/var/lib/postgresql/data"},
				{"type": "tmpfs", "destination": "/tmp", "tmpfs_size": 67108864},
			},
		})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
}

func TestPrivilegedOptionsNeedThePrivilegedPermission(t *testing.T) {
	cases := map[string]map[string]any{
		"privileged":   {"privileged": true},
		"cap_add":      {"cap_add": []string{"SYS_ADMIN"}},
		"devices":      {"devices": []string{"/dev/sda"}},
		"security_opt": {"security_opt": []string{"apparmor=unconfined"}},
		"sysctls":      {"sysctls": map[string]string{"net.ipv4.ip_forward": "1"}},
	}

	env := newEnv(t, fake.New())

	for name, security := range cases {
		t.Run(name, func(t *testing.T) {
			body := map[string]any{"name": "priv", "image": "alpine", "security": security}

			rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers", body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("operator status = %d, want 403 (%s)", rec.Code, rec.Body.String())
			}
			if _, message := errorOf(t, rec); !strings.Contains(message, name) {
				t.Errorf("message %q does not name the option that was refused", message)
			}

			// The same request from an admin goes through.
			rec = send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/containers", body)
			if rec.Code != http.StatusCreated {
				t.Errorf("admin status = %d, want 201 (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// host networking shares the host's loopback, which is where an unprotected
// service listens; it belongs behind the same gate as --privileged.
func TestHostNetworkingNeedsThePrivilegedPermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{"name": "hostnet", "image": "alpine", "network": map[string]any{"name": "host"}})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateRejectsAMalformedSpecWithTheField(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{
			"name":  "bad",
			"image": "alpine",
			"ports": []map[string]any{{"container_port": 70000}},
		})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not an error envelope: %v", err)
	}
	if body.Error.Details["field"] != "ports" {
		t.Errorf("details = %v, want the offending field", body.Error.Details)
	}
}

func TestCreateNeedsTheCreatePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleViewer), http.MethodPost, APIPrefix+"/containers",
		map[string]any{"name": "nope", "image": "alpine"})

	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want 403", rec.Code)
	}
}

func TestCreateIsAudited(t *testing.T) {
	env := newEnv(t, fake.New())

	send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{"name": "audited", "image": "nginx:1.27"})

	entries, err := env.db.Audit.List(context.Background(), store.AuditFilter{Limit: 20})
	if err != nil {
		t.Fatalf("Audit.List() error = %v", err)
	}

	for _, e := range entries {
		if e.Action == "container.create" && e.Result == store.ResultOK {
			return
		}
	}
	t.Errorf("no successful container.create entry in %d audit rows", len(entries))
}

// A refused request is exactly the one an operator will want to find later.
func TestARefusedCreateIsAudited(t *testing.T) {
	env := newEnv(t, fake.New())

	send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{
			"image":  "alpine",
			"mounts": []map[string]any{{"type": "bind", "source": "/etc", "destination": "/host"}},
		})

	entries, err := env.db.Audit.List(context.Background(), store.AuditFilter{Limit: 20})
	if err != nil {
		t.Fatalf("Audit.List() error = %v", err)
	}

	for _, e := range entries {
		if e.Action == "container.create" && e.Result == store.ResultError {
			return
		}
	}
	t.Error("a refused create left no audit trail")
}

// The wizard's path picker needs the whitelist; without it the operator finds
// out what is allowed only by being refused.
func TestAllowedPathsIsReadable(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/system/allowed-paths")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(body.Paths) != 1 {
		t.Errorf("paths = %v, want the one the test configured", body.Paths)
	}
}

func TestCreateSurfacesEngineFailures(t *testing.T) {
	f := fake.New()
	f.Fail(fake.OpCreateContainer, docker.NewError(docker.KindConflict, "container.create",
		"container", "api", "Conflict. The container name \"/api\" is already in use"))
	env := newEnv(t, f)

	rec := send(t, env.as(store.RoleOperator), http.MethodPost, APIPrefix+"/containers",
		map[string]any{"name": "api", "image": "nginx"})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if _, message := errorOf(t, rec); !strings.Contains(message, "already in use") {
		t.Errorf("message = %q, want the engine's own text", message)
	}
}
