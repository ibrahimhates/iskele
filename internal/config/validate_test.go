package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func valid() Config {
	c := Default()
	c.normalize()
	return c
}

func TestValidateAcceptsDefaults(t *testing.T) {
	c := valid()
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want the built-in defaults to be valid", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"empty listen", func(c *Config) { c.Listen = "" }, "listen is required"},
		{"listen without port", func(c *Config) { c.Listen = "127.0.0.1" }, "listen"},
		{"listen port not numeric", func(c *Config) { c.Listen = "127.0.0.1:http" }, "not a number"},
		{"listen port out of range", func(c *Config) { c.Listen = "127.0.0.1:70000" }, "out of range"},
		{"empty docker host", func(c *Config) { c.DockerHost = "" }, "docker_host"},
		{"docker host bad scheme", func(c *Config) { c.DockerHost = "ftp://x" }, "must start with"},
		{"docker host relative socket", func(c *Config) { c.DockerHost = "unix://relative.sock" }, "absolute"},
		{"docker host tcp without port", func(c *Config) { c.DockerHost = "tcp://10.0.0.1" }, "docker_host"},
		{"empty data dir", func(c *Config) { c.DataDir = "" }, "data_dir is required"},
		{"relative data dir", func(c *Config) { c.DataDir = "var/lib/iskele" }, "absolute"},
		{"relative allowed path", func(c *Config) { c.AllowedPaths = []string{"opt/stacks"} }, "absolute"},
		{"root allowed path", func(c *Config) { c.AllowedPaths = []string{"/"} }, "entire filesystem"},
		{"bad log level", func(c *Config) { c.LogLevel = "verbose" }, "log_level"},
		{"bad log format", func(c *Config) { c.LogFormat = "xml" }, "log_format"},
		{"tls without cert", func(c *Config) { c.TLS = TLS{Enabled: true, KeyFile: "/tmp/k"} }, "tls.cert_file"},
		{"zero access ttl", func(c *Config) { c.Session.AccessTTL = 0 }, "access_ttl must be positive"},
		{"zero refresh ttl", func(c *Config) { c.Session.RefreshTTL = 0 }, "refresh_ttl must be positive"},
		{
			"access ttl longer than refresh",
			func(c *Config) {
				c.Session.AccessTTL = Duration(48 * time.Hour)
				c.Session.RefreshTTL = Duration(time.Hour)
			},
			"must be shorter than",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mutate(&c)

			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate() error = nil, want an error mentioning %q", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("Validate() error = %q, want it to mention %q", err, tt.wantSub)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	c := valid()
	c.Listen = ""
	c.DataDir = "relative"
	c.LogLevel = "nope"

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want an error")
	}
	var ve *ValidationError
	if !asValidationError(err, &ve) {
		t.Fatalf("Validate() error type = %T, want *ValidationError", err)
	}
	if len(ve.Problems) != 3 {
		t.Errorf("Problems = %v, want all three problems reported at once", ve.Problems)
	}
}

func TestValidateAcceptsTLSWithReadableFiles(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	for _, p := range []string{cert, key} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	c := valid()
	c.TLS = TLS{Enabled: true, CertFile: cert, KeyFile: key}

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want readable TLS files to be accepted", err)
	}
}

func TestValidateRejectsUnreadableTLSFiles(t *testing.T) {
	c := valid()
	c.TLS = TLS{
		Enabled:  true,
		CertFile: filepath.Join(t.TempDir(), "missing-cert.pem"),
		KeyFile:  filepath.Join(t.TempDir(), "missing-key.pem"),
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want missing TLS files to be rejected")
	}
	if !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("Validate() error = %q, want it to say the files cannot be read", err)
	}
}

// asValidationError is a tiny stand-in for errors.As to keep the test free of
// extra imports while still asserting the concrete error type.
func asValidationError(err error, target **ValidationError) bool {
	ve, ok := err.(*ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
