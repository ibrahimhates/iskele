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
func (s *Container) Start(ctx context.Context, id string) error {
	id, err := normalizeID(id)
	if err != nil {
		return err
	}
	return s.docker.StartContainer(ctx, id)
}

// Stop stops a running container. A nil timeout keeps the engine default.
func (s *Container) Stop(ctx context.Context, id string, timeout *int) error {
	id, err := normalizeID(id)
	if err != nil {
		return err
	}
	return s.docker.StopContainer(ctx, id, docker.StopOptions{Timeout: timeout})
}

// Restart stops then starts a container.
func (s *Container) Restart(ctx context.Context, id string, timeout *int) error {
	id, err := normalizeID(id)
	if err != nil {
		return err
	}
	return s.docker.RestartContainer(ctx, id, docker.StopOptions{Timeout: timeout})
}

// RemoveOptions mirrors the query parameters of DELETE /containers/{id}.
type RemoveOptions struct {
	Force         bool
	RemoveVolumes bool
}

// Remove deletes a container.
func (s *Container) Remove(ctx context.Context, id string, opts RemoveOptions) error {
	id, err := normalizeID(id)
	if err != nil {
		return err
	}
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
