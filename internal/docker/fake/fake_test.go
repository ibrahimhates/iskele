package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// These tests pin the behaviors the handler and service suites rely on. If
// the fake drifts from the real engine's contract, those suites would pass
// while production breaks.

const runningID = "c1000000000000000000000000000000000000000000000000000000000000a"

func TestFakeRecordsCalls(t *testing.T) {
	c := New()
	ctx := context.Background()

	if _, err := c.ListContainers(ctx, docker.ListContainersOptions{All: true}); err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if err := c.StartContainer(ctx, runningID); err != nil {
		t.Fatalf("StartContainer() error = %v", err)
	}

	calls := c.Calls()
	if len(calls) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(calls))
	}
	if calls[0].Op != OpListContainers || calls[1].Op != OpStartContainer {
		t.Errorf("calls = %+v", calls)
	}
	if calls[1].ID != runningID {
		t.Errorf("start call id = %q", calls[1].ID)
	}
}

func TestFakeInjectsAndClearsErrors(t *testing.T) {
	c := New()
	boom := errors.New("boom")

	c.Fail(OpListVolumes, boom)
	if _, err := c.ListVolumes(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the injected failure", err)
	}

	c.Fail(OpListVolumes, nil)
	if _, err := c.ListVolumes(context.Background()); err != nil {
		t.Errorf("error = %v, want the injection cleared", err)
	}
}

func TestFakeResetClearsCallsAndErrors(t *testing.T) {
	c := New()
	c.Fail(OpPing, errors.New("boom"))
	_, _ = c.Ping(context.Background())

	c.Reset()

	if len(c.Calls()) != 0 {
		t.Errorf("calls = %d, want 0 after Reset", len(c.Calls()))
	}
	if _, err := c.Ping(context.Background()); err != nil {
		t.Errorf("error = %v, want injections cleared by Reset", err)
	}
}

func TestFakeMirrorsEngineListSemantics(t *testing.T) {
	c := New()
	ctx := context.Background()

	// Without All, the engine returns running containers only.
	running, err := c.ListContainers(ctx, docker.ListContainersOptions{})
	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if len(running) != 1 || running[0].State != "running" {
		t.Errorf("running list = %+v", running)
	}

	all, err := c.ListContainers(ctx, docker.ListContainersOptions{All: true})
	if err != nil {
		t.Fatalf("ListContainers(all) error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("all list has %d entries, want 2", len(all))
	}
}

func TestFakeLifecycleUpdatesState(t *testing.T) {
	c := New()
	ctx := context.Background()

	if err := c.StopContainer(ctx, runningID, docker.StopOptions{}); err != nil {
		t.Fatalf("StopContainer() error = %v", err)
	}
	detail, err := c.InspectContainer(ctx, runningID)
	if err != nil {
		t.Fatalf("InspectContainer() error = %v", err)
	}
	if detail.State != "exited" {
		t.Errorf("State = %q, want the stop to be reflected", detail.State)
	}

	if err := c.StartContainer(ctx, runningID); err != nil {
		t.Fatalf("StartContainer() error = %v", err)
	}
	detail, _ = c.InspectContainer(ctx, runningID)
	if detail.State != "running" {
		t.Errorf("State = %q, want the start to be reflected", detail.State)
	}
}

func TestFakeRefusesToRemoveRunningContainerWithoutForce(t *testing.T) {
	c := New()
	ctx := context.Background()

	err := c.RemoveContainer(ctx, runningID, docker.RemoveContainerOptions{})
	if !docker.IsConflict(err) {
		t.Fatalf("error = %v, want KindConflict like the real engine", err)
	}

	if err := c.RemoveContainer(ctx, runningID, docker.RemoveContainerOptions{Force: true}); err != nil {
		t.Fatalf("forced remove failed: %v", err)
	}
	if _, err := c.InspectContainer(ctx, runningID); !docker.IsNotFound(err) {
		t.Errorf("error = %v, want the container to be gone", err)
	}
}

func TestFakeReportsNotFoundLikeTheEngine(t *testing.T) {
	c := New()
	ctx := context.Background()

	operations := map[string]func() error{
		"inspect": func() error { _, err := c.InspectContainer(ctx, "ghost"); return err },
		"raw":     func() error { _, err := c.InspectContainerRaw(ctx, "ghost"); return err },
		"start":   func() error { return c.StartContainer(ctx, "ghost") },
		"stop":    func() error { return c.StopContainer(ctx, "ghost", docker.StopOptions{}) },
		"restart": func() error { return c.RestartContainer(ctx, "ghost", docker.StopOptions{}) },
		"remove":  func() error { return c.RemoveContainer(ctx, "ghost", docker.RemoveContainerOptions{}) },
	}

	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			err := op()
			if !docker.IsNotFound(err) {
				t.Errorf("error = %v, want KindNotFound", err)
			}
			if docker.Resource(err) != "container" {
				t.Errorf("Resource() = %q, want %q", docker.Resource(err), "container")
			}
		})
	}
}

func TestFakeCloseIsRecorded(t *testing.T) {
	c := New()

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !c.Closed {
		t.Error("Closed = false after Close()")
	}
}

func TestFakeIsSafeForConcurrentUse(t *testing.T) {
	c := New()
	ctx := context.Background()
	done := make(chan struct{})

	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 20; j++ {
				_, _ = c.ListContainers(ctx, docker.ListContainersOptions{All: true})
				_, _ = c.InspectContainer(ctx, runningID)
				_ = c.StartContainer(ctx, runningID)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
