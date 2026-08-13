package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/compose"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/store"
)

// StackEventKind says what one deploy event is about.
type StackEventKind string

// Stack event kinds.
const (
	// StackEventStep is progress: a network created, an image pulled, a
	// container started.
	StackEventStep StackEventKind = "step"
	// StackEventLog is a line from a build the deploy had to run first.
	StackEventLog StackEventKind = "log"
	// StackEventWarn is something the operator should read but which did not
	// stop the deploy.
	StackEventWarn StackEventKind = "warn"
	// StackEventDone closes a successful deploy.
	StackEventDone StackEventKind = "done"
)

// StackEvent is one line of a deploy's progress.
type StackEvent struct {
	Kind StackEventKind `json:"kind"`
	// Service is empty for project-level work.
	Service string `json:"service,omitempty"`
	Message string `json:"message"`
	// Container is set when a step produced one.
	Container string `json:"container,omitempty"`
}

// UpOptions carries the caller's rights and what they asked for.
type UpOptions struct {
	// Privileged reports whether the caller may set privileged options. The
	// handler decides this; the service enforces it.
	Privileged bool
	// Pull re-pulls every image even when it is already present.
	Pull bool
	// Recreate rebuilds every container, even ones whose definition has not
	// changed. Without it, an unchanged service is left running.
	Recreate bool
	// Services limits the deploy to these services and nothing else.
	Services []string
}

// Up deploys a stack and streams its progress.
//
// The channels close when the deploy ends, whichever way it ends, and the
// stack's status is written before they do — so a client that re-reads the
// stack after the stream closes sees the outcome.
//
// Two deploys of one stack would race each other into a half-built state, so
// the second is refused here rather than left to produce a mess the operator
// then has to read.
func (s *StackService) Up(ctx context.Context, stack store.Stack, opts UpOptions,
	actor audit.Actor, meta RequestMeta,
) (<-chan StackEvent, <-chan error, error) {
	deployCtx, done, err := s.claim(ctx, stack, "stack.up", actor.Username)
	if err != nil {
		return nil, nil, err
	}

	events := make(chan StackEvent, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		defer done()

		err := s.up(deployCtx, stack, opts, events)
		s.auditStack(deployCtx, actor, meta, "stack.up", stack, err)

		// The outcome must be recorded even when the request that started the
		// deploy is gone.
		finishCtx := context.WithoutCancel(deployCtx)
		switch {
		case err != nil:
			_ = s.stacks.SetStatus(finishCtx, stack.ID, store.StackFailed, docker.Message(err))
			select {
			case errs <- err:
			default:
			}
		default:
			_ = s.stacks.SetStatus(finishCtx, stack.ID, store.StackDeployed, "")
		}
	}()

	return events, errs, nil
}

// claim registers a stack operation as a cancellable task, refusing a second
// one on the same stack.
//
// The task is keyed by the stack's id, which is what lets
// POST /stacks/{id}/cancel reach the work without a second identifier to carry
// around — the same arrangement builds use.
func (s *StackService) claim(ctx context.Context, stack store.Stack, kind, username string) (context.Context, func(), error) {
	if s.tasks == nil {
		return ctx, func() {}, nil
	}
	if task, err := s.tasks.Get(stack.ID); err == nil && !task.State.Terminal() {
		return nil, nil, fmt.Errorf("%w: %s started %s", ErrStackBusy, task.Username, task.Kind)
	}

	// The work outlives the request that asked for it: an operator who closes
	// the tab has not asked to abandon a half-deployed stack. The timeout
	// bounds one that is stuck rather than slow.
	rooted, stop := context.WithTimeout(context.WithoutCancel(ctx), stackTimeout)
	workCtx := s.tasks.StartWithID(rooted, stack.ID, kind, stack.Name, username)

	return workCtx, func() {
		s.tasks.Finish(stack.ID, nil)
		stop()
	}, nil
}

func (s *StackService) up(ctx context.Context, stack store.Stack, opts UpOptions, out chan<- StackEvent) error {
	if err := s.stacks.SetStatus(ctx, stack.ID, store.StackDeploying, ""); err != nil {
		return err
	}

	conversion, warnings, err := s.plan(ctx, stack)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		emit(ctx, out, StackEvent{
			Kind:    StackEventWarn,
			Service: warning.Service,
			Message: warning.Field + ": " + warning.Message,
		})
	}

	selected := conversion.Services
	if len(opts.Services) > 0 {
		selected, err = selectServices(conversion.Services, opts.Services)
		if err != nil {
			return err
		}
	}

	// Everything that can be refused is refused before anything is created, so
	// a stack that cannot deploy leaves no half-built remains behind.
	var problems []Problem
	for _, plan := range selected {
		problems = append(problems, s.problemsFor(plan, opts.Privileged)...)
	}
	if len(problems) > 0 {
		return &StackRefused{Problems: problems}
	}

	if networkErr := s.ensureNetworks(ctx, conversion.Networks, out); networkErr != nil {
		return networkErr
	}
	if volumeErr := s.ensureVolumes(ctx, conversion.Volumes, out); volumeErr != nil {
		return volumeErr
	}

	existing, err := s.containersOf(ctx, stack.Name)
	if err != nil {
		return err
	}

	for _, plan := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.upService(ctx, stack, plan, opts, existing, out); err != nil {
			return fmt.Errorf("service %s: %w", plan.Name, err)
		}
	}

	emit(ctx, out, StackEvent{Kind: StackEventDone, Message: "stack deployed"})
	return nil
}

// StackRefused reports the reasons a deploy was not attempted.
type StackRefused struct {
	Problems []Problem
}

func (e *StackRefused) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		parts = append(parts, fmt.Sprintf("%s (%s): %s", problem.Service, problem.Field, problem.Message))
	}
	return "this stack cannot be deployed: " + strings.Join(parts, "; ")
}

// upService brings one service to the state the compose file asks for.
func (s *StackService) upService(ctx context.Context, stack store.Stack, plan compose.ServicePlan,
	opts UpOptions, existing []docker.Container, out chan<- StackEvent,
) error {
	if plan.Build != nil {
		if err := s.buildFor(ctx, stack, plan, out); err != nil {
			return err
		}
	} else if err := s.pullFor(ctx, plan, opts.Pull, out); err != nil {
		return err
	}

	hash := configHash(plan.Spec)
	byName := map[string]docker.Container{}
	for _, container := range existing {
		byName[container.Name] = container
	}

	for replica := 1; replica <= plan.Replicas; replica++ {
		name := plan.Spec.Name
		if name == "" {
			name = compose.ContainerName(stack.Name, plan.Name, replica)
		}

		current, found := byName[name]
		if found && !opts.Recreate && current.Labels[LabelConfigHash] == hash {
			if strings.EqualFold(current.State, "running") {
				emit(ctx, out, StackEvent{
					Kind: StackEventStep, Service: plan.Name, Container: current.ID,
					Message: name + " is up to date",
				})
				continue
			}
			// Same definition, not running: starting it is enough, and keeps
			// whatever is in its writable layer.
			if err := s.docker.StartContainer(ctx, current.ID); err == nil {
				emit(ctx, out, StackEvent{
					Kind: StackEventStep, Service: plan.Name, Container: current.ID,
					Message: name + " started",
				})
				continue
			}
			// It would not start; fall through and recreate it.
		}

		if found {
			if err := s.removeContainer(ctx, current.ID); err != nil {
				return fmt.Errorf("replace %s: %w", name, err)
			}
			emit(ctx, out, StackEvent{
				Kind: StackEventStep, Service: plan.Name,
				Message: name + " removed for replacement",
			})
		}

		id, err := s.createReplica(ctx, plan, name, replica, hash)
		if err != nil {
			return err
		}
		emit(ctx, out, StackEvent{
			Kind: StackEventStep, Service: plan.Name, Container: id,
			Message: name + " started",
		})
	}

	// A service scaled down leaves containers behind that the file no longer
	// asks for. They belong to the stack, so removing them is this deploy's job.
	return s.removeSurplus(ctx, plan, byName, out)
}

// createReplica creates and starts one container of a service.
func (s *StackService) createReplica(ctx context.Context, plan compose.ServicePlan,
	name string, replica int, hash string,
) (string, error) {
	spec := plan.Spec
	spec.Name = name
	spec.Labels = replicaLabels(spec.Labels, replica, hash)

	createSpec, err := docker.BuildCreateSpec(spec)
	if err != nil {
		return "", err
	}

	id, err := s.docker.CreateContainer(ctx, createSpec)
	if err != nil {
		return "", err
	}

	// A container is created attached to one network; the rest are joined
	// afterwards, which is the only way the engine offers.
	for i, attachment := range plan.Networks {
		if i == 0 {
			continue
		}
		if err := s.docker.ConnectNetwork(ctx, attachment.Name, id, docker.ConnectOptions{
			Aliases:     attachment.Aliases,
			IPv4Address: attachment.IPv4Address,
			IPv6Address: attachment.IPv6Address,
		}); err != nil {
			return id, fmt.Errorf("attach %s to %s: %w", name, attachment.Name, err)
		}
	}

	if err := s.docker.StartContainer(ctx, id); err != nil {
		// The container exists and is the operator's to inspect; destroying it
		// would throw away the evidence of why it would not start.
		return id, fmt.Errorf("%s was created but did not start: %w", name, err)
	}
	return id, nil
}

// removeSurplus deletes the containers a scaled-down service left behind.
func (s *StackService) removeSurplus(ctx context.Context, plan compose.ServicePlan,
	byName map[string]docker.Container, out chan<- StackEvent,
) error {
	for name, container := range byName {
		if container.Labels[compose.LabelService] != plan.Name {
			continue
		}
		number, err := strconv.Atoi(container.Labels[compose.LabelReplica])
		if err != nil || number <= plan.Replicas {
			continue
		}

		if err := s.removeContainer(ctx, container.ID); err != nil {
			return fmt.Errorf("remove surplus %s: %w", name, err)
		}
		emit(ctx, out, StackEvent{
			Kind: StackEventStep, Service: plan.Name,
			Message: name + " removed; the file asks for fewer replicas",
		})
	}
	return nil
}

// buildFor builds a service's image before its container is created.
func (s *StackService) buildFor(ctx context.Context, stack store.Stack,
	plan compose.ServicePlan, out chan<- StackEvent,
) error {
	if s.builder == nil {
		return fmt.Errorf("service %s builds its image, but building is not available", plan.Name)
	}

	emit(ctx, out, StackEvent{
		Kind: StackEventStep, Service: plan.Name,
		Message: "building " + plan.Build.Image,
	})

	req := BuildRequest{
		ContextDir: plan.Build.Context,
		Dockerfile: plan.Build.Dockerfile,
		Tags:       []string{plan.Build.Image},
		BuildArgs:  plan.Build.Args,
		Target:     plan.Build.Target,
	}

	record, err := s.builder.Start(ctx, req, audit.Actor{Username: stack.CreatedBy}, RequestMeta{})
	if err != nil {
		return err
	}

	events, errs := s.builder.Run(ctx, record, req)
	for events != nil || errs != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if line := strings.TrimRight(event.Stream, "\n"); line != "" {
				emit(ctx, out, StackEvent{Kind: StackEventLog, Service: plan.Name, Message: line})
			}

		case buildErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if buildErr != nil {
				return fmt.Errorf("build %s: %w", plan.Build.Image, buildErr)
			}
		}
	}
	return nil
}

// pullFor applies a service's pull policy.
func (s *StackService) pullFor(ctx context.Context, plan compose.ServicePlan, force bool, out chan<- StackEvent) error {
	spec := plan.Spec
	if force {
		spec.PullPolicy = docker.PullAlways
	}
	if spec.PullPolicy != docker.PullAlways {
		// The engine pulls a missing image on create by itself; pulling anyway
		// would defeat the point of the policy on a slow link.
		return nil
	}

	emit(ctx, out, StackEvent{Kind: StackEventStep, Service: plan.Name, Message: "pulling " + spec.Image})

	auth, err := s.registries.AuthFor(ctx, spec.Image)
	if err != nil {
		return err
	}
	events, errs := s.docker.PullImageProgress(ctx, docker.PullOptions{Ref: spec.Image, Auth: auth})
	return drainPull(ctx, events, errs)
}

// DownOptions says how much of a stack to take away.
type DownOptions struct {
	// Volumes removes the stack's named volumes too. It is off by default
	// because it is the one part of a stack that cannot be recreated: a
	// database's data lives there.
	Volumes bool
	// Networks removes the networks the stack created. On by default in the
	// handler, because a network with nothing attached is only clutter.
	Networks bool
}

// Down stops and removes a stack's containers.
func (s *StackService) Down(ctx context.Context, stack store.Stack, opts DownOptions,
	actor audit.Actor, meta RequestMeta,
) (StackActionResult, error) {
	result, err := s.down(ctx, stack, opts)
	s.auditStack(ctx, actor, meta, "stack.down", stack, err)

	if err == nil {
		_ = s.stacks.SetStatus(ctx, stack.ID, store.StackStopped, "")
	}
	return result, err
}

// StackActionResult reports what a lifecycle action touched.
type StackActionResult struct {
	Containers []string `json:"containers"`
	Networks   []string `json:"networks,omitempty"`
	Volumes    []string `json:"volumes,omitempty"`
	// Failed names what could not be done, so a partial result is legible.
	Failed []string `json:"failed,omitempty"`
}

func (s *StackService) down(ctx context.Context, stack store.Stack, opts DownOptions) (StackActionResult, error) {
	result := StackActionResult{Containers: []string{}}

	containers, err := s.containersOf(ctx, stack.Name)
	if err != nil {
		return result, err
	}

	for _, container := range containers {
		if err := s.removeContainer(ctx, container.ID); err != nil {
			result.Failed = append(result.Failed, container.Name+": "+docker.Message(err))
			continue
		}
		result.Containers = append(result.Containers, container.Name)
	}

	// Resources come after the containers: a network with a container still on
	// it cannot be removed, and a volume still mounted is in use.
	conversion, _, planErr := s.plan(ctx, stack)
	if planErr != nil {
		// The file no longer parses, so the resources it declared are unknown.
		// The containers are still gone, which is the part that matters.
		return result, nil
	}

	if opts.Networks {
		for _, network := range conversion.Networks {
			if network.External {
				continue
			}
			if err := s.docker.RemoveNetwork(ctx, network.Name); err != nil {
				if !docker.IsNotFound(err) {
					result.Failed = append(result.Failed, network.Name+": "+docker.Message(err))
				}
				continue
			}
			result.Networks = append(result.Networks, network.Name)
		}
	}

	if opts.Volumes {
		for _, vol := range conversion.Volumes {
			if vol.External {
				continue
			}
			if err := s.docker.RemoveVolume(ctx, vol.Name, false); err != nil {
				if !docker.IsNotFound(err) {
					result.Failed = append(result.Failed, vol.Name+": "+docker.Message(err))
				}
				continue
			}
			result.Volumes = append(result.Volumes, vol.Name)
		}
	}

	return result, nil
}

// StackAction is a lifecycle verb applied to every container in a stack.
type StackAction string

// Stack actions.
const (
	StackStop    StackAction = "stop"
	StackStart   StackAction = "start"
	StackRestart StackAction = "restart"
)

// Act applies stop, start or restart to a whole stack.
//
// Order matters for start as much as it does for a deploy: a service whose
// dependency is not up yet crash-loops for as long as the dependency takes.
func (s *StackService) Act(ctx context.Context, stack store.Stack, action StackAction,
	actor audit.Actor, meta RequestMeta,
) (StackActionResult, error) {
	result, err := s.act(ctx, stack, action)
	s.auditStack(ctx, actor, meta, "stack."+string(action), stack, err)

	if err == nil {
		switch action {
		case StackStop:
			_ = s.stacks.SetStatus(ctx, stack.ID, store.StackStopped, "")
		case StackStart, StackRestart:
			_ = s.stacks.SetStatus(ctx, stack.ID, store.StackDeployed, "")
		}
	}
	return result, err
}

func (s *StackService) act(ctx context.Context, stack store.Stack, action StackAction) (StackActionResult, error) {
	result := StackActionResult{Containers: []string{}}

	containers, err := s.containersOf(ctx, stack.Name)
	if err != nil {
		return result, err
	}
	ordered := s.orderContainers(ctx, stack, containers, action != StackStart)

	for _, container := range ordered {
		var actErr error
		switch action {
		case StackStop:
			actErr = s.docker.StopContainer(ctx, container.ID, docker.StopOptions{})
		case StackStart:
			actErr = s.docker.StartContainer(ctx, container.ID)
		case StackRestart:
			actErr = s.docker.RestartContainer(ctx, container.ID, docker.StopOptions{})
		default:
			return result, fmt.Errorf("unknown stack action %q", action)
		}

		if actErr != nil {
			result.Failed = append(result.Failed, container.Name+": "+docker.Message(actErr))
			continue
		}
		result.Containers = append(result.Containers, container.Name)
	}

	return result, nil
}

// Pull re-fetches every image a stack's services use.
func (s *StackService) Pull(ctx context.Context, stack store.Stack,
	actor audit.Actor, meta RequestMeta,
) (<-chan StackEvent, <-chan error, error) {
	pullCtx, done, err := s.claim(ctx, stack, "stack.pull", actor.Username)
	if err != nil {
		return nil, nil, err
	}

	events := make(chan StackEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		defer done()

		err := s.pull(pullCtx, stack, events)
		s.auditStack(pullCtx, actor, meta, "stack.pull", stack, err)
		if err != nil {
			select {
			case errs <- err:
			default:
			}
		}
	}()

	return events, errs, nil
}

func (s *StackService) pull(ctx context.Context, stack store.Stack, out chan<- StackEvent) error {
	conversion, _, err := s.plan(ctx, stack)
	if err != nil {
		return err
	}

	for _, plan := range conversion.Services {
		if plan.Build != nil {
			emit(ctx, out, StackEvent{
				Kind: StackEventWarn, Service: plan.Name,
				Message: "builds its image; nothing to pull",
			})
			continue
		}
		if err := s.pullFor(ctx, plan, true, out); err != nil {
			return fmt.Errorf("service %s: %w", plan.Name, err)
		}
	}

	emit(ctx, out, StackEvent{Kind: StackEventDone, Message: "images pulled"})
	return nil
}

// Scale changes how many containers one service runs, without editing the
// compose file.
//
// The change is not written back: the file is the operator's, and a panel that
// silently rewrites it would lose their comments and formatting. The next `up`
// restores what the file says, which is what makes the file authoritative.
func (s *StackService) Scale(ctx context.Context, stack store.Stack, service string, replicas int,
	opts UpOptions, actor audit.Actor, meta RequestMeta,
) (<-chan StackEvent, <-chan error, error) {
	scaleCtx, done, err := s.claim(ctx, stack, "stack.scale", actor.Username)
	if err != nil {
		return nil, nil, err
	}

	events := make(chan StackEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		defer done()

		err := s.scale(scaleCtx, stack, service, replicas, opts, events)
		s.auditStack(scaleCtx, actor, meta, "stack.scale", stack, err)
		if err != nil {
			select {
			case errs <- err:
			default:
			}
		}
	}()

	return events, errs, nil
}

func (s *StackService) scale(ctx context.Context, stack store.Stack, service string,
	replicas int, opts UpOptions, out chan<- StackEvent,
) error {
	if replicas < 0 {
		return &docker.SpecError{Field: "replicas", Message: "a service cannot run a negative number of times"}
	}

	conversion, _, err := s.plan(ctx, stack)
	if err != nil {
		return err
	}

	var target *compose.ServicePlan
	for i := range conversion.Services {
		if conversion.Services[i].Name == service {
			target = &conversion.Services[i]
		}
	}
	if target == nil {
		return fmt.Errorf("%w: %s", ErrNoSuchService, service)
	}

	var problems []Problem
	problems = append(problems, s.problemsFor(*target, opts.Privileged)...)
	if len(problems) > 0 {
		return &StackRefused{Problems: problems}
	}

	declared := target.Replicas
	target.Replicas = replicas

	existing, err := s.containersOf(ctx, stack.Name)
	if err != nil {
		return err
	}
	if err := s.upService(ctx, stack, *target, opts, existing, out); err != nil {
		return err
	}

	emit(ctx, out, StackEvent{
		Kind: StackEventDone,
		Message: fmt.Sprintf("%s now runs %d container(s); the compose file still says %d",
			service, replicas, declared),
	})
	return nil
}

// ReconcileDeploying closes out stacks a restart interrupted.
//
// A deploy is bound to the process running it, so a row still marked deploying
// after a restart can never finish on its own.
func (s *StackService) ReconcileDeploying(ctx context.Context) (int, error) {
	stacks, err := s.stacks.Deploying(ctx)
	if err != nil {
		return 0, err
	}

	closed := 0
	for _, stack := range stacks {
		if err := s.stacks.SetStatus(ctx, stack.ID, store.StackFailed,
			"iskeled restarted while this stack was deploying"); err == nil {
			closed++
		}
	}
	return closed, nil
}

// orderContainers sorts a stack's containers by dependency order.
//
// reverse is for stopping: a database should go down after the thing using it,
// not before.
func (s *StackService) orderContainers(ctx context.Context, stack store.Stack,
	containers []docker.Container, reverse bool,
) []docker.Container {
	conversion, _, err := s.plan(ctx, stack)
	if err != nil {
		// Without a parseable file there is no dependency order to honor. The
		// containers are still there and still worth acting on.
		return containers
	}

	rank := make(map[string]int, len(conversion.Services))
	for i, plan := range conversion.Services {
		rank[plan.Name] = i
	}

	ordered := make([]docker.Container, len(containers))
	copy(ordered, containers)

	sort.SliceStable(ordered, func(i, j int) bool {
		left, leftOK := rank[ordered[i].Labels[compose.LabelService]]
		right, rightOK := rank[ordered[j].Labels[compose.LabelService]]
		switch {
		case !leftOK && !rightOK:
			return ordered[i].Name < ordered[j].Name
		case !leftOK:
			return false
		case !rightOK:
			return true
		case left == right:
			return ordered[i].Name < ordered[j].Name
		case reverse:
			return left > right
		default:
			return left < right
		}
	})

	return ordered
}

// removeContainer stops and deletes one container.
func (s *StackService) removeContainer(ctx context.Context, id string) error {
	err := s.docker.RemoveContainer(ctx, id, docker.RemoveContainerOptions{Force: true})
	if err != nil && docker.IsNotFound(err) {
		return nil
	}
	return err
}

// ensureNetworks creates the networks a stack declares.
func (s *StackService) ensureNetworks(ctx context.Context, plans []compose.NetworkPlan, out chan<- StackEvent) error {
	if len(plans) == 0 {
		return nil
	}

	existing, err := s.docker.ListNetworks(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(existing))
	for _, network := range existing {
		present[network.Name] = true
	}

	for _, plan := range plans {
		if present[plan.Name] {
			continue
		}
		if plan.External {
			return fmt.Errorf("network %s is declared external but does not exist", plan.Name)
		}
		if _, err := s.docker.CreateNetwork(ctx, plan.Options); err != nil {
			return fmt.Errorf("create network %s: %w", plan.Name, err)
		}
		emit(ctx, out, StackEvent{Kind: StackEventStep, Message: "network " + plan.Name + " created"})
	}
	return nil
}

// ensureVolumes creates the volumes a stack declares.
func (s *StackService) ensureVolumes(ctx context.Context, plans []compose.VolumePlan, out chan<- StackEvent) error {
	if len(plans) == 0 {
		return nil
	}

	existing, err := s.docker.ListVolumes(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(existing))
	for _, vol := range existing {
		present[vol.Name] = true
	}

	for _, plan := range plans {
		if present[plan.Name] {
			continue
		}
		if plan.External {
			return fmt.Errorf("volume %s is declared external but does not exist", plan.Name)
		}
		if _, err := s.docker.CreateVolume(ctx, plan.Options); err != nil {
			return fmt.Errorf("create volume %s: %w", plan.Name, err)
		}
		emit(ctx, out, StackEvent{Kind: StackEventStep, Message: "volume " + plan.Name + " created"})
	}
	return nil
}

// selectServices narrows a deploy to the named services.
func selectServices(plans []compose.ServicePlan, names []string) ([]compose.ServicePlan, error) {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}

	out := make([]compose.ServicePlan, 0, len(names))
	for _, plan := range plans {
		if wanted[plan.Name] {
			out = append(out, plan)
			delete(wanted, plan.Name)
		}
	}

	for name := range wanted {
		return nil, fmt.Errorf("%w: %s", ErrNoSuchService, name)
	}
	return out, nil
}

// replicaLabels adds the per-container labels to a service's own.
func replicaLabels(labels map[string]string, replica int, hash string) map[string]string {
	out := make(map[string]string, len(labels)+3)
	for key, value := range labels {
		out[key] = value
	}

	number := strconv.Itoa(replica)
	out[compose.LabelReplica] = number
	out[compose.LabelComposeNumber] = number
	out[LabelConfigHash] = hash
	return out
}

// emit sends one event, giving up if the reader is gone.
func emit(ctx context.Context, out chan<- StackEvent, event StackEvent) {
	select {
	case out <- event:
	case <-ctx.Done():
	}
}
