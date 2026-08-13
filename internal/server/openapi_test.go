package server

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/docker/fake"
)

// specPath matches a path key in docs/openapi.yaml: two spaces, a slash, then
// the path, then a colon at the end of the line.
var specPath = regexp.MustCompile(`(?m)^  (/[^\s:]*):\s*$`)

// documentedPaths reads the paths out of the OpenAPI document.
//
// A YAML parse would be stricter, but it would also pull a dependency into a
// test whose whole job is to compare two lists of strings — and the file's
// path keys are the one part of it with a shape this regular.
func documentedPaths(t *testing.T) map[string]bool {
	t.Helper()

	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read the OpenAPI document: %v", err)
	}

	out := map[string]bool{}
	for _, match := range specPath.FindAllStringSubmatch(string(raw), -1) {
		out[match[1]] = true
	}
	if len(out) < 50 {
		t.Fatalf("found only %d paths in the document; the regexp is probably wrong", len(out))
	}
	return out
}

// mountedPaths walks the router and returns every route it actually serves,
// normalized to the shape the OpenAPI document uses.
func mountedPaths(t *testing.T, handler chi.Routes) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	err := chi.Walk(handler, func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path := strings.TrimPrefix(route, APIPrefix)

		// chi records a subtree's index route as a trailing slash; the
		// document writes it without one.
		if path != "/" {
			path = strings.TrimSuffix(path, "/")
		}
		// chi spells a wildcard `/*`; those are the SPA fallbacks, which the
		// API document has no business describing.
		if strings.Contains(path, "*") {
			return nil
		}
		if path == "" {
			return nil
		}
		out[path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	return out
}

// The spec is the API's specification, not documentation written afterwards:
// the UI is generated from it, so a route that exists and is not described is
// a route no client knows about, and a described route that does not exist is
// worse — it is a promise.
func TestEveryMountedRouteIsDocumented(t *testing.T) {
	env := newEnv(t, fake.New())

	routes, ok := env.raw.(chi.Routes)
	if !ok {
		t.Fatal("the router is no longer a chi.Routes; this test cannot walk it")
	}

	documented := documentedPaths(t)
	mounted := mountedPaths(t, routes)

	// Paths served outside the versioned API prefix. They are in the document
	// under their own names, which the prefix trim above does not produce.
	outsideTheAPI := map[string]bool{
		"/api/v1/health":  true,
		"/api/v1/version": true,
	}

	var missing []string
	for path := range mounted {
		if documented[path] || outsideTheAPI[path] {
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		t.Errorf("mounted but not in docs/openapi.yaml: %v", missing)
	}
}

// The other direction. A path in the document that nothing serves is a
// promise the daemon does not keep, and it is the harder one to notice:
// nothing fails until a client tries it.
func TestEveryDocumentedRouteIsMounted(t *testing.T) {
	env := newEnv(t, fake.New())

	routes, ok := env.raw.(chi.Routes)
	if !ok {
		t.Fatal("the router is no longer a chi.Routes; this test cannot walk it")
	}

	documented := documentedPaths(t)
	mounted := mountedPaths(t, routes)

	// Served without the version prefix, so the walk records them under their
	// full path.
	servedElsewhere := map[string]bool{
		"/health":  true,
		"/version": true,
	}

	var missing []string
	for path := range documented {
		if mounted[path] || servedElsewhere[path] {
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		t.Errorf("in docs/openapi.yaml but not served: %v", missing)
	}
}

// A guard on the two tests above: if the walk returned nothing they would both
// pass while checking nothing at all.
func TestTheRouteWalkFindsRoutes(t *testing.T) {
	env := newEnv(t, fake.New())

	routes, ok := env.raw.(chi.Routes)
	if !ok {
		t.Fatal("the router is no longer a chi.Routes")
	}

	if got := len(mountedPaths(t, routes)); got < 50 {
		t.Fatalf("the walk found %d routes; the daemon serves far more than that", got)
	}
}
