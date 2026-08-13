package docker

import (
	"context"
	"io"
	"time"
)

// LogOptions selects which log lines to stream.
type LogOptions struct {
	// Follow keeps the stream open and delivers new lines as they arrive.
	Follow bool
	// Tail is how many trailing lines to send first; 0 means "all".
	Tail int
	// Timestamps prefixes each line with the engine's timestamp.
	Timestamps bool
	// Stdout and Stderr select the streams. Both false means both on: a
	// request that selects nothing wants everything.
	Stdout bool
	Stderr bool
	// Since, when set, skips lines older than this.
	Since time.Time
}

// LogLine is one line of container output.
type LogLine struct {
	// Stream is "stdout" or "stderr".
	Stream string `json:"stream"`
	// Timestamp is the engine's timestamp, zero when not requested.
	Timestamp time.Time `json:"timestamp,omitzero"`
	// Message is the line without its trailing newline.
	Message string `json:"message"`
}

// Stats is one sample of a container's resource usage.
//
// The engine reports cumulative counters; these are the deltas and
// percentages the UI actually plots, computed here so every caller agrees.
type Stats struct {
	Timestamp time.Time `json:"timestamp"`
	// CPUPercent is usage across all cores, so 200 means two cores saturated.
	CPUPercent float64 `json:"cpu_percent"`
	// MemoryUsage excludes page cache, matching what `docker stats` shows.
	MemoryUsage   int64   `json:"memory_usage"`
	MemoryLimit   int64   `json:"memory_limit"`
	MemoryPercent float64 `json:"memory_percent"`
	NetworkRx     int64   `json:"network_rx"`
	NetworkTx     int64   `json:"network_tx"`
	BlockRead     int64   `json:"block_read"`
	BlockWrite    int64   `json:"block_write"`
	PIDs          int64   `json:"pids"`
}

// ExecOptions describes a command to run inside a container.
type ExecOptions struct {
	Cmd  []string
	TTY  bool
	User string
	// WorkingDir overrides the image's working directory.
	WorkingDir string
	Env        []string
	// Rows and Cols size the pseudo-terminal at creation, so the first frame
	// the shell paints is already the right shape.
	Rows uint
	Cols uint
}

// ExecSession is a live exec attachment.
type ExecSession struct {
	// ID identifies the exec instance, needed to resize it.
	ID string
	// Conn carries stdin to the process.
	Conn io.WriteCloser
	// Reader carries the process output. With a TTY it is a raw stream; without
	// one it is Docker's multiplexed format, which Demux untangles.
	Reader io.Reader
	// TTY reports whether a pseudo-terminal was allocated.
	TTY bool
	// Close releases the hijacked connection.
	Close func()
}

// Event is one Docker engine event.
type Event struct {
	Type   string            `json:"type"`
	Action string            `json:"action"`
	Actor  string            `json:"actor"`
	Name   string            `json:"name,omitempty"`
	Scope  string            `json:"scope,omitempty"`
	Attrs  map[string]string `json:"attributes,omitempty"`
	Time   time.Time         `json:"time"`
}

// Streamer is the streaming half of the engine API.
//
// It is separate from Client so the pieces can be reviewed on their own, but
// the concrete engine implements both and Client embeds it.
type Streamer interface {
	// ContainerLogs streams a container's output until ctx ends. Lines are
	// delivered on the returned channel, which is closed when the stream ends;
	// a non-nil error on the error channel explains an abnormal end.
	ContainerLogs(ctx context.Context, id string, opts LogOptions) (<-chan LogLine, <-chan error)

	// ContainerStats samples a container's resource usage until ctx ends.
	ContainerStats(ctx context.Context, id string) (<-chan Stats, <-chan error)

	// Exec starts a command inside a container and attaches to it.
	Exec(ctx context.Context, id string, opts ExecOptions) (*ExecSession, error)

	// ResizeExec changes the pseudo-terminal size of a running exec.
	ResizeExec(ctx context.Context, execID string, rows, cols uint) error

	// ExecExitCode reports the exit status of a finished exec.
	ExecExitCode(ctx context.Context, execID string) (int, error)

	// Events streams engine events until ctx ends.
	Events(ctx context.Context) (<-chan Event, <-chan error)
}
