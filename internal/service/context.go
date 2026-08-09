package service

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Build context errors.
var (
	ErrContextTooLarge = errors.New("build context is too large")
	ErrNoDockerfile    = errors.New("no Dockerfile in the build context")
)

// DefaultMaxContextBytes bounds a build context.
//
// A context is read into a tar the engine then receives over a socket, so an
// operator who points the build at their home directory would otherwise send
// gigabytes and wedge the daemon. 512 MB is generous for a real project and
// small enough to fail fast on a mistake.
const DefaultMaxContextBytes int64 = 512 << 20

// ContextOptions controls how a build context is packed.
type ContextOptions struct {
	// Dir is the context root, already checked against the whitelist.
	Dir string
	// Dockerfile is the build file's path relative to Dir. Empty means
	// "Dockerfile".
	Dockerfile string
	// MaxBytes caps the uncompressed context. Zero means the default.
	MaxBytes int64
}

// ContextStats describes what was packed.
type ContextStats struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
	// Excluded counts the entries .dockerignore kept out, which is the number
	// an operator checks when a build sees a file they expected excluded.
	Excluded int `json:"excluded"`
}

// WriteBuildContext packs a directory into a tar stream for the engine.
//
// It streams rather than buffering: a context is by definition a pile of files
// on disk, and holding a copy in memory to send it over a socket would double
// the cost of every build for nothing.
func WriteBuildContext(w io.Writer, opts ContextOptions) (ContextStats, error) {
	var stats ContextStats

	root := filepath.Clean(opts.Dir)
	limit := opts.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxContextBytes
	}

	dockerfile := strings.TrimSpace(opts.Dockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	// The engine takes the Dockerfile path relative to the context root, so a
	// name that climbs out of it is not a build we can run.
	if filepath.IsAbs(dockerfile) || strings.Contains(filepath.ToSlash(dockerfile), "../") {
		return stats, fmt.Errorf("dockerfile %q must be inside the build context", opts.Dockerfile)
	}
	if _, err := os.Stat(filepath.Join(root, dockerfile)); err != nil {
		return stats, fmt.Errorf("%w: %s", ErrNoDockerfile, dockerfile)
	}

	patterns, err := readDockerignore(root)
	if err != nil {
		return stats, err
	}

	tw := tar.NewWriter(w)

	walkErr := filepath.Walk(root, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fullPath == root {
			return nil
		}

		rel, relErr := filepath.Rel(root, fullPath)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(rel)

		if matchesDockerignore(patterns, slashed, info.IsDir()) {
			stats.Excluded++
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		switch {
		case info.Mode().IsDir():
			return writeTarDir(tw, slashed, info)

		case info.Mode().IsRegular():
			if stats.Bytes+info.Size() > limit {
				return fmt.Errorf("%w: over %d bytes at %s", ErrContextTooLarge, limit, slashed)
			}
			written, writeErr := writeTarFile(tw, fullPath, slashed, info)
			if writeErr != nil {
				return writeErr
			}
			stats.Files++
			stats.Bytes += written
			return nil

		case info.Mode()&os.ModeSymlink != 0:
			return writeTarSymlink(tw, fullPath, slashed, info)

		default:
			// Sockets, devices and fifos are not part of a build context and
			// the engine would reject them anyway.
			return nil
		}
	})

	if walkErr != nil {
		// The tar is abandoned; closing it would append a footer to a stream
		// the caller is about to discard.
		return stats, walkErr
	}

	if err := tw.Close(); err != nil {
		return stats, fmt.Errorf("finish build context: %w", err)
	}
	return stats, nil
}

// writeTarDir records a directory entry, which the engine needs for empty
// directories a build expects to exist.
func writeTarDir(tw *tar.Writer, name string, info os.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name + "/"
	return tw.WriteHeader(header)
}

// writeTarFile copies one regular file into the archive.
func writeTarFile(tw *tar.Writer, fullPath, name string, info os.FileInfo) (int64, error) {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return 0, err
	}
	header.Name = name

	if writeErr := tw.WriteHeader(header); writeErr != nil {
		return 0, writeErr
	}

	file, err := os.Open(fullPath) //nolint:gosec // the path came from a whitelisted walk
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	written, err := io.Copy(tw, file)
	if err != nil {
		return written, fmt.Errorf("read %s: %w", name, err)
	}
	// A file that grew between the walk and the copy would desynchronize the
	// tar, so the mismatch is reported rather than shipped.
	if written != info.Size() {
		return written, fmt.Errorf("%s changed while the context was being read", name)
	}
	return written, nil
}

// writeTarSymlink records a link as a link rather than following it.
//
// Following would silently pull in whatever it points at — including a target
// outside the context, which is precisely what a build must not be able to
// reach through a directory the operator chose.
func writeTarSymlink(tw *tar.Writer, fullPath, name string, info os.FileInfo) error {
	target, err := os.Readlink(fullPath)
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, target)
	if err != nil {
		return err
	}
	header.Name = name
	return tw.WriteHeader(header)
}

// readDockerignore loads the exclusion patterns from the context root.
func readDockerignore(root string) ([]ignorePattern, error) {
	file, err := os.Open(filepath.Join(root, ".dockerignore")) //nolint:gosec // inside the whitelisted context
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .dockerignore: %w", err)
	}
	defer func() { _ = file.Close() }()

	var patterns []ignorePattern
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negated := strings.HasPrefix(line, "!")
		if negated {
			line = strings.TrimSpace(line[1:])
			if line == "" {
				continue
			}
		}

		patterns = append(patterns, ignorePattern{
			pattern: path.Clean(filepath.ToSlash(line)),
			negated: negated,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read .dockerignore: %w", err)
	}
	return patterns, nil
}

// ignorePattern is one .dockerignore line.
type ignorePattern struct {
	pattern string
	negated bool
}

// matchesDockerignore reports whether an entry is excluded.
//
// The last matching pattern wins, which is what makes `!keep.me` after `*`
// work — the ordering is the whole point of the format.
func matchesDockerignore(patterns []ignorePattern, name string, isDir bool) bool {
	excluded := false

	for _, p := range patterns {
		if !ignoreMatches(p.pattern, name, isDir) {
			continue
		}
		excluded = !p.negated
	}
	return excluded
}

// ignoreMatches reports whether one pattern covers a path.
func ignoreMatches(pattern, name string, isDir bool) bool {
	if ok, err := path.Match(pattern, name); err == nil && ok {
		return true
	}

	// A pattern naming a directory excludes everything under it, which is how
	// "node_modules" keeps out node_modules/foo/bar.
	if strings.HasPrefix(name, pattern+"/") {
		return true
	}

	// "**/" matches at any depth. path.Match has no such concept, so the
	// prefix is stripped and the rest matched against the base name.
	if rest, found := strings.CutPrefix(pattern, "**/"); found {
		if ok, err := path.Match(rest, path.Base(name)); err == nil && ok {
			return true
		}
		if strings.HasSuffix(name, "/"+rest) {
			return true
		}
	}

	// A directory pattern written with a trailing slash.
	if isDir && strings.HasSuffix(pattern, "/") {
		if ok, err := path.Match(strings.TrimSuffix(pattern, "/"), name); err == nil && ok {
			return true
		}
	}

	return false
}
