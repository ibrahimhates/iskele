// Package web carries the compiled single-page application into the binary.
//
// The Go file lives beside the frontend sources because go:embed can only
// reach into its own directory tree; the server package consumes what this
// one exports.
package web

import (
	"embed"
	"io/fs"
)

// dist holds the Vite build output. The `all:` prefix keeps files whose names
// begin with a dot or an underscore, which a hashed asset directory can
// contain and which the default embed rules would silently drop.
//
// The directory is committed with only a .gitkeep in it, so `go build` works
// on a checkout that has never run `npm run build`; `make build` runs the
// frontend build first, and Bundled reports which of the two happened.
//
//go:embed all:dist
var dist embed.FS

// FS returns the SPA file tree, rooted at the directory holding index.html.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: the embed directive guarantees "dist" exists, and
		// fs.Sub only fails on an invalid path.
		panic("web: embedded dist is not a directory: " + err.Error())
	}
	return sub
}

// Bundled reports whether this binary carries a real frontend build.
//
// A developer running `go build ./cmd/iskeled` gets a binary with an empty
// dist; the server uses this to serve an explanatory page rather than a 404
// that looks like a routing bug.
func Bundled() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
