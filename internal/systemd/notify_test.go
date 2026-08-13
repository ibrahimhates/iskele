package systemd

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// listen opens a datagram socket and returns its path plus a function that
// reads the next message.
func listen(t *testing.T) (path string, next func() string) {
	t.Helper()

	// A short path: a unix socket address is capped near 108 bytes, and
	// t.TempDir() under a long test name can exceed it.
	dir, err := os.MkdirTemp("", "nt")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path = filepath.Join(dir, "s")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return path, func() string {
		t.Helper()
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
		buf := make([]byte, 256)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(buf[:n])
	}
}

func env(pairs map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := pairs[key]
		return v, ok
	}
}

// A daemon started by hand must behave exactly as it did before any of this
// existed: no socket, no error, no output.
func TestNotifierIsInertWithoutSystemd(t *testing.T) {
	t.Parallel()

	n := New(env(map[string]string{}))
	if n.Enabled() {
		t.Fatal("Enabled() is true with no NOTIFY_SOCKET")
	}
	for name, err := range map[string]error{
		"Ready":    n.Ready(),
		"Stopping": n.Stopping(),
		"Watchdog": n.Watchdog(),
		"Status":   n.Status("serving"),
		"Close":    n.Close(),
	} {
		if err != nil {
			t.Errorf("%s() error = %v, want nil", name, err)
		}
	}
	if got := n.WatchdogInterval(env(map[string]string{"WATCHDOG_USEC": "1000000"})); got != 0 {
		t.Errorf("WatchdogInterval() = %v with no socket, want 0", got)
	}
}

func TestNotifierSendsTheProtocolMessages(t *testing.T) {
	t.Parallel()

	path, next := listen(t)
	n := New(env(map[string]string{"NOTIFY_SOCKET": path}))
	t.Cleanup(func() { _ = n.Close() })

	if !n.Enabled() {
		t.Fatal("Enabled() is false with NOTIFY_SOCKET set")
	}

	cases := []struct {
		name string
		send func() error
		want string
	}{
		{"ready", n.Ready, "READY=1"},
		{"watchdog", n.Watchdog, "WATCHDOG=1"},
		{"stopping", n.Stopping, "STOPPING=1"},
		{"status", func() error { return n.Status("serving on 127.0.0.1:8377") },
			"STATUS=serving on 127.0.0.1:8377"},
	}
	for _, tc := range cases {
		if err := tc.send(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := next(); got != tc.want {
			t.Errorf("%s sent %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A newline in the status would be read as the start of another assignment,
// which is how a status line turns into a protocol injection.
func TestStatusCannotSmuggleASecondAssignment(t *testing.T) {
	t.Parallel()

	path, next := listen(t)
	n := New(env(map[string]string{"NOTIFY_SOCKET": path}))
	t.Cleanup(func() { _ = n.Close() })

	if err := n.Status("degraded\nREADY=1"); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if got := next(); got != "STATUS=degraded READY=1" {
		t.Errorf("sent %q, want the newline flattened", got)
	}
}

func TestWatchdogIntervalIsHalfTheDeadline(t *testing.T) {
	t.Parallel()

	path, _ := listen(t)
	n := New(env(map[string]string{"NOTIFY_SOCKET": path}))
	t.Cleanup(func() { _ = n.Close() })

	me := strconv.Itoa(os.Getpid())

	cases := map[string]struct {
		env  map[string]string
		want time.Duration
	}{
		"no watchdog":     {map[string]string{}, 0},
		"ten seconds":     {map[string]string{"WATCHDOG_USEC": "10000000"}, 5 * time.Second},
		"with our pid":    {map[string]string{"WATCHDOG_USEC": "10000000", "WATCHDOG_PID": me}, 5 * time.Second},
		"another process": {map[string]string{"WATCHDOG_USEC": "10000000", "WATCHDOG_PID": "1"}, 0},
		"not a number":    {map[string]string{"WATCHDOG_USEC": "soon"}, 0},
		"zero":            {map[string]string{"WATCHDOG_USEC": "0"}, 0},
		"negative":        {map[string]string{"WATCHDOG_USEC": "-1"}, 0},
	}

	for name, tc := range cases {
		if got := n.WatchdogInterval(env(tc.env)); got != tc.want {
			t.Errorf("%s: WatchdogInterval() = %v, want %v", name, got, tc.want)
		}
	}
}

// systemd uses an abstract socket by default; Go spells that with a leading
// NUL rather than the '@' systemd puts in the variable.
func TestAbstractSocketNameIsTranslated(t *testing.T) {
	t.Parallel()

	name := "@iskele-test-" + strconv.Itoa(os.Getpid())
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: "\x00" + name[1:], Net: "unixgram"})
	if err != nil {
		t.Skipf("abstract sockets are unavailable here: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	n := New(env(map[string]string{"NOTIFY_SOCKET": name}))
	t.Cleanup(func() { _ = n.Close() })

	if readyErr := n.Ready(); readyErr != nil {
		t.Fatalf("Ready() error = %v", readyErr)
	}

	if deadlineErr := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
		t.Fatalf("set deadline: %v", deadlineErr)
	}
	buf := make([]byte, 64)
	read, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:read]) != "READY=1" {
		t.Errorf("received %q", buf[:read])
	}
}

// A socket that is not there must be reported, not swallowed: the operator
// would otherwise see a unit that never becomes active with nothing in the log.
func TestSendReportsAnUnreachableSocket(t *testing.T) {
	t.Parallel()

	n := New(env(map[string]string{"NOTIFY_SOCKET": "/nonexistent/iskele.sock"}))
	if err := n.Ready(); err == nil {
		t.Fatal("Ready() succeeded against a missing socket")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	path, _ := listen(t)
	n := New(env(map[string]string{"NOTIFY_SOCKET": path}))

	if err := n.Ready(); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	for range 3 {
		if err := n.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}
