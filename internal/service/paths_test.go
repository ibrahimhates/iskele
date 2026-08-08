package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathGuardAcceptsPathsUnderARoot(t *testing.T) {
	guard := NewPathGuard([]string{"/srv", "/opt/stacks"})

	for _, path := range []string{
		"/srv",
		"/srv/data",
		"/srv/data/nested/deeper",
		"/opt/stacks/app/config.yaml",
		"/srv/./data",
		"/srv/data/../data",
	} {
		if err := guard.Check(path); err != nil {
			t.Errorf("Check(%q) = %v, want it allowed", path, err)
		}
	}
}

func TestPathGuardRefusesEverythingElse(t *testing.T) {
	guard := NewPathGuard([]string{"/srv", "/opt/stacks"})

	for _, path := range []string{
		"/",
		"/etc",
		"/etc/shadow",
		"/var/run/docker.sock",
		"/home/user",
		"relative/path",
		"",
		"   ",
	} {
		err := guard.Check(path)
		if !errors.Is(err, ErrPathNotAllowed) {
			t.Errorf("Check(%q) = %v, want it refused", path, err)
		}
	}
}

// "/srv-other" shares a string prefix with "/srv" but is a different
// directory; a naive strings.HasPrefix would let it through.
func TestPathGuardComparesComponentsNotStrings(t *testing.T) {
	guard := NewPathGuard([]string{"/srv"})

	for _, path := range []string{"/srv-other", "/srvsomething", "/srv-other/data"} {
		if err := guard.Check(path); !errors.Is(err, ErrPathNotAllowed) {
			t.Errorf("Check(%q) = %v, want it refused", path, err)
		}
	}
}

// Traversal is the obvious attack on a prefix check, and Clean handles it — but
// only if the check happens after cleaning, which is what this pins.
func TestPathGuardRefusesTraversalOutOfARoot(t *testing.T) {
	guard := NewPathGuard([]string{"/srv/data"})

	for _, path := range []string{
		"/srv/data/../../etc",
		"/srv/data/../other",
		"/srv/data/./../..",
	} {
		if err := guard.Check(path); !errors.Is(err, ErrPathNotAllowed) {
			t.Errorf("Check(%q) = %v, want it refused", path, err)
		}
	}
}

// A symlink inside an allowed root can point anywhere; following it is the
// whole reason the guard resolves before comparing.
func TestPathGuardFollowsSymlinksBeforeDeciding(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	allowed := filepath.Join(root, "allowed")
	if err := os.Mkdir(allowed, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	escape := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	guard := NewPathGuard([]string{allowed})

	if err := guard.Check(filepath.Join(allowed, "real")); err != nil {
		t.Errorf("a plain path under the root was refused: %v", err)
	}
	if err := guard.Check(escape); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("Check(symlink out of the root) = %v, want it refused", err)
	}
}

// A path the engine would create on demand does not exist yet, and refusing it
// for that reason alone would break a legitimate first run.
func TestPathGuardAllowsAPathThatDoesNotExistYet(t *testing.T) {
	root := t.TempDir()
	guard := NewPathGuard([]string{root})

	if err := guard.Check(filepath.Join(root, "not", "created", "yet")); err != nil {
		t.Errorf("Check(missing path under the root) = %v, want it allowed", err)
	}
}

// A misconfiguration must fail closed: no whitelist means no bind mounts, not
// unrestricted access to the host.
func TestPathGuardWithNoRootsRefusesEverything(t *testing.T) {
	guard := NewPathGuard(nil)

	err := guard.Check("/srv/data")
	if !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("Check() = %v, want it refused", err)
	}

	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Fatal("the error does not carry the path")
	}
	if pathErr.Error() == "" {
		t.Error("the message is empty")
	}
}

func TestPathGuardCheckAllReportsTheFirstRefusal(t *testing.T) {
	guard := NewPathGuard([]string{"/srv"})

	err := guard.CheckAll([]string{"/srv/a", "/etc/passwd", "/srv/b"})

	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error = %v, want a PathError", err)
	}
	if pathErr.Path != "/etc/passwd" {
		t.Errorf("refused path = %q, want the one that failed", pathErr.Path)
	}
}

func TestPathGuardErrorNamesTheConfiguredRoots(t *testing.T) {
	guard := NewPathGuard([]string{"/srv", "/opt/stacks"})

	err := guard.Check("/etc")

	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error = %v, want a PathError", err)
	}
	if len(pathErr.Allowed) != 2 {
		t.Errorf("Allowed = %v, want both roots so the operator can see them", pathErr.Allowed)
	}
}
