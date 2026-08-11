// Package hostinfo reads the machine the daemon runs on.
//
// It is the only package that imports gopsutil, for the same reason
// internal/docker is the only one that imports the engine SDK: host metrics
// are read through platform-specific files and syscalls, and confining that to
// one place keeps the rest of the daemon portable and testable.
//
// Every reading here is best effort. A daemon running inside a container may
// have no /proc/diskstats, no swap and no load average, and a dashboard that
// refuses to render because one of six numbers is unavailable is worse than
// one that shows five. Failures are collected in Metrics.Errors rather than
// returned.
package hostinfo

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// errNoCPUTimes stands in when the platform returns a reading with no CPUs in
// it, which gopsutil reports as success.
var errNoCPUTimes = errors.New("the platform reported no CPU times")

// usageOf measures the filesystem holding path.
func usageOf(ctx context.Context, path string) (*disk.UsageStat, error) {
	return disk.UsageWithContext(ctx, path)
}

// Metrics is one reading of the host.
type Metrics struct {
	Hostname string `json:"hostname,omitempty"`
	// Platform is the distribution and its version, e.g. "debian 12".
	Platform string `json:"platform,omitempty"`
	Kernel   string `json:"kernel,omitempty"`
	// Uptime is how long the host has been up, in seconds.
	Uptime int64 `json:"uptime"`
	// BootTime is when the host came up, so a client can render an absolute
	// time without trusting its own clock offset.
	BootTime time.Time `json:"boot_time,omitzero"`

	CPU    CPU      `json:"cpu"`
	Memory Memory   `json:"memory"`
	Swap   Memory   `json:"swap"`
	Disks  []Disk   `json:"disks"`
	Load   *Load    `json:"load,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// CPU is processor usage across all cores.
type CPU struct {
	// Cores is the number of logical CPUs.
	Cores int `json:"cores"`
	// Percent is busy time since the previous reading, 0..100. It is -1 on the
	// very first reading of a process, where there is no previous sample to
	// compare against.
	Percent float64 `json:"percent"`
	// Model names the processor, when the platform reports one.
	Model string `json:"model,omitempty"`
}

// Memory is a total/used pair. It describes RAM and swap alike; swap on a host
// without any is simply all zeros.
type Memory struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Percent   float64 `json:"percent"`
}

// Disk is the usage of one filesystem the daemon cares about.
type Disk struct {
	// Path is the directory that was measured, not the mount point: it is what
	// the operator configured, and what they will recognize.
	Path       string  `json:"path"`
	Label      string  `json:"label"`
	Filesystem string  `json:"filesystem,omitempty"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
}

// Load is the 1/5/15-minute run queue average. It is nil on platforms that do
// not have one.
type Load struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

// Target is a directory to measure, with the name to show it under.
type Target struct {
	Label string
	Path  string
}

// Collector reads the host, remembering enough between readings to report CPU
// busy time as a percentage.
//
// gopsutil can do that itself, but only through a package-level variable
// shared by every caller in the process, or by blocking for a sampling
// interval. Neither suits an HTTP handler, so the previous sample is kept
// here: two dashboards polling at different rates then cannot corrupt each
// other's numbers.
type Collector struct {
	mu   sync.Mutex
	prev cpu.TimesStat
	// seen is false until the first reading, where there is nothing to
	// subtract from.
	seen bool

	// startedAt is when the daemon came up, so uptime is reported next to the
	// host's own.
	startedAt time.Time
}

// New returns a Collector that reports daemon uptime relative to now.
func New() *Collector {
	return &Collector{startedAt: time.Now()}
}

// Uptime is how long the daemon has been running.
func (c *Collector) Uptime() time.Duration { return time.Since(c.startedAt) }

// StartedAt is when the daemon came up.
func (c *Collector) StartedAt() time.Time { return c.startedAt }

// Read takes one reading, measuring each target directory's filesystem.
//
// Duplicate targets are measured once: the data directory and the engine's
// root directory are usually on the same filesystem, and reporting the same
// bar twice would only invite the reader to add them together.
func (c *Collector) Read(ctx context.Context, targets []Target) Metrics {
	m := Metrics{}
	var problems problemList

	if info, err := host.InfoWithContext(ctx); err != nil {
		problems.add("host", err)
	} else {
		m.Hostname = info.Hostname
		m.Platform = platformOf(info.Platform, info.PlatformVersion)
		m.Kernel = info.KernelVersion
		m.Uptime = int64(info.Uptime)                         //nolint:gosec // seconds since boot, not attacker-controlled
		m.BootTime = time.Unix(int64(info.BootTime), 0).UTC() //nolint:gosec // as above
	}

	m.CPU = c.readCPU(ctx, &problems)
	m.Memory, m.Swap = readMemory(ctx, &problems)
	m.Load = readLoad(ctx, &problems)
	m.Disks = readDisks(ctx, targets, &problems)
	m.Errors = problems.list

	return m
}

// readCPU reports busy time since the previous call.
func (c *Collector) readCPU(ctx context.Context, problems *problemList) CPU {
	out := CPU{Cores: runtime.NumCPU(), Percent: -1}

	if counts, err := cpu.CountsWithContext(ctx, true); err == nil && counts > 0 {
		out.Cores = counts
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		out.Model = infos[0].ModelName
	}

	times, err := cpu.TimesWithContext(ctx, false)
	if err != nil || len(times) == 0 {
		if err == nil {
			err = errNoCPUTimes
		}
		problems.add("cpu", err)
		return out
	}
	now := times[0]

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.seen {
		out.Percent = busyPercent(c.prev, now)
	}
	c.prev = now
	c.seen = true

	return out
}

// busyPercent is the share of the interval between two readings that was not
// idle. A counter that went backwards — a host that was suspended, or a
// container whose cgroup was reset — yields 0 rather than a negative or wildly
// large number.
func busyPercent(prev, now cpu.TimesStat) float64 {
	totalDelta := total(now) - total(prev)
	idleDelta := idle(now) - idle(prev)
	if totalDelta <= 0 || idleDelta < 0 {
		return 0
	}

	busy := (totalDelta - idleDelta) / totalDelta * 100
	return clampPercent(busy)
}

// total is every second the CPU accounted for, busy or not.
func total(t cpu.TimesStat) float64 {
	return t.User + t.System + t.Nice + t.Iowait + t.Irq +
		t.Softirq + t.Steal + t.Idle
}

// idle counts iowait as idle: the CPU had nothing to run, which is what a
// utilization figure is asked to convey.
func idle(t cpu.TimesStat) float64 { return t.Idle + t.Iowait }

func readMemory(ctx context.Context, problems *problemList) (ram, swap Memory) {
	if vm, err := mem.VirtualMemoryWithContext(ctx); err != nil {
		problems.add("memory", err)
	} else {
		ram = Memory{
			Total:     vm.Total,
			Used:      vm.Used,
			Available: vm.Available,
			Percent:   clampPercent(vm.UsedPercent),
		}
	}

	// Swap is optional in a way RAM is not: a host with none is normal, and
	// gopsutil reports that as zeros rather than an error.
	if sw, err := mem.SwapMemoryWithContext(ctx); err != nil {
		problems.add("swap", err)
	} else {
		swap = Memory{
			Total:     sw.Total,
			Used:      sw.Used,
			Available: sw.Free,
			Percent:   clampPercent(sw.UsedPercent),
		}
	}

	return ram, swap
}

func readLoad(ctx context.Context, problems *problemList) *Load {
	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		// Windows has no load average; saying so on every reading would be
		// noise, so only a real failure is reported.
		if runtime.GOOS == "windows" {
			return nil
		}
		problems.add("load", err)
		return nil
	}

	return &Load{One: avg.Load1, Five: avg.Load5, Fifteen: avg.Load15}
}

// readDisks measures each target, skipping the ones that resolve to a
// filesystem already measured.
func readDisks(ctx context.Context, targets []Target, problems *problemList) []Disk {
	out := make([]Disk, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))

	for _, target := range targets {
		if target.Path == "" {
			continue
		}
		path := filepath.Clean(target.Path)

		usage, err := usageOf(ctx, path)
		if err != nil {
			problems.add("disk "+target.Label, err)
			continue
		}
		// The key is the filesystem, not the path: /var/lib/iskele and
		// /var/lib/docker are usually the same disk.
		key := usage.Fstype + "\x00" + fmt.Sprint(usage.Total) + "\x00" + fmt.Sprint(usage.Free)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, Disk{
			Path:       path,
			Label:      target.Label,
			Filesystem: usage.Fstype,
			Total:      usage.Total,
			Used:       usage.Used,
			Free:       usage.Free,
			Percent:    clampPercent(usage.UsedPercent),
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// platformOf joins the distribution and its version, tolerating either being
// empty.
func platformOf(name, version string) string {
	switch {
	case name == "":
		return version
	case version == "":
		return name
	default:
		return name + " " + version
	}
}

// clampPercent keeps a reported share inside 0..100 so a bar chart cannot be
// asked to draw 104%.
func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		// One decimal is as much precision as any of these readings carry.
		return float64(int64(v*10+0.5)) / 10
	}
}

// problemList collects the readings that failed.
type problemList struct{ list []string }

func (p *problemList) add(what string, err error) {
	p.list = append(p.list, fmt.Sprintf("%s: %v", what, err))
}
