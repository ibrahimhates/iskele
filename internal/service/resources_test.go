package service

import (
	"context"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/hostinfo"
)

func TestImageListForwardsFilters(t *testing.T) {
	f := fake.New()
	svc := NewImage(f, nil, nil)
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

	volumes, err := NewVolume(f, nil).List(context.Background())
	if err != nil {
		t.Fatalf("volume List() error = %v", err)
	}
	if len(volumes) != 1 || volumes[0].Name != "web-data" {
		t.Errorf("volumes = %+v", volumes)
	}

	networks, err := NewNetwork(f, nil).List(context.Background())
	if err != nil {
		t.Fatalf("network List() error = %v", err)
	}
	if len(networks) != 1 || networks[0].Name != "bridge" {
		t.Errorf("networks = %+v", networks)
	}
}

func TestSystemServiceReadsEngineState(t *testing.T) {
	f := fake.New()
	svc := NewSystem(f, nil)
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
		"images":   func() error { _, err := NewImage(f, nil, nil).List(ctx, ImageListOptions{}); return err },
		"volumes":  func() error { _, err := NewVolume(f, nil).List(ctx); return err },
		"networks": func() error { _, err := NewNetwork(f, nil).List(ctx); return err },
		"info":     func() error { _, err := NewSystem(f, nil).Info(ctx); return err },
		"df":       func() error { _, err := NewSystem(f, nil).DiskUsage(ctx); return err },
		"ping":     func() error { _, err := NewSystem(f, nil).Ping(ctx); return err },
	}

	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); !docker.IsUnavailable(err) {
				t.Errorf("error = %v, want KindUnavailable", err)
			}
		})
	}
}

func TestHostReportsTheMachineAndTheDaemon(t *testing.T) {
	f := fake.New()
	svc := NewSystem(f, []hostinfo.Target{{Label: "data", Path: t.TempDir()}})

	report := svc.Host(context.Background())

	if report.Engine == nil {
		t.Fatalf("no engine summary: %s", report.EngineError)
	}
	if report.Engine.Version != "28.5.2" || report.Engine.Running != 1 {
		t.Errorf("engine = %+v", report.Engine)
	}
	if report.Daemon.Version == "" || report.Daemon.GoVersion == "" {
		t.Errorf("daemon = %+v", report.Daemon)
	}
	if report.Daemon.StartedAt.IsZero() {
		t.Error("the daemon start time was not reported")
	}
	if report.Memory.Total == 0 {
		t.Error("no memory reading")
	}
	if len(report.Disks) == 0 {
		t.Error("no disk reading for the data directory")
	}
	// The very first reading has no previous sample to measure against, and
	// says so rather than reporting usage since boot.
	if report.CPU.Percent != -1 {
		t.Errorf("first CPU reading = %v, want -1", report.CPU.Percent)
	}
	if second := svc.Host(context.Background()); second.CPU.Percent < 0 {
		t.Errorf("second CPU reading = %v, want a real percentage", second.CPU.Percent)
	}
}

// Host metrics do not come from Docker, so they must survive Docker being
// down: this panel is most useful exactly then.
func TestHostReportsTheMachineWhenTheEngineIsDown(t *testing.T) {
	f := fake.New()
	f.Fail(fake.OpInfo, docker.NewError(docker.KindUnavailable, "info", "system", "", "daemon is gone"))

	report := NewSystem(f, []hostinfo.Target{{Label: "data", Path: t.TempDir()}}).Host(context.Background())

	if report.Engine != nil {
		t.Errorf("engine = %+v, want nil", report.Engine)
	}
	if report.EngineError == "" {
		t.Error("the engine failure was not explained")
	}
	if report.Memory.Total == 0 || len(report.Disks) == 0 {
		t.Error("the host reading was lost along with the engine")
	}
}
