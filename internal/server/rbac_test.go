package server

import (
	"net/http"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/store"
)

// TestRBACMatrix walks every protected route against every role.
//
// This is the security-critical table of the whole daemon: a mistake here
// hands container control to an account that should only be able to look.
func TestRBACMatrix(t *testing.T) {
	type route struct {
		method string
		path   string
		// allowed lists the roles that must be able to call this route.
		allowed []store.Role
	}

	viewer, operator, admin := store.RoleViewer, store.RoleOperator, store.RoleAdmin
	readers := []store.Role{viewer, operator, admin}
	operators := []store.Role{operator, admin}

	routes := []route{
		// Reads: open to every role.
		{http.MethodGet, "/containers", readers},
		{http.MethodGet, "/containers/" + runningID, readers},
		{http.MethodGet, "/containers/" + runningID + "/inspect", readers},
		{http.MethodGet, "/images", readers},
		{http.MethodGet, "/volumes", readers},
		{http.MethodGet, "/networks", readers},
		{http.MethodGet, "/system/ping", readers},
		{http.MethodGet, "/system/info", readers},
		{http.MethodGet, "/system/df", readers},
		{http.MethodGet, "/system/host", readers},
		{http.MethodGet, "/auth/me", readers},

		// Lifecycle: operator and admin only.
		{http.MethodPost, "/containers/" + runningID + "/start", operators},
		{http.MethodPost, "/containers/" + runningID + "/stop", operators},
		{http.MethodPost, "/containers/" + runningID + "/restart", operators},
		{http.MethodDelete, "/containers/" + stoppedID, operators},
	}

	// One environment for the whole table. Building a fresh one per case would
	// re-derive an argon2id hash 40-odd times and dominate the suite's runtime;
	// only DELETE removes a resource, so it gets its own environment below.
	shared := newEnv(t, fake.New())

	for _, rt := range routes {
		for _, role := range []store.Role{viewer, operator, admin} {
			name := string(role) + " " + rt.method + " " + rt.path
			t.Run(name, func(t *testing.T) {
				env := shared
				if rt.method == http.MethodDelete {
					env = newEnv(t, fake.New())
				}
				rec := request(t, env.as(role), rt.method, APIPrefix+rt.path)

				if allowedFor(rt.allowed, role) {
					if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
						t.Errorf("status = %d, want %s to be allowed", rec.Code, role)
					}
					return
				}

				if rec.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want 403 for %s", rec.Code, role)
				}
				code, msg := errorOf(t, rec)
				if code != string(httpx.CodeForbidden) {
					t.Errorf("code = %q, want %q", code, httpx.CodeForbidden)
				}
				if msg == "" {
					t.Error("the refusal carries no explanation")
				}
			})
		}
	}
}

func allowedFor(allowed []store.Role, role store.Role) bool {
	for _, r := range allowed {
		if r == role {
			return true
		}
	}
	return false
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.anonymous()

	protected := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/containers"},
		{http.MethodGet, "/images"},
		{http.MethodGet, "/volumes"},
		{http.MethodGet, "/networks"},
		{http.MethodGet, "/system/info"},
		{http.MethodGet, "/auth/me"},
		{http.MethodPost, "/containers/" + runningID + "/start"},
		{http.MethodDelete, "/containers/" + runningID},
	}

	for _, p := range protected {
		rec := request(t, h, p.method, APIPrefix+p.path)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", p.method, p.path, rec.Code)
			continue
		}
		if code, _ := errorOf(t, rec); code != string(httpx.CodeUnauthorized) {
			t.Errorf("%s %s: code = %q", p.method, p.path, code)
		}
	}
}

func TestHealthAndVersionStayOpen(t *testing.T) {
	// A watchdog must be able to poll the daemon without a credential.
	env := newEnv(t, fake.New())

	for _, path := range []string{"/health", "/version"} {
		if rec := request(t, env.anonymous(), http.MethodGet, APIPrefix+path); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 without authentication", path, rec.Code)
		}
	}
}

func TestInvalidTokenIsRejectedRatherThanIgnored(t *testing.T) {
	env := newEnv(t, fake.New())

	// A garbage credential must not silently downgrade to "anonymous" — that
	// would turn a clear 401 into a confusing 403 or a public read.
	rec := request(t, env.withToken("not-a-real-token"), http.MethodGet, APIPrefix+"/containers")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRoleHasMatchesTheDocumentedMatrix(t *testing.T) {
	type want struct {
		role  store.Role
		perm  middleware.Permission
		allow bool
	}

	cases := []want{
		{store.RoleViewer, middleware.PermRead, true},
		{store.RoleViewer, middleware.PermOperate, false},
		{store.RoleViewer, middleware.PermCreate, false},
		{store.RoleViewer, middleware.PermDelete, false},
		{store.RoleViewer, middleware.PermBuild, false},
		{store.RoleViewer, middleware.PermPrune, false},
		{store.RoleViewer, middleware.PermPrivileged, false},
		{store.RoleViewer, middleware.PermAdmin, false},

		{store.RoleOperator, middleware.PermRead, true},
		{store.RoleOperator, middleware.PermOperate, true},
		{store.RoleOperator, middleware.PermCreate, true},
		{store.RoleOperator, middleware.PermDelete, true},
		{store.RoleOperator, middleware.PermBuild, false},
		{store.RoleOperator, middleware.PermPrune, false},
		{store.RoleOperator, middleware.PermPrivileged, false},
		{store.RoleOperator, middleware.PermAdmin, false},

		{store.RoleAdmin, middleware.PermRead, true},
		{store.RoleAdmin, middleware.PermOperate, true},
		{store.RoleAdmin, middleware.PermCreate, true},
		{store.RoleAdmin, middleware.PermDelete, true},
		{store.RoleAdmin, middleware.PermBuild, true},
		{store.RoleAdmin, middleware.PermPrune, true},
		{store.RoleAdmin, middleware.PermPrivileged, true},
		{store.RoleAdmin, middleware.PermAdmin, true},
	}

	for _, c := range cases {
		if got := middleware.RoleHas(c.role, c.perm); got != c.allow {
			t.Errorf("RoleHas(%q, %q) = %v, want %v", c.role, c.perm, got, c.allow)
		}
	}
}

func TestUnknownRoleHasNoPermissions(t *testing.T) {
	// A corrupted or future role value must fail closed.
	for _, perm := range []middleware.Permission{
		middleware.PermRead, middleware.PermOperate, middleware.PermAdmin,
	} {
		if middleware.RoleHas("superuser", perm) {
			t.Errorf("an unknown role was granted %q", perm)
		}
		if middleware.RoleHas("", perm) {
			t.Errorf("the empty role was granted %q", perm)
		}
	}
}

func TestPermissionsOfIsStableAndOrdered(t *testing.T) {
	first := middleware.PermissionsOf(store.RoleOperator)
	second := middleware.PermissionsOf(store.RoleOperator)

	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			// A map-ordered result would make the UI reorder on every request.
			t.Fatalf("order is not stable: %v vs %v", first, second)
		}
	}
	if len(first) != 4 {
		t.Errorf("operator has %d permissions, want 4: %v", len(first), first)
	}
}
