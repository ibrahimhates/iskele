package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture reads one of the compose files under testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(content)
}

func TestParseResolvesInterpolation(t *testing.T) {
	project, _, err := Parse(context.Background(), Input{
		Name:       "blog",
		Compose:    fixture(t, "wordpress.yaml"),
		Env:        "DB_ROOT_PASSWORD=root-secret\nDB_PASSWORD=hunter2\nHTTP_PORT=9090\n",
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if project.Name != "blog" {
		t.Errorf("project name = %q, want blog", project.Name)
	}

	db := project.Services["db"]
	if got := deref(db.Environment["MARIADB_PASSWORD"]); got != "hunter2" {
		t.Errorf("MARIADB_PASSWORD = %q, want hunter2", got)
	}

	wp := project.Services["wordpress"]
	if len(wp.Ports) != 1 || wp.Ports[0].Published != "9090" {
		t.Errorf("ports = %+v, want the interpolated 9090", wp.Ports)
	}
}

func TestParseAppliesEnvDefaults(t *testing.T) {
	// `${HTTP_PORT:-8080}` with nothing set is the whole point of a default.
	project, _, err := Parse(context.Background(), Input{
		Name:       "blog",
		Compose:    fixture(t, "wordpress.yaml"),
		Env:        "DB_ROOT_PASSWORD=root-secret\n",
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := project.Services["wordpress"].Ports[0].Published; got != "8080" {
		t.Errorf("published port = %q, want the 8080 default", got)
	}
}

// `${VAR:?message}` is how a compose file says a value is required. Deploying
// without it would produce a database with an empty root password.
func TestParseRefusesAMissingRequiredVariable(t *testing.T) {
	_, _, err := Parse(context.Background(), Input{
		Name:       "blog",
		Compose:    fixture(t, "wordpress.yaml"),
		Env:        "",
		WorkingDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want a failure for the unset DB_ROOT_PASSWORD")
	}
	if !strings.Contains(err.Error(), "DB_ROOT_PASSWORD") {
		t.Errorf("error = %q, want it to name the variable", err)
	}
}

// The daemon's own environment holds a secret key path and whatever the unit
// file sets. A stack must not be able to read it.
func TestParseDoesNotSeeTheDaemonEnvironment(t *testing.T) {
	t.Setenv("ISKELE_TEST_LEAK", "leaked")

	_, _, err := Parse(context.Background(), Input{
		Name:       "leak",
		Compose:    "services:\n  app:\n    image: alpine:${ISKELE_TEST_LEAK:?not visible}\n",
		WorkingDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want the daemon's environment to be invisible")
	}
}

func TestParseEnvHandlesQuotingAndComments(t *testing.T) {
	env, err := ParseEnv("# a comment\nPLAIN=one\nQUOTED=\"two words\"\nEMPTY=\n\nSPACED = three\n")
	if err != nil {
		t.Fatalf("ParseEnv() error = %v", err)
	}

	for key, want := range map[string]string{
		"PLAIN":  "one",
		"QUOTED": "two words",
		"EMPTY":  "",
		"SPACED": "three",
	} {
		if got := env[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestParseRejectsAnEmptyName(t *testing.T) {
	_, _, err := Parse(context.Background(), Input{Compose: fixture(t, "redis.yaml")})
	if err == nil {
		t.Fatal("Parse() error = nil, want a name to be required")
	}
}

func TestParseRejectsAFileWithNoServices(t *testing.T) {
	_, _, err := Parse(context.Background(), Input{
		Name:       "empty",
		Compose:    "volumes:\n  data:\n",
		WorkingDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want ErrNoServices")
	}
}

func TestParseReportsBrokenYAML(t *testing.T) {
	_, _, err := Parse(context.Background(), Input{
		Name:       "broken",
		Compose:    "services:\n  app:\n  image: [unclosed\n",
		WorkingDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want a parse failure")
	}

	var composeErr *Error
	if !errors.As(err, &composeErr) {
		t.Fatalf("error type = %T, want *compose.Error", err)
	}
}

// depends_on is a promise about order. A cycle makes it unkeepable, and the
// operator should hear that here rather than watch a deploy hang.
func TestParseRefusesADependencyCycle(t *testing.T) {
	_, _, err := Parse(context.Background(), Input{
		Name: "cycle",
		Compose: "services:\n" +
			"  a:\n    image: alpine\n    depends_on: [b]\n" +
			"  b:\n    image: alpine\n    depends_on: [a]\n",
		WorkingDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want a cycle to be refused")
	}
}

func TestServiceOrderPutsDependenciesFirst(t *testing.T) {
	project, _, err := Parse(context.Background(), Input{
		Name:       "jobs",
		Compose:    fixture(t, "scaled.yaml"),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	order, err := ServiceOrder(context.Background(), project)
	if err != nil {
		t.Fatalf("ServiceOrder() error = %v", err)
	}

	queue := slices.Index(order, "queue")
	worker := slices.Index(order, "worker")
	scheduler := slices.Index(order, "scheduler")

	if queue > worker || worker > scheduler {
		t.Errorf("order = %v, want queue before worker before scheduler", order)
	}
}

func TestSortedServiceNames(t *testing.T) {
	project, _, err := Parse(context.Background(), Input{
		Name:       "net",
		Compose:    fixture(t, "networks.yaml"),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := SortedServiceNames(project); !slices.Equal(got, []string{"api", "proxy"}) {
		t.Errorf("names = %v, want [api proxy]", got)
	}
}

// Relative paths only mean something against a directory, and the engine needs
// an absolute one — as does the whitelist check that comes before it.
func TestParseResolvesRelativeBindMounts(t *testing.T) {
	dir := t.TempDir()

	project, _, err := Parse(context.Background(), Input{
		Name:       "site",
		Compose:    fixture(t, "binds.yaml"),
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var sources []string
	for _, volume := range project.Services["web"].Volumes {
		sources = append(sources, volume.Source)
	}

	want := filepath.Join(dir, "site")
	if !slices.Contains(sources, want) {
		t.Errorf("sources = %v, want the relative ./site resolved to %s", sources, want)
	}
}

// deref reads a compose environment value, which is a pointer because compose
// distinguishes "set to nothing" from "pass through from the host".
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// compose-go reports an unset variable through a log line, not through its
// return value. Deploying a database whose password silently became the empty
// string is exactly the kind of thing an operator has to be told about.
func TestParseCapturesTheParsersOwnWarnings(t *testing.T) {
	_, warnings, err := Parse(context.Background(), Input{
		Name:       "blog",
		Compose:    fixture(t, "wordpress.yaml"),
		Env:        "DB_ROOT_PASSWORD=root-secret\n",
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var found bool
	for _, warning := range warnings {
		if strings.Contains(warning.Message, "DB_PASSWORD") {
			found = true
			if warning.Field != "interpolation" {
				t.Errorf("field = %q, want interpolation", warning.Field)
			}
		}
	}
	if !found {
		t.Errorf("warnings = %+v, want one naming the unset DB_PASSWORD", warnings)
	}
}

// The obsolete `version:` key is on almost every compose file in the world.
// Repeating a warning about it on every deploy teaches operators to ignore them.
func TestParseDropsTheObsoleteVersionWarning(t *testing.T) {
	_, warnings, err := Parse(context.Background(), Input{
		Name:       "old",
		Compose:    "version: \"3.8\"\nservices:\n  app:\n    image: alpine\n",
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, warning := range warnings {
		if strings.Contains(warning.Message, "version") {
			t.Errorf("warnings = %+v, want the version notice dropped", warnings)
		}
	}
}
