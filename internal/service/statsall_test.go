package service

import (
	"context"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
)

func TestStatsAllTagsEverySampleWithItsContainer(t *testing.T) {
	svc, _ := newContainerService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	samples, errs := svc.StatsAll(ctx)

	select {
	case sample := <-samples:
		if sample.ID != runningID {
			t.Errorf("sample.ID = %q, want the running container", sample.ID)
		}
		if sample.CPUPercent == 0 && sample.MemoryUsage == 0 {
			t.Errorf("sample carries no measurements: %+v", sample)
		}
	case err := <-errs:
		t.Fatalf("StatsAll() error = %v", err)
	case <-ctx.Done():
		t.Fatal("no sample arrived")
	}
}

// Only running containers have anything to measure, and the fake's second
// container is stopped.
func TestStatsAllWatchesOnlyRunningContainers(t *testing.T) {
	svc, f := newContainerService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	samples, _ := svc.StatsAll(ctx)
	<-samples

	watched := map[string]bool{}
	for _, call := range f.CallsFor(fake.OpContainerStats) {
		watched[call.ID] = true
	}
	if len(watched) != 1 || !watched[runningID] {
		t.Errorf("stats opened for %v, want only the running container", watched)
	}
}

// A daemon that is down on the first scan is worth reporting: the alternative
// is a stream that stays silent and looks like a UI bug.
func TestStatsAllReportsAnUnreachableEngineImmediately(t *testing.T) {
	svc, f := newContainerService(t)
	f.Fail(fake.OpListContainers, docker.NewError(
		docker.KindUnavailable, "container.list", "container", "", "daemon is gone"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	samples, errs := svc.StatsAll(ctx)

	select {
	case err := <-errs:
		if !docker.IsUnavailable(err) {
			t.Errorf("error = %v, want the engine failure", err)
		}
	case <-samples:
		t.Fatal("a sample arrived from an unreachable engine")
	case <-ctx.Done():
		t.Fatal("the failure was never reported")
	}

	// And the stream ends rather than spinning on a daemon that is not there.
	select {
	case _, ok := <-samples:
		if ok {
			t.Error("the sample channel stayed open after a fatal error")
		}
	case <-time.After(2 * time.Second):
		t.Error("the sample channel was never closed")
	}
}

func TestStatsAllStopsEveryEngineStreamWhenTheClientLeaves(t *testing.T) {
	svc, _ := newContainerService(t)

	ctx, cancel := context.WithCancel(context.Background())
	samples, errs := svc.StatsAll(ctx)
	<-samples

	cancel()

	deadline := time.After(5 * time.Second)
	for samples != nil || errs != nil {
		select {
		case _, ok := <-samples:
			if !ok {
				samples = nil
			}
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		case <-deadline:
			t.Fatal("the multiplexer did not shut down after the context ended")
		}
	}
}
