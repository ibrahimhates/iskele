package server

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/store"
)

// auditPage is the body of GET /audit.
type auditPage struct {
	Items []struct {
		ID           int64  `json:"id"`
		Username     string `json:"username"`
		Action       string `json:"action"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		Result       string `json:"result"`
		Detail       string `json:"detail"`
		CreatedAt    string `json:"created_at"`
	} `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func auditOf(t *testing.T, h http.Handler, query string) auditPage {
	t.Helper()

	rec := request(t, h, http.MethodGet, APIPrefix+"/audit"+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audit%s = %d: %s", query, rec.Code, rec.Body)
	}

	var page auditPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	return page
}

// The trail is written by the actions themselves, so the fixtures here are
// real operations rather than rows inserted behind the API's back.
func TestAuditRecordsWhatTheAPIDid(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	if rec := request(t, admin, http.MethodPost,
		APIPrefix+"/containers/"+runningID+"/stop"); rec.Code != http.StatusOK {
		t.Fatalf("stop = %d", rec.Code)
	}

	page := auditOf(t, admin, "")
	if page.Total == 0 {
		t.Fatal("the trail is empty after a bootstrap, three logins and a stop")
	}

	var sawStop, sawBootstrap bool
	for _, entry := range page.Items {
		switch entry.Action {
		case "container.stop":
			sawStop = true
			if entry.Username != "admin" || entry.ResourceID != runningID {
				t.Errorf("stop entry = %+v", entry)
			}
		case "auth.bootstrap":
			sawBootstrap = true
		}
	}
	if !sawStop {
		t.Error("the stop was not recorded")
	}
	if !sawBootstrap {
		t.Error("the bootstrap was not recorded")
	}

	// Newest first: the stop just happened, the bootstrap started the suite.
	if len(page.Items) > 1 && page.Items[0].ID < page.Items[1].ID {
		t.Error("entries are not newest first")
	}
}

func TestAuditFiltersNarrowTheTrail(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	for _, action := range []string{"stop", "start", "restart"} {
		if rec := request(t, admin, http.MethodPost,
			APIPrefix+"/containers/"+runningID+"/"+action); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", action, rec.Code)
		}
	}

	byAction := auditOf(t, admin, "?action=container.stop")
	if byAction.Total != 1 {
		t.Errorf("action filter matched %d entries, want 1", byAction.Total)
	}

	byType := auditOf(t, admin, "?resource_type=container")
	if byType.Total < 3 {
		t.Errorf("resource_type filter matched %d entries, want at least 3", byType.Total)
	}

	byActor := auditOf(t, admin, "?user_id="+env.userID(t, "admin"))
	if byActor.Total == 0 {
		t.Error("the actor filter matched nothing")
	}

	// A filter matching nothing is an empty page, not an error, and the array
	// must still be an array.
	empty := auditOf(t, admin, "?action=container.nonesuch")
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Errorf("unmatched filter returned %+v", empty)
	}
	if !strings.Contains(
		request(t, admin, http.MethodGet, APIPrefix+"/audit?action=nope").Body.String(),
		`"items":[]`,
	) {
		t.Error("an empty page returned null rather than []")
	}
}

func TestAuditFiltersByResult(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	// A failed action is recorded with its outcome, which is the half of the
	// trail an operator actually goes looking for.
	if rec := request(t, admin, http.MethodPost,
		APIPrefix+"/containers/does-not-exist/stop"); rec.Code == http.StatusOK {
		t.Fatalf("stopping a missing container succeeded: %d", rec.Code)
	}

	failures := auditOf(t, admin, "?result=error")
	if failures.Total == 0 {
		t.Fatal("the failed action was not recorded as a failure")
	}
	for _, entry := range failures.Items {
		if entry.Result != "error" {
			t.Errorf("entry %+v is not a failure", entry)
		}
	}
}

func TestAuditPagesWithATotal(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	for range 5 {
		request(t, admin, http.MethodPost, APIPrefix+"/containers/"+runningID+"/restart")
	}

	first := auditOf(t, admin, "?limit=2")
	if len(first.Items) != 2 {
		t.Fatalf("page holds %d entries, want 2", len(first.Items))
	}
	// The total counts every match, not the page: "2 of 2" and "2 of 40" tell
	// an operator very different things.
	if first.Total <= 2 {
		t.Fatalf("total = %d, want the whole result", first.Total)
	}

	second := auditOf(t, admin, "?limit=2&offset=2")
	if len(second.Items) != 2 {
		t.Fatalf("second page holds %d entries", len(second.Items))
	}
	if second.Total != first.Total {
		t.Errorf("total changed between pages: %d then %d", first.Total, second.Total)
	}
	if first.Items[0].ID == second.Items[0].ID {
		t.Error("the second page repeats the first")
	}
}

func TestAuditRefusesAMalformedFilter(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	cases := map[string]string{
		"bad date":       "?from=yesterday",
		"bad result":     "?result=maybe",
		"reversed range": "?from=2026-08-11&to=2026-08-01",
		"zero limit":     "?limit=0",
		"negative page":  "?offset=-1",
		"limit not int":  "?limit=lots",
	}

	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			rec := request(t, admin, http.MethodGet, APIPrefix+"/audit"+query)
			if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 4xx: %s", rec.Code, rec.Body)
			}
		})
	}
}

// A date picker sends a bare date; reading it as anything but midnight UTC
// would make the filter depend on the server's timezone.
func TestAuditAcceptsABareDate(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	if page := auditOf(t, admin, "?from=2000-01-01"); page.Total == 0 {
		t.Error("everything is after 2000 and nothing matched")
	}
	if page := auditOf(t, admin, "?from=2999-01-01"); page.Total != 0 {
		t.Errorf("%d entries were recorded in the future", page.Total)
	}
}

func TestAuditExportsCSV(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	rec := request(t, admin, http.MethodGet, APIPrefix+"/audit/export?format=csv")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q", ct)
	}
	// Without this the browser renders the file instead of saving it.
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("the export holds %d rows, want a header and some entries", len(rows))
	}
	if rows[0][0] != "id" || rows[0][1] != "created_at" || rows[0][2] != "username" {
		t.Errorf("header = %v", rows[0])
	}
	// Every row has the same width as the header, or a spreadsheet will
	// silently shift columns.
	for i, row := range rows {
		if len(row) != len(rows[0]) {
			t.Fatalf("row %d has %d columns, want %d", i, len(row), len(rows[0]))
		}
	}
}

func TestAuditExportsJSON(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	rec := request(t, admin, http.MethodGet, APIPrefix+"/audit/export?format=json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var entries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("the export is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if len(entries) == 0 {
		t.Fatal("the export is empty")
	}
	if entries[0]["action"] == nil {
		t.Errorf("entry = %v", entries[0])
	}
}

// The export honors the same filter as the listing: an operator narrows the
// screen, then exports what they are looking at.
func TestAuditExportHonorsTheFilter(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	request(t, admin, http.MethodPost, APIPrefix+"/containers/"+runningID+"/stop")
	request(t, admin, http.MethodPost, APIPrefix+"/containers/"+runningID+"/start")

	rec := request(t, admin, http.MethodGet, APIPrefix+"/audit/export?format=json&action=container.stop")
	var entries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("the export is not valid JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the filtered export holds %d entries, want 1", len(entries))
	}
	if entries[0]["action"] != "container.stop" {
		t.Errorf("entry = %v", entries[0])
	}
}

func TestAuditExportRefusesAnUnknownFormat(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleAdmin), http.MethodGet, APIPrefix+"/audit/export?format=xml")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
	}
}

func TestAuditFacetsComeFromTheTrail(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	request(t, admin, http.MethodPost, APIPrefix+"/containers/"+runningID+"/stop")

	rec := request(t, admin, http.MethodGet, APIPrefix+"/audit/facets")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var facets struct {
		Actions       []string `json:"actions"`
		ResourceTypes []string `json:"resource_types"`
		Actors        []struct {
			UserID   string `json:"user_id"`
			Username string `json:"username"`
		} `json:"actors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &facets); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	if !contains(facets.Actions, "container.stop") {
		t.Errorf("actions = %v", facets.Actions)
	}
	if !contains(facets.ResourceTypes, "container") {
		t.Errorf("resource types = %v", facets.ResourceTypes)
	}
	var sawAdmin bool
	for _, actor := range facets.Actors {
		if actor.Username == "admin" {
			sawAdmin = true
		}
	}
	if !sawAdmin {
		t.Errorf("actors = %v", facets.Actors)
	}
}

func TestAuditIsAdminOnly(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, role := range []store.Role{store.RoleOperator, store.RoleViewer} {
		for _, path := range []string{"/audit", "/audit/facets", "/audit/export"} {
			if rec := request(t, env.as(role), http.MethodGet, APIPrefix+path); rec.Code != http.StatusForbidden {
				t.Errorf("%s reached %s: %d", role, path, rec.Code)
			}
		}
	}
}

// The trail is append-only over the API. There is no endpoint that edits or
// deletes a record, and there must not be: one an admin can rewrite is not an
// audit trail.
func TestAuditHasNoWriteEndpoints(t *testing.T) {
	env := newEnv(t, fake.New())
	admin := env.as(store.RoleAdmin)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := request(t, admin, method, APIPrefix+"/audit")
		if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Errorf("%s /audit = %d, want it unroutable", method, rec.Code)
		}
	}
	if rec := request(t, admin, http.MethodDelete, APIPrefix+"/audit/1"); rec.Code != http.StatusNotFound &&
		rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /audit/1 = %d, want it unroutable", rec.Code)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// toJSON renders a decoded body back to a stable string, for comparing two
// snapshots of the same structure.
func toJSON(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
