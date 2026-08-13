package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
)

const fakeImage = "sha256:img1"

func TestImageOperationsRejectABlankReference(t *testing.T) {
	f := fake.New()
	svc := NewImage(f, nil, nil)
	ctx := context.Background()

	operations := map[string]func() error{
		"Remove": func() error {
			_, err := svc.Remove(ctx, " ", docker.RemoveImageOptions{}, audit.Actor{}, RequestMeta{})
			return err
		},
		"Tag":     func() error { return svc.Tag(ctx, "", "x", audit.Actor{}, RequestMeta{}) },
		"History": func() error { _, err := svc.History(ctx, "\t"); return err },
		"Inspect": func() error { _, err := svc.Inspect(ctx, ""); return err },
	}

	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			if err := op(); !errors.Is(err, ErrEmptyName) {
				t.Errorf("error = %v, want ErrEmptyName", err)
			}
		})
	}

	if len(f.Calls()) != 0 {
		t.Errorf("the engine was reached %d times with a blank name", len(f.Calls()))
	}
}

func TestImageTagNeedsATarget(t *testing.T) {
	svc := NewImage(fake.New(), nil, nil)

	err := svc.Tag(context.Background(), fakeImage, "  ", audit.Actor{}, RequestMeta{})

	var specErr *docker.SpecError
	if !errors.As(err, &specErr) || specErr.Field != "tag" {
		t.Errorf("error = %v, want a SpecError naming the tag", err)
	}
}

func TestImageRemoveForwardsItsOptions(t *testing.T) {
	f := fake.New()
	svc := NewImage(f, nil, nil)

	if _, err := svc.Remove(context.Background(), fakeImage,
		docker.RemoveImageOptions{Force: true, NoPrune: true}, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	calls := f.CallsFor(fake.OpRemoveImage)
	if len(calls) != 1 {
		t.Fatalf("engine called %d times", len(calls))
	}
	opts, ok := calls[0].Opts.(docker.RemoveImageOptions)
	if !ok || !opts.Force || !opts.NoPrune {
		t.Errorf("options not forwarded: %+v", calls[0].Opts)
	}
}

func TestImagePullRejectsABlankReference(t *testing.T) {
	svc := NewImage(fake.New(), nil, nil)

	_, errs := svc.Pull(context.Background(), "  ", audit.Actor{}, RequestMeta{})

	err := <-errs
	var specErr *docker.SpecError
	if !errors.As(err, &specErr) {
		t.Errorf("error = %v, want a SpecError", err)
	}
}

func TestImagePullForwardsProgress(t *testing.T) {
	f := fake.New()
	svc := NewImage(f, nil, nil)

	events, errs := svc.Pull(context.Background(), "nginx:1.27", audit.Actor{}, RequestMeta{})

	count := 0
	for range events {
		count++
	}
	if err := <-errs; err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if count == 0 {
		t.Error("no progress reached the caller")
	}
}

func TestVolumeOperationsRejectABlankName(t *testing.T) {
	f := fake.New()
	svc := NewVolume(f, nil)
	ctx := context.Background()

	operations := map[string]func() error{
		"Get":     func() error { _, err := svc.Get(ctx, " "); return err },
		"Inspect": func() error { _, err := svc.Inspect(ctx, ""); return err },
		"Remove":  func() error { return svc.Remove(ctx, "\t", false, audit.Actor{}, RequestMeta{}) },
	}

	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			if err := op(); !errors.Is(err, ErrEmptyName) {
				t.Errorf("error = %v, want ErrEmptyName", err)
			}
		})
	}
}

func TestVolumeLifecycleThroughTheService(t *testing.T) {
	svc := NewVolume(fake.New(), nil)
	ctx := context.Background()

	created, err := svc.Create(ctx, docker.CreateVolumeOptions{Name: "  pgdata  "},
		audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "pgdata" {
		t.Errorf("name = %q, want it trimmed", created.Name)
	}

	if _, err := svc.Get(ctx, "pgdata"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if err := svc.Remove(ctx, "pgdata", false, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := svc.Get(ctx, "pgdata"); !docker.IsNotFound(err) {
		t.Errorf("error after removal = %v, want not-found", err)
	}
}

func TestNetworkCreateNeedsAName(t *testing.T) {
	f := fake.New()
	svc := NewNetwork(f, nil)

	_, err := svc.Create(context.Background(), docker.CreateNetworkOptions{Name: "  "},
		audit.Actor{}, RequestMeta{})
	if !errors.Is(err, ErrEmptyName) {
		t.Errorf("error = %v, want ErrEmptyName", err)
	}
	if len(f.CallsFor(fake.OpCreateNetwork)) != 0 {
		t.Error("the engine was reached with a blank network name")
	}
}

func TestNetworkConnectValidatesBothSides(t *testing.T) {
	svc := NewNetwork(fake.New(), nil)
	ctx := context.Background()

	if err := svc.Connect(ctx, "", "abc", docker.ConnectOptions{},
		audit.Actor{}, RequestMeta{}); !errors.Is(err, ErrEmptyName) {
		t.Errorf("blank network = %v, want ErrEmptyName", err)
	}
	if err := svc.Connect(ctx, "bridge", "  ", docker.ConnectOptions{},
		audit.Actor{}, RequestMeta{}); !errors.Is(err, ErrEmptyID) {
		t.Errorf("blank container = %v, want ErrEmptyID", err)
	}
}

func TestNetworkConnectAndDisconnectReachTheEngine(t *testing.T) {
	f := fake.New()
	svc := NewNetwork(f, nil)
	ctx := context.Background()

	if err := svc.Connect(ctx, "bridge", runningID,
		docker.ConnectOptions{Aliases: []string{"api"}}, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := svc.Disconnect(ctx, "bridge", runningID, true, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	if len(f.CallsFor(fake.OpConnectNetwork)) != 1 || len(f.CallsFor(fake.OpDisconnectNetwork)) != 1 {
		t.Error("the engine did not see both operations")
	}
}

func TestPruneReportsWhatWasReclaimed(t *testing.T) {
	f := fake.New()
	ctx := context.Background()

	images, err := NewImage(f, nil, nil).Prune(ctx, true, audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("image prune error = %v", err)
	}
	if images.Deleted == nil {
		t.Error("the image prune report has a nil list, which a client would iterate")
	}

	volumes, err := NewVolume(f, nil).Prune(ctx, audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("volume prune error = %v", err)
	}
	if volumes.Deleted == nil {
		t.Error("the volume prune report has a nil list")
	}

	networks, err := NewNetwork(f, nil).Prune(ctx, audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("network prune error = %v", err)
	}
	// Networks occupy no disk, so a non-zero figure would be a fabrication.
	if networks.SpaceReclaimed != 0 {
		t.Errorf("network space_reclaimed = %d, want 0", networks.SpaceReclaimed)
	}
}
