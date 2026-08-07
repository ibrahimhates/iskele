// Package fake provides an in-memory [docker.Client] for tests.
//
// It records every call and lets a test inject a failure per operation, so
// handler and service tests can cover the error paths without a live daemon.
package fake

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// Operation names accepted by Client.Fail.
const (
	OpPing                = "Ping"
	OpInfo                = "Info"
	OpDiskUsage           = "DiskUsage"
	OpListContainers      = "ListContainers"
	OpInspectContainer    = "InspectContainer"
	OpInspectContainerRaw = "InspectContainerRaw"
	OpStartContainer      = "StartContainer"
	OpStopContainer       = "StopContainer"
	OpRestartContainer    = "RestartContainer"
	OpRemoveContainer     = "RemoveContainer"
	OpListImages          = "ListImages"
	OpListVolumes         = "ListVolumes"
	OpListNetworks        = "ListNetworks"
	OpClose               = "Close"
)

// Call records one invocation.
type Call struct {
	Op   string
	ID   string
	Opts any
}

// Client is an in-memory Docker engine.
//
// The zero value is usable but empty; New returns one pre-populated with a
// small, realistic fixture set.
type Client struct {
	mu sync.Mutex

	Containers []docker.Container
	Details    map[string]docker.ContainerDetail
	RawInspect map[string]json.RawMessage
	Images     []docker.Image
	Volumes    []docker.Volume
	Networks   []docker.Network
	SystemInfo docker.SystemInfo
	Usage      docker.DiskUsage
	Pong       docker.Pong

	// errs maps an operation name to the error it should return.
	errs map[string]error
	// calls records every invocation in order.
	calls []Call
	// Closed reports whether Close has been called.
	Closed bool
}

var _ docker.Client = (*Client)(nil)

// New returns a fake pre-loaded with one running and one exited container, an
// image, a volume and the default bridge network.
func New() *Client {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	running := docker.Container{
		ID:      "c1000000000000000000000000000000000000000000000000000000000000a",
		Name:    "web",
		Names:   []string{"web"},
		Image:   "nginx:1.27",
		ImageID: "sha256:img1",
		Command: "nginx -g 'daemon off;'",
		Created: created,
		State:   "running",
		Status:  "Up 2 hours (healthy)",
		Health:  "healthy",
		Ports: []docker.Port{
			{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		},
		Labels:     map[string]string{"app": "web"},
		Networks:   []string{"bridge"},
		Mounts:     []string{"/usr/share/nginx/html"},
		SizeRW:     -1,
		SizeRootFS: -1,
	}
	stopped := docker.Container{
		ID:         "c2000000000000000000000000000000000000000000000000000000000000b",
		Name:       "worker",
		Names:      []string{"worker"},
		Image:      "alpine:3.20",
		ImageID:    "sha256:img2",
		Command:    "sh",
		Created:    created.Add(-time.Hour),
		State:      "exited",
		Status:     "Exited (0) 5 minutes ago",
		Ports:      []docker.Port{},
		Labels:     map[string]string{},
		Networks:   []string{},
		Mounts:     []string{},
		SizeRW:     -1,
		SizeRootFS: -1,
	}

	c := &Client{
		Containers: []docker.Container{running, stopped},
		Images: []docker.Image{{
			ID:          "sha256:img1",
			RepoTags:    []string{"nginx:1.27"},
			RepoDigests: []string{},
			Created:     created,
			Size:        142_000_000,
			Labels:      map[string]string{},
			Containers:  1,
		}},
		Volumes: []docker.Volume{{
			Name:       "web-data",
			Driver:     "local",
			Mountpoint: "/var/lib/docker/volumes/web-data/_data",
			Scope:      "local",
			CreatedAt:  created,
			Labels:     map[string]string{},
			Options:    map[string]string{},
			Size:       -1,
			RefCount:   -1,
		}},
		Networks: []docker.Network{{
			ID:      "n100000000000000000000000000000000000000000000000000000000000000",
			Name:    "bridge",
			Driver:  "bridge",
			Scope:   "local",
			Created: created,
			IPAM:    []docker.IPAMConfig{{Subnet: "172.17.0.0/16", Gateway: "172.17.0.1"}},
			Labels:  map[string]string{},
		}},
		SystemInfo: docker.SystemInfo{
			ServerVersion:     "28.5.2",
			APIVersion:        "1.51",
			Name:              "test-host",
			OSType:            "linux",
			Architecture:      "x86_64",
			NCPU:              4,
			MemTotal:          8 << 30,
			Containers:        2,
			ContainersRunning: 1,
			ContainersStopped: 1,
			Images:            1,
		},
		Usage: docker.DiskUsage{
			LayersSize: 142_000_000,
			Images:     docker.DiskUsageEntry{Count: 1, Size: 142_000_000},
			Containers: docker.DiskUsageEntry{Count: 2},
			Volumes:    docker.DiskUsageEntry{Count: 1},
		},
		Pong: docker.Pong{APIVersion: "1.51", OSType: "linux"},
	}

	c.Details = map[string]docker.ContainerDetail{
		running.ID: {
			Container:    running,
			RestartCount: 0,
			Cmd:          []string{"nginx", "-g", "daemon off;"},
			Env:          []string{"PATH=/usr/local/sbin:/usr/local/bin"},
			WorkingDir:   "/",
			Pid:          4242,
			StartedAt:    created,
			MountPoints:  []docker.MountPoint{},
			NetworkList:  []docker.NetworkAttachment{},
		},
		stopped.ID: {
			Container:    stopped,
			RestartCount: 2,
			ExitCode:     0,
			StartedAt:    created.Add(-time.Hour),
			FinishedAt:   created.Add(-30 * time.Minute),
			MountPoints:  []docker.MountPoint{},
			NetworkList:  []docker.NetworkAttachment{},
		},
	}
	c.RawInspect = map[string]json.RawMessage{
		running.ID: json.RawMessage(`{"Id":"` + running.ID + `","Name":"/web"}`),
		stopped.ID: json.RawMessage(`{"Id":"` + stopped.ID + `","Name":"/worker"}`),
	}
	return c
}

// Fail makes the named operation return err until cleared with Fail(op, nil).
func (c *Client) Fail(op string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errs == nil {
		c.errs = map[string]error{}
	}
	if err == nil {
		delete(c.errs, op)
		return
	}
	c.errs[op] = err
}

// Calls returns a copy of the recorded invocations.
func (c *Client) Calls() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Call, len(c.calls))
	copy(out, c.calls)
	return out
}

// CallsFor returns the recorded invocations of one operation.
func (c *Client) CallsFor(op string) []Call {
	var out []Call
	for _, call := range c.Calls() {
		if call.Op == op {
			out = append(out, call)
		}
	}
	return out
}

// Reset clears recorded calls and injected errors.
func (c *Client) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = nil
	c.errs = nil
}

// record logs the call and returns the injected error, if any.
func (c *Client) record(op, id string, opts any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, Call{Op: op, ID: id, Opts: opts})
	return c.errs[op]
}

// containerExists reports whether id names a known container.
func (c *Client) containerExists(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ct := range c.Containers {
		if ct.ID == id || ct.Name == id {
			return true
		}
	}
	return false
}

// notFound builds the same error shape the real engine produces.
func notFound(op, id string) error {
	return docker.NewError(docker.KindNotFound, op, "container", id, "no such container: "+id)
}

func (c *Client) Ping(_ context.Context) (docker.Pong, error) {
	if err := c.record(OpPing, "", nil); err != nil {
		return docker.Pong{}, err
	}
	return c.Pong, nil
}

func (c *Client) Info(_ context.Context) (docker.SystemInfo, error) {
	if err := c.record(OpInfo, "", nil); err != nil {
		return docker.SystemInfo{}, err
	}
	return c.SystemInfo, nil
}

func (c *Client) DiskUsage(_ context.Context) (docker.DiskUsage, error) {
	if err := c.record(OpDiskUsage, "", nil); err != nil {
		return docker.DiskUsage{}, err
	}
	return c.Usage, nil
}

func (c *Client) ListContainers(_ context.Context, opts docker.ListContainersOptions) ([]docker.Container, error) {
	if err := c.record(OpListContainers, "", opts); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]docker.Container, 0, len(c.Containers))
	for _, ct := range c.Containers {
		// The real engine only returns running containers unless All is set.
		if !opts.All && ct.State != "running" {
			continue
		}
		out = append(out, ct)
	}
	return out, nil
}

func (c *Client) InspectContainer(_ context.Context, id string) (docker.ContainerDetail, error) {
	if err := c.record(OpInspectContainer, id, nil); err != nil {
		return docker.ContainerDetail{}, err
	}

	c.mu.Lock()
	d, ok := c.Details[id]
	c.mu.Unlock()
	if !ok {
		return docker.ContainerDetail{}, notFound("container.inspect", id)
	}
	return d, nil
}

func (c *Client) InspectContainerRaw(_ context.Context, id string) (docker.RawInspect, error) {
	if err := c.record(OpInspectContainerRaw, id, nil); err != nil {
		return nil, err
	}

	c.mu.Lock()
	raw, ok := c.RawInspect[id]
	c.mu.Unlock()
	if !ok {
		return nil, notFound("container.inspect", id)
	}
	return raw, nil
}

func (c *Client) StartContainer(_ context.Context, id string) error {
	if err := c.record(OpStartContainer, id, nil); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.start", id)
	}
	c.setState(id, "running")
	return nil
}

func (c *Client) StopContainer(_ context.Context, id string, opts docker.StopOptions) error {
	if err := c.record(OpStopContainer, id, opts); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.stop", id)
	}
	c.setState(id, "exited")
	return nil
}

func (c *Client) RestartContainer(_ context.Context, id string, opts docker.StopOptions) error {
	if err := c.record(OpRestartContainer, id, opts); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.restart", id)
	}
	c.setState(id, "running")
	return nil
}

func (c *Client) RemoveContainer(_ context.Context, id string, opts docker.RemoveContainerOptions) error {
	if err := c.record(OpRemoveContainer, id, opts); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.remove", id)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, ct := range c.Containers {
		if ct.ID != id && ct.Name != id {
			continue
		}
		// The engine refuses to remove a running container without --force.
		if ct.State == "running" && !opts.Force {
			return docker.NewError(docker.KindConflict, "container.remove", "container", id,
				"cannot remove container "+id+": container is running: stop the container before removing or force remove")
		}
		c.Containers = append(c.Containers[:i], c.Containers[i+1:]...)
		delete(c.Details, ct.ID)
		delete(c.RawInspect, ct.ID)
		return nil
	}
	return notFound("container.remove", id)
}

func (c *Client) ListImages(_ context.Context, opts docker.ListImagesOptions) ([]docker.Image, error) {
	if err := c.record(OpListImages, "", opts); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]docker.Image, len(c.Images))
	copy(out, c.Images)
	return out, nil
}

func (c *Client) ListVolumes(_ context.Context) ([]docker.Volume, error) {
	if err := c.record(OpListVolumes, "", nil); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]docker.Volume, len(c.Volumes))
	copy(out, c.Volumes)
	return out, nil
}

func (c *Client) ListNetworks(_ context.Context) ([]docker.Network, error) {
	if err := c.record(OpListNetworks, "", nil); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]docker.Network, len(c.Networks))
	copy(out, c.Networks)
	return out, nil
}

func (c *Client) Close() error {
	if err := c.record(OpClose, "", nil); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Closed = true
	return nil
}

// setState updates a container's state, mirroring what the engine would do
// after a lifecycle call.
func (c *Client) setState(id, state string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Containers {
		if c.Containers[i].ID != id && c.Containers[i].Name != id {
			continue
		}
		c.Containers[i].State = state
		if d, ok := c.Details[c.Containers[i].ID]; ok {
			// State is promoted from the embedded Container.
			d.State = state
			c.Details[c.Containers[i].ID] = d
		}
		return
	}
}
