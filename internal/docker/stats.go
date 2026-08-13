package docker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/docker/docker/api/types/container"
)

// ContainerStats samples a container's resource usage until ctx ends.
func (e *engine) ContainerStats(ctx context.Context, id string) (<-chan Stats, <-chan error) {
	out := make(chan Stats, 8)
	errs := make(chan error, 1)

	resp, err := e.api.ContainerStats(ctx, id, true)
	if err != nil {
		errs <- classify("container.stats", "container", id, err)
		close(out)
		close(errs)
		return out, errs
	}

	go func() {
		defer close(out)
		defer close(errs)
		defer func() { _ = resp.Body.Close() }()

		dec := json.NewDecoder(resp.Body)
		for {
			if ctx.Err() != nil {
				return
			}

			var raw container.StatsResponse
			if err := dec.Decode(&raw); err != nil {
				if decodeErr := endOfStream(err); decodeErr != nil {
					select {
					case errs <- classify("container.stats", "container", id, decodeErr):
					default:
					}
				}
				return
			}

			select {
			case out <- toStats(raw):
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, errs
}

// toStats turns the engine's cumulative counters into the deltas and
// percentages the UI plots.
func toStats(raw container.StatsResponse) Stats {
	s := Stats{
		Timestamp:   raw.Read.UTC(),
		MemoryLimit: int64(raw.MemoryStats.Limit), //nolint:gosec // engine-reported byte count
		PIDs:        int64(raw.PidsStats.Current), //nolint:gosec // engine-reported count
	}
	if s.Timestamp.IsZero() {
		s.Timestamp = time.Now().UTC()
	}

	s.CPUPercent = cpuPercent(raw)
	s.MemoryUsage = memoryUsage(raw)
	if s.MemoryLimit > 0 {
		s.MemoryPercent = float64(s.MemoryUsage) / float64(s.MemoryLimit) * 100
	}

	for _, net := range raw.Networks {
		s.NetworkRx += int64(net.RxBytes) //nolint:gosec // engine-reported byte count
		s.NetworkTx += int64(net.TxBytes) //nolint:gosec // engine-reported byte count
	}

	for _, entry := range raw.BlkioStats.IoServiceBytesRecursive {
		switch entry.Op {
		case "read", "Read":
			s.BlockRead += int64(entry.Value) //nolint:gosec // engine-reported byte count
		case "write", "Write":
			s.BlockWrite += int64(entry.Value) //nolint:gosec // engine-reported byte count
		}
	}

	return s
}

// cpuPercent computes usage the way `docker stats` does: the container's CPU
// delta over the system's, scaled by the number of cores, so 200% means two
// cores saturated.
func cpuPercent(raw container.StatsResponse) float64 {
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)

	if systemDelta <= 0 || cpuDelta < 0 {
		// The first sample has no previous reading to compare against.
		return 0
	}

	cores := float64(raw.CPUStats.OnlineCPUs)
	if cores == 0 {
		cores = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
	}
	if cores == 0 {
		cores = 1
	}

	return cpuDelta / systemDelta * cores * 100
}

// memoryUsage subtracts the page cache, matching what `docker stats` reports
// and what an operator means by "how much memory is this using".
func memoryUsage(raw container.StatsResponse) int64 {
	usage := int64(raw.MemoryStats.Usage) //nolint:gosec // engine-reported byte count

	// cgroup v2 reports "inactive_file"; v1 reports "total_inactive_file".
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if cached, ok := raw.MemoryStats.Stats[key]; ok {
			if reduced := usage - int64(cached); reduced >= 0 { //nolint:gosec // engine-reported
				return reduced
			}
			return 0
		}
	}
	return usage
}

// RingBuffer keeps the last N stats samples so a client that connects late
// still gets a chart rather than an empty axis.
type RingBuffer struct {
	samples []Stats
	next    int
	full    bool
}

// NewRingBuffer creates a buffer holding size samples.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 60
	}
	return &RingBuffer{samples: make([]Stats, size)}
}

// Add stores one sample, overwriting the oldest when full.
func (b *RingBuffer) Add(s Stats) {
	b.samples[b.next] = s
	b.next = (b.next + 1) % len(b.samples)
	if b.next == 0 {
		b.full = true
	}
}

// Snapshot returns the buffered samples, oldest first.
func (b *RingBuffer) Snapshot() []Stats {
	if !b.full {
		out := make([]Stats, b.next)
		copy(out, b.samples[:b.next])
		return out
	}

	out := make([]Stats, 0, len(b.samples))
	out = append(out, b.samples[b.next:]...)
	out = append(out, b.samples[:b.next]...)
	return out
}

// Len reports how many samples are buffered.
func (b *RingBuffer) Len() int {
	if b.full {
		return len(b.samples)
	}
	return b.next
}
