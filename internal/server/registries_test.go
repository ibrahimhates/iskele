package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/store"
)

func createRegistry(t *testing.T, env *testEnv, body map[string]any) map[string]any {
	t.Helper()

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/registries", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return created
}

// The password is the whole point of the table; it must never come back out.
func TestRegistryPasswordIsNeverReturned(t *testing.T) {
	env := newEnv(t, fake.New())

	created := createRegistry(t, env, map[string]any{
		"name":     "ghcr",
		"server":   "ghcr.io",
		"username": "deploy",
		"password": "super-secret-token",
	})

	if _, present := created["password"]; present {
		t.Error("the create response carries a password field")
	}
	if created["has_password"] != true {
		t.Errorf("has_password = %v, want true", created["has_password"])
	}

	list := request(t, env.as(store.RoleAdmin), http.MethodGet, APIPrefix+"/registries")
	if strings.Contains(list.Body.String(), "super-secret-token") {
		t.Fatal("the listing leaked the stored password")
	}
}

// What lands in the database has to be ciphertext, not the password with extra
// steps; this reads the row directly rather than trusting the API surface.
func TestRegistryPasswordIsEncryptedAtRest(t *testing.T) {
	env := newEnv(t, fake.New())

	createRegistry(t, env, map[string]any{
		"name":     "ghcr",
		"server":   "ghcr.io",
		"username": "deploy",
		"password": "super-secret-token",
	})

	rows, err := env.db.Registries.List(context.Background())
	if err != nil {
		t.Fatalf("Registries.List() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Password == "" {
		t.Fatal("nothing was stored")
	}
	if strings.Contains(rows[0].Password, "super-secret-token") {
		t.Error("the password is stored in the clear")
	}
}

// A zero time.Time serializes as year 1, which a UI renders as a date two
// thousand years ago rather than as "never".
func TestRegistryResponseOmitsANeverUsedTimestamp(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/registries",
		map[string]any{"name": "ghcr", "server": "ghcr.io"})

	if strings.Contains(rec.Body.String(), "0001-01-01") {
		t.Errorf("body = %s, want the unused timestamp omitted", rec.Body.String())
	}

	list := request(t, env.as(store.RoleAdmin), http.MethodGet, APIPrefix+"/registries")
	if strings.Contains(list.Body.String(), "0001-01-01") {
		t.Errorf("listing = %s, want the unused timestamp omitted", list.Body.String())
	}
}

// The create response has to carry what was actually written; a client that
// renders it should not have to re-fetch to learn when the row was made.
func TestRegistryCreateResponseCarriesItsTimestamps(t *testing.T) {
	env := newEnv(t, fake.New())

	created := createRegistry(t, env, map[string]any{"name": "ghcr", "server": "ghcr.io"})

	createdAt, _ := created["created_at"].(string)
	if createdAt == "" || strings.HasPrefix(createdAt, "0001-") {
		t.Errorf("created_at = %q", createdAt)
	}
}

func TestRegistryServerIsNormalized(t *testing.T) {
	env := newEnv(t, fake.New())

	created := createRegistry(t, env, map[string]any{
		"name":     "hub",
		"server":   "https://index.docker.io/",
		"username": "user",
		"password": "pw",
	})

	if created["server"] != "docker.io" {
		t.Errorf("server = %v, want the canonical docker.io", created["server"])
	}
}

func TestTwoRegistriesForOneServerIsAConflict(t *testing.T) {
	env := newEnv(t, fake.New())

	createRegistry(t, env, map[string]any{"name": "a", "server": "ghcr.io"})

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/registries",
		map[string]any{"name": "b", "server": "ghcr.io"})
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

// A blank password on an edit means "keep the stored one": the UI is never
// given the value, so it cannot send it back.
func TestUpdatingWithoutAPasswordKeepsTheStoredOne(t *testing.T) {
	env := newEnv(t, fake.New())

	created := createRegistry(t, env, map[string]any{
		"name": "ghcr", "server": "ghcr.io", "username": "deploy", "password": "keep-me",
	})
	id, _ := created["id"].(string)

	before, err := env.db.Registries.ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}

	rec := send(t, env.as(store.RoleAdmin), http.MethodPut, APIPrefix+"/registries/"+id,
		map[string]any{"name": "ghcr renamed", "server": "ghcr.io", "username": "deploy"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	after, err := env.db.Registries.ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if after.Password != before.Password {
		t.Error("an edit without a password field erased the credential")
	}
	if after.Name != "ghcr renamed" {
		t.Errorf("name = %q, want it updated", after.Name)
	}
}

func TestRegistryValidation(t *testing.T) {
	env := newEnv(t, fake.New())

	cases := map[string]map[string]any{
		"no name":                 {"server": "ghcr.io"},
		"no server":               {"name": "x"},
		"server with a path":      {"name": "x", "server": "ghcr.io/org"},
		"password without a user": {"name": "x", "server": "ghcr.io", "password": "pw"},
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/registries", body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// Registry credentials reach outside this host, which is why they are the one
// resource operators cannot touch.
func TestRegistriesAreAdminOnly(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, role := range []store.Role{store.RoleViewer, store.RoleOperator} {
		t.Run(string(role), func(t *testing.T) {
			if rec := request(t, env.as(role), http.MethodGet, APIPrefix+"/registries"); rec.Code != http.StatusForbidden {
				t.Errorf("list status = %d, want 403", rec.Code)
			}
			rec := send(t, env.as(role), http.MethodPost, APIPrefix+"/registries",
				map[string]any{"name": "x", "server": "ghcr.io"})
			if rec.Code != http.StatusForbidden {
				t.Errorf("create status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestRegistryDelete(t *testing.T) {
	env := newEnv(t, fake.New())

	created := createRegistry(t, env, map[string]any{"name": "ghcr", "server": "ghcr.io"})
	id, _ := created["id"].(string)

	rec := request(t, env.as(store.RoleAdmin), http.MethodDelete, APIPrefix+"/registries/"+id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body.String())
	}

	again := request(t, env.as(store.RoleAdmin), http.MethodDelete, APIPrefix+"/registries/"+id)
	if again.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", again.Code)
	}
}

// The audit trail records the change but never the credential itself.
func TestRegistryChangesAreAuditedWithoutTheSecret(t *testing.T) {
	env := newEnv(t, fake.New())

	createRegistry(t, env, map[string]any{
		"name": "ghcr", "server": "ghcr.io", "username": "deploy", "password": "super-secret-token",
	})

	entries, err := env.db.Audit.List(context.Background(), store.AuditFilter{Limit: 20})
	if err != nil {
		t.Fatalf("Audit.List() error = %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Action != "registry.create" {
			continue
		}
		found = true
		if strings.Contains(e.Detail, "super-secret-token") {
			t.Error("the audit entry carries the password")
		}
		if !strings.Contains(e.Detail, "ghcr.io") {
			t.Errorf("detail = %q, want the server recorded", e.Detail)
		}
	}
	if !found {
		t.Error("no registry.create audit entry")
	}
}
