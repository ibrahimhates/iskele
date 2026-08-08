package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ValidationError collects every problem found in a configuration so the
// operator can fix all of them in one pass instead of one per restart.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return "invalid configuration: " + e.Problems[0]
	}
	return fmt.Sprintf("invalid configuration:\n  - %s", strings.Join(e.Problems, "\n  - "))
}

var (
	validLogLevels  = []string{"debug", "info", "warn", "error"}
	validLogFormats = []string{"auto", "text", "json"}
)

// Validate reports every problem with the resolved configuration.
func (c *Config) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if c.Listen == "" {
		add("listen is required (e.g. 127.0.0.1:8377)")
	} else if _, _, err := splitHostPort(c.Listen); err != nil {
		add("listen %q is not a valid host:port address: %v", c.Listen, err)
	}

	if err := validateDockerHost(c.DockerHost); err != nil {
		add("docker_host %q is invalid: %v", c.DockerHost, err)
	}

	if c.DataDir == "" {
		add("data_dir is required")
	} else if !filepath.IsAbs(c.DataDir) {
		add("data_dir %q must be an absolute path", c.DataDir)
	}

	if c.SecretKeyFile == "" {
		add("secret_key_file is required")
	} else if !filepath.IsAbs(c.SecretKeyFile) {
		add("secret_key_file %q must be an absolute path", c.SecretKeyFile)
	}

	for _, p := range c.AllowedPaths {
		if !filepath.IsAbs(p) {
			add("allowed_paths entry %q must be an absolute path", p)
		} else if p == "/" {
			add("allowed_paths entry %q would expose the entire filesystem", p)
		}
	}

	if !contains(validLogLevels, c.LogLevel) {
		add("log_level %q is invalid (want one of %s)", c.LogLevel, strings.Join(validLogLevels, ", "))
	}
	if !contains(validLogFormats, c.LogFormat) {
		add("log_format %q is invalid (want one of %s)", c.LogFormat, strings.Join(validLogFormats, ", "))
	}

	if c.TLS.Enabled {
		switch {
		case c.TLS.CertFile == "":
			add("tls.cert_file is required when tls.enabled is true")
		case !fileReadable(c.TLS.CertFile):
			add("tls.cert_file %q cannot be read", c.TLS.CertFile)
		}
		switch {
		case c.TLS.KeyFile == "":
			add("tls.key_file is required when tls.enabled is true")
		case !fileReadable(c.TLS.KeyFile):
			add("tls.key_file %q cannot be read", c.TLS.KeyFile)
		}
	}

	if c.Session.AccessTTL <= 0 {
		add("session.access_ttl must be positive (got %s)", c.Session.AccessTTL)
	}
	if c.Session.RefreshTTL <= 0 {
		add("session.refresh_ttl must be positive (got %s)", c.Session.RefreshTTL)
	}
	if c.Session.AccessTTL > 0 && c.Session.RefreshTTL > 0 && c.Session.AccessTTL >= c.Session.RefreshTTL {
		add("session.access_ttl (%s) must be shorter than session.refresh_ttl (%s)",
			c.Session.AccessTTL, c.Session.RefreshTTL)
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// validateDockerHost accepts the endpoint forms the Docker SDK understands.
func validateDockerHost(host string) error {
	switch {
	case host == "":
		return errors.New("required (e.g. unix:///var/run/docker.sock)")
	case strings.HasPrefix(host, "unix://"):
		p := strings.TrimPrefix(host, "unix://")
		if !filepath.IsAbs(p) {
			return errors.New("socket path must be absolute")
		}
		return nil
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		addr := host[strings.Index(host, "//")+2:]
		if addr == "" {
			return errors.New("missing host:port")
		}
		if _, _, err := splitHostPort(addr); err != nil {
			return err
		}
		return nil
	default:
		return errors.New("must start with unix://, tcp://, http:// or https://")
	}
}

// splitHostPort validates an address and returns its parts. Unlike
// net.SplitHostPort it also rejects out-of-range and non-numeric ports.
func splitHostPort(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	if port == "" {
		return "", "", errors.New("missing port")
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return "", "", fmt.Errorf("port %q is not a number", port)
	}
	if n < 1 || n > 65535 {
		return "", "", fmt.Errorf("port %d is out of range 1-65535", n)
	}
	return host, port, nil
}

func fileReadable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	f, err := os.Open(path) //nolint:gosec // operator-supplied path by design
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
