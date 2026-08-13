// Package service holds the business layer between the HTTP handlers and the
// Docker engine / database. Handlers never touch the Docker SDK directly.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
)

// ErrEmptyID is returned when a caller passes a blank container reference.
// It is a plain sentinel so handlers can map it to 400 without a type switch.
var ErrEmptyID = errors.New("container id is required")

// Container implements container lifecycle operations.
//
// From M2 onward this is where audit records and events are emitted, which is
// why even the pass-through methods live here rather than in the handlers.
type Container struct {
	docker   docker.Client
	recorder *audit.Recorder
}

// NewContainer builds the container service on top of an engine client.
//
// recorder may be nil in tests that do not assert on the audit trail; the
// recorder itself tolerates a nil receiver.
func NewContainer(client docker.Client, recorder *audit.Recorder) *Container {
	return &Container{docker: client, recorder: recorder}
}

// ListOptions mirrors the query parameters of GET /containers.
type ListOptions struct {
	All    bool
	Size   bool
	Label  []string
	Status []string
	Name   string
}

// List returns the containers matching opts.
func (s *Container) List(ctx context.Context, opts ListOptions) ([]docker.Container, error) {
	return s.docker.ListContainers(ctx, docker.ListContainersOptions{
		All:    opts.All,
		Size:   opts.Size,
		Label:  opts.Label,
		Status: opts.Status,
		Name:   opts.Name,
	})
}

// Get returns the detailed view of one container.
func (s *Container) Get(ctx context.Context, id string) (docker.ContainerDetail, error) {
	id, err := normalizeID(id)
	if err != nil {
		return docker.ContainerDetail{}, err
	}
	return s.docker.InspectContainer(ctx, id)
}

// Inspect returns the engine's raw inspect payload.
func (s *Container) Inspect(ctx context.Context, id string) (docker.RawInspect, error) {
	id, err := normalizeID(id)
	if err != nil {
		return nil, err
	}
	return s.docker.InspectContainerRaw(ctx, id)
}

// Start starts a stopped container.
func (s *Container) Start(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	return s.record(ctx, "container.start", id, nil, actor, meta, s.start)
}

// Stop stops a running container. A nil timeout keeps the engine default.
func (s *Container) Stop(ctx context.Context, id string, timeout *int, actor audit.Actor, meta RequestMeta) error {
	return s.record(ctx, "container.stop", id, timeoutDetail(timeout), actor, meta,
		func(ctx context.Context, id string) error { return s.stop(ctx, id, timeout) })
}

// Restart stops then starts a container.
func (s *Container) Restart(ctx context.Context, id string, timeout *int, actor audit.Actor, meta RequestMeta) error {
	return s.record(ctx, "container.restart", id, timeoutDetail(timeout), actor, meta,
		func(ctx context.Context, id string) error { return s.restart(ctx, id, timeout) })
}

// The unrecorded primitives. Batch writes one audit record per container
// itself, so it calls these directly: an action recorded twice reads as two
// actions, and an operator counting stops would be counting wrong.
func (s *Container) start(ctx context.Context, id string) error {
	return s.docker.StartContainer(ctx, id)
}

func (s *Container) stop(ctx context.Context, id string, timeout *int) error {
	return s.docker.StopContainer(ctx, id, docker.StopOptions{Timeout: timeout})
}

func (s *Container) restart(ctx context.Context, id string, timeout *int) error {
	return s.docker.RestartContainer(ctx, id, docker.StopOptions{Timeout: timeout})
}

// timeoutDetail records an explicit stop timeout, and nothing when the engine
// default was used.
func timeoutDetail(timeout *int) map[string]any {
	if timeout == nil {
		return nil
	}
	return map[string]any{"timeout": *timeout}
}

// record runs one lifecycle operation and writes an audit entry for it,
// whether it succeeded or not.
//
// A refused action belongs in the trail as much as a successful one: "who
// tried to remove this and could not" is a question the log has to answer.
func (s *Container) record(ctx context.Context, action, id string, detail map[string]any,
	actor audit.Actor, meta RequestMeta, fn func(context.Context, string) error,
) error {
	id, err := normalizeID(id)
	if err != nil {
		return err
	}

	err = fn(ctx, id)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       action,
		ResourceType: "container",
		ResourceID:   id,
		Err:          err,
		Detail:       detail,
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return err
}

// RemoveOptions mirrors the query parameters of DELETE /containers/{id}.
type RemoveOptions struct {
	Force         bool
	RemoveVolumes bool
}

// Remove deletes a container.
func (s *Container) Remove(ctx context.Context, id string, opts RemoveOptions,
	actor audit.Actor, meta RequestMeta,
) error {
	detail := map[string]any{"force": opts.Force, "volumes": opts.RemoveVolumes}
	return s.record(ctx, "container.remove", id, detail, actor, meta,
		func(ctx context.Context, id string) error { return s.remove(ctx, id, opts) })
}

func (s *Container) remove(ctx context.Context, id string, opts RemoveOptions) error {
	return s.docker.RemoveContainer(ctx, id, docker.RemoveContainerOptions{
		Force:         opts.Force,
		RemoveVolumes: opts.RemoveVolumes,
	})
}

// normalizeID trims a container reference and rejects empty ones before they
// reach the engine, where they would turn into a confusing 404.
func normalizeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ErrEmptyID
	}
	return id, nil
}

// Prune removes every stopped container.
//
// The engine decides which those are: a listing this code filtered itself
// would be a moment out of date, and "stopped when I looked" is not the same
// as "stopped when I deleted it".
func (s *Container) Prune(ctx context.Context, actor audit.Actor, meta RequestMeta) (docker.PruneReport, error) {
	report, err := s.docker.PruneContainers(ctx)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "container.prune",
		ResourceType: "container",
		Err:          err,
		Detail: map[string]any{
			"deleted":         len(report.Deleted),
			"space_reclaimed": report.SpaceReclaimed,
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return report, err
}
