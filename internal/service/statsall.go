package service

import (
	"context"
	"sync"
	"time"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// statsRescanInterval is how often the multiplexer re-lists running containers
// to pick up ones that started and drop ones that stopped.
//
// Polling rather than following the engine's event stream: a rescan is one
// cheap call, and a container that appears a few seconds late in a list view
// costs nothing, while a dropped event would leave a row permanently blank.
const statsRescanInterval = 10 * time.Second

// statsFanInBuffer lets a slow browser fall behind without stalling any of the
// per-container readers behind it.
const statsFanInBuffer = 256

// IdentifiedStats is one sample, tagged with the container it came from.
type IdentifiedStats struct {
	ID string `json:"id"`
	docker.Stats
}

// StatsAll streams resource usage for every running container over a single
// channel.
//
// The list view needs CPU and memory per row, and one connection per row would
// exhaust the browser's per-origin connection limit at six containers. This
// fans every engine stream into one, so the client opens exactly one.
func (s *Container) StatsAll(ctx context.Context) (<-chan IdentifiedStats, <-chan error) {
	out := make(chan IdentifiedStats, statsFanInBuffer)
	errs := make(chan error, 1)

	go func() {
		m := &statsMux{svc: s, out: out, watching: map[string]context.CancelFunc{}}

		// Order matters: the per-container readers own the send side of `out`,
		// so every one of them must have returned before it is closed.
		// Canceling their contexts only asks them to stop.
		defer func() {
			m.stopAll()
			m.followers.Wait()
			close(out)
			close(errs)
		}()

		// A failure on the very first scan is worth reporting: it usually means
		// the daemon is unreachable, and the client should be told rather than
		// left watching an empty stream. Later failures are transient by
		// comparison — the next tick retries.
		if err := m.rescan(ctx); err != nil {
			select {
			case errs <- err:
			default:
			}
			return
		}

		ticker := time.NewTicker(statsRescanInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.rescan(ctx)
			}
		}
	}()

	return out, errs
}

// statsMux keeps one engine stream per running container alive.
type statsMux struct {
	svc *Container
	out chan<- IdentifiedStats

	mu       sync.Mutex
	watching map[string]context.CancelFunc

	// followers counts the running per-container readers, so the owner can
	// wait for them before closing the channel they send on.
	followers sync.WaitGroup
}

// rescan starts streams for containers that appeared and stops the ones whose
// containers are gone.
func (m *statsMux) rescan(ctx context.Context) error {
	running, err := m.svc.List(ctx, ListOptions{})
	if err != nil {
		return err
	}

	live := make(map[string]struct{}, len(running))
	for _, c := range running {
		live[c.ID] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, cancel := range m.watching {
		if _, ok := live[id]; !ok {
			cancel()
			delete(m.watching, id)
		}
	}

	for id := range live {
		if _, ok := m.watching[id]; ok {
			continue
		}
		streamCtx, cancel := context.WithCancel(ctx)
		m.watching[id] = cancel
		m.followers.Add(1)
		go func() {
			defer m.followers.Done()
			m.follow(streamCtx, id)
		}()
	}

	return nil
}

// follow forwards one container's samples until its stream ends.
func (m *statsMux) follow(ctx context.Context, id string) {
	samples, errs := m.svc.docker.ContainerStats(ctx, id)

	for {
		select {
		case <-ctx.Done():
			return

		case sample, ok := <-samples:
			if !ok {
				m.forget(id)
				return
			}
			select {
			case m.out <- IdentifiedStats{ID: id, Stats: sample}:
			case <-ctx.Done():
				return
			}

		case _, ok := <-errs:
			// One container's failure is not the stream's failure: a container
			// that stops mid-sample must not blank out every other row. The
			// next rescan decides whether it is really gone.
			if !ok {
				m.forget(id)
				return
			}
		}
	}
}

// forget drops a finished stream so a later rescan can start it again.
func (m *statsMux) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.watching[id]; ok {
		cancel()
		delete(m.watching, id)
	}
}

// stopAll ends every stream this multiplexer owns.
func (m *statsMux) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.watching {
		cancel()
		delete(m.watching, id)
	}
}
