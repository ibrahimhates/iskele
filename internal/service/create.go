package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
)

// ErrPrivilegedDenied reports a container definition that needs admin rights.
var ErrPrivilegedDenied = errors.New("this option requires the privileged permission")

// PrivilegedError names the options that pushed a request over the line, so
// the operator can drop them rather than guess which one was the problem.
type PrivilegedError struct {
	Options []string
}

func (e *PrivilegedError) Error() string {
	return fmt.Sprintf("%s may only be set by a user with the privileged permission",
		strings.Join(e.Options, ", "))
}

func (e *PrivilegedError) Unwrap() error { return ErrPrivilegedDenied }

// Creator builds containers from the wizard's definition.
//
// It sits in front of [Container] rather than inside it because creating is
// the one operation that can hand over the host: it is where the path
// whitelist and the privileged-option gate are enforced.
type Creator struct {
	docker     docker.Client
	registries *Registry
	paths      *PathGuard
	recorder   *audit.Recorder
}

// NewCreator builds the create service.
func NewCreator(client docker.Client, registries *Registry,
	paths *PathGuard, recorder *audit.Recorder,
) *Creator {
	return &Creator{
		docker:     client,
		registries: registries,
		paths:      paths,
		recorder:   recorder,
	}
}

// CreateOptions carries the caller's rights alongside the definition.
type CreateOptions struct {
	// Privileged reports whether the caller holds the privileged permission.
	// The handler decides this; the service enforces it.
	Privileged bool
}

// CreateResult is what the caller gets back.
type CreateResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Started bool   `json:"started"`
}

// Create builds a container from spec.
//
// The order is deliberate: validate everything that can be checked locally
// before touching the engine, so a rejected request has changed nothing. The
// pull is the one step that runs first and leaves a trace — an image on disk —
// which is harmless.
func (s *Creator) Create(ctx context.Context, spec docker.ContainerSpec, opts CreateOptions,
	actor audit.Actor, meta RequestMeta,
) (CreateResult, error) {
	result, err := s.create(ctx, spec, opts)
	s.auditCreate(ctx, actor, meta, spec, result, err)
	return result, err
}

func (s *Creator) create(ctx context.Context, spec docker.ContainerSpec, opts CreateOptions) (CreateResult, error) {
	if err := s.paths.CheckAll(docker.BindSources(spec)); err != nil {
		return CreateResult{}, err
	}
	if err := checkPrivilegedOptions(spec, opts.Privileged); err != nil {
		return CreateResult{}, err
	}

	createSpec, err := docker.BuildCreateSpec(spec)
	if err != nil {
		return CreateResult{}, err
	}

	if pullErr := s.ensureImage(ctx, spec); pullErr != nil {
		return CreateResult{}, pullErr
	}

	id, err := s.docker.CreateContainer(ctx, createSpec)
	if err != nil {
		return CreateResult{}, err
	}

	result := CreateResult{ID: id, Name: createSpec.Name, Image: spec.Image}

	if spec.Start {
		if startErr := s.docker.StartContainer(ctx, id); startErr != nil {
			// The container exists and is the operator's to inspect or remove;
			// destroying it would throw away the evidence of why it would not
			// start. The error says which half succeeded.
			return result, fmt.Errorf("container %s was created but did not start: %w", id, startErr)
		}
		result.Started = true
	}

	return result, nil
}

// ensureImage applies the spec's pull policy before the container is created.
func (s *Creator) ensureImage(ctx context.Context, spec docker.ContainerSpec) error {
	policy := strings.ToLower(strings.TrimSpace(spec.PullPolicy))
	switch policy {
	case docker.PullNever:
		return nil
	case docker.PullAlways:
		// Fall through to the pull below.
	case "", docker.PullMissing:
		// The engine pulls a missing image on create by itself, so there is
		// nothing to do here; pulling anyway would defeat the point of the
		// policy on a slow link.
		return nil
	default:
		return &docker.SpecError{
			Field:   "pull_policy",
			Message: fmt.Sprintf("unknown policy %q; use missing, always or never", spec.PullPolicy),
		}
	}

	auth, err := s.registries.AuthFor(ctx, spec.Image)
	if err != nil {
		return err
	}

	events, errs := s.docker.PullImageProgress(ctx, docker.PullOptions{Ref: spec.Image, Auth: auth})
	return drainPull(ctx, events, errs)
}

// drainPull consumes a pull to completion and reports the first failure.
func drainPull(ctx context.Context, events <-chan docker.PullEvent, errs <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case _, ok := <-events:
			if !ok {
				events = nil
			}

		case err, ok := <-errs:
			if !ok {
				errs = nil
				break
			}
			if err != nil {
				return err
			}
		}

		if events == nil && errs == nil {
			return nil
		}
	}
}

// privilegedOptions names every field that widens what a container may do.
//
// Each of these is, in some configuration, a route from container to host
// root: --privileged directly, a device or a capability by attaching to host
// hardware, a security-opt by turning off the confinement that would stop the
// rest, and a sysctl by reaching kernel parameters shared with the host.
func checkPrivilegedOptions(spec docker.ContainerSpec, allowed bool) error {
	if allowed {
		return nil
	}

	var used []string
	if spec.Security.Privileged {
		used = append(used, "privileged")
	}
	if len(spec.Security.CapAdd) > 0 {
		used = append(used, "cap_add")
	}
	if len(spec.Security.Devices) > 0 {
		used = append(used, "devices")
	}
	if len(spec.Security.SecurityOpt) > 0 {
		used = append(used, "security_opt")
	}
	if len(spec.Security.Sysctls) > 0 {
		used = append(used, "sysctls")
	}
	// host networking shares the host's whole stack, including loopback —
	// which is where an unprotected service usually listens.
	if isHostNamespace(spec.Network.Name) {
		used = append(used, "network=host")
	}

	if len(used) == 0 {
		return nil
	}
	return &PrivilegedError{Options: used}
}

// isHostNamespace reports the network modes that drop the container's own
// namespace.
func isHostNamespace(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host":
		return true
	default:
		return false
	}
}

// auditCreate records the attempt, successful or not.
//
// The environment is deliberately not in Detail: it routinely carries
// database passwords and API keys, and an audit log is exactly the file an
// operator forwards to a log collector.
func (s *Creator) auditCreate(ctx context.Context, actor audit.Actor, meta RequestMeta,
	spec docker.ContainerSpec, result CreateResult, err error,
) {
	detail := map[string]any{
		"name":       spec.Name,
		"image":      spec.Image,
		"privileged": spec.Security.Privileged,
		"start":      spec.Start,
		"binds":      docker.BindSources(spec),
		"network":    spec.Network.Name,
	}
	if len(spec.Ports) > 0 {
		published := make([]string, 0, len(spec.Ports))
		for _, p := range spec.Ports {
			if p.HostPort != "" {
				published = append(published, fmt.Sprintf("%s:%d", p.HostPort, p.ContainerPort))
			}
		}
		detail["published_ports"] = published
	}

	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "container.create",
		ResourceType: "container",
		ResourceID:   result.ID,
		Err:          err,
		Detail:       detail,
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
}
