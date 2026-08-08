package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetFillsRuntimeFields(t *testing.T) {
	got := Get()

	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}
	want := runtime.GOOS + "/" + runtime.GOARCH
	if got.Platform != want {
		t.Errorf("Platform = %q, want %q", got.Platform, want)
	}
	if got.Version == "" {
		t.Error("Version must not be empty")
	}
}

func TestGetUsesLdflagValues(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = origVersion, origCommit, origDate })

	Version, Commit, BuildDate = "v1.2.3", "abcdef123456", "2026-01-01T00:00:00Z"

	got := Get()
	if got.Version != "v1.2.3" || got.Commit != "abcdef123456" || got.BuildDate != "2026-01-01T00:00:00Z" {
		t.Errorf("ldflag values not propagated: %+v", got)
	}
}

func TestStringMentionsVersionAndCommit(t *testing.T) {
	origVersion, origCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = origVersion, origCommit })

	Version, Commit = "v9.9.9", "deadbeef"

	s := String()
	if !strings.Contains(s, "v9.9.9") || !strings.Contains(s, "deadbeef") {
		t.Errorf("String() = %q, want it to contain version and commit", s)
	}
	if !strings.HasPrefix(s, "iskeled ") {
		t.Errorf("String() = %q, want it to start with the binary name", s)
	}
}
