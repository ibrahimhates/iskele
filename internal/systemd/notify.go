// Package systemd implements the small part of the sd_notify protocol a
// long-running service needs.
//
// It is written out rather than pulled in: the protocol is one datagram to the
// socket named in NOTIFY_SOCKET, and a dependency for that would be more code
// to audit than the thing it replaces. Everything here is a no-op when the
// variable is unset, so a daemon started by hand behaves exactly as before.
package systemd

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Notifier sends readiness and liveness messages to systemd.
//
// The zero value is usable and does nothing, which is what a process started
// outside systemd wants.
type Notifier struct {
	// socket is the abstract or filesystem path from NOTIFY_SOCKET. Empty
	// means there is nothing to notify.
	socket string

	mu   sync.Mutex
	conn *net.UnixConn
}

// New reads the environment and returns a Notifier.
//
// lookupEnv is injected so a test does not have to mutate the process
// environment; pass os.LookupEnv in production.
func New(lookupEnv func(string) (string, bool)) *Notifier {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	socket, ok := lookupEnv("NOTIFY_SOCKET")
	if !ok || socket == "" {
		return &Notifier{}
	}
	return &Notifier{socket: socket}
}

// Enabled reports whether this process was started by systemd with
// notification requested.
func (n *Notifier) Enabled() bool { return n != nil && n.socket != "" }

// Ready tells systemd the service is up and serving. Until this arrives, a
// Type=notify unit counts as still starting, and units ordered after it wait.
func (n *Notifier) Ready() error { return n.send("READY=1") }

// Stopping tells systemd the shutdown was deliberate, so a slow drain is not
// mistaken for a hang.
func (n *Notifier) Stopping() error { return n.send("STOPPING=1") }

// Status sets the one-line description `systemctl status` shows.
func (n *Notifier) Status(text string) error {
	// A newline would be read as the start of another assignment.
	return n.send("STATUS=" + strings.ReplaceAll(text, "\n", " "))
}

// Watchdog sends one keep-alive ping.
func (n *Notifier) Watchdog() error { return n.send("WATCHDOG=1") }

// WatchdogInterval is how often Watchdog should be called, or zero when the
// unit did not ask for one.
//
// systemd sets WATCHDOG_USEC to the deadline; pinging at half of it is the
// documented convention, and it leaves room for one missed tick.
func (n *Notifier) WatchdogInterval(lookupEnv func(string) (string, bool)) time.Duration {
	if !n.Enabled() {
		return 0
	}
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	// WATCHDOG_PID, when set, names the process the deadline belongs to. A
	// child that inherited the environment must not answer for its parent.
	if pid, ok := lookupEnv("WATCHDOG_PID"); ok && pid != strconv.Itoa(os.Getpid()) {
		return 0
	}

	raw, ok := lookupEnv("WATCHDOG_USEC")
	if !ok {
		return 0
	}
	usec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || usec <= 0 {
		return 0
	}
	return time.Duration(usec) * time.Microsecond / 2
}

// Close releases the socket.
func (n *Notifier) Close() error {
	if n == nil {
		return nil
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.conn == nil {
		return nil
	}
	err := n.conn.Close()
	n.conn = nil
	return err
}

// send writes one datagram, dialing on first use.
func (n *Notifier) send(message string) error {
	if !n.Enabled() {
		return nil
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.conn == nil {
		conn, err := dial(n.socket)
		if err != nil {
			return err
		}
		n.conn = conn
	}

	if _, err := n.conn.Write([]byte(message)); err != nil {
		// A failed write leaves the connection in an unknown state; drop it so
		// the next call redials rather than repeating the failure forever.
		_ = n.conn.Close()
		n.conn = nil
		return fmt.Errorf("notify systemd: %w", err)
	}
	return nil
}

// dial opens the notification socket.
//
// A leading '@' means the abstract namespace, which Go expresses as a leading
// NUL. systemd uses an abstract socket by default and a filesystem one when
// the unit asks for it.
func dial(socket string) (*net.UnixConn, error) {
	addr := socket
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return nil, fmt.Errorf("dial systemd notify socket: %w", err)
	}
	return conn, nil
}
