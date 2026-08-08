package docker

import (
	"context"
	"time"

	dockerclient "github.com/docker/docker/client"
)

// Client is everything the rest of Iskele may do to the Docker Engine.
//
// Handlers and services depend on this interface only, so the whole engine can
// be replaced by [github.com/ibrahimhates/iskele/internal/docker/fake.Client]
// in tests. Later milestones extend it (create, exec, logs, build, events).
type Client interface {
	// Ping verifies the daemon is reachable and returns its API version.
	Ping(ctx context.Context) (Pong, error)
	// Info summarizes the engine and host.
	Info(ctx context.Context) (SystemInfo, error)
	// DiskUsage is the `docker system df` report.
	DiskUsage(ctx context.Context) (DiskUsage, error)

	ListContainers(ctx context.Context, opts ListContainersOptions) ([]Container, error)
	InspectContainer(ctx context.Context, id string) (ContainerDetail, error)
	InspectContainerRaw(ctx context.Context, id string) (RawInspect, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, opts StopOptions) error
	RestartContainer(ctx context.Context, id string, opts StopOptions) error
	RemoveContainer(ctx context.Context, id string, opts RemoveContainerOptions) error
	PauseContainer(ctx context.Context, id string) error
	UnpauseContainer(ctx context.Context, id string) error
	KillContainer(ctx context.Context, id, signal string) error
	RenameContainer(ctx context.Context, id, newName string) error

	// CreateContainer and RawInspectConfig exist for redeploy: recreating a
	// container from its own definition.
	CreateContainer(ctx context.Context, spec CreateSpec) (string, error)
	RawInspectConfig(ctx context.Context, id string) (CreateSpec, error)
	PullImage(ctx context.Context, ref string) error

	ListImages(ctx context.Context, opts ListImagesOptions) ([]Image, error)
	ListVolumes(ctx context.Context) ([]Volume, error)
	ListNetworks(ctx context.Context) ([]Network, error)

	// Streaming operations: logs, stats, exec and engine events.
	Streamer

	// Close releases the underlying HTTP transport.
	Close() error
}

// Pong is the result of a successful ping.
type Pong struct {
	APIVersion string `json:"api_version"`
	OSType     string `json:"os_type,omitempty"`
}

// MinimumAPIVersion is the oldest Engine API this code is written against
// (Docker 20.10). Version negotiation may settle lower on an older daemon, in
// which case Connect refuses rather than failing later in unclear ways.
const MinimumAPIVersion = "1.41"

// engine is the real Client, backed by the official SDK.
type engine struct {
	api  *dockerclient.Client
	host string
}

// compile-time check that the SDK-backed implementation satisfies Client.
var _ Client = (*engine)(nil)

// Connect dials the daemon at host and negotiates the API version.
//
// It returns an error of Kind KindUnavailable when the daemon cannot be
// reached, so the caller can start the HTTP server anyway and surface the
// problem in the UI rather than refusing to boot.
func Connect(ctx context.Context, host string, timeout time.Duration) (Client, error) {
	api, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(host),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, unavailable("docker.connect", host, err)
	}

	e := &engine{api: api, host: host}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := e.Ping(pingCtx); err != nil {
		_ = api.Close()
		return nil, err
	}
	return e, nil
}

// Ping reports the daemon's API version, or a KindUnavailable error.
func (e *engine) Ping(ctx context.Context) (Pong, error) {
	p, err := e.api.Ping(ctx)
	if err != nil {
		return Pong{}, unavailable("docker.ping", e.host, err)
	}
	return Pong{APIVersion: p.APIVersion, OSType: p.OSType}, nil
}

// Close releases the SDK's transport.
func (e *engine) Close() error {
	if e.api == nil {
		return nil
	}
	return e.api.Close()
}
