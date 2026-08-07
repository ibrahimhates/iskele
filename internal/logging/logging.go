// Package logging configures the structured logger used across iskeled.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options controls how the logger is built.
type Options struct {
	// Level is one of debug, info, warn, error. Unknown values fall back to info.
	Level string
	// Format is one of auto, text, json. "auto" picks text when Output is a
	// terminal and json otherwise, so journald gets structured records.
	Format string
	// Output defaults to os.Stderr.
	Output io.Writer
	// AddSource includes file:line in every record. Enabled automatically at
	// debug level.
	AddSource bool
}

// New builds a slog.Logger from opts. It never fails: an unrecognized level or
// format degrades to the default rather than preventing the daemon from
// starting, since the logger is needed to report that very problem.
func New(opts Options) *slog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stderr
	}

	level := ParseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource || level == slog.LevelDebug,
	}

	var handler slog.Handler
	if useJSON(opts.Format, out) {
		handler = slog.NewJSONHandler(out, handlerOpts)
	} else {
		handler = slog.NewTextHandler(out, handlerOpts)
	}
	return slog.New(handler)
}

// ParseLevel maps a configuration string onto a slog level, defaulting to info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// useJSON decides the handler for a format string. "auto" keeps interactive
// runs readable and non-interactive runs machine-parsable.
func useJSON(format string, out io.Writer) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return true
	case "text":
		return false
	default:
		return !isTerminal(out)
	}
}

// isTerminal reports whether out is an interactive character device.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
