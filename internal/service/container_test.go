package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
)

func newContainerService(t *testing.T) (*Container, *fake.Client) {
	t.Helper()
	f := fake.New()
	return NewContainer(f, nil), f
}

const runningID = "c1000000000000000000000000000000000000000000000000000000000000a"

func TestListPassesOptionsToTheEngine(t *testing.T) {
	svc, f := newContainerService(t)

	_, err := svc.List(context.Background(), ListOptions{
		All:    true,
		Size:   true,
		Label:  []string{"app=web"},
		Status: []string{"running"},
		Name:   "web",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	calls := f.CallsFor(fake.OpListContainers)
	if len(calls) != 1 {
		t.Fatalf("engine called %d times, want 1", len(calls))
	}
	opts, ok := calls[0].Opts.(docker.ListContainersOptions)
	if !ok {
		t.Fatalf("engine received %T, want docker.ListContainersOptions", calls[0].Opts)
	}
	if !opts.All || !opts.Size || opts.Name != "web" {
		t.Errorf("options not forwarded: %+v", opts)
	}
	if len(opts.Label) != 1 || opts.Label[0] != "app=web" {
		t.Errorf("Label = %v", opts.Label)
	}
	if len(opts.Status) != 1 || opts.Status[0] != "running" {
		t.Errorf("Status = %v", opts.Status)
	}
}

func TestListDefaultsToRunningContainersOnly(t *testing.T) {
	svc, _ := newContainerService(t)

	running, err := svc.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(running) != 1 {
		t.Fatalf("got %d containers, want only the running one", len(running))
	}

	all, err := svc.List(context.Background(), ListOptions{All: true})
	if err != nil {
		t.Fatalf("List(all) error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("got %d containers with all=true, want 2", len(all))
	}
}

func TestGetReturnsDetail(t *testing.T) {
	svc, _ := newContainerService(t)

	detail, err := svc.Get(context.Background(), runningID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if detail.Name != "web" {
		t.Errorf("Name = %q", detail.Name)
	}
}

func TestGetUnknownContainerIsNotFound(t *testing.T) {
	svc, _ := newContainerService(t)

	_, err := svc.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("Get() error = nil, want a not-found error")
	}
	if !docker.IsNotFound(err) {
		t.Errorf("error = %v, want KindNotFound", err)
	}
}

func TestBlankIDsAreRejectedBeforeTheEngine(t *testing.T) {
	svc, f := newContainerService(t)
	ctx := context.Background()

	operations := map[string]func() error{
		"Get":     func() error { _, err := svc.Get(ctx, "  "); return err },
		"Inspect": func() error { _, err := svc.Inspect(ctx, ""); return err },
		"Start":   func() error { return svc.Start(ctx, "", audit.Actor{}, RequestMeta{}) },
		"Stop":    func() error { return svc.Stop(ctx, "", nil, audit.Actor{}, RequestMeta{}) },
		"Restart": func() error { return svc.Restart(ctx, "\t", nil, audit.Actor{}, RequestMeta{}) },
		"Remove":  func() error { return svc.Remove(ctx, "", RemoveOptions{}, audit.Actor{}, RequestMeta{}) },
	}

	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			err := op()
			if !errors.Is(err, ErrEmptyID) {
				t.Errorf("error = %v, want ErrEmptyID", err)
			}
		})
	}

	if len(f.Calls()) != 0 {
		t.Errorf("engine was called %d times, want the blank id rejected first", len(f.Calls()))
	}
}

func TestIDsAreTrimmedBeforeReachingTheEngine(t *testing.T) {
	svc, f := newContainerService(t)

	if err := svc.Start(context.Background(), "  "+runningID+"  ", audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	calls := f.CallsFor(fake.OpStartContainer)
	if len(calls) != 1 || calls[0].ID != runningID {
		t.Errorf("engine received id %q, want it trimmed", calls[0].ID)
	}
}

func TestLifecycleOperationsReachTheEngine(t *testing.T) {
	svc, f := newContainerService(t)
	ctx := context.Background()
	timeout := 5

	if err := svc.Stop(ctx, runningID, &timeout, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := svc.Start(ctx, runningID, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := svc.Restart(ctx, runningID, nil, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}

	stopCalls := f.CallsFor(fake.OpStopContainer)
	if len(stopCalls) != 1 {
		t.Fatalf("stop called %d times", len(stopCalls))
	}
	opts, ok := stopCalls[0].Opts.(docker.StopOptions)
	if !ok || opts.Timeout == nil || *opts.Timeout != 5 {
		t.Errorf("stop timeout not forwarded: %+v", stopCalls[0].Opts)
	}

	restartCalls := f.CallsFor(fake.OpRestartContainer)
	if len(restartCalls) != 1 {
		t.Fatalf("restart called %d times", len(restartCalls))
	}
	restartOpts, ok := restartCalls[0].Opts.(docker.StopOptions)
	if !ok || restartOpts.Timeout != nil {
		t.Errorf("restart timeout = %+v, want nil to keep the engine default", restartCalls[0].Opts)
	}
}

func TestRemoveRunningContainerNeedsForce(t *testing.T) {
	svc, _ := newContainerService(t)
	ctx := context.Background()

	err := svc.Remove(ctx, runningID, RemoveOptions{}, audit.Actor{}, RequestMeta{})
	if err == nil {
		t.Fatal("Remove() error = nil, want a conflict for a running container")
	}
	if !docker.IsConflict(err) {
		t.Errorf("error = %v, want KindConflict", err)
	}

	if err := svc.Remove(ctx, runningID, RemoveOptions{Force: true}, audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Remove(force) error = %v", err)
	}

	if _, err := svc.Get(ctx, runningID); !docker.IsNotFound(err) {
		t.Errorf("container still present after a forced remove: %v", err)
	}
}

func TestRemoveForwardsVolumeFlag(t *testing.T) {
	svc, f := newContainerService(t)

	err := svc.Remove(context.Background(), runningID, RemoveOptions{Force: true, RemoveVolumes: true}, audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	calls := f.CallsFor(fake.OpRemoveContainer)
	opts, ok := calls[0].Opts.(docker.RemoveContainerOptions)
	if !ok || !opts.RemoveVolumes || !opts.Force {
		t.Errorf("remove options not forwarded: %+v", calls[0].Opts)
	}
}

func TestEngineFailuresPropagate(t *testing.T) {
	svc, f := newContainerService(t)
	boom := docker.NewError(docker.KindUnavailable, "container.list", "container", "", "daemon is gone")
	f.Fail(fake.OpListContainers, boom)

	_, err := svc.List(context.Background(), ListOptions{})
	if !docker.IsUnavailable(err) {
		t.Errorf("error = %v, want the engine failure to propagate unchanged", err)
	}
}

func TestInspectReturnsRawPayload(t *testing.T) {
	svc, _ := newContainerService(t)

	raw, err := svc.Inspect(context.Background(), runningID)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("raw inspect payload is empty")
	}
}
