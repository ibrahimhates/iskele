package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newBrowser(t *testing.T, files map[string]string) (*Browser, string) {
	t.Helper()
	root := buildTree(t, files)
	return NewBrowser(NewPathGuard([]string{root})), root
}

func TestBrowseListsADirectory(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{
		"Dockerfile":  "FROM alpine\n",
		"app/main.go": "package main\n",
		"README.md":   "hello\n",
	})

	listing, err := browser.Browse(context.Background(), root)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}

	names := map[string]bool{}
	for _, entry := range listing.Entries {
		names[entry.Name] = entry.IsDir
	}
	if len(names) != 3 {
		t.Fatalf("entries = %v", names)
	}
	if !names["app"] {
		t.Error("app should be listed as a directory")
	}
	if names["Dockerfile"] {
		t.Error("Dockerfile should not be listed as a directory")
	}
}

// Directories first is the order an operator navigating with the keyboard
// expects.
func TestBrowseSortsDirectoriesFirst(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{
		"Dockerfile": "FROM alpine\n",
		"zzz/keep":   "x\n",
		"aaa.txt":    "y\n",
	})

	listing, err := browser.Browse(context.Background(), root)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}

	if !listing.Entries[0].IsDir || listing.Entries[0].Name != "zzz" {
		t.Errorf("first entry = %+v, want the directory", listing.Entries[0])
	}
}

// The UI offers the build file without a second round trip.
func TestBrowseReportsDockerfiles(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{
		"Dockerfile":       "FROM alpine\n",
		"Dockerfile.prod":  "FROM alpine\n",
		"api.dockerfile":   "FROM alpine\n",
		"notadockerfile.c": "int main(){}\n",
	})

	listing, err := browser.Browse(context.Background(), root)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}

	want := map[string]bool{"Dockerfile": true, "Dockerfile.prod": true, "api.dockerfile": true}
	if len(listing.Dockerfiles) != len(want) {
		t.Fatalf("dockerfiles = %v", listing.Dockerfiles)
	}
	for _, name := range listing.Dockerfiles {
		if !want[name] {
			t.Errorf("%q was offered as a Dockerfile", name)
		}
	}
}

// Browsing is the same trust boundary as bind mounting, seen from the other
// side; a path outside the whitelist must not open.
func TestBrowseRefusesPathsOutsideTheWhitelist(t *testing.T) {
	browser, _ := newBrowser(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	for _, path := range []string{"/etc", "/", "/root", "/var/run"} {
		if _, err := browser.Browse(context.Background(), path); !errors.Is(err, ErrPathNotAllowed) {
			t.Errorf("Browse(%q) = %v, want it refused", path, err)
		}
	}
}

func TestBrowseRefusesTraversalOutOfTheRoot(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	if _, err := browser.Browse(context.Background(), filepath.Join(root, "..", "..")); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("traversal was allowed: %v", err)
	}
}

// A link inside an allowed root can point anywhere; opening it must fail.
func TestBrowseDoesNotFollowASymlinkOutOfTheRoot(t *testing.T) {
	outside := t.TempDir()
	browser, root := newBrowser(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	if _, err := browser.Browse(context.Background(), link); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("Browse(symlink out of the root) = %v, want it refused", err)
	}
}

// The picker's first request has no path at all; answering with an error would
// make it impossible to open.
func TestBrowseWithNoPathReturnsTheRoots(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	listing, err := browser.Browse(context.Background(), "")
	if err != nil {
		t.Fatalf("Browse(\"\") error = %v", err)
	}

	if len(listing.Entries) != 1 || listing.Entries[0].Path != root {
		t.Errorf("entries = %+v, want the configured root", listing.Entries)
	}
	if listing.Parent != "" {
		t.Errorf("Parent = %q, want nothing above the roots", listing.Parent)
	}
}

// There is nothing above an allowed root that this installation permits, so
// offering a parent link there would only produce a refusal.
func TestBrowseHasNoParentAtAnAllowedRoot(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{"app/main.go": "package main\n"})

	atRoot, err := browser.Browse(context.Background(), root)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if atRoot.Parent != "" {
		t.Errorf("Parent = %q at an allowed root, want empty", atRoot.Parent)
	}

	inside, err := browser.Browse(context.Background(), filepath.Join(root, "app"))
	if err != nil {
		t.Fatalf("Browse(app) error = %v", err)
	}
	if inside.Parent != root {
		t.Errorf("Parent = %q, want %q", inside.Parent, root)
	}
}

func TestBrowseRefusesAFile(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	_, err := browser.Browse(context.Background(), filepath.Join(root, "Dockerfile"))
	if !errors.Is(err, ErrNotADirectory) {
		t.Errorf("error = %v, want ErrNotADirectory", err)
	}
}

func TestBrowseReportsTheWhitelistSoTheUICanShowIt(t *testing.T) {
	browser, root := newBrowser(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	listing, err := browser.Browse(context.Background(), root)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(listing.AllowedRoots) != 1 || listing.AllowedRoots[0] != root {
		t.Errorf("AllowedRoots = %v", listing.AllowedRoots)
	}
}

// With no whitelist there is nothing to browse, and the empty listing says so
// rather than exposing the filesystem.
func TestBrowseWithNoWhitelistShowsNothing(t *testing.T) {
	browser := NewBrowser(NewPathGuard(nil))

	listing, err := browser.Browse(context.Background(), "")
	if err != nil {
		t.Fatalf("Browse(\"\") error = %v", err)
	}
	if len(listing.Entries) != 0 {
		t.Errorf("entries = %+v, want none", listing.Entries)
	}

	if _, err := browser.Browse(context.Background(), "/srv"); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("Browse(/srv) = %v, want it refused", err)
	}
}
