// Package version exposes build metadata that is stamped into the binary at
// link time via -ldflags.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// These values are overridden at build time. See the Makefile.
var (
	// Version is the semantic version of the build (e.g. "v0.1.0").
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
	// BuildDate is the RFC3339 timestamp of the build.
	BuildDate = "unknown"
)

// Info describes the running build. It is returned by GET /api/v1/version.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get returns the build metadata of the running binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    commit(),
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// String renders a single-line summary, used by the --version flag.
func String() string {
	i := Get()
	return fmt.Sprintf("iskeled %s (commit %s, built %s, %s, %s)",
		i.Version, i.Commit, i.BuildDate, i.GoVersion, i.Platform)
}

// commit falls back to the VCS revision embedded by the Go toolchain when the
// value was not supplied through ldflags (e.g. `go run ./cmd/iskeled`).
func commit() string {
	if Commit != "unknown" && Commit != "" {
		return Commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Commit
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return Commit
}
