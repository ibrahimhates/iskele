package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

func TestCatalogListsEveryTemplateWithItsCategories(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/templates")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Items []struct {
			ID              string `json:"id"`
			Title           string `json:"title"`
			Category        string `json:"category"`
			NeedsPrivileged bool   `json:"needs_privileged"`
			Deployed        int    `json:"deployed"`
		} `json:"items"`
		Total      int      `json:"total"`
		Categories []string `json:"categories"`
		Problems   []any    `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}

	if body.Total != 20 || len(body.Items) != 20 {
		t.Fatalf("catalog = %d templates, want 20", body.Total)
	}
	if len(body.Categories) < 3 {
		t.Errorf("categories = %v, want the three the catalog uses", body.Categories)
	}
	if len(body.Problems) != 0 {
		t.Errorf("problems = %v, want none from the shipped catalog", body.Problems)
	}

	// The summary fields have to survive the embedded template's encoding.
	for _, item := range body.Items {
		if item.ID == "" || item.Title == "" || item.Category == "" {
			t.Fatalf("item = %+v, want the template's own fields", item)
		}
		if item.ID == "wg-easy" && !item.NeedsPrivileged {
			t.Error("wg-easy needs NET_ADMIN; the catalog should say so in advance")
		}
	}
}

func TestCatalogReadsOneTemplateWithItsFields(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/templates/postgres")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var template struct {
		ID     string `json:"id"`
		Fields []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Generate bool   `json:"generate"`
			Default  string `json:"default"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &template); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if template.ID != "postgres" || len(template.Fields) == 0 {
		t.Fatalf("template = %+v, want postgres with its questions", template)
	}

	for _, field := range template.Fields {
		if field.Type == "password" {
			if field.Default != "" {
				t.Errorf("field %s ships a default password", field.Name)
			}
			if !field.Generate {
				t.Errorf("field %s should offer to generate a value", field.Name)
			}
		}
	}
}

func TestCatalogUnknownTemplateIs404(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/templates/nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCatalogDeployCreatesTheContainer(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost,
		APIPrefix+"/templates/redis/deploy", map[string]any{
			"name": "cache",
			"values": map[string]string{
				"password": "a-generated-secret-value",
				"port":     "6380",
			},
		})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var result struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Image    string `json:"image"`
		Template string `json:"template"`
		Notes    string `json:"notes"`
		Started  bool   `json:"started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}

	if result.Name != "cache" || result.Template != "redis" {
		t.Errorf("result = %+v, want the named container from the redis template", result)
	}
	if strings.Contains(result.Image, "{{") {
		t.Errorf("image = %q, still holds a placeholder", result.Image)
	}
	// The notes are the point of the after-deploy screen, and they carry the
	// operator's own answers.
	if !strings.Contains(result.Notes, "6380") {
		t.Errorf("notes = %q, want the port the operator chose", result.Notes)
	}
}

// A form with three mistakes should come back with three, not the first.
func TestCatalogDeployReportsEveryBadAnswer(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost,
		APIPrefix+"/templates/postgres/deploy", map[string]any{
			"values": map[string]string{"port": "99999", "username": "1-bad-name"},
		})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Fields []struct {
					Field   string `json:"field"`
					Message string `json:"message"`
				} `json:"fields"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Error.Code != string(httpx.CodeValidationFailed) {
		t.Errorf("code = %q, want VALIDATION_FAILED", body.Error.Code)
	}
	if len(body.Error.Details.Fields) < 3 {
		t.Errorf("fields = %+v, want the missing password and both bad answers",
			body.Error.Details.Fields)
	}
}

// A catalog entry is not a way around the privileged gate.
func TestCatalogDeployRefusesPrivilegedTemplatesWithoutThePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	values := map[string]string{
		"host":          "vpn.example.com",
		"password_hash": "$2y$10$examplehashvalue",
	}

	rec := send(t, env.as(store.RoleOperator), http.MethodPost,
		APIPrefix+"/templates/wg-easy/deploy", map[string]any{"name": "vpn", "values": values})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("as operator: status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if _, message := errorOf(t, rec); !strings.Contains(message, "cap_add") {
		t.Errorf("message = %q, want it to name the option that stopped it", message)
	}

	rec = send(t, env.as(store.RoleAdmin), http.MethodPost,
		APIPrefix+"/templates/wg-easy/deploy", map[string]any{"name": "vpn", "values": values})
	if rec.Code != http.StatusCreated {
		t.Fatalf("as admin: status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
}

// A template's bind mount goes through the same whitelist as any other.
func TestCatalogDeployRefusesAPathOutsideTheWhitelist(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleAdmin), http.MethodPost,
		APIPrefix+"/templates/nginx/deploy", map[string]any{
			"name":   "web",
			"values": map[string]string{"site_dir": "/etc"},
		})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodePathNotAllowed) {
		t.Errorf("code = %q, want PATH_NOT_ALLOWED", code)
	}
}

func TestCatalogDeployNeedsTheCreatePermission(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := send(t, env.as(store.RoleViewer), http.MethodPost,
		APIPrefix+"/templates/redis/deploy", map[string]any{
			"values": map[string]string{"password": "a-generated-secret-value"},
		})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestGeneratedSecretsAreLongAndDifferent(t *testing.T) {
	env := newEnv(t, fake.New())

	secret := func(query string) string {
		t.Helper()
		rec := send(t, env.as(store.RoleViewer), http.MethodPost,
			APIPrefix+"/templates/secret"+query, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
		}
		var body struct {
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response is not JSON: %v", err)
		}
		return body.Secret
	}

	first, second := secret(""), secret("")
	if len(first) != 32 {
		t.Errorf("length = %d, want the 32-character default", len(first))
	}
	if first == second {
		t.Error("two generated secrets are identical")
	}

	if got := secret("?length=64"); len(got) != 64 {
		t.Errorf("length = %d, want 64", len(got))
	}
	// A length nobody should get is clamped rather than honored.
	if got := secret("?length=4"); len(got) < 12 {
		t.Errorf("length = %d, want a short request clamped up", len(got))
	}
}
