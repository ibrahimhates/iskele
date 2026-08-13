package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/compose"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Stack errors.
var (
	ErrStackNotFound = errors.New("stack not found")
	ErrStackBusy     = errors.New("this stack is already being deployed")
	ErrNoSuchService = errors.New("no such service in this stack")
	ErrComposeSource = errors.New("this stack's compose file could not be read")
)

// LabelConfigHash records what a container was created from.
//
// It is how a redeploy tells "this service is already running exactly this"
// from "this service changed": compose does the same, under the same label, so
// a stack the CLI deployed is judged the same way.
const LabelConfigHash = "com.docker.compose.config-hash"

// StackService manages compose stacks.
//
// It is the only place that turns a compose file into containers, and it goes
// through the same two gates the create wizard does — the path whitelist and
// the privileged-option permission. A compose file must not be a way around
// either: `privileged: true` in YAML is the same request as ticking the box.
type StackService struct {
	docker docker.Client
	stacks *store.StackRepo
	// registries supplies the credentials a private image needs.
	registries *Registry
	builder    *Builder
	paths      *PathGuard
	tasks      *TaskRegistry
	recorder   *audit.Recorder
	// stateDir is where each stack's working copy lives.
	stateDir string
}

// NewStackService builds the stack service.
func NewStackService(client docker.Client, stacks *store.StackRepo, registries *Registry,
	builder *Builder, paths *PathGuard, tasks *TaskRegistry, recorder *audit.Recorder,
	stateDir string,
) *StackService {
	return &StackService{
		docker:     client,
		stacks:     stacks,
		registries: registries,
		builder:    builder,
		paths:      paths,
		tasks:      tasks,
		recorder:   recorder,
		stateDir:   stateDir,
	}
}

// StackInput is what an operator submits when creating or editing a stack.
type StackInput struct {
	Name    string            `json:"name"`
	Source  store.StackSource `json:"source"`
	Compose string            `json:"compose"`
	Env     string            `json:"env"`
	// Path is the compose file for a file-backed stack.
	Path string `json:"path,omitempty"`
	// GitURL and GitRef describe a git-backed stack.
	GitURL string `json:"git_url,omitempty"`
	GitRef string `json:"git_ref,omitempty"`
}

// StackDetail is a stack plus what the engine says about it right now.
type StackDetail struct {
	store.Stack
	// Services is the stack's services with their live containers.
	Services []ServiceStatus `json:"services"`
	// Warnings are the compose fields Iskele will not act on.
	Warnings []compose.Warning `json:"warnings"`
	// ParseError is set when the stored compose file no longer parses, which
	// is possible after an edit: the stack is still listed, and still says why.
	ParseError string `json:"parse_error,omitempty"`
	// EngineError is set when the daemon could not be reached. The stack's
	// definition is still returned — an operator whose Docker is down still
	// needs to read and fix their compose file.
	EngineError string `json:"engine_error,omitempty"`
}

// MarshalJSON flattens a stack and its live state into one object.
//
// It has to be written by hand: [store.Stack] carries its own MarshalJSON, and
// an embedded type's method is promoted to the outer struct — so the default
// encoding of this type would be the stack alone, silently dropping every
// field below it.
func (d StackDetail) MarshalJSON() ([]byte, error) {
	extra := map[string]any{
		"services": d.Services,
		"warnings": d.Warnings,
	}
	if d.ParseError != "" {
		extra["parse_error"] = d.ParseError
	}
	if d.EngineError != "" {
		extra["engine_error"] = d.EngineError
	}
	return marshalMerged(d.Stack, extra)
}

// ServiceStatus is one service and the containers it currently has.
type ServiceStatus struct {
	Name string `json:"name"`
	// Replicas is what the compose file asks for.
	Replicas int `json:"replicas"`
	// Running counts the containers actually up.
	Running    int                `json:"running"`
	Image      string             `json:"image"`
	Ports      []docker.Port      `json:"ports,omitempty"`
	Containers []docker.Container `json:"containers"`
	// Drifted reports a container running a configuration that no longer
	// matches the compose file, which is what `up` would replace.
	Drifted bool `json:"drifted"`
}

// Validate parses a stack's content without deploying it.
//
// It is the same work `up` does before touching the engine, which is the point:
// the editor's "validate" button and a deploy agree, because they run the same
// code.
func (s *StackService) Validate(ctx context.Context, in StackInput, privileged bool) (ValidationReport, error) {
	report := ValidationReport{Warnings: []compose.Warning{}, Problems: []Problem{}}

	project, parseWarnings, err := compose.Parse(ctx, compose.Input{
		Name:       in.Name,
		Compose:    in.Compose,
		Env:        in.Env,
		WorkingDir: s.workingDirFor(in),
	})
	report.Warnings = append(report.Warnings, parseWarnings...)
	if err != nil {
		report.Valid = false
		report.Error = err.Error()
		return report, nil
	}

	order, err := compose.ServiceOrder(ctx, project)
	if err != nil {
		report.Valid = false
		report.Error = err.Error()
		return report, nil
	}

	conversion, err := compose.Convert(project, order)
	if err != nil {
		report.Valid = false
		report.Error = err.Error()
		return report, nil
	}
	report.Warnings = append(report.Warnings, conversion.Warnings...)

	for _, plan := range conversion.Services {
		report.Services = append(report.Services, plan.Name)
		report.Problems = append(report.Problems, s.problemsFor(plan, privileged)...)
	}

	report.Valid = len(report.Problems) == 0
	return report, nil
}

// ValidationReport is what the editor gets back.
type ValidationReport struct {
	Valid bool `json:"valid"`
	// Error is a file that would not parse at all.
	Error    string            `json:"error,omitempty"`
	Services []string          `json:"services,omitempty"`
	Warnings []compose.Warning `json:"warnings"`
	// Problems are the reasons this stack would be refused: a path outside
	// the whitelist, an option the caller may not set.
	Problems []Problem `json:"problems"`
}

// Problem is one reason a stack cannot be deployed.
type Problem struct {
	Service string `json:"service"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// problemsFor reports what would stop a service from being created.
//
// The checks are the create service's own, run early so the editor can show
// all of them at once rather than one per failed deploy.
func (s *StackService) problemsFor(plan compose.ServicePlan, privileged bool) []Problem {
	var problems []Problem

	for _, source := range docker.BindSources(plan.Spec) {
		if err := s.paths.Check(source); err != nil {
			problems = append(problems, Problem{
				Service: plan.Name,
				Field:   "volumes",
				Message: err.Error(),
			})
		}
	}

	if plan.Build != nil {
		if err := s.paths.Check(plan.Build.Context); err != nil {
			problems = append(problems, Problem{
				Service: plan.Name,
				Field:   "build.context",
				Message: err.Error(),
			})
		}
	}

	if err := checkPrivilegedOptions(plan.Spec, privileged); err != nil {
		problems = append(problems, Problem{
			Service: plan.Name,
			Field:   "security",
			Message: err.Error(),
		})
	}

	if _, err := docker.BuildCreateSpec(plan.Spec); err != nil {
		var specErr *docker.SpecError
		field := "service"
		if errors.As(err, &specErr) {
			field = specErr.Field
		}
		problems = append(problems, Problem{Service: plan.Name, Field: field, Message: err.Error()})
	}

	return problems
}

// Create records a new stack.
func (s *StackService) Create(ctx context.Context, in StackInput, actor audit.Actor, meta RequestMeta) (store.Stack, error) {
	stack, err := s.create(ctx, in, actor)
	s.auditStack(ctx, actor, meta, "stack.create", stack, err)
	return stack, err
}

func (s *StackService) create(ctx context.Context, in StackInput, actor audit.Actor) (store.Stack, error) {
	name := normalizeStackName(in.Name)
	if name == "" {
		return store.Stack{}, &docker.SpecError{Field: "name", Message: "a stack needs a name"}
	}
	if !in.Source.Valid() {
		return store.Stack{}, &docker.SpecError{
			Field:   "source",
			Message: fmt.Sprintf("%q is not a stack source; use editor, file or git", in.Source),
		}
	}

	id, err := auth.NewID()
	if err != nil {
		return store.Stack{}, err
	}

	stack := store.Stack{
		ID:          id,
		Name:        name,
		Source:      in.Source,
		Path:        strings.TrimSpace(in.Path),
		GitURL:      strings.TrimSpace(in.GitURL),
		GitRef:      strings.TrimSpace(in.GitRef),
		Compose:     in.Compose,
		Env:         in.Env,
		CreatedBy:   actor.Username,
		CreatedByID: actor.UserID,
		Status:      store.StackCreated,
	}
	stack.WorkingDir = s.stackDir(id)

	if err := s.loadSource(ctx, &stack); err != nil {
		return store.Stack{}, err
	}
	if err := s.stacks.Create(ctx, &stack); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return store.Stack{}, fmt.Errorf("a stack named %q already exists: %w", name, err)
		}
		return store.Stack{}, err
	}

	return stack, nil
}

// Update replaces a stack's content.
func (s *StackService) Update(ctx context.Context, id string, in StackInput,
	actor audit.Actor, meta RequestMeta,
) (store.Stack, error) {
	stack, err := s.update(ctx, id, in)
	s.auditStack(ctx, actor, meta, "stack.update", stack, err)
	return stack, err
}

func (s *StackService) update(ctx context.Context, id string, in StackInput) (store.Stack, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return store.Stack{}, err
	}

	if in.Source != "" && in.Source != stack.Source {
		if !in.Source.Valid() {
			return store.Stack{}, &docker.SpecError{
				Field:   "source",
				Message: fmt.Sprintf("%q is not a stack source", in.Source),
			}
		}
		stack.Source = in.Source
	}

	stack.Compose = in.Compose
	stack.Env = in.Env
	if in.Path != "" {
		stack.Path = strings.TrimSpace(in.Path)
	}
	if in.GitURL != "" {
		stack.GitURL = strings.TrimSpace(in.GitURL)
	}
	if in.GitRef != "" {
		stack.GitRef = strings.TrimSpace(in.GitRef)
	}
	if stack.WorkingDir == "" {
		stack.WorkingDir = s.stackDir(stack.ID)
	}

	if err := s.loadSource(ctx, &stack); err != nil {
		return store.Stack{}, err
	}
	if err := s.stacks.Update(ctx, &stack); err != nil {
		return store.Stack{}, err
	}
	return stack, nil
}

// loadSource fills in the compose content for a stack that has it elsewhere.
//
// The content is copied into the record rather than read at deploy time: what
// ran has to stay knowable after the file on disk moves on.
func (s *StackService) loadSource(ctx context.Context, stack *store.Stack) error {
	switch stack.Source {
	case store.StackSourceEditor:
		if strings.TrimSpace(stack.Compose) == "" {
			return &docker.SpecError{Field: "compose", Message: "an editor stack needs a compose file"}
		}
		return nil

	case store.StackSourceFile:
		return s.loadFromFile(stack)

	case store.StackSourceGit:
		return s.loadFromGit(ctx, stack)

	default:
		return &docker.SpecError{Field: "source", Message: "unknown stack source"}
	}
}

// loadFromFile reads a compose file from this host.
func (s *StackService) loadFromFile(stack *store.Stack) error {
	path := strings.TrimSpace(stack.Path)
	if path == "" {
		return &docker.SpecError{Field: "path", Message: "a file stack needs a compose file path"}
	}
	if err := s.paths.Check(path); err != nil {
		return err
	}

	content, err := os.ReadFile(path) //nolint:gosec // the path was just checked against the whitelist
	if err != nil {
		return fmt.Errorf("%w: %s", ErrComposeSource, err)
	}
	stack.Compose = string(content)

	// A compose file's relative paths resolve against its own directory, which
	// is what an operator editing that file expects.
	stack.WorkingDir = filepath.Dir(path)

	// An .env beside the compose file is compose's own convention.
	envPath := filepath.Join(filepath.Dir(path), ".env")
	if envContent, envErr := os.ReadFile(envPath); envErr == nil { //nolint:gosec // same directory, same whitelist
		stack.Env = string(envContent)
	}

	return nil
}

// Get returns one stack record.
func (s *StackService) Get(ctx context.Context, id string) (store.Stack, error) {
	stack, err := s.stacks.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Stack{}, ErrStackNotFound
	}
	return stack, err
}

// List returns every stack.
//
// The env content is stripped: it routinely holds database passwords, and a
// listing is the one response most likely to be logged or cached.
func (s *StackService) List(ctx context.Context) ([]store.Stack, error) {
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range stacks {
		stacks[i].Env = ""
	}
	return stacks, nil
}

// Detail returns a stack with what the engine currently reports for it.
func (s *StackService) Detail(ctx context.Context, id string) (StackDetail, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return StackDetail{}, err
	}

	detail := StackDetail{Stack: stack, Warnings: []compose.Warning{}, Services: []ServiceStatus{}}

	conversion, warnings, convertErr := s.plan(ctx, stack)
	detail.Warnings = warnings
	if convertErr != nil {
		// A stack whose file stopped parsing is still a stack: it has
		// containers, and the operator needs to be told why it will not deploy.
		detail.ParseError = convertErr.Error()
		return detail, nil
	}

	// A stack read is not a Docker operation first: the compose file, its
	// services and its warnings are all knowable without the engine. Failing
	// the whole request when the daemon is down would leave an operator unable
	// to even look at the file they need to fix.
	containers, err := s.containersOf(ctx, stack.Name)
	if err != nil {
		detail.EngineError = docker.Message(err)
		containers = nil
	}

	detail.Services = s.statusFor(conversion, containers)
	return detail, nil
}

// plan parses and converts a stored stack.
func (s *StackService) plan(ctx context.Context, stack store.Stack) (compose.Conversion, []compose.Warning, error) {
	project, warnings, err := compose.Parse(ctx, compose.Input{
		Name:       stack.Name,
		Compose:    stack.Compose,
		Env:        stack.Env,
		WorkingDir: stack.WorkingDir,
	})
	if err != nil {
		return compose.Conversion{}, warnings, err
	}

	order, err := compose.ServiceOrder(ctx, project)
	if err != nil {
		return compose.Conversion{}, warnings, err
	}

	conversion, err := compose.Convert(project, order)
	if err != nil {
		return compose.Conversion{}, warnings, err
	}

	return conversion, append(warnings, conversion.Warnings...), nil
}

// statusFor matches a conversion against the containers that exist.
func (s *StackService) statusFor(conversion compose.Conversion, containers []docker.Container) []ServiceStatus {
	byService := map[string][]docker.Container{}
	for _, container := range containers {
		byService[container.Labels[compose.LabelService]] = append(
			byService[container.Labels[compose.LabelService]], container)
	}

	out := make([]ServiceStatus, 0, len(conversion.Services))
	for _, plan := range conversion.Services {
		status := ServiceStatus{
			Name:       plan.Name,
			Replicas:   plan.Replicas,
			Image:      plan.Spec.Image,
			Containers: byService[plan.Name],
		}
		if status.Containers == nil {
			status.Containers = []docker.Container{}
		}

		hash := configHash(plan.Spec)
		for _, container := range status.Containers {
			if strings.EqualFold(container.State, "running") {
				status.Running++
			}
			if container.Labels[LabelConfigHash] != hash {
				status.Drifted = true
			}
			status.Ports = append(status.Ports, container.Ports...)
		}
		if len(status.Containers) != plan.Replicas {
			status.Drifted = true
		}

		out = append(out, status)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// containersOf lists every container belonging to a stack, running or not.
func (s *StackService) containersOf(ctx context.Context, name string) ([]docker.Container, error) {
	return s.docker.ListContainers(ctx, docker.ListContainersOptions{
		All:   true,
		Label: []string{compose.LabelComposeProject + "=" + name},
	})
}

// Delete removes a stack record.
func (s *StackService) Delete(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	err = s.stacks.Delete(ctx, id)
	s.auditStack(ctx, actor, meta, "stack.delete", stack, err)
	if err != nil {
		return err
	}

	// The working copy goes with the record; the containers do not. Removing
	// them is `down`, and an operator who already stopped them by hand should
	// not have this bring anything back.
	if stack.WorkingDir != "" && strings.HasPrefix(stack.WorkingDir, s.stateDir) {
		_ = os.RemoveAll(stack.WorkingDir)
	}
	return nil
}

// stackDir is where a stack's working copy lives.
func (s *StackService) stackDir(id string) string {
	if s.stateDir == "" {
		return ""
	}
	return filepath.Join(s.stateDir, id)
}

// workingDirFor picks the directory a not-yet-saved stack validates against.
func (s *StackService) workingDirFor(in StackInput) string {
	if in.Source == store.StackSourceFile && in.Path != "" {
		return filepath.Dir(in.Path)
	}
	return s.stateDir
}

// auditStack records a stack lifecycle event.
func (s *StackService) auditStack(ctx context.Context, actor audit.Actor, meta RequestMeta,
	action string, stack store.Stack, err error,
) {
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       action,
		ResourceType: "stack",
		ResourceID:   stack.ID,
		Err:          err,
		Detail: map[string]any{
			"name":   stack.Name,
			"source": string(stack.Source),
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
}

// configHash fingerprints what a container was created from.
//
// Two containers with the same hash were created from the same definition, so
// `up` can leave one alone instead of recreating it — which is the difference
// between a redeploy that restarts one changed service and one that restarts
// everything.
func configHash(spec docker.ContainerSpec) string {
	// The name is excluded: replica 1 and replica 2 of a service are the same
	// definition, and a hash that disagreed would recreate both every time.
	copy := spec
	copy.Name = ""
	copy.Labels = withoutHashLabels(spec.Labels)

	encoded, err := json.Marshal(copy)
	if err != nil {
		// Marshaling a struct of plain types cannot fail; a hash of nothing
		// would still be stable, and treating it as changed is the safe way to
		// be wrong.
		return ""
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// withoutHashLabels drops the labels that are derived rather than declared, so
// the hash does not depend on itself.
func withoutHashLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}

	out := make(map[string]string, len(labels))
	for key, value := range labels {
		if key == LabelConfigHash || key == compose.LabelReplica || key == compose.LabelComposeNumber {
			continue
		}
		out[key] = value
	}
	return out
}

// normalizeStackName trims a name to what a compose project and a container
// name can both carry.
func normalizeStackName(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))

	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

// stackTimeout bounds one lifecycle operation.
//
// A deploy that pulls several images over a slow link is legitimately slow;
// one that has not finished in this long is stuck.
const stackTimeout = 30 * time.Minute

// loadFromGit clones or updates a git-backed stack and reads its compose file.
func (s *StackService) loadFromGit(ctx context.Context, stack *store.Stack) error {
	if err := compose.CheckGitURL(stack.GitURL); err != nil {
		return err
	}
	if stack.WorkingDir == "" {
		return &docker.SpecError{Field: "source", Message: "a git stack needs a working directory"}
	}

	result, err := compose.Checkout(ctx, compose.GitSource{
		URL: stack.GitURL,
		Ref: stack.GitRef,
		Dir: stack.WorkingDir,
	})
	if err != nil {
		return fmt.Errorf("%w: %s", ErrComposeSource, err)
	}
	stack.GitCommit = result.Commit
	stack.GitRef = result.Ref

	path := strings.TrimSpace(stack.Path)
	if path == "" {
		if path, err = compose.ComposeFileIn(stack.WorkingDir); err != nil {
			return fmt.Errorf("%w: %s", ErrComposeSource, err)
		}
	} else {
		// A path inside the repository, not an absolute one: the repository is
		// Iskele's own working copy, and reaching outside it with `../` would
		// step around the whitelist that a file stack goes through.
		if filepath.IsAbs(path) || strings.Contains(filepath.ToSlash(path), "../") {
			return &docker.SpecError{
				Field:   "path",
				Message: "a git stack's compose path must be inside the repository",
			}
		}
		path = filepath.Join(stack.WorkingDir, path)
	}

	content, err := os.ReadFile(path) //nolint:gosec // path is confined to the stack's own working copy
	if err != nil {
		return fmt.Errorf("%w: %s", ErrComposeSource, err)
	}
	stack.Compose = string(content)

	// An .env committed beside the compose file is compose's own convention.
	// The stack's stored .env wins: it is where an operator puts the values
	// that must not be in a repository.
	if strings.TrimSpace(stack.Env) == "" {
		if envContent, envErr := os.ReadFile(filepath.Join(filepath.Dir(path), ".env")); envErr == nil { //nolint:gosec // same working copy
			stack.Env = string(envContent)
		}
	}

	return nil
}

// Diff reports what saving and deploying the submitted content would do to a
// stack as it currently stands.
func (s *StackService) Diff(ctx context.Context, id string, in StackInput) (compose.Diff, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return compose.Diff{}, err
	}

	workingDir := stack.WorkingDir
	if workingDir == "" {
		workingDir = s.stateDir
	}

	return compose.Compare(ctx,
		compose.Input{
			Name:       stack.Name,
			Compose:    stack.Compose,
			Env:        stack.Env,
			WorkingDir: workingDir,
		},
		compose.Input{
			Name:       stack.Name,
			Compose:    in.Compose,
			Env:        in.Env,
			WorkingDir: workingDir,
		})
}

// StackLogLine is one line of a stack's output, tagged with where it came from.
//
// Reading a deploy means reading what every service said, interleaved; the
// service and container names are what make an interleaved stream legible.
type StackLogLine struct {
	Type      string    `json:"t"`
	Service   string    `json:"service"`
	Container string    `json:"container"`
	Stream    string    `json:"s,omitempty"`
	Timestamp time.Time `json:"ts,omitzero"`
	Message   string    `json:"m"`
}

// Logs streams every container in a stack over one channel.
//
// One stream rather than one per service: the browser would otherwise have to
// interleave them itself, and doing that correctly needs timestamps the
// operator did not necessarily ask for.
func (s *StackService) Logs(ctx context.Context, stack store.Stack, services []string,
	opts docker.LogOptions,
) (<-chan StackLogLine, <-chan error, error) {
	containers, err := s.containersOf(ctx, stack.Name)
	if err != nil {
		return nil, nil, err
	}

	wanted := make(map[string]bool, len(services))
	for _, name := range services {
		wanted[name] = true
	}

	selected := make([]docker.Container, 0, len(containers))
	for _, container := range containers {
		if len(wanted) > 0 && !wanted[container.Labels[compose.LabelService]] {
			continue
		}
		selected = append(selected, container)
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("%w: this stack has no containers to read", ErrNoSuchService)
	}

	lines := make(chan StackLogLine, 128)
	errs := make(chan error, len(selected))

	var wg sync.WaitGroup
	for _, container := range selected {
		wg.Add(1)
		go func(container docker.Container) {
			defer wg.Done()
			s.streamOne(ctx, container, opts, lines, errs)
		}(container)
	}

	go func() {
		wg.Wait()
		close(lines)
		close(errs)
	}()

	return lines, errs, nil
}

// streamOne pipes one container's output into the shared channel.
func (s *StackService) streamOne(ctx context.Context, container docker.Container,
	opts docker.LogOptions, out chan<- StackLogLine, errs chan<- error,
) {
	service := container.Labels[compose.LabelService]
	if service == "" {
		service = container.Name
	}

	containerLines, containerErrs := s.docker.ContainerLogs(ctx, container.ID, opts)

	for containerLines != nil || containerErrs != nil {
		select {
		case <-ctx.Done():
			return

		case line, ok := <-containerLines:
			if !ok {
				containerLines = nil
				continue
			}
			select {
			case out <- StackLogLine{
				Type:      "log",
				Service:   service,
				Container: container.Name,
				Stream:    line.Stream,
				Timestamp: line.Timestamp,
				Message:   line.Message,
			}:
			case <-ctx.Done():
				return
			}

		case err, ok := <-containerErrs:
			if !ok {
				containerErrs = nil
				continue
			}
			if err != nil {
				// One container's stream ending is not the stack's stream
				// ending: the others are still worth reading.
				select {
				case errs <- fmt.Errorf("%s: %w", container.Name, err):
				default:
				}
			}
		}
	}
}
