package service

import (
	"context"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
)

func TestImageListForwardsFilters(t *testing.T) {
	f := fake.New()
	svc := NewImage(f)
	dangling := true

	images, err := svc.List(context.Background(), ImageListOptions{
		All:      true,
		Dangling: &dangling,
		Label:    []string{"app=web"},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("got %d images, want 1", len(images))
	}

	opts, ok := f.CallsFor(fake.OpListImages)[0].Opts.(docker.ListImagesOptions)
	if !ok {
		t.Fatalf("engine received %T", f.CallsFor(fake.OpListImages)[0].Opts)
	}
	if !opts.All || opts.Dangling == nil || !*opts.Dangling {
		t.Errorf("options not forwarded: %+v", opts)
	}
	if len(opts.Label) != 1 {
		t.Errorf("Label = %v", opts.Label)
	}
}

func TestVolumeAndNetworkList(t *testing.T) {
	f := fake.New()

	volumes, err := NewVolume(f).List(context.Background())
	if err != nil {
		t.Fatalf("volume List() error = %v", err)
	}
	if len(volumes) != 1 || volumes[0].Name != "web-data" {
		t.Errorf("volumes = %+v", volumes)
	}

	networks, err := NewNetwork(f).List(context.Background())
	if err != nil {
		t.Fatalf("network List() error = %v", err)
	}
	if len(networks) != 1 || networks[0].Name != "bridge" {
		t.Errorf("networks = %+v", networks)
	}
}

func TestSystemServiceReadsEngineState(t *testing.T) {
	f := fake.New()
	svc := NewSystem(f)
	ctx := context.Background()

	info, err := svc.Info(ctx)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.ServerVersion != "28.5.2" || info.ContainersRunning != 1 {
		t.Errorf("info = %+v", info)
	}

	usage, err := svc.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if usage.Images.Count != 1 {
		t.Errorf("usage = %+v", usage)
	}

	pong, err := svc.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if pong.APIVersion == "" {
		t.Error("Ping() returned no API version")
	}
}

func TestResourceServicesPropagateEngineFailures(t *testing.T) {
	f := fake.New()
	boom := docker.NewError(docker.KindUnavailable, "op", "system", "", "daemon is gone")

	for _, op := range []string{fake.OpListImages, fake.OpListVolumes, fake.OpListNetworks, fake.OpInfo, fake.OpDiskUsage, fake.OpPing} {
		f.Fail(op, boom)
	}

	ctx := context.Background()
	checks := map[string]func() error{
		"images":   func() error { _, err := NewImage(f).List(ctx, ImageListOptions{}); return err },
		"volumes":  func() error { _, err := NewVolume(f).List(ctx); return err },
		"networks": func() error { _, err := NewNetwork(f).List(ctx); return err },
		"info":     func() error { _, err := NewSystem(f).Info(ctx); return err },
		"df":       func() error { _, err := NewSystem(f).DiskUsage(ctx); return err },
		"ping":     func() error { _, err := NewSystem(f).Ping(ctx); return err },
	}

	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); !docker.IsUnavailable(err) {
				t.Errorf("error = %v, want KindUnavailable", err)
			}
		})
	}
}
