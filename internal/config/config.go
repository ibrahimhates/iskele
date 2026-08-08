// Package config loads iskeled's runtime configuration.
//
// Values are resolved with the following precedence, highest first:
//
//	command-line flag  >  environment variable  >  YAML file  >  built-in default
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultConfigFile is read when neither --config nor ISKELE_CONFIG is given.
// A missing file at this path is not an error; a missing explicit path is.
const DefaultConfigFile = "/etc/iskele/config.yaml"

// ErrVersionRequested is returned by Load when --version was passed. The caller
// is expected to print the version and exit successfully.
var ErrVersionRequested = errors.New("version requested")

// Config is the fully resolved configuration of a running iskeled instance.
type Config struct {
	// Listen is the TCP address the HTTP server binds to, e.g. "127.0.0.1:8377".
	Listen string `yaml:"listen"`
	// DockerHost is the Docker Engine endpoint (unix:// or tcp://).
	DockerHost string `yaml:"docker_host"`
	// DataDir holds the SQLite database, build logs and other mutable state.
	DataDir string `yaml:"data_dir"`
	// SecretKeyFile holds the master key used to encrypt stored secrets and to
	// derive the JWT signing key. It must be readable only by iskeled's user.
	SecretKeyFile string `yaml:"secret_key_file"`
	// AllowedPaths restricts which host directories may be used for bind
	// mounts and build contexts. Everything outside is rejected.
	AllowedPaths []string `yaml:"allowed_paths"`
	// LogLevel is one of debug, info, warn, error.
	LogLevel string `yaml:"log_level"`
	// LogFormat is one of auto, text, json. "auto" picks text on a TTY.
	LogFormat string `yaml:"log_format"`

	TLS     TLS     `yaml:"tls"`
	Session Session `yaml:"session"`

	// ConfigFile records which file the values were loaded from, if any.
	ConfigFile string `yaml:"-"`
}

// TLS configures the optional built-in HTTPS listener.
type TLS struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Session configures token lifetimes.
type Session struct {
	AccessTTL  Duration `yaml:"access_ttl"`
	RefreshTTL Duration `yaml:"refresh_ttl"`
}

// Default returns the built-in configuration, matching deploy/config.example.yaml.
func Default() Config {
	return Config{
		Listen:        "127.0.0.1:8377",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       "/var/lib/iskele",
		SecretKeyFile: "/etc/iskele/secret.key",
		AllowedPaths:  []string{"/opt/stacks", "/srv"},
		LogLevel:      "info",
		LogFormat:     "auto",
		TLS:           TLS{Enabled: false},
		Session: Session{
			AccessTTL:  Duration(15 * time.Minute),
			RefreshTTL: Duration(168 * time.Hour),
		},
	}
}

// Load resolves the configuration from args (without the program name), the
// environment and the YAML file, then validates the result.
//
// lookupEnv is injected so tests do not have to mutate the process
// environment; pass os.LookupEnv in production.
func Load(args []string, lookupEnv func(string) (string, bool), errOut io.Writer) (*Config, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	fs, raw := newFlagSet(errOut)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if raw.showVersion {
		return nil, ErrVersionRequested
	}
	if rest := fs.Args(); len(rest) > 0 {
		return nil, fmt.Errorf("unexpected argument %q", rest[0])
	}

	set := setFlags(fs)

	cfg := Default()

	// 1. YAML file (lowest precedence above defaults).
	path, explicit := resolveConfigPath(set["config"], raw.configFile, lookupEnv)
	if err := applyFile(&cfg, path, explicit); err != nil {
		return nil, err
	}

	// 2. Environment.
	if err := applyEnv(&cfg, lookupEnv); err != nil {
		return nil, err
	}

	// 3. Flags (highest precedence).
	applyFlags(&cfg, set, raw)

	cfg.normalize()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// rawFlags holds the flag values before precedence is applied. A value is only
// used when its flag was actually present on the command line.
type rawFlags struct {
	configFile    string
	listen        string
	dockerHost    string
	dataDir       string
	secretKeyFile string
	allowedPaths  string
	logLevel      string
	logFormat     string
	tlsEnabled    bool
	tlsCertFile   string
	tlsKeyFile    string
	accessTTL     time.Duration
	refreshTTL    time.Duration
	showVersion   bool
}

func newFlagSet(errOut io.Writer) (*flag.FlagSet, *rawFlags) {
	var r rawFlags
	fs := flag.NewFlagSet("iskeled", flag.ContinueOnError)
	if errOut != nil {
		fs.SetOutput(errOut)
	}

	fs.StringVar(&r.configFile, "config", DefaultConfigFile, "path to the YAML configuration file")
	fs.StringVar(&r.listen, "listen", "", "address to bind the HTTP server to (host:port)")
	fs.StringVar(&r.dockerHost, "docker-host", "", "Docker Engine endpoint (unix:// or tcp://)")
	fs.StringVar(&r.dataDir, "data-dir", "", "directory for the database and build logs")
	fs.StringVar(&r.secretKeyFile, "secret-key-file", "", "path to the master key file (created with mode 0600 if absent)")
	fs.StringVar(&r.allowedPaths, "allowed-paths", "", "comma-separated host directories usable for bind mounts and builds")
	fs.StringVar(&r.logLevel, "log-level", "", "log level: debug, info, warn, error")
	fs.StringVar(&r.logFormat, "log-format", "", "log format: auto, text, json")
	fs.BoolVar(&r.tlsEnabled, "tls", false, "serve HTTPS using --tls-cert and --tls-key")
	fs.StringVar(&r.tlsCertFile, "tls-cert", "", "path to the TLS certificate file")
	fs.StringVar(&r.tlsKeyFile, "tls-key", "", "path to the TLS private key file")
	fs.DurationVar(&r.accessTTL, "access-ttl", 0, "lifetime of access tokens")
	fs.DurationVar(&r.refreshTTL, "refresh-ttl", 0, "lifetime of refresh tokens")
	fs.BoolVar(&r.showVersion, "version", false, "print version information and exit")

	fs.Usage = func() {
		out := fs.Output()
		// Usage text goes to a best-effort writer; a failed write here has no
		// recovery path worth reporting.
		_, _ = fmt.Fprint(out, "iskeled - Docker management panel daemon\n\nUsage:\n  iskeled [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprint(out, "\nEnvironment variables (override the config file, overridden by flags):\n")
		for _, e := range envNames {
			_, _ = fmt.Fprintf(out, "  %s\n", e)
		}
	}
	return fs, &r
}

var envNames = []string{
	"ISKELE_CONFIG", "ISKELE_LISTEN", "ISKELE_DOCKER_HOST", "ISKELE_DATA_DIR",
	"ISKELE_SECRET_KEY_FILE",
	"ISKELE_ALLOWED_PATHS", "ISKELE_LOG_LEVEL", "ISKELE_LOG_FORMAT",
	"ISKELE_TLS_ENABLED", "ISKELE_TLS_CERT_FILE", "ISKELE_TLS_KEY_FILE",
	"ISKELE_ACCESS_TTL", "ISKELE_REFRESH_TTL",
}

// setFlags reports which flags were explicitly provided on the command line.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// resolveConfigPath returns the config file to read and whether the location
// was chosen explicitly (in which case a missing file is an error).
func resolveConfigPath(flagSet bool, flagValue string, lookupEnv func(string) (string, bool)) (path string, explicit bool) {
	if flagSet {
		return flagValue, true
	}
	if v, ok := lookupEnv("ISKELE_CONFIG"); ok && v != "" {
		return v, true
	}
	return DefaultConfigFile, false
}

func applyFile(cfg *Config, path string, explicit bool) error {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied path by design
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return nil
		}
		return fmt.Errorf("read config file %s: %w", path, err)
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	cfg.ConfigFile = path
	return nil
}

func applyEnv(cfg *Config, lookupEnv func(string) (string, bool)) error {
	str := func(name string, dst *string) {
		if v, ok := lookupEnv(name); ok && v != "" {
			*dst = v
		}
	}
	str("ISKELE_LISTEN", &cfg.Listen)
	str("ISKELE_DOCKER_HOST", &cfg.DockerHost)
	str("ISKELE_DATA_DIR", &cfg.DataDir)
	str("ISKELE_SECRET_KEY_FILE", &cfg.SecretKeyFile)
	str("ISKELE_LOG_LEVEL", &cfg.LogLevel)
	str("ISKELE_LOG_FORMAT", &cfg.LogFormat)
	str("ISKELE_TLS_CERT_FILE", &cfg.TLS.CertFile)
	str("ISKELE_TLS_KEY_FILE", &cfg.TLS.KeyFile)

	if v, ok := lookupEnv("ISKELE_ALLOWED_PATHS"); ok && v != "" {
		cfg.AllowedPaths = splitList(v)
	}
	if v, ok := lookupEnv("ISKELE_TLS_ENABLED"); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("ISKELE_TLS_ENABLED: %w", err)
		}
		cfg.TLS.Enabled = b
	}
	if v, ok := lookupEnv("ISKELE_ACCESS_TTL"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("ISKELE_ACCESS_TTL: %w", err)
		}
		cfg.Session.AccessTTL = Duration(d)
	}
	if v, ok := lookupEnv("ISKELE_REFRESH_TTL"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("ISKELE_REFRESH_TTL: %w", err)
		}
		cfg.Session.RefreshTTL = Duration(d)
	}
	return nil
}

func applyFlags(cfg *Config, set map[string]bool, r *rawFlags) {
	if set["listen"] {
		cfg.Listen = r.listen
	}
	if set["docker-host"] {
		cfg.DockerHost = r.dockerHost
	}
	if set["data-dir"] {
		cfg.DataDir = r.dataDir
	}
	if set["secret-key-file"] {
		cfg.SecretKeyFile = r.secretKeyFile
	}
	if set["allowed-paths"] {
		cfg.AllowedPaths = splitList(r.allowedPaths)
	}
	if set["log-level"] {
		cfg.LogLevel = r.logLevel
	}
	if set["log-format"] {
		cfg.LogFormat = r.logFormat
	}
	if set["tls"] {
		cfg.TLS.Enabled = r.tlsEnabled
	}
	if set["tls-cert"] {
		cfg.TLS.CertFile = r.tlsCertFile
	}
	if set["tls-key"] {
		cfg.TLS.KeyFile = r.tlsKeyFile
	}
	if set["access-ttl"] {
		cfg.Session.AccessTTL = Duration(r.accessTTL)
	}
	if set["refresh-ttl"] {
		cfg.Session.RefreshTTL = Duration(r.refreshTTL)
	}
}

// normalize trims and cleans path-like values so validation and later prefix
// checks operate on canonical strings.
func (c *Config) normalize() {
	c.Listen = strings.TrimSpace(c.Listen)
	c.DockerHost = strings.TrimSpace(c.DockerHost)
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	c.LogFormat = strings.ToLower(strings.TrimSpace(c.LogFormat))

	if c.DataDir != "" {
		c.DataDir = filepath.Clean(strings.TrimSpace(c.DataDir))
	}
	if c.SecretKeyFile != "" {
		c.SecretKeyFile = filepath.Clean(strings.TrimSpace(c.SecretKeyFile))
	}

	cleaned := make([]string, 0, len(c.AllowedPaths))
	seen := make(map[string]struct{}, len(c.AllowedPaths))
	for _, p := range c.AllowedPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		cleaned = append(cleaned, p)
	}
	c.AllowedPaths = cleaned
}

// DBPath is the location of the SQLite database inside DataDir.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "iskele.db") }

// BuildLogDir is where build logs are archived.
func (c *Config) BuildLogDir() string { return filepath.Join(c.DataDir, "builds") }

// PubliclyBound reports whether the listener is reachable from outside the
// host, which warrants a TLS / reverse-proxy warning.
func (c *Config) PubliclyBound() bool {
	host, _, err := splitHostPort(c.Listen)
	if err != nil {
		return false
	}
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return false
	default:
		return true
	}
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Duration is a time.Duration that unmarshals from a YAML string such as "15m".
type Duration time.Duration

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String implements fmt.Stringer.
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML accepts both "15m" and an integer number of seconds.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}

	var secs int64
	if err := node.Decode(&secs); err != nil {
		return fmt.Errorf("invalid duration %q: want a string like \"15m\"", node.Value)
	}
	*d = Duration(time.Duration(secs) * time.Second)
	return nil
}

// MarshalYAML renders the duration back as a human-readable string.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }
