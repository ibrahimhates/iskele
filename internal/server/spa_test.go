package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/web"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/index-abc123.js": {Data: []byte("console.log(1)")},
		"favicon.svg":            {Data: []byte("<svg/>")},
	}
}

func serveSPA(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	spaHandler(testAssets(), true).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestSPAServesTheShellForClientSideRoutes(t *testing.T) {
	for _, path := range []string{"/", "/login", "/containers/abc123/logs", "/settings/tokens"} {
		t.Run(path, func(t *testing.T) {
			rec := serveSPA(t, http.MethodGet, path)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "id=root") {
				t.Errorf("body = %q, want index.html", rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
				t.Errorf("Cache-Control = %q, want no-cache: the shell names the hashed bundles", got)
			}
		})
	}
}

func TestSPAServesStaticFilesAsThemselves(t *testing.T) {
	rec := serveSPA(t, http.MethodGet, "/assets/index-abc123.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "console.log(1)" {
		t.Errorf("body = %q, want the asset itself", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != immutableCacheControl {
		t.Errorf("Cache-Control = %q, want the immutable policy for a hashed name", got)
	}
}

func TestSPARevalidatesUnhashedFiles(t *testing.T) {
	rec := serveSPA(t, http.MethodGet, "/favicon.svg")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q: favicon.svg keeps its name across releases", got)
	}
}

// A stale shell asking for a bundle that no longer exists must not be answered
// with HTML: the browser would report a syntax error in a JavaScript file
// rather than a missing one.
func TestSPADoesNotFallBackForMissingAssets(t *testing.T) {
	rec := serveSPA(t, http.MethodGet, "/assets/index-gone.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "id=root") {
		t.Error("a missing asset was answered with index.html")
	}
}

func TestSPARejectsNonReadMethods(t *testing.T) {
	rec := serveSPA(t, http.MethodPost, "/login")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), string(httpx.CodeMethodNotAllowed)) {
		t.Errorf("body = %q, want the standard error envelope", rec.Body.String())
	}
}

func TestSPADoesNotEscapeItsRoot(t *testing.T) {
	// http.ServeMux and path.Clean both collapse the traversal; the assertion
	// is that it lands on the shell rather than on something outside the tree.
	rec := serveSPA(t, http.MethodGet, "/assets/../../etc/passwd")

	if rec.Code == http.StatusOK && !strings.Contains(rec.Body.String(), "id=root") {
		t.Errorf("traversal returned %q", rec.Body.String())
	}
}

func TestSPAWithoutABundleExplainsItself(t *testing.T) {
	rec := httptest.NewRecorder()
	spaHandler(fstest.MapFS{}, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "make build") {
		t.Errorf("body = %q, want the instruction to build the frontend", rec.Body.String())
	}
}

// The router must keep answering JSON under /api even though everything else
// now falls through to HTML.
func TestUnknownAPIPathStaysJSONWhileTheRestIsTheSPA(t *testing.T) {
	h := testRouter(t)

	api := do(t, h, http.MethodGet, APIPrefix+"/nope")
	if got := api.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("api Content-Type = %q, want JSON", got)
	}

	ui := do(t, h, http.MethodGet, "/containers")
	if got := ui.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("ui Content-Type = %q, want HTML", got)
	}
}

func TestAPIPathsAreMatchedBySegment(t *testing.T) {
	cases := map[string]bool{
		"/api":           true,
		"/api/v1":        true,
		"/api/v1/health": true,
		"/apixyz":        false,
		"/":              false,
		"/containers":    false,
	}
	for path, want := range cases {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", path, got, want)
		}
	}
}

// The embedded tree is what ships; this checks the wiring rather than the
// contents, which depend on whether `make web` has run in this checkout.
func TestEmbeddedTreeIsReadable(t *testing.T) {
	if _, err := web.FS().Open("."); err != nil {
		t.Fatalf("embedded dist is not readable: %v", err)
	}
}
