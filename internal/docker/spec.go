package docker

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockermount "github.com/docker/docker/api/types/mount"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/go-connections/nat"
)

// ContainerSpec is a container definition in plain, JSON-friendly types.
//
// [CreateSpec] carries the engine's own structures and exists so redeploy can
// reproduce a container byte for byte. This is the other direction: what the
// create form sends, in terms an operator recognizes, which the service layer
// can validate (path whitelist, privileged permission) before any of it
// reaches the SDK.
type ContainerSpec struct {
	Name  string `json:"name"`
	Image string `json:"image"`

	// PullPolicy is "missing" (default), "always" or "never".
	PullPolicy string `json:"pull_policy,omitempty"`

	Command    []string `json:"command,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`
	User       string   `json:"user,omitempty"`
	Hostname   string   `json:"hostname,omitempty"`
	DomainName string   `json:"domain_name,omitempty"`

	TTY       bool `json:"tty,omitempty"`
	OpenStdin bool `json:"open_stdin,omitempty"`

	Env    []EnvVar          `json:"env,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`

	Ports  []PortMapping `json:"ports,omitempty"`
	Mounts []MountSpec   `json:"mounts,omitempty"`

	RestartPolicy RestartPolicy `json:"restart_policy"`
	Resources     Resources     `json:"resources"`
	Network       NetworkSpec   `json:"network"`
	HealthCheck   *HealthSpec   `json:"health_check,omitempty"`
	Logging       LoggingSpec   `json:"logging"`
	Security      SecuritySpec  `json:"security"`

	AutoRemove bool `json:"auto_remove,omitempty"`
	Init       bool `json:"init,omitempty"`
	// Start runs the container as soon as it is created, which is what the
	// wizard's "create and start" does.
	Start bool `json:"start,omitempty"`
}

// EnvVar is one environment entry. Value is kept separate from Key so a value
// containing "=" survives the round trip.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PortMapping publishes one container port on the host.
type PortMapping struct {
	// HostIP is optional; empty publishes on every interface, which is worth
	// the operator noticing.
	HostIP string `json:"host_ip,omitempty"`
	// HostPort empty means the engine picks a free one.
	HostPort      string `json:"host_port,omitempty"`
	ContainerPort int    `json:"container_port"`
	// Protocol is "tcp" (default) or "udp".
	Protocol string `json:"protocol,omitempty"`
}

// MountSpec is one bind, named volume or tmpfs mount.
type MountSpec struct {
	// Type is "bind", "volume" or "tmpfs".
	Type string `json:"type"`
	// Source is a host path for a bind, a volume name for a volume, and
	// unused for tmpfs.
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	// TmpfsSize is a byte count; 0 leaves the engine's default.
	TmpfsSize int64 `json:"tmpfs_size,omitempty"`
	// BindPropagation is one of rprivate, private, rshared, shared, rslave,
	// slave. Empty leaves the engine's default.
	BindPropagation string `json:"bind_propagation,omitempty"`
	// CreateHostPath creates a missing bind source rather than failing.
	CreateHostPath bool `json:"create_host_path,omitempty"`
}

// RestartPolicy mirrors `docker run --restart`.
type RestartPolicy struct {
	// Name is "no" (default), "always", "unless-stopped" or "on-failure".
	Name string `json:"name,omitempty"`
	// MaxRetries only applies to on-failure.
	MaxRetries int `json:"max_retries,omitempty"`
}

// Resources are the cgroup limits.
type Resources struct {
	// CPUs is a core count, as `docker run --cpus` takes it (1.5 = one and a
	// half cores).
	CPUs float64 `json:"cpus,omitempty"`
	// CPUShares is the relative weight under contention (default 1024).
	CPUShares int64 `json:"cpu_shares,omitempty"`
	// CPUSetCPUs pins the container to specific cores, e.g. "0-3".
	CPUSetCPUs string `json:"cpuset_cpus,omitempty"`
	// Memory and MemoryReservation are byte counts.
	Memory            int64 `json:"memory,omitempty"`
	MemoryReservation int64 `json:"memory_reservation,omitempty"`
	// MemorySwap is the combined memory+swap limit; -1 allows unlimited swap.
	MemorySwap int64 `json:"memory_swap,omitempty"`
	// PidsLimit caps the process count; 0 leaves it unlimited.
	PidsLimit int64 `json:"pids_limit,omitempty"`
	// ShmSize is /dev/shm in bytes; 0 leaves the engine's 64 MB default.
	ShmSize int64 `json:"shm_size,omitempty"`
	// OOMKillDisable is deliberately not exposed: disabling the OOM killer on
	// a container without a memory limit can wedge the whole host.
}

// NetworkSpec attaches the container to a network.
type NetworkSpec struct {
	// Name is a network name or "bridge"/"host"/"none". Empty means the
	// engine's default bridge.
	Name    string   `json:"name,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	// IPv4Address requests a static address, which only works on a network
	// with a user-defined subnet.
	IPv4Address string `json:"ipv4_address,omitempty"`
	IPv6Address string `json:"ipv6_address,omitempty"`
	// ExtraHosts are "hostname:ip" entries added to /etc/hosts.
	ExtraHosts []string `json:"extra_hosts,omitempty"`
	// DNS, DNSSearch and DNSOptions override the container's resolver.
	DNS        []string `json:"dns,omitempty"`
	DNSSearch  []string `json:"dns_search,omitempty"`
	DNSOptions []string `json:"dns_options,omitempty"`
	// MacAddress requests a fixed MAC.
	MacAddress string `json:"mac_address,omitempty"`
}

// HealthSpec is the container's health probe.
type HealthSpec struct {
	// Test is the probe command. A single element is run through a shell, as
	// CMD-SHELL does; several are exec'd directly.
	Test []string `json:"test"`
	// Interval, Timeout and StartPeriod are Go durations ("30s", "1m").
	Interval    string `json:"interval,omitempty"`
	Timeout     string `json:"timeout,omitempty"`
	StartPeriod string `json:"start_period,omitempty"`
	Retries     int    `json:"retries,omitempty"`
	// Disable turns off a health check inherited from the image.
	Disable bool `json:"disable,omitempty"`
}

// LoggingSpec selects the container's log driver.
type LoggingSpec struct {
	Driver  string            `json:"driver,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

// SecuritySpec holds the options that widen what a container may do.
//
// Every field here is admin-only at the service layer: each one is, in some
// configuration, a path from container to host root.
type SecuritySpec struct {
	Privileged     bool     `json:"privileged,omitempty"`
	CapAdd         []string `json:"cap_add,omitempty"`
	CapDrop        []string `json:"cap_drop,omitempty"`
	SecurityOpt    []string `json:"security_opt,omitempty"`
	Devices        []string `json:"devices,omitempty"`
	ReadOnlyRootFS bool     `json:"read_only_root_fs,omitempty"`
	// Sysctls are kernel parameters set inside the container's namespaces.
	Sysctls map[string]string `json:"sysctls,omitempty"`
}

// Mount types.
const (
	MountTypeBind   = "bind"
	MountTypeVolume = "volume"
	MountTypeTmpfs  = "tmpfs"
)

// Pull policies.
const (
	PullMissing = "missing"
	PullAlways  = "always"
	PullNever   = "never"
)

// SpecError reports a container definition the engine would reject, caught
// before the request is sent so the message can name the field.
type SpecError struct {
	Field   string
	Message string
}

func (e *SpecError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

// newSpecError is shorthand for the checks below.
func newSpecError(field, format string, args ...any) *SpecError {
	return &SpecError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// BuildCreateSpec converts an operator-facing definition into the engine's.
//
// It validates as it goes, so a malformed port or duration is reported with
// the field that carried it rather than as an opaque 400 from the daemon.
func BuildCreateSpec(spec ContainerSpec) (CreateSpec, error) {
	if strings.TrimSpace(spec.Image) == "" {
		return CreateSpec{}, newSpecError("image", "an image is required")
	}

	config := &dockercontainer.Config{
		Image:      strings.TrimSpace(spec.Image),
		WorkingDir: spec.WorkingDir,
		User:       spec.User,
		Hostname:   spec.Hostname,
		Domainname: spec.DomainName,
		Tty:        spec.TTY,
		OpenStdin:  spec.OpenStdin,
		Labels:     spec.Labels,
		Env:        buildEnv(spec.Env),
	}
	if len(spec.Command) > 0 {
		config.Cmd = strslice.StrSlice(spec.Command)
	}
	if len(spec.Entrypoint) > 0 {
		config.Entrypoint = strslice.StrSlice(spec.Entrypoint)
	}

	health, err := buildHealthCheck(spec.HealthCheck)
	if err != nil {
		return CreateSpec{}, err
	}
	config.Healthcheck = health

	exposed, bindings, err := buildPorts(spec.Ports)
	if err != nil {
		return CreateSpec{}, err
	}
	config.ExposedPorts = exposed

	mounts, tmpfs, err := buildMounts(spec.Mounts)
	if err != nil {
		return CreateSpec{}, err
	}

	restart, err := buildRestartPolicy(spec.RestartPolicy)
	if err != nil {
		return CreateSpec{}, err
	}

	hostConfig := &dockercontainer.HostConfig{
		PortBindings:   bindings,
		Mounts:         mounts,
		Tmpfs:          tmpfs,
		RestartPolicy:  restart,
		AutoRemove:     spec.AutoRemove,
		Privileged:     spec.Security.Privileged,
		ReadonlyRootfs: spec.Security.ReadOnlyRootFS,
		CapAdd:         strslice.StrSlice(spec.Security.CapAdd),
		CapDrop:        strslice.StrSlice(spec.Security.CapDrop),
		SecurityOpt:    spec.Security.SecurityOpt,
		Sysctls:        spec.Security.Sysctls,
		ExtraHosts:     spec.Network.ExtraHosts,
		DNS:            spec.Network.DNS,
		DNSSearch:      spec.Network.DNSSearch,
		DNSOptions:     spec.Network.DNSOptions,
		Resources:      buildResources(spec.Resources),
	}
	if spec.Init {
		enabled := true
		hostConfig.Init = &enabled
	}
	if spec.Logging.Driver != "" {
		hostConfig.LogConfig = dockercontainer.LogConfig{
			Type:   spec.Logging.Driver,
			Config: spec.Logging.Options,
		}
	}
	if devices, devErr := buildDevices(spec.Security.Devices); devErr != nil {
		return CreateSpec{}, devErr
	} else if len(devices) > 0 {
		hostConfig.Devices = devices
	}

	// A named network goes on HostConfig so the container starts attached to
	// it rather than to the default bridge and then being moved.
	networking := buildNetworking(spec.Network)
	if name := strings.TrimSpace(spec.Network.Name); name != "" {
		hostConfig.NetworkMode = dockercontainer.NetworkMode(name)
	}

	return CreateSpec{
		Name:             strings.TrimSpace(spec.Name),
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networking,
	}, nil
}

// buildEnv renders the environment in the engine's KEY=VALUE form, sorted so
// two identical specs produce identical requests.
func buildEnv(vars []EnvVar) []string {
	if len(vars) == 0 {
		return nil
	}
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		key := strings.TrimSpace(v.Key)
		if key == "" {
			continue
		}
		out = append(out, key+"="+v.Value)
	}
	sort.Strings(out)
	return out
}

// buildPorts turns the mapping rows into the engine's exposed-port set and
// host bindings.
func buildPorts(ports []PortMapping) (nat.PortSet, nat.PortMap, error) {
	if len(ports) == 0 {
		return nil, nil, nil
	}

	exposed := nat.PortSet{}
	bindings := nat.PortMap{}

	for _, p := range ports {
		if p.ContainerPort < 1 || p.ContainerPort > 65535 {
			return nil, nil, newSpecError("ports",
				"container port %d is outside 1-65535", p.ContainerPort)
		}

		proto := strings.ToLower(strings.TrimSpace(p.Protocol))
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" && proto != "sctp" {
			return nil, nil, newSpecError("ports", "unknown protocol %q", p.Protocol)
		}

		port, err := nat.NewPort(proto, strconv.Itoa(p.ContainerPort))
		if err != nil {
			return nil, nil, newSpecError("ports", "%s", err.Error())
		}
		exposed[port] = struct{}{}

		// No host port means "expose only": the port is reachable from other
		// containers but not published on the host.
		if strings.TrimSpace(p.HostPort) == "" && strings.TrimSpace(p.HostIP) == "" {
			continue
		}
		if hostPort := strings.TrimSpace(p.HostPort); hostPort != "" {
			if err := validatePortValue(hostPort); err != nil {
				return nil, nil, err
			}
		}

		bindings[port] = append(bindings[port], nat.PortBinding{
			HostIP:   strings.TrimSpace(p.HostIP),
			HostPort: strings.TrimSpace(p.HostPort),
		})
	}

	return exposed, bindings, nil
}

// validatePortValue accepts a single port or a "start-end" range, which is
// what the engine allows on the host side.
func validatePortValue(value string) error {
	start, end, isRange := strings.Cut(value, "-")
	for _, part := range []string{start, end} {
		if part == "" {
			if isRange {
				return newSpecError("ports", "malformed host port range %q", value)
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 65535 {
			return newSpecError("ports", "host port %q is not a port number", value)
		}
	}
	return nil
}

// buildMounts splits the mount rows into engine mounts and the tmpfs map.
func buildMounts(specs []MountSpec) ([]dockermount.Mount, map[string]string, error) {
	var mounts []dockermount.Mount
	var tmpfs map[string]string

	for _, m := range specs {
		dest := strings.TrimSpace(m.Destination)
		if dest == "" {
			return nil, nil, newSpecError("mounts", "every mount needs a destination")
		}
		if !strings.HasPrefix(dest, "/") {
			return nil, nil, newSpecError("mounts",
				"destination %q must be an absolute path inside the container", dest)
		}

		switch strings.ToLower(strings.TrimSpace(m.Type)) {
		case MountTypeTmpfs:
			if tmpfs == nil {
				tmpfs = map[string]string{}
			}
			opts := "rw"
			if m.ReadOnly {
				opts = "ro"
			}
			if m.TmpfsSize > 0 {
				opts += ",size=" + strconv.FormatInt(m.TmpfsSize, 10)
			}
			tmpfs[dest] = opts

		case MountTypeBind:
			source := strings.TrimSpace(m.Source)
			if source == "" {
				return nil, nil, newSpecError("mounts", "a bind mount needs a host path")
			}
			if !strings.HasPrefix(source, "/") {
				return nil, nil, newSpecError("mounts",
					"bind source %q must be an absolute host path", source)
			}
			mount := dockermount.Mount{
				Type:     dockermount.TypeBind,
				Source:   source,
				Target:   dest,
				ReadOnly: m.ReadOnly,
			}
			if m.BindPropagation != "" || m.CreateHostPath {
				mount.BindOptions = &dockermount.BindOptions{
					Propagation: dockermount.Propagation(m.BindPropagation),
				}
				if m.CreateHostPath {
					mount.BindOptions.CreateMountpoint = true
				}
			}
			mounts = append(mounts, mount)

		case MountTypeVolume, "":
			source := strings.TrimSpace(m.Source)
			mount := dockermount.Mount{
				Type:     dockermount.TypeVolume,
				Source:   source, // empty means an anonymous volume
				Target:   dest,
				ReadOnly: m.ReadOnly,
			}
			mounts = append(mounts, mount)

		default:
			return nil, nil, newSpecError("mounts", "unknown mount type %q", m.Type)
		}
	}

	return mounts, tmpfs, nil
}

// buildRestartPolicy validates the policy name and its retry count.
func buildRestartPolicy(policy RestartPolicy) (dockercontainer.RestartPolicy, error) {
	name := strings.TrimSpace(policy.Name)
	if name == "" {
		name = "no"
	}

	switch dockercontainer.RestartPolicyMode(name) {
	case dockercontainer.RestartPolicyDisabled,
		dockercontainer.RestartPolicyAlways,
		dockercontainer.RestartPolicyUnlessStopped:
		if policy.MaxRetries != 0 {
			return dockercontainer.RestartPolicy{}, newSpecError("restart_policy",
				"a retry count only applies to on-failure")
		}
		return dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyMode(name)}, nil

	case dockercontainer.RestartPolicyOnFailure:
		if policy.MaxRetries < 0 {
			return dockercontainer.RestartPolicy{}, newSpecError("restart_policy",
				"the retry count cannot be negative")
		}
		return dockercontainer.RestartPolicy{
			Name:              dockercontainer.RestartPolicyOnFailure,
			MaximumRetryCount: policy.MaxRetries,
		}, nil

	default:
		return dockercontainer.RestartPolicy{}, newSpecError("restart_policy",
			"unknown policy %q; use no, always, unless-stopped or on-failure", policy.Name)
	}
}

// nanoCPUsPerCore is what the engine expects for `--cpus`.
const nanoCPUsPerCore = 1_000_000_000

// buildResources maps the limit fields onto the engine's cgroup settings.
func buildResources(r Resources) dockercontainer.Resources {
	out := dockercontainer.Resources{
		CPUShares:         r.CPUShares,
		CpusetCpus:        r.CPUSetCPUs,
		Memory:            r.Memory,
		MemoryReservation: r.MemoryReservation,
		MemorySwap:        r.MemorySwap,
	}
	if r.CPUs > 0 {
		out.NanoCPUs = int64(r.CPUs * nanoCPUsPerCore)
	}
	if r.PidsLimit > 0 {
		limit := r.PidsLimit
		out.PidsLimit = &limit
	}
	return out
}

// buildDevices parses `--device` entries of the form host[:container[:perms]].
func buildDevices(devices []string) ([]dockercontainer.DeviceMapping, error) {
	if len(devices) == 0 {
		return nil, nil
	}

	out := make([]dockercontainer.DeviceMapping, 0, len(devices))
	for _, raw := range devices {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		parts := strings.Split(entry, ":")
		if len(parts) > 3 {
			return nil, newSpecError("devices",
				"%q has too many parts; use host[:container[:permissions]]", raw)
		}
		host := parts[0]
		if !strings.HasPrefix(host, "/") {
			return nil, newSpecError("devices", "device %q must be an absolute path", host)
		}

		mapping := dockercontainer.DeviceMapping{
			PathOnHost:        host,
			PathInContainer:   host,
			CgroupPermissions: "rwm",
		}
		if len(parts) > 1 && parts[1] != "" {
			mapping.PathInContainer = parts[1]
		}
		if len(parts) > 2 && parts[2] != "" {
			if strings.Trim(parts[2], "rwm") != "" {
				return nil, newSpecError("devices",
					"device permissions %q may only contain r, w and m", parts[2])
			}
			mapping.CgroupPermissions = parts[2]
		}
		out = append(out, mapping)
	}
	return out, nil
}

// buildHealthCheck validates the probe and its durations.
func buildHealthCheck(spec *HealthSpec) (*dockercontainer.HealthConfig, error) {
	if spec == nil {
		return nil, nil
	}
	if spec.Disable {
		// The engine reads NONE as "the image's health check does not apply".
		return &dockercontainer.HealthConfig{Test: []string{"NONE"}}, nil
	}

	test := spec.Test
	if len(test) == 0 {
		return nil, newSpecError("health_check", "a health check needs a command")
	}
	// A bare command line is run through a shell, which is what an operator
	// typing `curl -f localhost/health` expects.
	if len(test) == 1 {
		test = []string{"CMD-SHELL", test[0]}
	} else if test[0] != "CMD" && test[0] != "CMD-SHELL" && test[0] != "NONE" {
		test = append([]string{"CMD"}, test...)
	}

	interval, err := parseSpecDuration("health_check.interval", spec.Interval)
	if err != nil {
		return nil, err
	}
	timeout, err := parseSpecDuration("health_check.timeout", spec.Timeout)
	if err != nil {
		return nil, err
	}
	startPeriod, err := parseSpecDuration("health_check.start_period", spec.StartPeriod)
	if err != nil {
		return nil, err
	}
	if spec.Retries < 0 {
		return nil, newSpecError("health_check.retries", "the retry count cannot be negative")
	}

	return &dockercontainer.HealthConfig{
		Test:        test,
		Interval:    interval,
		Timeout:     timeout,
		StartPeriod: startPeriod,
		Retries:     spec.Retries,
	}, nil
}

// parseSpecDuration accepts an empty string as "leave the engine's default".
func parseSpecDuration(field, value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, newSpecError(field, "%q is not a duration (try 30s or 1m)", value)
	}
	if d < 0 {
		return 0, newSpecError(field, "a duration cannot be negative")
	}
	return d, nil
}

// buildNetworking attaches the container to its network with any aliases and
// static addresses the operator asked for.
func buildNetworking(spec NetworkSpec) *dockernetwork.NetworkingConfig {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil
	}
	if len(spec.Aliases) == 0 && spec.IPv4Address == "" &&
		spec.IPv6Address == "" && spec.MacAddress == "" {
		// NetworkMode alone is enough; an endpoint config with nothing in it
		// makes the engine reject host and none networking.
		return nil
	}

	endpoint := &dockernetwork.EndpointSettings{
		Aliases:    spec.Aliases,
		MacAddress: spec.MacAddress,
	}
	if spec.IPv4Address != "" || spec.IPv6Address != "" {
		endpoint.IPAMConfig = &dockernetwork.EndpointIPAMConfig{
			IPv4Address: spec.IPv4Address,
			IPv6Address: spec.IPv6Address,
		}
	}

	return &dockernetwork.NetworkingConfig{
		EndpointsConfig: map[string]*dockernetwork.EndpointSettings{name: endpoint},
	}
}

// BindSources lists the host paths a spec would mount, so the service layer
// can check them against the configured whitelist without knowing how mounts
// are represented.
func BindSources(spec ContainerSpec) []string {
	var out []string
	for _, m := range spec.Mounts {
		if strings.EqualFold(strings.TrimSpace(m.Type), MountTypeBind) {
			if source := strings.TrimSpace(m.Source); source != "" {
				out = append(out, source)
			}
		}
	}
	return out
}
