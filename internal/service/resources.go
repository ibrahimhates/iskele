package service

import (
	"context"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/hostinfo"
	"github.com/ibrahimhates/iskele/internal/version"
)

// Image serves image listing and management.
type Image struct {
	docker docker.Client
	// registries supplies the credential for a pull from a private registry.
	registries *Registry
	recorder   *audit.Recorder
}

// NewImage builds the image service. registries and recorder may be nil in
// tests that only read.
func NewImage(client docker.Client, registries *Registry, recorder *audit.Recorder) *Image {
	return &Image{docker: client, registries: registries, recorder: recorder}
}

// ImageListOptions mirrors the query parameters of GET /images.
type ImageListOptions struct {
	All      bool
	Dangling *bool
	Label    []string
}

// List returns the images matching opts.
func (s *Image) List(ctx context.Context, opts ImageListOptions) ([]docker.Image, error) {
	return s.docker.ListImages(ctx, docker.ListImagesOptions{
		All:      opts.All,
		Dangling: opts.Dangling,
		Label:    opts.Label,
	})
}

// Volume serves volume listing and management.
type Volume struct {
	docker   docker.Client
	recorder *audit.Recorder
}

// NewVolume builds the volume service.
func NewVolume(client docker.Client, recorder *audit.Recorder) *Volume {
	return &Volume{docker: client, recorder: recorder}
}

// List returns every volume.
func (s *Volume) List(ctx context.Context) ([]docker.Volume, error) {
	return s.docker.ListVolumes(ctx)
}

// Network serves network listing and management.
type Network struct {
	docker   docker.Client
	recorder *audit.Recorder
}

// NewNetwork builds the network service.
func NewNetwork(client docker.Client, recorder *audit.Recorder) *Network {
	return &Network{docker: client, recorder: recorder}
}

// List returns every network.
func (s *Network) List(ctx context.Context) ([]docker.Network, error) {
	return s.docker.ListNetworks(ctx)
}

// System serves engine-wide information.
type System struct {
	docker docker.Client
	// host reads the machine the daemon runs on. It keeps the previous CPU
	// sample, so there is exactly one per daemon.
	host *hostinfo.Collector
	// diskTargets are the directories whose free space an operator cares
	// about; the engine's own root directory is added at read time.
	diskTargets []hostinfo.Target
}

// NewSystem builds the system service. diskTargets may be empty, in which case
// only the engine's root directory is measured.
func NewSystem(client docker.Client, diskTargets []hostinfo.Target) *System {
	return &System{docker: client, host: hostinfo.New(), diskTargets: diskTargets}
}

// Info summarizes the engine and host.
func (s *System) Info(ctx context.Context) (docker.SystemInfo, error) {
	return s.docker.Info(ctx)
}

// DiskUsage reports what `docker system df` shows.
func (s *System) DiskUsage(ctx context.Context) (docker.DiskUsage, error) {
	return s.docker.DiskUsage(ctx)
}

// Ping reports whether the engine is reachable.
func (s *System) Ping(ctx context.Context) (docker.Pong, error) {
	return s.docker.Ping(ctx)
}

// HostReport is what GET /system/host answers: the machine, plus the daemon
// running on it.
type HostReport struct {
	hostinfo.Metrics
	Daemon DaemonInfo `json:"daemon"`
	// Engine summarizes the Docker daemon, or is nil when it is unreachable.
	// The host's own numbers do not depend on the engine, so they are still
	// reported when it is down.
	Engine *EngineSummary `json:"engine,omitempty"`
	// EngineError explains a nil Engine.
	EngineError string `json:"engine_error,omitempty"`
}

// DaemonInfo is what iskeled can say about itself.
type DaemonInfo struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit,omitempty"`
	GoVersion string    `json:"go_version"`
	StartedAt time.Time `json:"started_at"`
	// Uptime is in seconds, to match the host's.
	Uptime int64 `json:"uptime"`
}

// EngineSummary is the handful of engine facts a dashboard shows beside the
// host metrics, so it does not have to fetch and filter the whole of
// /system/info.
type EngineSummary struct {
	Version    string `json:"version"`
	APIVersion string `json:"api_version"`
	OSType     string `json:"os_type,omitempty"`
	RootDir    string `json:"root_dir,omitempty"`
	Containers int    `json:"containers"`
	Running    int    `json:"running"`
	Paused     int    `json:"paused"`
	Stopped    int    `json:"stopped"`
	Images     int    `json:"images"`
}

// Host takes one reading of the machine.
//
// The engine is consulted first, but only to learn where its root directory
// is and to summarize it: an unreachable engine costs the report its Engine
// field and nothing else. A panel that goes blank because Docker is down is
// exactly the panel an operator needs while Docker is down.
func (s *System) Host(ctx context.Context) HostReport {
	targets := s.diskTargets

	report := HostReport{}
	if info, err := s.docker.Info(ctx); err != nil {
		report.EngineError = docker.Message(err)
	} else {
		report.Engine = &EngineSummary{
			Version:    info.ServerVersion,
			APIVersion: info.APIVersion,
			OSType:     info.OSType,
			RootDir:    info.DockerRootDir,
			Containers: info.Containers,
			Running:    info.ContainersRunning,
			Paused:     info.ContainersPaused,
			Stopped:    info.ContainersStopped,
			Images:     info.Images,
		}
		if info.DockerRootDir != "" {
			targets = append(append([]hostinfo.Target(nil), targets...),
				hostinfo.Target{Label: "docker", Path: info.DockerRootDir})
		}
	}

	report.Metrics = s.host.Read(ctx, targets)

	build := version.Get()
	report.Daemon = DaemonInfo{
		Version:   build.Version,
		Commit:    build.Commit,
		GoVersion: build.GoVersion,
		StartedAt: s.host.StartedAt().UTC(),
		Uptime:    int64(s.host.Uptime().Seconds()),
	}

	return report
}
