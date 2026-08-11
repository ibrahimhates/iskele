package templates

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// builtin holds the templates shipped inside the binary.
//
// Embedded rather than installed as files: the catalog has to work on a
// machine with no network and no package manager, and a panel whose app list
// depends on somebody having copied a directory is a panel that is empty on
// half the installs.
//
//go:embed catalog/*.json
var builtin embed.FS

// ErrNotFound reports a template that is not in the catalog.
var ErrNotFound = errors.New("template not found")

// Catalog is the set of templates this installation offers.
type Catalog struct {
	mu        sync.RWMutex
	templates map[string]*Template
	order     []string
	// problems records the custom files that would not load, so an operator
	// who wrote one can be told why rather than wonder where it went.
	problems []LoadProblem
}

// LoadProblem is a template file that could not be used.
type LoadProblem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Load builds a catalog from the embedded entries plus a custom directory.
//
// A broken custom file is reported, not fatal: one operator's typo must not
// take the whole catalog — including every shipped entry — off the screen.
func Load(customDir string, log *slog.Logger) (*Catalog, error) {
	catalog := &Catalog{templates: map[string]*Template{}, problems: []LoadProblem{}}

	if err := catalog.loadBuiltin(); err != nil {
		// A shipped template that fails its own schema is a build-time bug.
		return nil, err
	}
	catalog.loadCustom(customDir, log)
	catalog.sort()

	return catalog, nil
}

// loadBuiltin reads the embedded catalog.
func (c *Catalog) loadBuiltin() error {
	entries, err := fs.ReadDir(builtin, "catalog")
	if err != nil {
		return fmt.Errorf("read the embedded catalog: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		content, readErr := builtin.ReadFile(filepath.Join("catalog", entry.Name()))
		if readErr != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), readErr)
		}

		template, parseErr := parse(content, SourceBuiltin)
		if parseErr != nil {
			return fmt.Errorf("embedded template %s: %w", entry.Name(), parseErr)
		}
		c.templates[template.ID] = template
	}

	return nil
}

// loadCustom reads an operator's own templates, overriding shipped ones by id.
//
// Overriding is deliberate: pinning a version or changing a default for the
// whole installation should not mean forking the binary.
func (c *Catalog) loadCustom(dir string, log *slog.Logger) {
	if strings.TrimSpace(dir) == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		// The directory is optional; not having one is the normal case.
		return
	}
	if err != nil {
		c.problems = append(c.problems, LoadProblem{Path: dir, Message: err.Error()})
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())

		content, readErr := os.ReadFile(path) //nolint:gosec // an operator's own configured directory
		if readErr != nil {
			c.problems = append(c.problems, LoadProblem{Path: path, Message: readErr.Error()})
			continue
		}

		template, parseErr := parse(content, SourceCustom)
		if parseErr != nil {
			c.problems = append(c.problems, LoadProblem{Path: path, Message: parseErr.Error()})
			if log != nil {
				log.Warn("ignoring an unusable template",
					slog.String("path", path), slog.Any("error", parseErr))
			}
			continue
		}

		c.templates[template.ID] = template
	}
}

// parse decodes and validates one template file.
func parse(content []byte, source string) (*Template, error) {
	var template Template

	decoder := json.NewDecoder(strings.NewReader(string(content)))
	// An unknown key is almost always a typo in a field name, and silently
	// ignoring it produces a template that is subtly not what was written.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&template); err != nil {
		return nil, fmt.Errorf("not a valid template file: %w", err)
	}

	template.Source = source
	if err := template.Validate(); err != nil {
		return nil, err
	}
	return &template, nil
}

// sort orders the catalog by category then title, which is how it is shown.
func (c *Catalog) sort() {
	c.order = make([]string, 0, len(c.templates))
	for id := range c.templates {
		c.order = append(c.order, id)
	}

	sort.Slice(c.order, func(i, j int) bool {
		left, right := c.templates[c.order[i]], c.templates[c.order[j]]
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		return left.Title < right.Title
	})
}

// List returns every template, ordered by category then title.
func (c *Catalog) List() []Template {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]Template, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, *c.templates[id])
	}
	return out
}

// Get returns one template by id.
func (c *Catalog) Get(id string) (Template, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	template, ok := c.templates[id]
	if !ok {
		return Template{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return *template, nil
}

// Categories returns the categories in use, alphabetically.
func (c *Catalog) Categories() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := map[string]bool{}
	out := make([]string, 0, 8)
	for _, template := range c.templates {
		if !seen[template.Category] {
			seen[template.Category] = true
			out = append(out, template.Category)
		}
	}
	sort.Strings(out)
	return out
}

// Problems returns the custom files that could not be loaded.
func (c *Catalog) Problems() []LoadProblem {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]LoadProblem, len(c.problems))
	copy(out, c.problems)
	return out
}

// Len is how many templates the catalog holds.
func (c *Catalog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.templates)
}
