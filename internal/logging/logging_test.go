package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"DEBUG":    slog.LevelDebug,
		" info ":   slog.LevelInfo,
		"warn":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
	}
	for in, want := range tests {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestJSONFormatEmitsParsableRecords(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Level: "info", Format: "json", Output: &buf})

	log.Info("listening", "addr", "127.0.0.1:8377")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, buf.String())
	}
	if record["msg"] != "listening" {
		t.Errorf("msg = %v, want %q", record["msg"], "listening")
	}
	if record["addr"] != "127.0.0.1:8377" {
		t.Errorf("addr = %v", record["addr"])
	}
}

func TestTextFormatIsHumanReadable(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Level: "info", Format: "text", Output: &buf})

	log.Info("hello", "k", "v")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "k=v") {
		t.Errorf("output = %q, want key=value text format", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("output = %q, want text rather than JSON", out)
	}
}

func TestLevelFiltersLowerRecords(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Level: "warn", Format: "text", Output: &buf})

	log.Debug("debug message")
	log.Info("info message")
	if buf.Len() != 0 {
		t.Errorf("output = %q, want debug and info to be filtered at warn level", buf.String())
	}

	log.Warn("warn message")
	if !strings.Contains(buf.String(), "warn message") {
		t.Errorf("output = %q, want the warn record to pass", buf.String())
	}
}

func TestAutoFormatFallsBackToJSONForNonTerminals(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Level: "info", Format: "auto", Output: &buf})

	log.Info("hello")

	if !strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("output = %q, want JSON when the writer is not a terminal", buf.String())
	}
}

func TestDebugLevelAddsSource(t *testing.T) {
	var buf bytes.Buffer
	log := New(Options{Level: "debug", Format: "json", Output: &buf})

	log.Debug("with source")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := record["source"]; !ok {
		t.Errorf("record = %v, want a source field at debug level", record)
	}
}

func TestNewDefaultsToStderrWithoutPanicking(t *testing.T) {
	// Output is nil on purpose: New must not dereference it.
	if log := New(Options{}); log == nil {
		t.Fatal("New() returned nil")
	}
}
