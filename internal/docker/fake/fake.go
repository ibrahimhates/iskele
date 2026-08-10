// Package fake provides an in-memory [docker.Client] for tests.
//
// It records every call and lets a test inject a failure per operation, so
// handler and service tests can cover the error paths without a live daemon.
package fake

import (
	"context"
	"encoding/json"
	"io"
	"strings"
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

	// pullEvents overrides the replayed pull progress when non-nil.
	pullEvents []docker.PullEvent
	// buildEvents overrides the replayed build output when non-nil.
	buildEvents []docker.BuildEvent
	// builtContexts records the tar contexts BuildImage was handed.
	builtContexts [][]byte

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
		if !matchesLabels(ct.Labels, opts.Label) {
			continue
		}
		if opts.Name != "" && !strings.Contains(ct.Name, opts.Name) {
			continue
		}
		out = append(out, ct)
	}
	return out, nil
}

// matchesLabels applies the engine's "key=value" label filter, which stacks
// depend on: a stack finds its own containers by label and nothing else.
func matchesLabels(labels map[string]string, filters []string) bool {
	for _, filter := range filters {
		key, value, hasValue := strings.Cut(filter, "=")
		got, ok := labels[key]
		if !ok || (hasValue && got != value) {
			return false
		}
	}
	return true
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

// Streaming operation names accepted by Client.Fail.
const (
	OpPauseContainer   = "PauseContainer"
	OpUnpauseContainer = "UnpauseContainer"
	OpKillContainer    = "KillContainer"
	OpRenameContainer  = "RenameContainer"
	OpCreateContainer  = "CreateContainer"
	OpInspectConfig    = "RawInspectConfig"
	OpPullImage        = "PullImage"
	OpContainerLogs    = "ContainerLogs"
	OpContainerStats   = "ContainerStats"
	OpExec             = "Exec"
	OpResizeExec       = "ResizeExec"
	OpExecExitCode     = "ExecExitCode"
	OpEvents           = "Events"
)

// LogLines is what ContainerLogs replays. Tests set it to drive a viewer.
var defaultLogLines = []docker.LogLine{
	{Stream: "stdout", Message: "starting"},
	{Stream: "stderr", Message: "a warning"},
	{Stream: "stdout", Message: "ready"},
}

func (c *Client) PauseContainer(_ context.Context, id string) error {
	if err := c.record(OpPauseContainer, id, nil); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.pause", id)
	}
	c.setState(id, "paused")
	return nil
}

func (c *Client) UnpauseContainer(_ context.Context, id string) error {
	if err := c.record(OpUnpauseContainer, id, nil); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.unpause", id)
	}
	c.setState(id, "running")
	return nil
}

func (c *Client) KillContainer(_ context.Context, id, signal string) error {
	if err := c.record(OpKillContainer, id, signal); err != nil {
		return err
	}
	if !c.containerExists(id) {
		return notFound("container.kill", id)
	}
	c.setState(id, "exited")
	return nil
}

func (c *Client) RenameContainer(_ context.Context, id, newName string) error {
	if err := c.record(OpRenameContainer, id, newName); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Containers {
		if c.Containers[i].ID != id && c.Containers[i].Name != id {
			continue
		}
		c.Containers[i].Name = newName
		c.Containers[i].Names = []string{newName}
		if d, ok := c.Details[c.Containers[i].ID]; ok {
			d.Name = newName
			c.Details[c.Containers[i].ID] = d
		}
		return nil
	}
	return notFound("container.rename", id)
}

func (c *Client) CreateContainer(_ context.Context, spec docker.CreateSpec) (string, error) {
	if err := c.record(OpCreateContainer, spec.Name, spec); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	id := "created-" + spec.Name
	created := docker.Container{
		ID: id, Name: spec.Name, Names: []string{spec.Name},
		State: "created", Status: "Created",
		Ports: []docker.Port{}, Labels: map[string]string{},
		Networks: []string{}, Mounts: []string{},
		SizeRW: -1, SizeRootFS: -1,
	}
	// The labels and image come back out of a listing on the real engine, and
	// stacks read both: the labels are how a stack finds its containers, and
	// the config hash among them is how it tells changed from unchanged.
	if spec.Config != nil {
		created.Image = spec.Config.Image
		for key, value := range spec.Config.Labels {
			created.Labels[key] = value
		}
	}
	c.Containers = append(c.Containers, created)
	c.Details[id] = docker.ContainerDetail{Container: created}
	c.RawInspect[id] = json.RawMessage(`{"Id":"` + id + `"}`)
	return id, nil
}

func (c *Client) RawInspectConfig(_ context.Context, id string) (docker.CreateSpec, error) {
	if err := c.record(OpInspectConfig, id, nil); err != nil {
		return docker.CreateSpec{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ct := range c.Containers {
		if ct.ID == id || ct.Name == id {
			return docker.CreateSpec{Name: ct.Name}, nil
		}
	}
	return docker.CreateSpec{}, notFound("container.inspect", id)
}

func (c *Client) PullImage(_ context.Context, ref string) error {
	if err := c.record(OpPullImage, ref, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) ContainerLogs(ctx context.Context, id string, opts docker.LogOptions) (<-chan docker.LogLine, <-chan error) {
	lines := make(chan docker.LogLine, len(defaultLogLines))
	errs := make(chan error, 1)

	if err := c.record(OpContainerLogs, id, opts); err != nil {
		errs <- err
		close(lines)
		close(errs)
		return lines, errs
	}
	if !c.containerExists(id) {
		errs <- notFound("container.logs", id)
		close(lines)
		close(errs)
		return lines, errs
	}

	go func() {
		defer close(lines)
		defer close(errs)
		for _, line := range defaultLogLines {
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		// A non-following stream ends after the backlog, like the engine's.
		if opts.Follow {
			<-ctx.Done()
		}
	}()

	return lines, errs
}

func (c *Client) ContainerStats(ctx context.Context, id string) (<-chan docker.Stats, <-chan error) {
	out := make(chan docker.Stats, 4)
	errs := make(chan error, 1)

	if err := c.record(OpContainerStats, id, nil); err != nil {
		errs <- err
		close(out)
		close(errs)
		return out, errs
	}
	if !c.containerExists(id) {
		errs <- notFound("container.stats", id)
		close(out)
		close(errs)
		return out, errs
	}

	go func() {
		defer close(out)
		defer close(errs)
		for i := 0; i < 3; i++ {
			sample := docker.Stats{
				Timestamp:     time.Now().UTC(),
				CPUPercent:    float64(10 + i),
				MemoryUsage:   int64(100<<20 + i),
				MemoryLimit:   1 << 30,
				MemoryPercent: 10,
				PIDs:          5,
			}
			select {
			case out <- sample:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done()
	}()

	return out, errs
}

func (c *Client) Exec(_ context.Context, id string, opts docker.ExecOptions) (*docker.ExecSession, error) {
	if err := c.record(OpExec, id, opts); err != nil {
		return nil, err
	}
	if !c.containerExists(id) {
		return nil, notFound("container.exec", id)
	}

	// A pipe pair stands in for the hijacked connection: whatever the client
	// writes to stdin comes back as output, which is enough to prove the
	// plumbing without a container.
	pr, pw := io.Pipe()
	return &docker.ExecSession{
		ID:     "exec-" + id,
		Conn:   pw,
		Reader: pr,
		TTY:    opts.TTY,
		Close:  func() { _ = pw.Close(); _ = pr.Close() },
	}, nil
}

func (c *Client) ResizeExec(_ context.Context, execID string, rows, cols uint) error {
	return c.record(OpResizeExec, execID, [2]uint{rows, cols})
}

func (c *Client) ExecExitCode(_ context.Context, execID string) (int, error) {
	if err := c.record(OpExecExitCode, execID, nil); err != nil {
		return 0, err
	}
	return 0, nil
}

func (c *Client) Events(ctx context.Context) (<-chan docker.Event, <-chan error) {
	out := make(chan docker.Event, 4)
	errs := make(chan error, 1)

	if err := c.record(OpEvents, "", nil); err != nil {
		errs <- err
		close(out)
		close(errs)
		return out, errs
	}

	go func() {
		defer close(out)
		defer close(errs)
		select {
		case out <- docker.Event{Type: "container", Action: "start", Actor: "c1", Name: "web", Time: time.Now().UTC()}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}()

	return out, errs
}

// Image, volume and network mutation names accepted by Client.Fail.
const (
	OpPullImageProgress = "PullImageProgress"
	OpRemoveImage       = "RemoveImage"
	OpPruneImages       = "PruneImages"
	OpTagImage          = "TagImage"
	OpImageHistory      = "ImageHistory"
	OpInspectImageRaw   = "InspectImageRaw"

	OpCreateVolume     = "CreateVolume"
	OpInspectVolume    = "InspectVolume"
	OpInspectVolumeRaw = "InspectVolumeRaw"
	OpRemoveVolume     = "RemoveVolume"
	OpPruneVolumes     = "PruneVolumes"

	OpCreateNetwork     = "CreateNetwork"
	OpInspectNetwork    = "InspectNetwork"
	OpInspectNetworkRaw = "InspectNetworkRaw"
	OpRemoveNetwork     = "RemoveNetwork"
	OpPruneNetworks     = "PruneNetworks"
	OpConnectNetwork    = "ConnectNetwork"
	OpDisconnectNetwork = "DisconnectNetwork"
)

// PullEvents is what PullImageProgress replays. Tests set it to drive a
// progress bar; the default is one small layer that completes.
var defaultPullEvents = []docker.PullEvent{
	{Status: "Pulling from library/nginx"},
	{ID: "layer1", Status: "Downloading", Current: 512, Total: 1024},
	{ID: "layer1", Status: "Download complete", Current: 1024, Total: 1024},
	{Status: "Status: Downloaded newer image"},
}

// PullEvents overrides the replayed pull progress when non-nil.
func (c *Client) SetPullEvents(events []docker.PullEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pullEvents = events
}

func (c *Client) PullImageProgress(ctx context.Context, opts docker.PullOptions) (<-chan docker.PullEvent, <-chan error) {
	events := make(chan docker.PullEvent, 8)
	errs := make(chan error, 1)

	if err := c.record(OpPullImageProgress, opts.Ref, opts); err != nil {
		errs <- err
		close(events)
		close(errs)
		return events, errs
	}

	c.mu.Lock()
	replay := c.pullEvents
	if replay == nil {
		replay = defaultPullEvents
	}
	c.mu.Unlock()

	go func() {
		defer close(events)
		defer close(errs)
		for _, e := range replay {
			select {
			case events <- e:
			case <-ctx.Done():
				return
			}
			if e.Error != "" {
				errs <- docker.NewError(docker.KindUnknown, "image.pull", "image", e.ID, e.Error)
				return
			}
		}
	}()

	return events, errs
}

func (c *Client) RemoveImage(_ context.Context, id string, opts docker.RemoveImageOptions) ([]docker.ImageDeleted, error) {
	if err := c.record(OpRemoveImage, id, opts); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for i, img := range c.Images {
		if img.ID != id && !containsString(img.RepoTags, id) {
			continue
		}
		if img.Containers > 0 && !opts.Force {
			return nil, docker.NewError(docker.KindConflict, "image.remove", "image", id,
				"image is in use by a container")
		}
		c.Images = append(c.Images[:i], c.Images[i+1:]...)
		return []docker.ImageDeleted{{Deleted: img.ID}}, nil
	}
	return nil, docker.NewError(docker.KindNotFound, "image.remove", "image", id, "no such image: "+id)
}

func (c *Client) PruneImages(_ context.Context, all bool) (docker.PruneReport, error) {
	if err := c.record(OpPruneImages, "", all); err != nil {
		return docker.PruneReport{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	report := docker.PruneReport{Deleted: []string{}}
	kept := c.Images[:0]
	for _, img := range c.Images {
		removable := img.Dangling || (all && img.Containers <= 0)
		if removable {
			report.Deleted = append(report.Deleted, img.ID)
			report.SpaceReclaimed += img.Size
			continue
		}
		kept = append(kept, img)
	}
	c.Images = kept
	return report, nil
}

func (c *Client) TagImage(_ context.Context, id, ref string) error {
	if err := c.record(OpTagImage, id, ref); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for i, img := range c.Images {
		if img.ID == id || containsString(img.RepoTags, id) {
			c.Images[i].RepoTags = append(c.Images[i].RepoTags, ref)
			c.Images[i].Dangling = false
			return nil
		}
	}
	return docker.NewError(docker.KindNotFound, "image.tag", "image", id, "no such image: "+id)
}

func (c *Client) ImageHistory(_ context.Context, id string) ([]docker.ImageHistoryEntry, error) {
	if err := c.record(OpImageHistory, id, nil); err != nil {
		return nil, err
	}
	if !c.imageExists(id) {
		return nil, docker.NewError(docker.KindNotFound, "image.history", "image", id, "no such image: "+id)
	}
	return []docker.ImageHistoryEntry{
		{ID: id, CreatedBy: "CMD [\"nginx\"]", Size: 0, Tags: []string{}},
		{ID: "<missing>", CreatedBy: "ADD file:… in /", Size: 1 << 20, Tags: []string{}},
	}, nil
}

func (c *Client) InspectImageRaw(_ context.Context, id string) (docker.RawInspect, error) {
	if err := c.record(OpInspectImageRaw, id, nil); err != nil {
		return nil, err
	}
	if !c.imageExists(id) {
		return nil, docker.NewError(docker.KindNotFound, "image.inspect", "image", id, "no such image: "+id)
	}
	return json.RawMessage(`{"Id":"` + id + `"}`), nil
}

// imageExists reports whether id names a known image, by ID or by tag.
func (c *Client) imageExists(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, img := range c.Images {
		if img.ID == id || containsString(img.RepoTags, id) {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func (c *Client) CreateVolume(_ context.Context, opts docker.CreateVolumeOptions) (docker.Volume, error) {
	if err := c.record(OpCreateVolume, opts.Name, opts); err != nil {
		return docker.Volume{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, v := range c.Volumes {
		if v.Name == opts.Name {
			// The real engine is idempotent here: creating an existing volume
			// returns it rather than failing.
			return v, nil
		}
	}

	created := docker.Volume{
		Name:       opts.Name,
		Driver:     orDefault(opts.Driver, "local"),
		Mountpoint: "/var/lib/docker/volumes/" + opts.Name + "/_data",
		Scope:      "local",
		CreatedAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Labels:     orEmptyMap(opts.Labels),
		Options:    orEmptyMap(opts.DriverOpts),
		Size:       -1,
		RefCount:   -1,
	}
	c.Volumes = append(c.Volumes, created)
	return created, nil
}

func (c *Client) InspectVolume(_ context.Context, name string) (docker.Volume, error) {
	if err := c.record(OpInspectVolume, name, nil); err != nil {
		return docker.Volume{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range c.Volumes {
		if v.Name == name {
			return v, nil
		}
	}
	return docker.Volume{}, docker.NewError(docker.KindNotFound, "volume.inspect", "volume", name,
		"no such volume: "+name)
}

func (c *Client) InspectVolumeRaw(ctx context.Context, name string) (docker.RawInspect, error) {
	if err := c.record(OpInspectVolumeRaw, name, nil); err != nil {
		return nil, err
	}
	if _, err := c.InspectVolume(ctx, name); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"Name":"` + name + `"}`), nil
}

func (c *Client) RemoveVolume(_ context.Context, name string, force bool) error {
	if err := c.record(OpRemoveVolume, name, force); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, v := range c.Volumes {
		if v.Name != name {
			continue
		}
		if v.RefCount > 0 && !force {
			return docker.NewError(docker.KindConflict, "volume.remove", "volume", name,
				"volume is in use")
		}
		c.Volumes = append(c.Volumes[:i], c.Volumes[i+1:]...)
		return nil
	}
	return docker.NewError(docker.KindNotFound, "volume.remove", "volume", name,
		"no such volume: "+name)
}

func (c *Client) PruneVolumes(_ context.Context) (docker.PruneReport, error) {
	if err := c.record(OpPruneVolumes, "", nil); err != nil {
		return docker.PruneReport{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	report := docker.PruneReport{Deleted: []string{}}
	kept := c.Volumes[:0]
	for _, v := range c.Volumes {
		if v.RefCount <= 0 {
			report.Deleted = append(report.Deleted, v.Name)
			continue
		}
		kept = append(kept, v)
	}
	c.Volumes = kept
	return report, nil
}

func (c *Client) CreateNetwork(_ context.Context, opts docker.CreateNetworkOptions) (docker.Network, error) {
	if err := c.record(OpCreateNetwork, opts.Name, opts); err != nil {
		return docker.Network{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, n := range c.Networks {
		if n.Name == opts.Name {
			return docker.Network{}, docker.NewError(docker.KindConflict, "network.create", "network",
				opts.Name, "network with name "+opts.Name+" already exists")
		}
	}

	ipam := opts.IPAM
	if ipam == nil {
		ipam = []docker.IPAMConfig{}
	}
	created := docker.Network{
		ID:         "net-" + opts.Name,
		Name:       opts.Name,
		Driver:     orDefault(opts.Driver, "bridge"),
		Scope:      "local",
		Created:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Internal:   opts.Internal,
		Attachable: opts.Attachable,
		EnableIPv6: opts.EnableIPv6,
		IPAM:       ipam,
		Labels:     orEmptyMap(opts.Labels),
	}
	c.Networks = append(c.Networks, created)
	return created, nil
}

func (c *Client) InspectNetwork(_ context.Context, id string) (docker.Network, error) {
	if err := c.record(OpInspectNetwork, id, nil); err != nil {
		return docker.Network{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.Networks {
		if n.ID == id || n.Name == id {
			return n, nil
		}
	}
	return docker.Network{}, docker.NewError(docker.KindNotFound, "network.inspect", "network", id,
		"no such network: "+id)
}

func (c *Client) InspectNetworkRaw(ctx context.Context, id string) (docker.RawInspect, error) {
	if err := c.record(OpInspectNetworkRaw, id, nil); err != nil {
		return nil, err
	}
	if _, err := c.InspectNetwork(ctx, id); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"Id":"` + id + `"}`), nil
}

func (c *Client) RemoveNetwork(_ context.Context, id string) error {
	if err := c.record(OpRemoveNetwork, id, nil); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, n := range c.Networks {
		if n.ID != id && n.Name != id {
			continue
		}
		if n.ContainerCount > 0 {
			return docker.NewError(docker.KindConflict, "network.remove", "network", id,
				"network has active endpoints")
		}
		c.Networks = append(c.Networks[:i], c.Networks[i+1:]...)
		return nil
	}
	return docker.NewError(docker.KindNotFound, "network.remove", "network", id,
		"no such network: "+id)
}

func (c *Client) PruneNetworks(_ context.Context) (docker.PruneReport, error) {
	if err := c.record(OpPruneNetworks, "", nil); err != nil {
		return docker.PruneReport{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	report := docker.PruneReport{Deleted: []string{}}
	kept := c.Networks[:0]
	for _, n := range c.Networks {
		// The engine never prunes its own predefined networks.
		predefined := n.Name == "bridge" || n.Name == "host" || n.Name == "none"
		if !predefined && n.ContainerCount == 0 {
			report.Deleted = append(report.Deleted, n.Name)
			continue
		}
		kept = append(kept, n)
	}
	c.Networks = kept
	return report, nil
}

func (c *Client) ConnectNetwork(ctx context.Context, networkID, containerID string, opts docker.ConnectOptions) error {
	if err := c.record(OpConnectNetwork, networkID, opts); err != nil {
		return err
	}
	if _, err := c.InspectNetwork(ctx, networkID); err != nil {
		return err
	}
	if !c.containerExists(containerID) {
		return notFound("network.connect", containerID)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, n := range c.Networks {
		if n.ID == networkID || n.Name == networkID {
			c.Networks[i].ContainerCount++
		}
	}
	return nil
}

func (c *Client) DisconnectNetwork(ctx context.Context, networkID, containerID string, _ bool) error {
	if err := c.record(OpDisconnectNetwork, networkID, containerID); err != nil {
		return err
	}
	if _, err := c.InspectNetwork(ctx, networkID); err != nil {
		return err
	}
	if !c.containerExists(containerID) {
		return notFound("network.disconnect", containerID)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, n := range c.Networks {
		if (n.ID == networkID || n.Name == networkID) && c.Networks[i].ContainerCount > 0 {
			c.Networks[i].ContainerCount--
		}
	}
	return nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// OpBuildImage is accepted by Client.Fail.
const OpBuildImage = "BuildImage"

// defaultBuildEvents is a small successful build: a base-image pull, three
// steps and the resulting image id.
var defaultBuildEvents = []docker.BuildEvent{
	{Status: "Pulling from library/alpine", ID: "3.20"},
	{Stream: "Step 1/3 : FROM alpine:3.20\n", Step: 1, TotalSteps: 3},
	{Stream: "Step 2/3 : COPY . /app\n", Step: 2, TotalSteps: 3},
	{Stream: "Step 3/3 : CMD [\"/app/run\"]\n", Step: 3, TotalSteps: 3},
	{Stream: "Successfully built sha256:built1\n"},
	{ImageID: "sha256:built1"},
}

// SetBuildEvents overrides the replayed build output.
func (c *Client) SetBuildEvents(events []docker.BuildEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buildEvents = events
}

// BuiltContexts returns the tar contexts the fake was handed, so a test can
// assert on what was actually packed.
func (c *Client) BuiltContexts() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.builtContexts))
	copy(out, c.builtContexts)
	return out
}

func (c *Client) BuildImage(ctx context.Context, opts docker.BuildOptions) (<-chan docker.BuildEvent, <-chan error) {
	events := make(chan docker.BuildEvent, 16)
	errs := make(chan error, 1)

	// The context is drained here rather than in the goroutine: the caller
	// closes its reader when BuildImage returns, exactly as the real engine
	// makes it safe to.
	var packed []byte
	if opts.Context != nil {
		packed, _ = io.ReadAll(opts.Context)
	}

	if err := c.record(OpBuildImage, strings.Join(opts.Tags, ","), opts); err != nil {
		errs <- err
		close(events)
		close(errs)
		return events, errs
	}

	c.mu.Lock()
	c.builtContexts = append(c.builtContexts, packed)
	replay := c.buildEvents
	if replay == nil {
		replay = defaultBuildEvents
	}
	c.mu.Unlock()

	go func() {
		defer close(events)
		defer close(errs)
		for _, e := range replay {
			select {
			case events <- e:
			case <-ctx.Done():
				return
			}
			if e.Error != "" {
				errs <- docker.NewError(docker.KindUnknown, "image.build", "image", "", e.Error)
				return
			}
		}
	}()

	return events, errs
}
