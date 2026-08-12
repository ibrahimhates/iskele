package docker

import (
	"context"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
)

// ListContainers returns the containers matching opts.
func (e *engine) ListContainers(ctx context.Context, opts ListContainersOptions) ([]Container, error) {
	summaries, err := e.api.ContainerList(ctx, container.ListOptions{
		All:     opts.All,
		Size:    opts.Size,
		Filters: containerFilters(opts),
	})
	if err != nil {
		return nil, classify("container.list", "container", "", err)
	}

	out := make([]Container, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, toContainer(s, opts.Size))
	}
	return out, nil
}

// InspectContainer returns the detailed view of a single container.
func (e *engine) InspectContainer(ctx context.Context, id string) (ContainerDetail, error) {
	resp, err := e.api.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerDetail{}, classify("container.inspect", "container", id, err)
	}
	return toContainerDetail(resp), nil
}

// InspectContainerRaw returns the engine's unmodified inspect JSON, which the
// UI shows in its Inspect tab.
func (e *engine) InspectContainerRaw(ctx context.Context, id string) (RawInspect, error) {
	_, raw, err := e.api.ContainerInspectWithRaw(ctx, id, false)
	if err != nil {
		return nil, classify("container.inspect", "container", id, err)
	}
	return raw, nil
}

// StartContainer starts a stopped container.
func (e *engine) StartContainer(ctx context.Context, id string) error {
	err := e.api.ContainerStart(ctx, id, container.StartOptions{})
	return classify("container.start", "container", id, err)
}

// StopContainer stops a running container, waiting up to opts.Timeout seconds
// before the engine sends SIGKILL.
func (e *engine) StopContainer(ctx context.Context, id string, opts StopOptions) error {
	err := e.api.ContainerStop(ctx, id, container.StopOptions{Timeout: opts.Timeout})
	return classify("container.stop", "container", id, err)
}

// RestartContainer stops then starts a container.
func (e *engine) RestartContainer(ctx context.Context, id string, opts StopOptions) error {
	err := e.api.ContainerRestart(ctx, id, container.StopOptions{Timeout: opts.Timeout})
	return classify("container.restart", "container", id, err)
}

// RemoveContainer deletes a container, optionally forcing it and removing its
// anonymous volumes.
func (e *engine) RemoveContainer(ctx context.Context, id string, opts RemoveContainerOptions) error {
	err := e.api.ContainerRemove(ctx, id, container.RemoveOptions{
		Force:         opts.Force,
		RemoveVolumes: opts.RemoveVolumes,
		RemoveLinks:   opts.RemoveLinks,
	})
	return classify("container.remove", "container", id, err)
}

// PruneContainers removes every stopped container.
//
// No filter is passed, so this is exactly `docker container prune`: containers
// in the created, exited or dead states go, running and paused ones stay. The
// engine decides what "stopped" means rather than this code guessing from a
// listing that may be a second out of date.
func (e *engine) PruneContainers(ctx context.Context) (PruneReport, error) {
	report, err := e.api.ContainersPrune(ctx, filters.NewArgs())
	if err != nil {
		return PruneReport{}, classify("container.prune", "container", "", err)
	}

	return PruneReport{
		Deleted: sortedStrings(report.ContainersDeleted),
		//nolint:gosec // a reclaimed byte count cannot overflow int64
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

// containerFilters translates our options into engine-side filters. Filtering
// at the daemon keeps large hosts responsive.
func containerFilters(opts ListContainersOptions) filters.Args {
	args := filters.NewArgs()
	for _, l := range opts.Label {
		args.Add("label", l)
	}
	for _, s := range opts.Status {
		args.Add("status", s)
	}
	if opts.Name != "" {
		args.Add("name", opts.Name)
	}
	return args
}

// healthPattern extracts the health state the engine appends to the status
// line, e.g. "Up 2 hours (healthy)".
var healthPattern = regexp.MustCompile(`\((healthy|unhealthy|health: starting)\)`)

func toContainer(s container.Summary, withSize bool) Container {
	c := Container{
		ID:         s.ID,
		Names:      trimNames(s.Names),
		Image:      s.Image,
		ImageID:    s.ImageID,
		Command:    s.Command,
		Created:    time.Unix(s.Created, 0).UTC(),
		State:      s.State,
		Status:     s.Status,
		Health:     healthFromStatus(s.Status),
		Ports:      toPorts(s.Ports),
		Labels:     s.Labels,
		Networks:   networkNames(s),
		Mounts:     mountDestinations(s.Mounts),
		SizeRW:     -1,
		SizeRootFS: -1,
	}
	if len(c.Names) > 0 {
		c.Name = c.Names[0]
	}
	if withSize {
		c.SizeRW = s.SizeRw
		c.SizeRootFS = s.SizeRootFs
	}
	if c.Labels == nil {
		c.Labels = map[string]string{}
	}
	return c
}

func toContainerDetail(r container.InspectResponse) ContainerDetail {
	d := ContainerDetail{}
	if r.ContainerJSONBase == nil {
		return d
	}

	d.ID = r.ID
	d.Name = strings.TrimPrefix(r.Name, "/")
	d.Names = []string{d.Name}
	d.Image = r.Image
	d.ImageID = r.Image
	d.Created = parseDockerTime(r.Created)
	d.RestartCount = r.RestartCount
	d.Platform = r.Platform
	d.Driver = r.Driver
	d.LogPath = r.LogPath
	d.Path = r.Path
	d.Args = r.Args
	d.SizeRW = -1
	d.SizeRootFS = -1
	d.Ports = []Port{}
	d.Mounts = []string{}
	d.Networks = []string{}
	d.Labels = map[string]string{}

	if r.State != nil {
		d.State = r.State.Status
		d.Status = r.State.Status
		d.ExitCode = r.State.ExitCode
		d.OOMKilled = r.State.OOMKilled
		d.Pid = r.State.Pid
		d.Error = r.State.Error
		d.StartedAt = parseDockerTime(r.State.StartedAt)
		d.FinishedAt = parseDockerTime(r.State.FinishedAt)

		if r.State.Health != nil {
			d.Health = r.State.Health.Status
			d.HealthCheck = &ContainerHealth{
				Status:        r.State.Health.Status,
				FailingStreak: r.State.Health.FailingStreak,
				LastOutput:    lastHealthOutput(r.State.Health.Log),
			}
		}
	}

	if r.Config != nil {
		d.Image = r.Config.Image
		d.Entrypoint = r.Config.Entrypoint
		d.Cmd = r.Config.Cmd
		d.Env = r.Config.Env
		d.WorkingDir = r.Config.WorkingDir
		d.User = r.Config.User
		d.Hostname = r.Config.Hostname
		if r.Config.Labels != nil {
			d.Labels = r.Config.Labels
		}
	}

	if r.HostConfig != nil {
		d.RestartPolicy = string(r.HostConfig.RestartPolicy.Name)
		d.Privileged = r.HostConfig.Privileged
	}

	d.MountPoints = make([]MountPoint, 0, len(r.Mounts))
	for _, m := range r.Mounts {
		d.MountPoints = append(d.MountPoints, MountPoint{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			Driver:      m.Driver,
			Mode:        m.Mode,
			RW:          m.RW,
			Propagation: string(m.Propagation),
		})
		d.Mounts = append(d.Mounts, m.Destination)
	}

	if r.NetworkSettings != nil {
		d.NetworkList = make([]NetworkAttachment, 0, len(r.NetworkSettings.Networks))
		for name, n := range r.NetworkSettings.Networks {
			if n == nil {
				continue
			}
			d.NetworkList = append(d.NetworkList, NetworkAttachment{
				Name:        name,
				NetworkID:   n.NetworkID,
				EndpointID:  n.EndpointID,
				IPAddress:   n.IPAddress,
				IPPrefixLen: n.IPPrefixLen,
				Gateway:     n.Gateway,
				MacAddress:  n.MacAddress,
				Aliases:     n.Aliases,
			})
			d.Networks = append(d.Networks, name)
		}
		sortStrings(d.Networks)
		sortAttachments(d.NetworkList)

		d.PortBindings = map[string][]Port{}
		for portProto, bindings := range r.NetworkSettings.Ports {
			key := string(portProto)
			for _, b := range bindings {
				d.PortBindings[key] = append(d.PortBindings[key], Port{
					IP:          b.HostIP,
					PublicPort:  parsePort(b.HostPort),
					PrivatePort: parsePort(portProto.Port()),
					Type:        portProto.Proto(),
				})
			}
		}
	}

	return d
}

func trimNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, strings.TrimPrefix(n, "/"))
	}
	return out
}

func healthFromStatus(status string) string {
	m := healthPattern.FindStringSubmatch(status)
	if len(m) < 2 {
		return ""
	}
	if m[1] == "health: starting" {
		return "starting"
	}
	return m[1]
}

func toPorts(ports []container.Port) []Port {
	out := make([]Port, 0, len(ports))
	for _, p := range ports {
		out = append(out, Port{
			IP:          p.IP,
			PrivatePort: p.PrivatePort,
			PublicPort:  p.PublicPort,
			Type:        p.Type,
		})
	}
	return out
}

func networkNames(s container.Summary) []string {
	if s.NetworkSettings == nil {
		return []string{}
	}
	out := make([]string, 0, len(s.NetworkSettings.Networks))
	for name := range s.NetworkSettings.Networks {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

func mountDestinations(mounts []container.MountPoint) []string {
	out := make([]string, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, m.Destination)
	}
	return out
}

func lastHealthOutput(log []*container.HealthcheckResult) string {
	if len(log) == 0 {
		return ""
	}
	last := log[len(log)-1]
	if last == nil {
		return ""
	}
	return strings.TrimSpace(last.Output)
}

// parseDockerTime accepts the RFC3339Nano timestamps the engine emits, and
// returns the zero time for the "never happened" sentinel it uses.
func parseDockerTime(s string) time.Time {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// PauseContainer freezes a running container's processes.
func (e *engine) PauseContainer(ctx context.Context, id string) error {
	return classify("container.pause", "container", id, e.api.ContainerPause(ctx, id))
}

// UnpauseContainer resumes a paused container.
func (e *engine) UnpauseContainer(ctx context.Context, id string) error {
	return classify("container.unpause", "container", id, e.api.ContainerUnpause(ctx, id))
}

// KillContainer sends a signal to a container's main process. An empty signal
// means SIGKILL, matching `docker kill`.
func (e *engine) KillContainer(ctx context.Context, id, signal string) error {
	return classify("container.kill", "container", id, e.api.ContainerKill(ctx, id, signal))
}

// RenameContainer changes a container's name.
func (e *engine) RenameContainer(ctx context.Context, id, newName string) error {
	return classify("container.rename", "container", id, e.api.ContainerRename(ctx, id, newName))
}

// CreateContainer creates a container from a spec and returns its ID.
func (e *engine) CreateContainer(ctx context.Context, spec CreateSpec) (string, error) {
	resp, err := e.api.ContainerCreate(ctx, spec.Config, spec.HostConfig, spec.NetworkingConfig, nil, spec.Name)
	if err != nil {
		return "", classify("container.create", "container", spec.Name, err)
	}
	return resp.ID, nil
}

// RawInspectConfig returns the SDK structures needed to recreate a container
// exactly as it is. Redeploy is the only caller: deriving a new container from
// an old one needs the engine's own view, not our projection.
func (e *engine) RawInspectConfig(ctx context.Context, id string) (CreateSpec, error) {
	resp, err := e.api.ContainerInspect(ctx, id)
	if err != nil {
		return CreateSpec{}, classify("container.inspect", "container", id, err)
	}
	if resp.ContainerJSONBase == nil || resp.Config == nil {
		return CreateSpec{}, NewError(KindUnknown, "container.inspect", "container", id,
			"the engine returned an incomplete inspect response")
	}

	spec := CreateSpec{
		Name:       strings.TrimPrefix(resp.Name, "/"),
		Config:     resp.Config,
		HostConfig: resp.HostConfig,
	}
	if resp.NetworkSettings != nil && len(resp.NetworkSettings.Networks) > 0 {
		spec.NetworkingConfig = &network.NetworkingConfig{
			EndpointsConfig: resp.NetworkSettings.Networks,
		}
	}
	return spec, nil
}

// PullImage pulls an image, draining the progress stream. Redeploy uses it to
// fetch a newer image before recreating a container.
func (e *engine) PullImage(ctx context.Context, ref string) error {
	body, err := e.api.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return classify("image.pull", "image", ref, err)
	}
	defer func() { _ = body.Close() }()

	// The pull only completes once the response body is consumed.
	if _, err := io.Copy(io.Discard, body); err != nil {
		return classify("image.pull", "image", ref, err)
	}
	return nil
}
