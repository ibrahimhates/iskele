package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
)

// logChannelBuffer lets a slow consumer fall a little behind without stalling
// the reader goroutine.
const logChannelBuffer = 256

// maxLogLineBytes bounds a single line. A container that prints megabytes
// without a newline must not be able to exhaust the daemon's memory.
const maxLogLineBytes = 64 << 10

// ContainerLogs streams a container's output until ctx ends.
func (e *engine) ContainerLogs(ctx context.Context, id string, opts LogOptions) (<-chan LogLine, <-chan error) {
	lines := make(chan LogLine, logChannelBuffer)
	errs := make(chan error, 1)

	// Selecting neither stream means the caller wants both.
	stdout, stderr := opts.Stdout, opts.Stderr
	if !stdout && !stderr {
		stdout, stderr = true, true
	}

	sdkOpts := container.LogsOptions{
		ShowStdout: stdout,
		ShowStderr: stderr,
		Follow:     opts.Follow,
		Timestamps: true, // always requested; stripped below when not wanted
		Tail:       tailValue(opts.Tail),
	}
	if !opts.Since.IsZero() {
		sdkOpts.Since = opts.Since.UTC().Format(time.RFC3339Nano)
	}

	body, err := e.api.ContainerLogs(ctx, id, sdkOpts)
	if err != nil {
		errs <- classify("container.logs", "container", id, err)
		close(lines)
		close(errs)
		return lines, errs
	}

	go func() {
		defer close(lines)
		defer close(errs)
		defer func() { _ = body.Close() }()

		// Whether the engine multiplexes depends on the container's TTY: with
		// one, the stream is raw; without, it carries 8-byte frame headers.
		tty := !isMultiplexed(ctx, e, id)

		if err := readLogStream(ctx, body, tty, opts.Timestamps, lines); err != nil {
			select {
			case errs <- classify("container.logs", "container", id, err):
			default:
			}
		}
	}()

	return lines, errs
}

// isMultiplexed reports whether the engine will frame this container's log
// stream. A failure to tell is answered with "multiplexed", the safer guess:
// mis-parsing framed data as raw would show binary headers to the operator,
// while the reverse degrades to a whole line of text.
func isMultiplexed(ctx context.Context, e *engine, id string) bool {
	info, err := e.api.ContainerInspect(ctx, id)
	if err != nil || info.Config == nil {
		return true
	}
	return !info.Config.Tty
}

// readLogStream parses the engine's log format into lines.
func readLogStream(ctx context.Context, r io.Reader, tty, wantTimestamps bool, out chan<- LogLine) error {
	reader := bufio.NewReaderSize(r, 32<<10)

	for {
		if ctx.Err() != nil {
			return nil
		}

		stream := "stdout"
		if !tty {
			// Docker frames each chunk: [stream byte][3 pad][4-byte length].
			var header [8]byte
			if _, err := io.ReadFull(reader, header[:]); err != nil {
				return endOfStream(err)
			}
			if header[0] == 2 {
				stream = "stderr"
			}
			size := binary.BigEndian.Uint32(header[4:])
			if err := emitFrame(ctx, reader, int(size), stream, wantTimestamps, out); err != nil {
				return err
			}
			continue
		}

		line, err := readLine(reader)
		if err != nil {
			if len(line) > 0 {
				emit(ctx, out, parseLogLine(line, stream, wantTimestamps))
			}
			return endOfStream(err)
		}
		emit(ctx, out, parseLogLine(line, stream, wantTimestamps))
	}
}

// emitFrame reads one multiplexed frame and splits it into lines.
func emitFrame(ctx context.Context, r io.Reader, size int, stream string, wantTimestamps bool, out chan<- LogLine) error {
	if size <= 0 {
		return nil
	}
	if size > maxLogLineBytes {
		size = maxLogLineBytes
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return endOfStream(err)
	}

	for _, line := range strings.Split(strings.TrimRight(string(buf), "\n"), "\n") {
		emit(ctx, out, parseLogLine(line, stream, wantTimestamps))
	}
	return nil
}

// readLine reads up to a newline, bounded by maxLogLineBytes.
func readLine(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for b.Len() < maxLogLineBytes {
		chunk, isPrefix, err := r.ReadLine()
		b.Write(chunk)
		if err != nil {
			return b.String(), err
		}
		if !isPrefix {
			return b.String(), nil
		}
	}
	return b.String(), nil
}

// parseLogLine splits the engine's leading timestamp off a line.
func parseLogLine(raw, stream string, wantTimestamps bool) LogLine {
	line := LogLine{Stream: stream}

	ts, rest, found := strings.Cut(raw, " ")
	if !found {
		line.Message = raw
		return line
	}

	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		// Not a timestamp after all; the whole thing is the message.
		line.Message = raw
		return line
	}

	line.Message = rest
	if wantTimestamps {
		line.Timestamp = parsed.UTC()
	}
	return line
}

// emit sends a line unless the caller has gone away.
func emit(ctx context.Context, out chan<- LogLine, line LogLine) {
	select {
	case out <- line:
	case <-ctx.Done():
	}
}

// endOfStream turns the expected end-of-stream errors into a clean finish.
func endOfStream(err error) error {
	switch {
	case err == nil, err == io.EOF, err == io.ErrUnexpectedEOF:
		return nil
	case errIsClosed(err):
		return nil
	default:
		return err
	}
}

// errIsClosed reports the errors a canceled stream produces, which are a
// normal shutdown rather than a failure worth showing the operator.
func errIsClosed(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "http: read on closed response body")
}

// tailValue renders the tail count the way the engine expects.
func tailValue(n int) string {
	if n <= 0 {
		return "all"
	}
	return strconv.Itoa(n)
}
