package server

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/web"
)

// assetPrefix is the directory Vite writes content-hashed files into. Anything
// under it may be cached forever, because a changed file gets a changed name.
const assetPrefix = "/assets/"

// immutableCacheControl is a year, the longest value the spec endorses.
const immutableCacheControl = "public, max-age=31536000, immutable"

// notBundledPage is what a `go build`-only binary answers with. It is a
// developer-facing notice, not a stand-in for the UI: a release binary always
// carries the real build, and this page says exactly how to produce one.
const notBundledPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>iskeled — UI not bundled</title></head>
<body style="font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;line-height:1.6">
<h1>UI not bundled</h1>
<p>This <code>iskeled</code> binary was built without the frontend assets, so
only the API at <code>/api/v1</code> is available.</p>
<p>Build the full binary with <code>make build</code>, which compiles the
frontend into <code>web/dist</code> before embedding it.</p>
</body></html>
`

// spaHandler serves the embedded single-page application.
//
// Static files are served as themselves; every other path returns index.html
// so that a deep link such as /containers/abc survives a page reload, which
// the router only sees client-side.
func spaHandler(assets fs.FS, bundled bool) http.Handler {
	if !bundled {
		return http.HandlerFunc(serveNotBundled)
	}

	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		// Bundled already proved index.html is readable; reaching here would
		// mean the embedded tree changed underneath us.
		return http.HandlerFunc(serveNotBundled)
	}

	// One timestamp for the process lifetime: the assets are frozen inside the
	// binary, so a restart is the only thing that can change them.
	modTime := time.Now()
	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.WriteError(w, r, httpx.NewError(http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed,
				"method %s is not allowed on %s", r.Method, r.URL.Path))
			return
		}

		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))

		if isFile(assets, clean) {
			if strings.HasPrefix(clean, assetPrefix) {
				w.Header().Set("Cache-Control", immutableCacheControl)
			} else {
				// Everything else — favicon, manifest — keeps its name across
				// releases, so it must be revalidated.
				w.Header().Set("Cache-Control", "no-cache")
			}
			r.URL.Path = clean
			files.ServeHTTP(w, r)
			return
		}

		// A missing file under /assets/ is a stale index referencing a build
		// that no longer exists. Answering with index.html would hand the
		// browser HTML where it expects JavaScript, and the console error
		// would point at the wrong thing.
		if strings.HasPrefix(clean, assetPrefix) {
			http.NotFound(w, r)
			return
		}

		serveIndex(w, r, index, modTime)
	})
}

// serveIndex writes the SPA shell.
func serveIndex(w http.ResponseWriter, r *http.Request, index []byte, modTime time.Time) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The shell names the hashed bundles, so a cached copy would pin the
	// browser to the previous release.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", modTime, strings.NewReader(string(index)))
}

// serveNotBundled explains a frontend-less build.
func serveNotBundled(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, notBundledPage)
	}
}

// isFile reports whether the tree holds a regular file at this path.
func isFile(assets fs.FS, urlPath string) bool {
	name := strings.TrimPrefix(urlPath, "/")
	if name == "" || !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

// newSPAHandler serves the frontend compiled into this binary.
func newSPAHandler() http.Handler {
	return spaHandler(web.FS(), web.Bundled())
}
