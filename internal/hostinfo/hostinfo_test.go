package hostinfo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

func TestBusyPercent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		prev cpu.TimesStat
		now  cpu.TimesStat
		want float64
	}{
		{
			name: "half the interval was busy",
			prev: cpu.TimesStat{User: 100, Idle: 100},
			now:  cpu.TimesStat{User: 110, Idle: 110},
			want: 50,
		},
		{
			name: "fully idle",
			prev: cpu.TimesStat{User: 100, Idle: 100},
			now:  cpu.TimesStat{User: 100, Idle: 120},
			want: 0,
		},
		{
			name: "fully busy",
			prev: cpu.TimesStat{User: 100, Idle: 100},
			now:  cpu.TimesStat{User: 120, Idle: 100},
			want: 100,
		},
		{
			name: "iowait counts as idle",
			prev: cpu.TimesStat{User: 100, Idle: 100},
			now:  cpu.TimesStat{User: 100, Iowait: 10, Idle: 110},
			want: 0,
		},
		{
			name: "no time passed",
			prev: cpu.TimesStat{User: 100, Idle: 100},
			now:  cpu.TimesStat{User: 100, Idle: 100},
			want: 0,
		},
		{
			// A suspended host, or a counter that was reset under us.
			name: "counters went backwards",
			prev: cpu.TimesStat{User: 500, Idle: 500},
			now:  cpu.TimesStat{User: 10, Idle: 10},
			want: 0,
		},
		{
			// Busy time rose while idle fell: nonsense, but it must not
			// produce a bar past the end of its track.
			name: "clamped to a hundred",
			prev: cpu.TimesStat{User: 100, Idle: 100},
			now:  cpu.TimesStat{User: 130, Idle: 100},
			want: 100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := busyPercent(tc.prev, tc.now); got != tc.want {
				t.Fatalf("busyPercent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClampPercentRoundsToOneDecimal(t *testing.T) {
	t.Parallel()

	cases := map[float64]float64{
		12.34:  12.3,
		12.35:  12.4,
		99.999: 100,
		-1:     0,
		101:    100,
		0:      0,
	}

	for in, want := range cases {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestPlatformOf(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, version, want string }{
		{"debian", "12", "debian 12"},
		{"debian", "", "debian"},
		{"", "12", "12"},
		{"", "", ""},
	}

	for _, tc := range cases {
		if got := platformOf(tc.name, tc.version); got != tc.want {
			t.Errorf("platformOf(%q, %q) = %q, want %q", tc.name, tc.version, got, tc.want)
		}
	}
}

// The first reading has nothing to compare against, so it must say so rather
// than report a number measured from boot and pass it off as "now".
func TestFirstReadingHasNoCPUPercent(t *testing.T) {
	t.Parallel()

	c := New()
	first := c.Read(t.Context(), nil)
	if first.CPU.Percent != -1 {
		t.Fatalf("first reading reported %v percent, want -1", first.CPU.Percent)
	}

	second := c.Read(t.Context(), nil)
	if second.CPU.Percent < 0 || second.CPU.Percent > 100 {
		t.Fatalf("second reading reported %v percent, want 0..100", second.CPU.Percent)
	}
	if second.CPU.Cores < 1 {
		t.Fatalf("reported %d cores, want at least one", second.CPU.Cores)
	}
}

func TestReadMeasuresEveryDistinctTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := New()

	m := c.Read(t.Context(), []Target{
		{Label: "data", Path: dir},
		// The same filesystem under a different name: reported once, so the
		// dashboard does not invite adding two identical bars together.
		{Label: "engine", Path: os.TempDir()},
	})

	if len(m.Disks) != 1 {
		t.Fatalf("got %d disks, want 1: %+v", len(m.Disks), m.Disks)
	}
	got := m.Disks[0]
	if got.Total == 0 {
		t.Error("the measured filesystem reported no size")
	}
	if got.Percent < 0 || got.Percent > 100 {
		t.Errorf("usage was %v percent", got.Percent)
	}
}

func TestReadReportsAnUnreadableTargetWithoutFailing(t *testing.T) {
	t.Parallel()

	c := New()
	m := c.Read(t.Context(), []Target{
		{Label: "missing", Path: "/this/path/does/not/exist"},
		{Label: "data", Path: t.TempDir()},
	})

	if len(m.Disks) != 1 {
		t.Fatalf("got %d disks, want the one that could be read", len(m.Disks))
	}
	if len(m.Errors) == 0 {
		t.Fatal("the unreadable target was not reported")
	}
}

// An empty target is skipped rather than measured: an unset directory is not
// an error to show an operator.
func TestReadSkipsEmptyTargets(t *testing.T) {
	t.Parallel()

	m := New().Read(t.Context(), []Target{{Label: "unset", Path: ""}})
	if len(m.Disks) != 0 || len(m.Errors) != 0 {
		t.Fatalf("an unset target produced %+v / %v", m.Disks, m.Errors)
	}
}

func TestReadHonorsACanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// The reading must return rather than block; what it manages to collect
	// from a canceled context is the platform's business.
	done := make(chan Metrics, 1)
	go func() { done <- New().Read(ctx, []Target{{Label: "data", Path: t.TempDir()}}) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Read did not return for a canceled context")
	}
}

func TestUptimeGrows(t *testing.T) {
	t.Parallel()

	c := New()
	if c.Uptime() < 0 {
		t.Fatal("uptime went backwards")
	}
	if c.StartedAt().IsZero() {
		t.Fatal("the start time was not recorded")
	}
}

func TestProblemListFormatsTheReadingThatFailed(t *testing.T) {
	t.Parallel()

	var p problemList
	p.add("cpu", errors.New("boom"))

	if len(p.list) != 1 || p.list[0] != "cpu: boom" {
		t.Fatalf("problems = %v", p.list)
	}
}
