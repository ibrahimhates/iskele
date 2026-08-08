package service

import (
	"context"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
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
}

// NewSystem builds the system service.
func NewSystem(client docker.Client) *System { return &System{docker: client} }

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
