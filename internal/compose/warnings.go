package compose

import (
	"io"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// compose-go reports the things it fixed up — an unset variable defaulted to
// an empty string, an obsolete `version:` key — through the global logrus
// logger rather than through its return value.
//
// That is a problem twice over for a daemon: the lines go to stderr in a format
// nothing else here uses, and an operator deploying a stack whose
// `${DB_PASSWORD}` is unset never sees the one message that would explain the
// database rejecting every connection. Both are fixed by capturing them.
var (
	// parseMu serializes parsing, because the hook below is installed on a
	// global logger and two concurrent parses would collect each other's
	// warnings. Parsing is milliseconds of CPU on a document a human typed;
	// the contention is not worth designing around.
	parseMu sync.Mutex
	// silenceOnce redirects logrus away from stderr. compose-go is the only
	// thing in this binary that uses it.
	silenceOnce sync.Once
)

// captureWarnings runs fn with compose-go's warnings collected rather than
// printed.
func captureWarnings(fn func() error) ([]Warning, error) {
	silenceOnce.Do(func() {
		logrus.SetOutput(io.Discard)
	})

	parseMu.Lock()
	defer parseMu.Unlock()

	hook := &warningHook{}
	logrus.AddHook(hook)
	defer removeHook(hook)

	err := fn()
	return hook.warnings(), err
}

// warningHook collects logrus warnings.
type warningHook struct {
	mu    sync.Mutex
	lines []string
}

// Levels reports which entries this hook wants. Only warnings: compose-go's
// info and debug lines are about its own internals.
func (h *warningHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.WarnLevel}
}

// Fire records one entry.
func (h *warningHook) Fire(entry *logrus.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines = append(h.lines, entry.Message)
	return nil
}

// warnings renders what was collected, dropping the noise an operator using a
// panel cannot act on.
func (h *warningHook) warnings() []Warning {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]Warning, 0, len(h.lines))
	for _, line := range h.lines {
		message := strings.TrimSpace(line)
		if message == "" || isNoise(message) {
			continue
		}
		out = append(out, Warning{Field: fieldOfWarning(message), Message: message})
	}
	return out
}

// isNoise drops warnings about things the panel already handles.
func isNoise(message string) bool {
	// `version:` is obsolete and ignored by every current parser. Repeating
	// that on every deploy trains operators to ignore warnings.
	return strings.Contains(message, "the attribute `version` is obsolete")
}

// fieldOfWarning guesses what a captured message is about, so the UI can group
// it the way it groups the warnings this package produces itself.
func fieldOfWarning(message string) string {
	if strings.Contains(message, "variable is not set") {
		return "interpolation"
	}
	return "compose"
}

// removeHook takes the hook back off the global logger.
//
// logrus has no removal API, so the hook set is rebuilt without this one.
func removeHook(target logrus.Hook) {
	hooks := logrus.StandardLogger().Hooks
	remaining := make(logrus.LevelHooks)

	for level, list := range hooks {
		for _, hook := range list {
			if hook != target {
				remaining[level] = append(remaining[level], hook)
			}
		}
	}
	logrus.StandardLogger().ReplaceHooks(remaining)
}
