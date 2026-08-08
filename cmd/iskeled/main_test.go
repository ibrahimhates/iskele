package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/config"
)

func TestRunVersionFlagExitsCleanly(t *testing.T) {
	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("run(--version) error = %v, want nil", err)
	}
}

func TestRunHelpFlagExitsCleanly(t *testing.T) {
	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("run(--help) error = %v, want nil", err)
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	err := run([]string{"--listen", "not-an-address:99999"})
	if err == nil {
		t.Fatal("run() error = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error = %v, want it to name the offending setting", err)
	}
}

func TestRunRejectsUnwritableDataDir(t *testing.T) {
	// A file where a directory is expected: MkdirAll must fail rather than
	// leaving the daemon running without usable state.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	err := run([]string{
		"--listen", "127.0.0.1:8377",
		"--data-dir", filepath.Join(blocker, "iskele"),
	})
	if err == nil {
		t.Fatal("run() error = nil, want a data dir error")
	}
	if !strings.Contains(err.Error(), "data dir") {
		t.Errorf("error = %v, want it to mention the data dir", err)
	}
}

func TestConfigSourceLabelsDefaults(t *testing.T) {
	if got := configSource(&config.Config{ConfigFile: ""}); got != "(defaults)" {
		t.Errorf("configSource() = %q, want %q", got, "(defaults)")
	}
	if got := configSource(&config.Config{ConfigFile: "/etc/iskele/config.yaml"}); got != "/etc/iskele/config.yaml" {
		t.Errorf("configSource() = %q", got)
	}
}
