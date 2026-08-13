package templates

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// load builds a catalog with no custom directory.
func load(t *testing.T) *Catalog {
	t.Helper()

	catalog, err := Load("", nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return catalog
}

// expected is the catalog the project commits to. Naming them here means a
// template that disappears in a refactor fails a test rather than a release.
var expected = []string{
	"redis", "postgres", "mysql", "mariadb", "mongodb",
	"cloudflared", "nginx", "caddy", "traefik", "portainer_agent",
	"uptime-kuma", "n8n", "vaultwarden", "minio", "rabbitmq",
	"adminer", "pgadmin", "watchtower", "gitea", "wg-easy",
}

func TestCatalogShipsEveryPromisedTemplate(t *testing.T) {
	catalog := load(t)

	for _, id := range expected {
		if _, err := catalog.Get(id); err != nil {
			t.Errorf("template %q is missing from the catalog", id)
		}
	}
	if catalog.Len() != len(expected) {
		t.Errorf("catalog holds %d templates, want %d", catalog.Len(), len(expected))
	}
}

// A shipped template that fails its own schema is a build-time bug, not
// something to discover when an operator clicks deploy.
func TestEveryBuiltinTemplateValidates(t *testing.T) {
	catalog := load(t)

	for _, template := range catalog.List() {
		if err := template.Validate(); err != nil {
			t.Errorf("%s: %v", template.ID, err)
		}
		if template.Source != SourceBuiltin {
			t.Errorf("%s: source = %q, want builtin", template.ID, template.Source)
		}
	}
}

// The point of a catalog is one click. Every template has to produce a valid
// container definition from its own defaults plus generated secrets.
func TestEveryTemplateRendersAValidSpec(t *testing.T) {
	catalog := load(t)

	for _, template := range catalog.List() {
		t.Run(template.ID, func(t *testing.T) {
			values := answersFor(template)

			spec, err := template.Render(template.ID, values)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			if spec.Image == "" || strings.Contains(spec.Image, "{{") {
				t.Errorf("image = %q, want it fully substituted", spec.Image)
			}
			if spec.Name != template.ID {
				t.Errorf("name = %q, want %q", spec.Name, template.ID)
			}
			if spec.Labels[LabelTemplate] != template.ID {
				t.Errorf("labels = %v, want the template's own", spec.Labels)
			}

			// Nothing may reach the engine with a placeholder still in it.
			for _, env := range spec.Env {
				if strings.Contains(env.Value, "{{") {
					t.Errorf("env %s = %q, still holds a placeholder", env.Key, env.Value)
				}
			}
			for _, mount := range spec.Mounts {
				if strings.Contains(mount.Source, "{{") || strings.Contains(mount.Destination, "{{") {
					t.Errorf("mount %+v still holds a placeholder", mount)
				}
				if mount.Source == "" {
					t.Errorf("mount %s has no source", mount.Destination)
				}
			}

			// The engine has to accept it. This is the same translation the
			// create wizard's payload goes through.
			if _, err := docker.BuildCreateSpec(spec); err != nil {
				t.Errorf("BuildCreateSpec() error = %v", err)
			}
		})
	}
}

// answersFor fills a template's required fields the way an operator would.
func answersFor(template Template) map[string]string {
	values := map[string]string{}

	for _, field := range template.Fields {
		if field.Default != "" && field.Type != FieldPassword {
			continue
		}
		switch field.Type {
		case FieldPassword:
			values[field.Name] = "generated-secret-value-0123456789"
		case FieldPath:
			values[field.Name] = "/srv/example"
		case FieldPort:
			values[field.Name] = "18080"
		case FieldNumber:
			values[field.Name] = "600"
		case FieldVolume:
			values[field.Name] = template.ID + "-data"
		case FieldSelect:
			if len(field.Options) > 0 {
				values[field.Name] = field.Options[0].Value
			}
		case FieldBool:
			values[field.Name] = "false"
		default:
			if field.Required {
				values[field.Name] = fillFor(field.Name)
			}
		}
	}
	return values
}

// fillFor answers a required text field with something its pattern accepts.
func fillFor(name string) string {
	switch {
	case strings.Contains(name, "email"):
		return "operator@example.com"
	case strings.Contains(name, "url"):
		return "http://localhost:3000/"
	default:
		return "example"
	}
}

func TestCatalogGroupsByCategory(t *testing.T) {
	catalog := load(t)

	categories := catalog.Categories()
	if len(categories) < 3 {
		t.Errorf("categories = %v, want databases, networking and tools at least", categories)
	}

	// The listing is ordered by category then title, which is how it is shown.
	list := catalog.List()
	for i := 1; i < len(list); i++ {
		if list[i-1].Category > list[i].Category {
			t.Fatalf("catalog is not ordered by category: %s before %s",
				list[i-1].Category, list[i].Category)
		}
	}
}

func TestCatalogRejectsAnUnknownTemplate(t *testing.T) {
	if _, err := load(t).Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// The templates that need capabilities, devices or the host network say so, so
// the catalog can warn before the operator fills in a form they cannot submit.
func TestPrivilegedTemplatesAreMarked(t *testing.T) {
	catalog := load(t)

	wg, err := catalog.Get("wg-easy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !wg.NeedsPrivileged() {
		t.Error("wg-easy adds NET_ADMIN and sysctls; it needs the privileged permission")
	}

	redis, err := catalog.Get("redis")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if redis.NeedsPrivileged() {
		t.Error("redis needs nothing privileged")
	}
}

// One operator's typo must not take the whole catalog — including every
// shipped entry — off the screen.
func TestCustomTemplatesLoadAlongsideBuiltins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "own.json"), `{
	  "id": "own", "title": "My App", "category": "tools",
	  "image": "example/app:1",
	  "fields": [{"name": "port", "label": "Port", "type": "port", "default": "9999"}],
	  "ports": [{"host": "{{port}}", "container": 80}]
	}`)
	writeFile(t, filepath.Join(dir, "broken.json"), `{"id": "broken"}`)
	writeFile(t, filepath.Join(dir, "notjson.txt"), `ignored`)

	catalog, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	own, err := catalog.Get("own")
	if err != nil {
		t.Fatalf("the custom template did not load: %v", err)
	}
	if own.Source != SourceCustom {
		t.Errorf("source = %q, want custom", own.Source)
	}

	if _, err := catalog.Get("redis"); err != nil {
		t.Error("a broken custom file took the shipped catalog with it")
	}

	problems := catalog.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0].Path, "broken.json") {
		t.Errorf("problems = %+v, want the broken file reported", problems)
	}
}

// Pinning a version for the whole installation should not mean forking the
// binary.
func TestACustomTemplateOverridesAShippedOne(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "redis.json"), `{
	  "id": "redis", "title": "Redis (pinned)", "category": "database",
	  "image": "redis:7.2-alpine"
	}`)

	catalog, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	redis, err := catalog.Get("redis")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if redis.Title != "Redis (pinned)" || redis.Source != SourceCustom {
		t.Errorf("redis = %+v, want the operator's own", redis)
	}
	if catalog.Len() != len(expected) {
		t.Errorf("catalog holds %d, want the override to replace rather than add", catalog.Len())
	}
}

func TestLoadIgnoresAMissingCustomDirectory(t *testing.T) {
	catalog, err := Load(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if catalog.Len() != len(expected) {
		t.Errorf("catalog holds %d templates, want the built-ins", catalog.Len())
	}
	if len(catalog.Problems()) != 0 {
		t.Errorf("problems = %+v, want none: not having a custom directory is normal",
			catalog.Problems())
	}
}

// A typo in a key would otherwise produce a template that is quietly not what
// was written.
func TestAnUnknownKeyIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "typo.json"), `{
	  "id": "typo", "title": "Typo", "category": "tools",
	  "image": "example/app:1", "prots": []
	}`)

	catalog, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := catalog.Get("typo"); err == nil {
		t.Error("a template with an unknown key loaded")
	}
	if len(catalog.Problems()) != 1 {
		t.Errorf("problems = %+v, want the typo reported", catalog.Problems())
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
