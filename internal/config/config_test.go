package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// envMap returns a lookup function backed by a map, so tests never touch the
// real process environment.
func envMap(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestDefaultsWhenNothingIsProvided(t *testing.T) {
	cfg, err := Load(nil, envMap(nil), io.Discard)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Default()
	if cfg.Listen != want.Listen {
		t.Errorf("Listen = %q, want %q", cfg.Listen, want.Listen)
	}
	if cfg.DockerHost != want.DockerHost {
		t.Errorf("DockerHost = %q, want %q", cfg.DockerHost, want.DockerHost)
	}
	if cfg.DataDir != want.DataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want.DataDir)
	}
	if cfg.Session.AccessTTL.Duration() != 15*time.Minute {
		t.Errorf("AccessTTL = %s, want 15m", cfg.Session.AccessTTL)
	}
	if cfg.Session.RefreshTTL.Duration() != 168*time.Hour {
		t.Errorf("RefreshTTL = %s, want 168h", cfg.Session.RefreshTTL)
	}
	if cfg.ConfigFile != "" {
		t.Errorf("ConfigFile = %q, want empty when no file was read", cfg.ConfigFile)
	}
}

func TestYAMLOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `
listen: "0.0.0.0:9000"
docker_host: "tcp://10.0.0.5:2375"
data_dir: "/data/iskele"
allowed_paths:
  - "/opt/apps"
log_level: "debug"
session:
  access_ttl: "5m"
  refresh_ttl: "24h"
`)

	cfg, err := Load([]string{"--config", path}, envMap(nil), io.Discard)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != "0.0.0.0:9000" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.DockerHost != "tcp://10.0.0.5:2375" {
		t.Errorf("DockerHost = %q", cfg.DockerHost)
	}
	if cfg.DataDir != "/data/iskele" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if len(cfg.AllowedPaths) != 1 || cfg.AllowedPaths[0] != "/opt/apps" {
		t.Errorf("AllowedPaths = %v", cfg.AllowedPaths)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.Session.AccessTTL.Duration() != 5*time.Minute {
		t.Errorf("AccessTTL = %s", cfg.Session.AccessTTL)
	}
	// Untouched keys keep their defaults.
	if cfg.LogFormat != "auto" {
		t.Errorf("LogFormat = %q, want the default to survive", cfg.LogFormat)
	}
	if cfg.ConfigFile != path {
		t.Errorf("ConfigFile = %q, want %q", cfg.ConfigFile, path)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	path := writeConfig(t, "listen: \"127.0.0.1:1111\"\nlog_level: \"warn\"\n")

	cfg, err := Load([]string{"--config", path}, envMap(map[string]string{
		"ISKELE_LISTEN":        "127.0.0.1:2222",
		"ISKELE_LOG_LEVEL":     "error",
		"ISKELE_ALLOWED_PATHS": "/a, /b ,/c",
		"ISKELE_ACCESS_TTL":    "1m",
	}), io.Discard)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != "127.0.0.1:2222" {
		t.Errorf("Listen = %q, want the env value to win over YAML", cfg.Listen)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want the env value to win over YAML", cfg.LogLevel)
	}
	want := []string{"/a", "/b", "/c"}
	if len(cfg.AllowedPaths) != len(want) {
		t.Fatalf("AllowedPaths = %v, want %v", cfg.AllowedPaths, want)
	}
	for i := range want {
		if cfg.AllowedPaths[i] != want[i] {
			t.Errorf("AllowedPaths[%d] = %q, want %q", i, cfg.AllowedPaths[i], want[i])
		}
	}
	if cfg.Session.AccessTTL.Duration() != time.Minute {
		t.Errorf("AccessTTL = %s, want 1m", cfg.Session.AccessTTL)
	}
}

func TestFlagOverridesEnvAndYAML(t *testing.T) {
	path := writeConfig(t, "listen: \"127.0.0.1:1111\"\nlog_level: \"warn\"\n")

	args := []string{
		"--config", path,
		"--listen", "127.0.0.1:3333",
		"--log-level", "debug",
		"--data-dir", "/var/tmp/iskele",
		"--allowed-paths", "/x,/y",
		"--access-ttl", "30s",
	}
	cfg, err := Load(args, envMap(map[string]string{
		"ISKELE_LISTEN":    "127.0.0.1:2222",
		"ISKELE_LOG_LEVEL": "error",
		"ISKELE_DATA_DIR":  "/env/data",
	}), io.Discard)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != "127.0.0.1:3333" {
		t.Errorf("Listen = %q, want the flag to win", cfg.Listen)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want the flag to win", cfg.LogLevel)
	}
	if cfg.DataDir != "/var/tmp/iskele" {
		t.Errorf("DataDir = %q, want the flag to win over env", cfg.DataDir)
	}
	if cfg.Session.AccessTTL.Duration() != 30*time.Second {
		t.Errorf("AccessTTL = %s, want 30s", cfg.Session.AccessTTL)
	}
}

func TestConfigPathFromEnv(t *testing.T) {
	path := writeConfig(t, "listen: \"127.0.0.1:4444\"\n")

	cfg, err := Load(nil, envMap(map[string]string{"ISKELE_CONFIG": path}), io.Discard)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Listen != "127.0.0.1:4444" {
		t.Errorf("Listen = %q, want the file named by ISKELE_CONFIG to be read", cfg.Listen)
	}
}

func TestMissingExplicitConfigFileIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	if _, err := Load([]string{"--config", missing}, envMap(nil), io.Discard); err == nil {
		t.Fatal("Load() error = nil, want an error for an explicitly named missing file")
	}

	// The same path via the environment is equally explicit.
	if _, err := Load(nil, envMap(map[string]string{"ISKELE_CONFIG": missing}), io.Discard); err == nil {
		t.Fatal("Load() error = nil, want an error for ISKELE_CONFIG pointing at a missing file")
	}
}

func TestMissingDefaultConfigFileIsNotAnError(t *testing.T) {
	// DefaultConfigFile almost certainly does not exist in the test sandbox;
	// its absence must not stop the daemon from starting.
	if _, err := os.Stat(DefaultConfigFile); err == nil {
		t.Skipf("%s exists in this environment", DefaultConfigFile)
	}
	if _, err := Load(nil, envMap(nil), io.Discard); err != nil {
		t.Fatalf("Load() error = %v, want defaults to be usable without a config file", err)
	}
}

func TestUnknownYAMLKeyIsRejected(t *testing.T) {
	path := writeConfig(t, "listen: \"127.0.0.1:8377\"\nlisten_typo: true\n")

	_, err := Load([]string{"--config", path}, envMap(nil), io.Discard)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for an unknown key")
	}
	if !strings.Contains(err.Error(), "listen_typo") {
		t.Errorf("error = %v, want it to name the unknown key", err)
	}
}

func TestInvalidDurationInYAML(t *testing.T) {
	path := writeConfig(t, "session:\n  access_ttl: \"fifteen minutes\"\n")

	_, err := Load([]string{"--config", path}, envMap(nil), io.Discard)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for an unparsable duration")
	}
}

func TestVersionFlagIsReported(t *testing.T) {
	_, err := Load([]string{"--version"}, envMap(nil), io.Discard)
	if !errors.Is(err, ErrVersionRequested) {
		t.Fatalf("Load() error = %v, want ErrVersionRequested", err)
	}
}

func TestUnexpectedPositionalArgument(t *testing.T) {
	_, err := Load([]string{"serve"}, envMap(nil), io.Discard)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for a stray positional argument")
	}
}

func TestNormalizeCleansAndDeduplicatesPaths(t *testing.T) {
	path := writeConfig(t, `
allowed_paths:
  - "/opt/stacks/"
  - "/opt/stacks"
  - "  /srv/apps/../apps  "
  - ""
`)

	cfg, err := Load([]string{"--config", path}, envMap(nil), io.Discard)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []string{"/opt/stacks", "/srv/apps"}
	if len(cfg.AllowedPaths) != len(want) {
		t.Fatalf("AllowedPaths = %v, want %v", cfg.AllowedPaths, want)
	}
	for i := range want {
		if cfg.AllowedPaths[i] != want[i] {
			t.Errorf("AllowedPaths[%d] = %q, want %q", i, cfg.AllowedPaths[i], want[i])
		}
	}
}

func TestPubliclyBound(t *testing.T) {
	tests := []struct {
		listen string
		want   bool
	}{
		{"127.0.0.1:8377", false},
		{"localhost:8377", false},
		{"[::1]:8377", false},
		{"0.0.0.0:8377", true},
		{"192.168.1.10:8377", true},
	}
	for _, tt := range tests {
		c := Config{Listen: tt.listen}
		if got := c.PubliclyBound(); got != tt.want {
			t.Errorf("PubliclyBound(%q) = %v, want %v", tt.listen, got, tt.want)
		}
	}
}

func TestDerivedPaths(t *testing.T) {
	c := Config{DataDir: "/var/lib/iskele"}
	if got := c.DBPath(); got != "/var/lib/iskele/iskele.db" {
		t.Errorf("DBPath() = %q", got)
	}
	if got := c.BuildLogDir(); got != "/var/lib/iskele/builds" {
		t.Errorf("BuildLogDir() = %q", got)
	}
}
