package server

import (
	"net/http"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/store"
)

func settingsOf(t *testing.T, h http.Handler) map[string]any {
	t.Helper()

	rec := request(t, h, http.MethodGet, APIPrefix+"/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings = %d: %s", rec.Code, rec.Body)
	}
	return bodyOf(t, rec)
}

func TestSettingsReportTheInstallationAndTheDefaults(t *testing.T) {
	env := newEnv(t, fake.New())

	view := settingsOf(t, env.as(store.RoleAdmin))

	// Nobody has touched them yet, so these are the defaults: keep the trail
	// forever, and warn about bind mounts.
	if view["audit_retention_days"] != float64(0) {
		t.Errorf("audit_retention_days = %v, want 0", view["audit_retention_days"])
	}
	if view["bind_mount_warning"] != true {
		t.Errorf("bind_mount_warning = %v, want true", view["bind_mount_warning"])
	}

	installation, ok := view["installation"].(map[string]any)
	if !ok {
		t.Fatalf("installation = %v", view["installation"])
	}
	if installation["docker_host"] == "" {
		t.Error("the socket path is not reported")
	}
	// The whitelist is the answer to "how much of this host can the panel
	// reach", so the screen has to be able to show it.
	paths, ok := installation["allowed_paths"].([]any)
	if !ok || len(paths) == 0 {
		t.Errorf("allowed_paths = %v", installation["allowed_paths"])
	}
	if installation["access_ttl"] == float64(0) {
		t.Error("the session lifetime is not reported")
	}
}

func TestSettingsUpdateOnlyWhatWasSent(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/settings",
		map[string]any{"audit_retention_days": 90})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := bodyOf(t, rec)["audit_retention_days"]; got != float64(90) {
		t.Errorf("audit_retention_days = %v, want 90", got)
	}

	// The other setting was not in the body, so it is untouched.
	if got := settingsOf(t, h)["bind_mount_warning"]; got != true {
		t.Errorf("bind_mount_warning = %v after an unrelated update", got)
	}

	// And the change survives a re-read: it is in the database, not memory.
	if got := settingsOf(t, h)["audit_retention_days"]; got != float64(90) {
		t.Errorf("audit_retention_days = %v after re-reading", got)
	}
}

func TestSettingsRefuseAnImpossibleRetention(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	for name, days := range map[string]int{
		"negative": -1,
		"absurd":   100_000,
	} {
		t.Run(name, func(t *testing.T) {
			rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/settings",
				map[string]any{"audit_retention_days": days})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
			}
		})
	}

	// Zero is not absurd: it is how an operator says "keep everything".
	if rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/settings",
		map[string]any{"audit_retention_days": 0}); rec.Code != http.StatusOK {
		t.Fatalf("retention of 0 was refused: %d", rec.Code)
	}
}

// The socket path and the whitelist are startup-time security boundaries. An
// admin who could widen `allowed_paths` from a browser would be one request
// away from mounting the whole filesystem into a container.
func TestSettingsCannotChangeTheSecurityBoundaries(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	before := settingsOf(t, h)["installation"]

	rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/settings", map[string]any{
		"docker_host":   "tcp://evil.example:2375",
		"allowed_paths": []string{"/"},
		"data_dir":      "/",
		"installation":  map[string]any{"allowed_paths": []string{"/"}},
	})
	// The decoder refuses unknown fields outright, which is stronger than
	// ignoring them: a client that thinks it is setting the whitelist is told
	// it is not, rather than getting a 200 that did nothing.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}

	after := settingsOf(t, h)["installation"]
	if toJSON(t, before) != toJSON(t, after) {
		t.Fatalf("the installation changed:\nbefore %s\nafter  %s",
			toJSON(t, before), toJSON(t, after))
	}
}

func TestSettingsAreAdminOnly(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, role := range []store.Role{store.RoleOperator, store.RoleViewer} {
		if rec := request(t, env.as(role), http.MethodGet, APIPrefix+"/settings"); rec.Code != http.StatusForbidden {
			t.Errorf("%s read the settings: %d", role, rec.Code)
		}
		rec := sendJSON(t, env.as(role), http.MethodPut, APIPrefix+"/settings",
			map[string]any{"audit_retention_days": 1})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s wrote the settings: %d", role, rec.Code)
		}
	}
}

// Changing a setting is an administrative act like any other.
func TestSettingsUpdateIsAudited(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	sendJSON(t, h, http.MethodPut, APIPrefix+"/settings", map[string]any{"audit_retention_days": 30})

	page := auditOf(t, h, "?action=settings.update")
	if page.Total != 1 {
		t.Fatalf("the update produced %d audit entries, want 1", page.Total)
	}
	if page.Items[0].Username != "admin" {
		t.Errorf("actor = %q", page.Items[0].Username)
	}
	// The record has to say what changed, or it answers nothing later.
	if page.Items[0].Detail == "" || page.Items[0].Detail == "{}" {
		t.Errorf("detail = %q", page.Items[0].Detail)
	}
}
