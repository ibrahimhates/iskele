package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrNotADirectory reports a browse request aimed at a file.
var ErrNotADirectory = errors.New("not a directory")

// maxBrowseEntries bounds one listing. A directory with a hundred thousand
// files would otherwise produce a response no browser can render and a
// serialization pass that pins a core.
const maxBrowseEntries = 2000

// DirEntry is one row of a directory listing.
type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	// Size is 0 for directories, which the engine does not measure either.
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	// Symlink reports an entry that points elsewhere. It is shown but never
	// followed out of the whitelist: Browse checks the target, so a link to
	// /etc simply does not open.
	Symlink bool `json:"symlink"`
}

// Listing is a browsable directory.
type Listing struct {
	Path string `json:"path"`
	// Parent is empty at an allowed root: there is nothing above it the
	// operator may see.
	Parent  string     `json:"parent,omitempty"`
	Entries []DirEntry `json:"entries"`
	// Truncated reports that the directory held more than maxBrowseEntries.
	Truncated bool `json:"truncated"`
	// Dockerfiles names the build files found here, so the UI can offer them
	// without a second round trip.
	Dockerfiles []string `json:"dockerfiles"`
	// AllowedRoots is the whitelist, so a client that browses straight to a
	// path can show where it may go instead.
	AllowedRoots []string `json:"allowed_roots"`
}

// Browser lists host directories inside the configured whitelist.
//
// It shares [PathGuard] with bind mounts on purpose: browsing and mounting are
// the same trust boundary seen from two sides, and a second implementation
// would be a second thing to get wrong.
type Browser struct {
	paths *PathGuard
}

// NewBrowser builds the directory browser.
func NewBrowser(paths *PathGuard) *Browser { return &Browser{paths: paths} }

// Roots returns the directories browsing may start from.
func (b *Browser) Roots() []string { return b.paths.Allowed() }

// Browse lists one directory.
//
// An empty path is answered with the whitelist itself rather than an error:
// that is what a UI opening the picker for the first time asks for.
func (b *Browser) Browse(_ context.Context, path string) (Listing, error) {
	roots := b.paths.Allowed()

	if strings.TrimSpace(path) == "" {
		return b.rootListing(roots), nil
	}
	if err := b.paths.Check(path); err != nil {
		return Listing{}, err
	}

	clean := filepath.Clean(strings.TrimSpace(path))
	// Resolve before reading, so the listing and the whitelist check agree on
	// which directory this is.
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	}

	info, err := os.Stat(clean)
	if err != nil {
		return Listing{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !info.IsDir() {
		return Listing{}, fmt.Errorf("%s: %w", path, ErrNotADirectory)
	}

	entries, err := os.ReadDir(clean)
	if err != nil {
		return Listing{}, fmt.Errorf("read %s: %w", path, err)
	}

	listing := Listing{
		Path:         clean,
		Parent:       b.parentOf(clean),
		Entries:      make([]DirEntry, 0, len(entries)),
		Dockerfiles:  []string{},
		AllowedRoots: roots,
	}

	if len(entries) > maxBrowseEntries {
		entries = entries[:maxBrowseEntries]
		listing.Truncated = true
	}

	for _, entry := range entries {
		row := DirEntry{
			Name:    entry.Name(),
			Path:    filepath.Join(clean, entry.Name()),
			IsDir:   entry.IsDir(),
			Symlink: entry.Type()&fs.ModeSymlink != 0,
		}

		// A failed stat is not worth dropping the row: the operator still
		// wants to see that the entry exists.
		if entryInfo, statErr := entry.Info(); statErr == nil {
			row.ModTime = entryInfo.ModTime().UTC()
			if !entry.IsDir() {
				row.Size = entryInfo.Size()
			}
		}

		// A symlink to a directory reports as a link, not a directory, so
		// browsing into it needs the resolved kind.
		if row.Symlink {
			if target, statErr := os.Stat(row.Path); statErr == nil {
				row.IsDir = target.IsDir()
			}
		}

		if !row.IsDir && looksLikeDockerfile(row.Name) {
			listing.Dockerfiles = append(listing.Dockerfiles, row.Name)
		}

		listing.Entries = append(listing.Entries, row)
	}

	// Directories first, then by name: the order an operator navigating with
	// the keyboard expects.
	sort.SliceStable(listing.Entries, func(i, j int) bool {
		if listing.Entries[i].IsDir != listing.Entries[j].IsDir {
			return listing.Entries[i].IsDir
		}
		return listing.Entries[i].Name < listing.Entries[j].Name
	})
	sort.Strings(listing.Dockerfiles)

	return listing, nil
}

// rootListing presents the whitelist as a directory of its own.
func (b *Browser) rootListing(roots []string) Listing {
	listing := Listing{
		Entries:      make([]DirEntry, 0, len(roots)),
		Dockerfiles:  []string{},
		AllowedRoots: roots,
	}

	for _, root := range roots {
		row := DirEntry{Name: root, Path: root, IsDir: true}
		if info, err := os.Stat(root); err == nil {
			row.ModTime = info.ModTime().UTC()
		}
		listing.Entries = append(listing.Entries, row)
	}
	return listing
}

// parentOf returns the directory above path, or "" when path is an allowed
// root — there is nothing above it this installation permits.
func (b *Browser) parentOf(path string) string {
	for _, root := range b.paths.Allowed() {
		if path == root {
			return ""
		}
	}

	parent := filepath.Dir(path)
	if parent == path {
		return ""
	}
	if err := b.paths.Check(parent); err != nil {
		return ""
	}
	return parent
}

// looksLikeDockerfile reports the names an operator would expect to be offered
// as a build file.
func looksLikeDockerfile(name string) bool {
	lower := strings.ToLower(name)
	return lower == "dockerfile" ||
		strings.HasPrefix(lower, "dockerfile.") ||
		strings.HasSuffix(lower, ".dockerfile")
}
