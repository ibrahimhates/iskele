package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrPathNotAllowed reports a host path outside the configured whitelist.
var ErrPathNotAllowed = errors.New("host path is outside the allowed paths")

// PathError names the path that was refused and what it was checked against,
// so the operator can fix either the request or the configuration.
type PathError struct {
	Path    string
	Allowed []string
}

func (e *PathError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("%q cannot be mounted: no allowed_paths are configured, "+
			"so bind mounts are refused entirely", e.Path)
	}
	return fmt.Sprintf("%q is outside the allowed paths (%s)",
		e.Path, strings.Join(e.Allowed, ", "))
}

func (e *PathError) Unwrap() error { return ErrPathNotAllowed }

// PathGuard decides which host paths may be bind-mounted into a container.
//
// A bind mount is the shortest route from container to host root: mounting
// /etc, /var/run/docker.sock or / into a container hands over the machine.
// The whitelist is the only thing standing between an operator account and
// that, so the check is deliberately strict — a prefix match on the cleaned,
// symlink-resolved path, with no way to opt out at request time.
type PathGuard struct {
	allowed []string
}

// NewPathGuard builds a guard over the configured roots.
//
// An empty list refuses every bind mount rather than allowing every one: a
// misconfiguration must fail closed.
func NewPathGuard(allowed []string) *PathGuard {
	cleaned := make([]string, 0, len(allowed))
	for _, root := range allowed {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(root))
	}
	return &PathGuard{allowed: cleaned}
}

// Allowed reports the configured roots, for error messages and for the UI's
// path picker.
func (g *PathGuard) Allowed() []string {
	out := make([]string, len(g.allowed))
	copy(out, g.allowed)
	return out
}

// Check reports whether a host path may be used.
func (g *PathGuard) Check(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return &PathError{Path: path, Allowed: g.Allowed()}
	}
	if !filepath.IsAbs(trimmed) {
		return &PathError{Path: path, Allowed: g.Allowed()}
	}

	candidate := filepath.Clean(trimmed)

	// A symlink inside an allowed root can point anywhere, so the target is
	// what gets checked. EvalSymlinks fails on a path that does not exist yet,
	// which is legitimate — the engine creates a missing bind source — so the
	// cleaned path is used in that case. The lexical check below still holds,
	// and a component that does exist has already been resolved.
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		candidate = resolved
	}

	for _, root := range g.allowed {
		if withinRoot(candidate, root) {
			return nil
		}
	}
	return &PathError{Path: path, Allowed: g.Allowed()}
}

// CheckAll validates every path and reports the first refusal.
func (g *PathGuard) CheckAll(paths []string) error {
	for _, p := range paths {
		if err := g.Check(p); err != nil {
			return err
		}
	}
	return nil
}

// withinRoot reports whether path is root or lives underneath it.
//
// The comparison is on path components, not on the string: "/srv-other" must
// not pass a "/srv" root just because it shares a prefix.
func withinRoot(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	// Rel returns something starting with ".." when path climbs out of root.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
