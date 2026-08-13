package service

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// buildTree writes a directory of files, creating parents as needed.
func buildTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()

	for name, content := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// packed lists the entries of a written context.
func packed(t *testing.T, opts ContextOptions) (map[string]string, ContextStats) {
	t.Helper()

	var buf bytes.Buffer
	stats, err := WriteBuildContext(&buf, opts)
	if err != nil {
		t.Fatalf("WriteBuildContext() error = %v", err)
	}

	entries := map[string]string{}
	reader := tar.NewReader(&buf)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		entries[header.Name] = string(body)
	}
	return entries, stats
}

func TestBuildContextPacksTheTree(t *testing.T) {
	root := buildTree(t, map[string]string{
		"Dockerfile":      "FROM alpine\n",
		"app/main.go":     "package main\n",
		"app/lib/util.go": "package lib\n",
		"README.md":       "hello\n",
	})

	entries, stats := packed(t, ContextOptions{Dir: root})

	for _, want := range []string{"Dockerfile", "app/main.go", "app/lib/util.go", "README.md"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("%s is missing from the context", want)
		}
	}
	if entries["Dockerfile"] != "FROM alpine\n" {
		t.Errorf("Dockerfile content = %q", entries["Dockerfile"])
	}
	if stats.Files != 4 {
		t.Errorf("Files = %d, want 4", stats.Files)
	}
	if stats.Bytes == 0 {
		t.Error("Bytes was not counted")
	}
}

// The engine needs directory entries for a build that expects an empty
// directory to exist.
func TestBuildContextRecordsDirectories(t *testing.T) {
	root := buildTree(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	entries, _ := packed(t, ContextOptions{Dir: root})

	if _, ok := entries["empty/"]; !ok {
		t.Errorf("the empty directory is missing; entries = %v", keysOf(entries))
	}
}

func TestBuildContextNeedsADockerfile(t *testing.T) {
	root := buildTree(t, map[string]string{"main.go": "package main\n"})

	_, err := WriteBuildContext(io.Discard, ContextOptions{Dir: root})
	if !errors.Is(err, ErrNoDockerfile) {
		t.Errorf("error = %v, want ErrNoDockerfile", err)
	}
}

func TestBuildContextAcceptsANamedDockerfile(t *testing.T) {
	root := buildTree(t, map[string]string{
		"docker/Dockerfile.prod": "FROM alpine\n",
	})

	entries, _ := packed(t, ContextOptions{Dir: root, Dockerfile: "docker/Dockerfile.prod"})

	if _, ok := entries["docker/Dockerfile.prod"]; !ok {
		t.Errorf("entries = %v", keysOf(entries))
	}
}

// A Dockerfile path that climbs out of the context is not a build we can run,
// and the engine would reject it in a less clear way.
func TestBuildContextRefusesADockerfileOutsideIt(t *testing.T) {
	root := buildTree(t, map[string]string{"Dockerfile": "FROM alpine\n"})

	for _, name := range []string{"../Dockerfile", "/etc/Dockerfile", "a/../../Dockerfile"} {
		if _, err := WriteBuildContext(io.Discard,
			ContextOptions{Dir: root, Dockerfile: name}); err == nil {
			t.Errorf("dockerfile %q was accepted", name)
		}
	}
}

func TestDockerignoreExcludesEntries(t *testing.T) {
	root := buildTree(t, map[string]string{
		"Dockerfile":                 "FROM alpine\n",
		".dockerignore":              "node_modules\n*.log\n.git\n",
		"app.js":                     "console.log(1)\n",
		"debug.log":                  "noise\n",
		"node_modules/left-pad/i.js": "module.exports = 1\n",
		".git/config":                "[core]\n",
	})

	entries, stats := packed(t, ContextOptions{Dir: root})

	if _, ok := entries["app.js"]; !ok {
		t.Error("app.js should have been packed")
	}
	for _, excluded := range []string{"debug.log", "node_modules/left-pad/i.js", ".git/config"} {
		if _, ok := entries[excluded]; ok {
			t.Errorf("%s should have been excluded", excluded)
		}
	}
	if stats.Excluded == 0 {
		t.Error("Excluded was not counted")
	}
}

// The last matching line wins; that ordering is the whole point of the format.
func TestDockerignoreNegationReincludes(t *testing.T) {
	root := buildTree(t, map[string]string{
		"Dockerfile":    "FROM alpine\n",
		".dockerignore": "*.env\n!prod.env\n",
		"dev.env":       "A=1\n",
		"prod.env":      "A=2\n",
	})

	entries, _ := packed(t, ContextOptions{Dir: root})

	if _, ok := entries["dev.env"]; ok {
		t.Error("dev.env should have been excluded")
	}
	if _, ok := entries["prod.env"]; !ok {
		t.Error("prod.env was re-included by ! and should have been packed")
	}
}

func TestDockerignoreMatchesAtAnyDepth(t *testing.T) {
	root := buildTree(t, map[string]string{
		"Dockerfile":            "FROM alpine\n",
		".dockerignore":         "**/*.tmp\n",
		"a/b/scratch.tmp":       "x\n",
		"keep.txt":              "y\n",
		"deep/nested/thing.tmp": "z\n",
	})

	entries, _ := packed(t, ContextOptions{Dir: root})

	if _, ok := entries["keep.txt"]; !ok {
		t.Error("keep.txt should have been packed")
	}
	for _, excluded := range []string{"a/b/scratch.tmp", "deep/nested/thing.tmp"} {
		if _, ok := entries[excluded]; ok {
			t.Errorf("%s should have been excluded", excluded)
		}
	}
}

func TestDockerignoreIgnoresCommentsAndBlanks(t *testing.T) {
	root := buildTree(t, map[string]string{
		"Dockerfile":    "FROM alpine\n",
		".dockerignore": "# a comment\n\n   \nsecret.txt\n",
		"secret.txt":    "shh\n",
		"public.txt":    "ok\n",
	})

	entries, _ := packed(t, ContextOptions{Dir: root})

	if _, ok := entries["secret.txt"]; ok {
		t.Error("secret.txt should have been excluded")
	}
	if _, ok := entries["public.txt"]; !ok {
		t.Error("public.txt should have been packed")
	}
}

// A context bigger than the limit has to fail before the engine is handed
// gigabytes over a socket.
func TestBuildContextRefusesAnOversizedTree(t *testing.T) {
	root := buildTree(t, map[string]string{
		"Dockerfile": "FROM alpine\n",
		"big.bin":    string(make([]byte, 4096)),
	})

	_, err := WriteBuildContext(io.Discard, ContextOptions{Dir: root, MaxBytes: 1024})
	if !errors.Is(err, ErrContextTooLarge) {
		t.Errorf("error = %v, want ErrContextTooLarge", err)
	}
}

// Following a link would silently pull in whatever it points at, including a
// target outside the context.
func TestBuildContextRecordsSymlinksWithoutFollowingThem(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	root := buildTree(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	link := filepath.Join(root, "escape")
	if err := os.Symlink(filepath.Join(outside, "secret"), link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	var buf bytes.Buffer
	if _, err := WriteBuildContext(&buf, ContextOptions{Dir: root}); err != nil {
		t.Fatalf("WriteBuildContext() error = %v", err)
	}

	reader := tar.NewReader(&buf)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if header.Name != "escape" {
			continue
		}
		if header.Typeflag != tar.TypeSymlink {
			t.Errorf("the link was packed as type %c, want a symlink entry", header.Typeflag)
		}
		body, _ := io.ReadAll(reader)
		if string(body) == "private" {
			t.Error("the link's target was followed and its contents packed")
		}
		return
	}
	t.Error("the symlink is missing from the context entirely")
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
