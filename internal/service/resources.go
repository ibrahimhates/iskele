package service

import (
	"context"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// Image serves the image listing.
type Image struct {
	docker docker.Client
}

// NewImage builds the image service.
func NewImage(client docker.Client) *Image { return &Image{docker: client} }

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

// Volume serves the volume listing.
type Volume struct {
	docker docker.Client
}

// NewVolume builds the volume service.
func NewVolume(client docker.Client) *Volume { return &Volume{docker: client} }

// List returns every volume.
func (s *Volume) List(ctx context.Context) ([]docker.Volume, error) {
	return s.docker.ListVolumes(ctx)
}

// Network serves the network listing.
type Network struct {
	docker docker.Client
}

// NewNetwork builds the network service.
func NewNetwork(client docker.Client) *Network { return &Network{docker: client} }

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
