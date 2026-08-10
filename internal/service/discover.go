package service

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/compose"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/store"
)

// LabelComposeConfigFiles is where `docker compose` records the file it
// deployed from. It is the only pointer back to a discovered stack's source.
const LabelComposeConfigFiles = "com.docker.compose.project.config_files"

// LabelComposeWorkingDir is the directory compose ran in.
const LabelComposeWorkingDir = "com.docker.compose.project.working_dir"

// DiscoveredStack is a compose project running on this host that Iskele has no
// record of.
//
// They exist because `docker compose up` on the command line produces exactly
// the same labeled containers Iskele does. Showing them is honest: an operator
// looking at a stack list wants to see what is running, not what this panel
// happens to have created.
type DiscoveredStack struct {
	Name string `json:"name"`
	// Services and Containers count what is running under this project.
	Services   []string `json:"services"`
	Containers int      `json:"containers"`
	Running    int      `json:"running"`
	// ConfigFile is the compose file the CLI deployed from, when it recorded
	// one. It is what an import would read.
	ConfigFile string `json:"config_file,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	// Importable reports whether the compose file is readable and inside
	// allowed_paths, which is what importing needs.
	Importable bool `json:"importable"`
	// Reason explains an unimportable stack.
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// Discover lists compose projects running on this host that are not stacks
// here yet.
func (s *StackService) Discover(ctx context.Context) ([]DiscoveredStack, error) {
	containers, err := s.docker.ListContainers(ctx, docker.ListContainersOptions{All: true})
	if err != nil {
		return nil, err
	}

	known, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}
	recorded := make(map[string]bool, len(known))
	for _, stack := range known {
		recorded[stack.Name] = true
	}

	grouped := map[string][]docker.Container{}
	for _, container := range containers {
		project := container.Labels[compose.LabelComposeProject]
		if project == "" || recorded[project] {
			continue
		}
		grouped[project] = append(grouped[project], container)
	}

	out := make([]DiscoveredStack, 0, len(grouped))
	for project, members := range grouped {
		out = append(out, s.describeDiscovered(project, members))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// describeDiscovered summarizes one discovered project.
func (s *StackService) describeDiscovered(project string, members []docker.Container) DiscoveredStack {
	found := DiscoveredStack{Name: project, Containers: len(members), Services: []string{}}

	seen := map[string]bool{}
	for _, container := range members {
		if service := container.Labels[compose.LabelComposeService]; service != "" && !seen[service] {
			seen[service] = true
			found.Services = append(found.Services, service)
		}
		if strings.EqualFold(container.State, "running") {
			found.Running++
		}
		if found.ConfigFile == "" {
			// The label holds a comma-separated list when several files were
			// merged; the first is the one an import would read.
			if files := container.Labels[LabelComposeConfigFiles]; files != "" {
				found.ConfigFile = strings.TrimSpace(strings.Split(files, ",")[0])
			}
		}
		if found.WorkingDir == "" {
			found.WorkingDir = container.Labels[LabelComposeWorkingDir]
		}
		if found.CreatedAt.IsZero() || container.Created.Before(found.CreatedAt) {
			found.CreatedAt = container.Created
		}
	}
	sort.Strings(found.Services)

	found.Importable, found.Reason = s.importable(found.ConfigFile)
	return found
}

// importable reports whether a discovered stack's compose file can be read.
func (s *StackService) importable(path string) (bool, string) {
	if path == "" {
		return false, "the containers do not record which compose file they came from"
	}
	if err := s.paths.Check(path); err != nil {
		return false, err.Error()
	}
	if _, err := os.Stat(path); err != nil {
		return false, "the compose file is no longer at " + path
	}
	return true, ""
}

// Import adopts a discovered compose project as a stack.
//
// Adoption is a record, not a redeploy: the containers keep running untouched.
// The next `up` is what brings them in line with the file, and that is the
// operator's decision to make.
func (s *StackService) Import(ctx context.Context, name string, actor audit.Actor, meta RequestMeta) (store.Stack, error) {
	stack, err := s.importStack(ctx, name, actor)
	s.auditStack(ctx, actor, meta, "stack.import", stack, err)
	return stack, err
}

func (s *StackService) importStack(ctx context.Context, name string, actor audit.Actor) (store.Stack, error) {
	if _, err := s.stacks.ByName(ctx, name); err == nil {
		return store.Stack{}, errors.New("a stack named " + name + " is already recorded here")
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Stack{}, err
	}

	discovered, err := s.Discover(ctx)
	if err != nil {
		return store.Stack{}, err
	}

	var target *DiscoveredStack
	for i := range discovered {
		if discovered[i].Name == name {
			target = &discovered[i]
		}
	}
	if target == nil {
		return store.Stack{}, ErrStackNotFound
	}
	if !target.Importable {
		return store.Stack{}, errors.New("this stack cannot be imported: " + target.Reason)
	}

	return s.create(ctx, StackInput{
		Name:   name,
		Source: store.StackSourceFile,
		Path:   target.ConfigFile,
	}, actor)
}
