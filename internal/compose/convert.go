package compose

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// Iskele's own labels, written onto everything a stack creates.
//
// They are what makes a stack a thing rather than a naming convention: `down`
// finds its containers by label, discovery recognizes containers started by
// `docker compose` elsewhere, and a container that outlives its stack can still
// say where it came from.
const (
	LabelStack   = "com.iskele.stack"
	LabelService = "com.iskele.service"
	// LabelReplica numbers a scaled service's containers from 1.
	LabelReplica = "com.iskele.replica"
	// LabelManaged marks the containers Iskele may remove on `down`.
	LabelManaged = "com.iskele.managed"
)

// Compose's own labels, which `docker compose` writes and reads.
//
// They are set as well as Iskele's: a stack deployed here stays legible to the
// CLI, so an operator is never locked into this panel to clean up after it.
const (
	LabelComposeProject = "com.docker.compose.project"
	LabelComposeService = "com.docker.compose.service"
	LabelComposeNumber  = "com.docker.compose.container-number"
)

// Warning is a compose field Iskele read but will not act on.
//
// Silence would be worse than a refusal: an operator whose `secrets:` were
// quietly dropped finds out when the application fails to start, and blames the
// application.
type Warning struct {
	// Service is empty for a project-level warning.
	Service string `json:"service,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Conversion is a project turned into things the engine can create.
type Conversion struct {
	// Services are the containers to create, already in dependency order.
	Services []ServicePlan `json:"services"`
	// Networks and Volumes are the project-level resources the services need.
	Networks []NetworkPlan `json:"networks"`
	Volumes  []VolumePlan  `json:"volumes"`
	Warnings []Warning     `json:"warnings"`
}

// ServicePlan is one compose service, ready to create.
type ServicePlan struct {
	Name string `json:"name"`
	// Replicas is how many containers this service runs.
	Replicas int `json:"replicas"`
	// Spec is the container definition, without the per-replica name.
	Spec docker.ContainerSpec `json:"spec"`
	// Networks lists every network this service joins, in declaration order.
	// Spec.Network holds the first; the rest are connected after creation,
	// because a container is created on one network and joined to the others.
	Networks []ServiceNetwork `json:"networks"`
	// DependsOn names the services that must be up first.
	DependsOn []string `json:"depends_on,omitempty"`
	// Build, when set, is the image this service builds rather than pulls.
	Build *BuildPlan `json:"build,omitempty"`
}

// ServiceNetwork is one network attachment.
type ServiceNetwork struct {
	// Name is the engine-level name, already namespaced for the stack.
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	IPv4Address string   `json:"ipv4_address,omitempty"`
	IPv6Address string   `json:"ipv6_address,omitempty"`
}

// NetworkPlan is a network the stack owns.
type NetworkPlan struct {
	// Name is what the engine will call it; Key is the name inside the file.
	Name string `json:"name"`
	Key  string `json:"key"`
	// External networks are not created and never removed: they belong to
	// somebody else.
	External bool                        `json:"external"`
	Options  docker.CreateNetworkOptions `json:"-"`
}

// VolumePlan is a volume the stack owns.
type VolumePlan struct {
	Name     string                     `json:"name"`
	Key      string                     `json:"key"`
	External bool                       `json:"external"`
	Options  docker.CreateVolumeOptions `json:"-"`
}

// BuildPlan is a service that builds its image instead of pulling it.
type BuildPlan struct {
	// Context is an absolute host path, checked against the whitelist before
	// anything is read.
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	Target     string            `json:"target,omitempty"`
	// Image is the tag the build produces, which the container then uses.
	Image string `json:"image"`
}

// Convert turns a parsed project into engine-level plans.
//
// The order matches deployment: dependencies first, so a caller can walk the
// slice and create as it goes.
func Convert(project *types.Project, order []string) (Conversion, error) {
	out := Conversion{
		Services: make([]ServicePlan, 0, len(project.Services)),
		Warnings: []Warning{},
	}

	out.Networks = convertNetworks(project, &out.Warnings)
	out.Volumes = convertVolumes(project, &out.Warnings)

	networkNames := make(map[string]string, len(out.Networks))
	for _, network := range out.Networks {
		networkNames[network.Key] = network.Name
	}
	volumeNames := make(map[string]string, len(out.Volumes))
	for _, vol := range out.Volumes {
		volumeNames[vol.Key] = vol.Name
	}

	if len(order) == 0 {
		order = SortedServiceNames(project)
	}

	for _, name := range order {
		service, ok := project.Services[name]
		if !ok {
			continue
		}

		plan, err := convertService(project, service, networkNames, volumeNames, &out.Warnings)
		if err != nil {
			return Conversion{}, err
		}
		out.Services = append(out.Services, plan)
	}

	return out, nil
}

// convertService turns one service into a container definition.
func convertService(project *types.Project, service types.ServiceConfig,
	networkNames, volumeNames map[string]string, warnings *[]Warning,
) (ServicePlan, error) {
	plan := ServicePlan{
		Name:      service.Name,
		Replicas:  replicasOf(service),
		DependsOn: dependenciesOf(service),
	}

	spec := docker.ContainerSpec{
		Image:      service.Image,
		Command:    []string(service.Command),
		Entrypoint: []string(service.Entrypoint),
		WorkingDir: service.WorkingDir,
		User:       service.User,
		Hostname:   service.Hostname,
		DomainName: service.DomainName,
		TTY:        service.Tty,
		OpenStdin:  service.StdinOpen,
		Init:       service.Init != nil && *service.Init,
		Start:      true,
		Env:        convertEnvironment(service.Environment),
		Labels:     convertLabels(project.Name, service),
		Ports:      convertPorts(service, warnings),
		RestartPolicy: convertRestart(service.Restart,
			service.Deploy, service.Name, warnings),
		Resources:   convertResources(service),
		HealthCheck: convertHealthCheck(service.HealthCheck),
		Logging:     convertLogging(service),
		Security:    convertSecurity(service),
	}

	mounts, err := convertVolumesFor(service, volumeNames)
	if err != nil {
		return ServicePlan{}, err
	}
	spec.Mounts = append(mounts, convertTmpfs(service)...)

	// container_name pins the name; the loader has already refused it in
	// combination with replicas, since one name cannot cover two containers.
	spec.Name = service.ContainerName

	plan.Networks = convertServiceNetworks(service, networkNames)
	spec.Network = firstNetwork(service, plan.Networks)
	spec.Network.ExtraHosts = service.ExtraHosts.AsList(":")
	spec.Network.DNS = []string(service.DNS)
	spec.Network.DNSSearch = []string(service.DNSSearch)
	spec.Network.DNSOptions = service.DNSOpts
	spec.Network.MacAddress = service.MacAddress

	if service.Build != nil {
		plan.Build = convertBuild(project.Name, service)
		if spec.Image == "" {
			spec.Image = plan.Build.Image
		}
		spec.PullPolicy = docker.PullNever
	}
	if spec.Image == "" {
		return ServicePlan{}, &Error{Message: fmt.Sprintf(
			"service %q has neither an image nor a build context", service.Name)}
	}
	if service.PullPolicy != "" && plan.Build == nil {
		spec.PullPolicy = convertPullPolicy(service.PullPolicy, service.Name, warnings)
	}

	collectServiceWarnings(service, warnings)
	plan.Spec = spec

	return plan, nil
}

// replicasOf reads how many containers a service runs.
//
// `deploy.replicas` and the older `scale` mean the same thing here; compose
// treats deploy as authoritative when both are set.
func replicasOf(service types.ServiceConfig) int {
	if service.Deploy != nil && service.Deploy.Replicas != nil && *service.Deploy.Replicas > 0 {
		return int(*service.Deploy.Replicas)
	}
	if service.Scale != nil && *service.Scale > 0 {
		return *service.Scale
	}
	return 1
}

// dependenciesOf lists the services this one waits for.
func dependenciesOf(service types.ServiceConfig) []string {
	if len(service.DependsOn) == 0 {
		return nil
	}

	names := make([]string, 0, len(service.DependsOn))
	for name := range service.DependsOn {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// convertEnvironment flattens compose's environment mapping.
//
// A nil value means "pass this through from the host", which is a compose CLI
// feature the daemon deliberately does not have: iskeled's environment is not
// the operator's shell, and inheriting from it would hand the container
// whatever the unit file happens to set. Such entries are dropped.
func convertEnvironment(env types.MappingWithEquals) []docker.EnvVar {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for key, value := range env {
		if value == nil {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]docker.EnvVar, 0, len(keys))
	for _, key := range keys {
		out = append(out, docker.EnvVar{Key: key, Value: *env[key]})
	}
	return out
}

// convertLabels merges the service's own labels with the ones that make it
// part of a stack.
func convertLabels(project string, service types.ServiceConfig) map[string]string {
	labels := make(map[string]string, len(service.Labels)+6)
	for key, value := range service.Labels {
		labels[key] = value
	}

	labels[LabelStack] = project
	labels[LabelService] = service.Name
	labels[LabelManaged] = "true"
	labels[LabelComposeProject] = project
	labels[LabelComposeService] = service.Name

	return labels
}

// convertPorts turns published ports into mappings.
func convertPorts(service types.ServiceConfig, warnings *[]Warning) []docker.PortMapping {
	if len(service.Ports) == 0 {
		return nil
	}

	out := make([]docker.PortMapping, 0, len(service.Ports))
	for _, port := range service.Ports {
		if port.Target == 0 {
			continue
		}

		mapping := docker.PortMapping{
			HostIP:        port.HostIP,
			HostPort:      port.Published,
			ContainerPort: int(port.Target),
			Protocol:      port.Protocol,
		}

		// A published range ("8000-8010:80") arrives as a single entry whose
		// Published is the range. The engine takes ranges, but a mapping that
		// silently publishes eleven ports is worth saying out loud.
		if strings.Contains(port.Published, "-") {
			*warnings = append(*warnings, Warning{
				Service: service.Name,
				Field:   "ports",
				Message: fmt.Sprintf("%q publishes a range; every port in it is exposed on the host", port.Published),
			})
		}

		out = append(out, mapping)
	}
	return out
}

// convertRestart maps compose's restart values onto the engine's.
func convertRestart(restart string, deploy *types.DeployConfig,
	service string, warnings *[]Warning,
) docker.RestartPolicy {
	if restart == "" && deploy != nil && deploy.RestartPolicy != nil {
		restart = deploy.RestartPolicy.Condition
	}

	name, retries := restart, 0
	if strings.HasPrefix(restart, "on-failure:") {
		name = "on-failure"
		if parsed, err := strconv.Atoi(strings.TrimPrefix(restart, "on-failure:")); err == nil {
			retries = parsed
		}
	}

	switch name {
	case "", "no", "none":
		return docker.RestartPolicy{Name: "no"}
	case "always", "any":
		return docker.RestartPolicy{Name: "always"}
	case "unless-stopped":
		return docker.RestartPolicy{Name: "unless-stopped"}
	case "on-failure":
		return docker.RestartPolicy{Name: "on-failure", MaxRetries: retries}
	default:
		*warnings = append(*warnings, Warning{
			Service: service,
			Field:   "restart",
			Message: fmt.Sprintf("%q is not a restart policy the engine knows; the container will not be restarted", restart),
		})
		return docker.RestartPolicy{Name: "no"}
	}
}

// convertResources reads the cgroup limits, from either the modern
// `deploy.resources` or the older top-level fields.
func convertResources(service types.ServiceConfig) docker.Resources {
	resources := docker.Resources{
		CPUShares:  service.CPUShares,
		CPUSetCPUs: service.CPUSet,
		PidsLimit:  service.PidsLimit,
		ShmSize:    int64(service.ShmSize),
		Memory:     int64(service.MemLimit),

		MemoryReservation: int64(service.MemReservation),
		MemorySwap:        int64(service.MemSwapLimit),
	}
	if service.CPUS > 0 {
		resources.CPUs = float64(service.CPUS)
	}

	if service.Deploy == nil {
		return resources
	}

	if limits := service.Deploy.Resources.Limits; limits != nil {
		if limits.NanoCPUs > 0 {
			resources.CPUs = float64(limits.NanoCPUs)
		}
		if limits.MemoryBytes > 0 {
			resources.Memory = int64(limits.MemoryBytes)
		}
		if limits.Pids > 0 {
			resources.PidsLimit = limits.Pids
		}
	}
	if reservations := service.Deploy.Resources.Reservations; reservations != nil {
		if reservations.MemoryBytes > 0 {
			resources.MemoryReservation = int64(reservations.MemoryBytes)
		}
	}

	return resources
}

// convertHealthCheck maps the probe.
func convertHealthCheck(check *types.HealthCheckConfig) *docker.HealthSpec {
	if check == nil {
		return nil
	}
	if check.Disable {
		return &docker.HealthSpec{Disable: true}
	}

	spec := &docker.HealthSpec{Test: []string(check.Test)}
	if check.Interval != nil {
		spec.Interval = time.Duration(*check.Interval).String()
	}
	if check.Timeout != nil {
		spec.Timeout = time.Duration(*check.Timeout).String()
	}
	if check.StartPeriod != nil {
		spec.StartPeriod = time.Duration(*check.StartPeriod).String()
	}
	if check.Retries != nil {
		// The engine stores retries as an int32. A compose file asking for more
		// is asking for something no health check would ever reach, so it is
		// clamped rather than allowed to wrap into a negative count.
		spec.Retries = math.MaxInt32
		if *check.Retries < math.MaxInt32 {
			spec.Retries = int(int32(*check.Retries))
		}
	}
	return spec
}

// convertLogging reads the log driver, from either form compose accepts.
func convertLogging(service types.ServiceConfig) docker.LoggingSpec {
	if service.Logging != nil {
		return docker.LoggingSpec{Driver: service.Logging.Driver, Options: service.Logging.Options}
	}
	return docker.LoggingSpec{Driver: service.LogDriver, Options: service.LogOpt}
}

// convertSecurity collects the options that widen what a container may do.
//
// They are carried through as written and refused later, by the same
// permission gate the create wizard goes through: a compose file must not be a
// way around it.
func convertSecurity(service types.ServiceConfig) docker.SecuritySpec {
	security := docker.SecuritySpec{
		Privileged:     service.Privileged,
		CapAdd:         service.CapAdd,
		CapDrop:        service.CapDrop,
		SecurityOpt:    service.SecurityOpt,
		ReadOnlyRootFS: service.ReadOnly,
		Sysctls:        service.Sysctls,
	}

	for _, device := range service.Devices {
		security.Devices = append(security.Devices, device.Source+":"+device.Target+":"+device.Permissions)
	}

	return security
}

// convertVolumesFor turns a service's volumes into mounts.
func convertVolumesFor(service types.ServiceConfig, volumeNames map[string]string) ([]docker.MountSpec, error) {
	if len(service.Volumes) == 0 {
		return nil, nil
	}

	out := make([]docker.MountSpec, 0, len(service.Volumes))
	for _, volume := range service.Volumes {
		mount := docker.MountSpec{
			Destination: volume.Target,
			ReadOnly:    volume.ReadOnly,
		}

		switch volume.Type {
		case string(types.VolumeTypeBind):
			mount.Type = docker.MountTypeBind
			mount.Source = volume.Source
			if volume.Bind != nil {
				mount.BindPropagation = volume.Bind.Propagation
				mount.CreateHostPath = bool(volume.Bind.CreateHostPath)
			}
		case string(types.VolumeTypeVolume):
			mount.Type = docker.MountTypeVolume
			mount.Source = volume.Source
			if named, ok := volumeNames[volume.Source]; ok {
				mount.Source = named
			}
		case string(types.VolumeTypeTmpfs):
			mount.Type = docker.MountTypeTmpfs
			if volume.Tmpfs != nil {
				mount.TmpfsSize = int64(volume.Tmpfs.Size)
			}
		default:
			return nil, &Error{Message: fmt.Sprintf(
				"service %q mounts %q, which Iskele does not support (%s)",
				service.Name, volume.Target, volume.Type)}
		}

		out = append(out, mount)
	}
	return out, nil
}

// convertTmpfs turns the shorthand `tmpfs:` list into mounts.
func convertTmpfs(service types.ServiceConfig) []docker.MountSpec {
	if len(service.Tmpfs) == 0 {
		return nil
	}

	out := make([]docker.MountSpec, 0, len(service.Tmpfs))
	for _, target := range service.Tmpfs {
		out = append(out, docker.MountSpec{Type: docker.MountTypeTmpfs, Destination: target})
	}
	return out
}

// convertServiceNetworks lists the networks a service joins.
func convertServiceNetworks(service types.ServiceConfig, networkNames map[string]string) []ServiceNetwork {
	if service.NetworkMode != "" {
		// host, none, container:<id> and service:<name> are not networks the
		// stack owns; they go straight through as the container's mode.
		return nil
	}

	keys := make([]string, 0, len(service.Networks))
	for key := range service.Networks {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]ServiceNetwork, 0, len(keys))
	for _, key := range keys {
		attachment := ServiceNetwork{Name: key}
		if named, ok := networkNames[key]; ok {
			attachment.Name = named
		}
		if config := service.Networks[key]; config != nil {
			attachment.Aliases = config.Aliases
			attachment.IPv4Address = config.Ipv4Address
			attachment.IPv6Address = config.Ipv6Address
		}
		// The service is reachable by its compose name on every network it
		// joins, which is what makes `db:3306` work from another service.
		attachment.Aliases = appendUnique(attachment.Aliases, service.Name)
		out = append(out, attachment)
	}
	return out
}

// firstNetwork picks the network the container is created on.
//
// A container is created attached to exactly one network; the rest are
// connected afterwards. Compose picks the same way — sorted by name — so a
// static IP on the first one lands where the file says.
func firstNetwork(service types.ServiceConfig, attachments []ServiceNetwork) docker.NetworkSpec {
	if service.NetworkMode != "" {
		return docker.NetworkSpec{Name: service.NetworkMode}
	}
	if len(attachments) == 0 {
		return docker.NetworkSpec{}
	}

	first := attachments[0]
	return docker.NetworkSpec{
		Name:        first.Name,
		Aliases:     first.Aliases,
		IPv4Address: first.IPv4Address,
		IPv6Address: first.IPv6Address,
	}
}

// convertNetworks reads the project's networks.
func convertNetworks(project *types.Project, warnings *[]Warning) []NetworkPlan {
	keys := make([]string, 0, len(project.Networks))
	for key := range project.Networks {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]NetworkPlan, 0, len(keys))
	for _, key := range keys {
		network := project.Networks[key]

		plan := NetworkPlan{
			Key:      key,
			Name:     networkName(project.Name, key, network),
			External: bool(network.External),
		}
		plan.Options = docker.CreateNetworkOptions{
			Name:       plan.Name,
			Driver:     network.Driver,
			Internal:   network.Internal,
			Attachable: network.Attachable,
			EnableIPv6: network.EnableIPv6 != nil && *network.EnableIPv6,
			Options:    network.DriverOpts,
			Labels:     resourceLabels(project.Name, network.Labels),
		}

		for _, config := range network.Ipam.Config {
			plan.Options.IPAM = append(plan.Options.IPAM, docker.IPAMConfig{
				Subnet:  config.Subnet,
				Gateway: config.Gateway,
				IPRange: config.IPRange,
			})
		}

		if network.Ipam.Driver != "" && network.Ipam.Driver != "default" {
			*warnings = append(*warnings, Warning{
				Field:   "networks." + key + ".ipam.driver",
				Message: "a custom IPAM driver is passed through; the engine refuses it when the plugin is missing",
			})
		}

		out = append(out, plan)
	}
	return out
}

// convertVolumes reads the project's volumes.
func convertVolumes(project *types.Project, warnings *[]Warning) []VolumePlan {
	keys := make([]string, 0, len(project.Volumes))
	for key := range project.Volumes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]VolumePlan, 0, len(keys))
	for _, key := range keys {
		vol := project.Volumes[key]

		plan := VolumePlan{
			Key:      key,
			Name:     volumeName(project.Name, key, vol),
			External: bool(vol.External),
		}
		plan.Options = docker.CreateVolumeOptions{
			Name:       plan.Name,
			Driver:     vol.Driver,
			DriverOpts: vol.DriverOpts,
			Labels:     resourceLabels(project.Name, vol.Labels),
		}
		out = append(out, plan)
	}

	if len(project.Configs) > 0 {
		*warnings = append(*warnings, Warning{
			Field:   "configs",
			Message: "configs are a swarm feature; Iskele does not create them, and services referencing one start without it",
		})
	}
	if len(project.Secrets) > 0 {
		*warnings = append(*warnings, Warning{
			Field:   "secrets",
			Message: "secrets are a swarm feature; use environment variables or a bind mount instead",
		})
	}

	return out
}

// convertBuild describes the image a service builds.
func convertBuild(project string, service types.ServiceConfig) *BuildPlan {
	build := service.Build

	plan := &BuildPlan{
		Context:    build.Context,
		Dockerfile: build.Dockerfile,
		Target:     build.Target,
		Image:      service.Image,
	}
	if plan.Image == "" {
		plan.Image = fmt.Sprintf("%s-%s:latest", project, service.Name)
	}

	if len(build.Args) > 0 {
		plan.Args = make(map[string]string, len(build.Args))
		for key, value := range build.Args {
			if value == nil {
				continue
			}
			plan.Args[key] = *value
		}
	}

	return plan
}

// convertPullPolicy maps compose's pull_policy onto the engine's.
func convertPullPolicy(policy, service string, warnings *[]Warning) string {
	switch policy {
	case "always":
		return docker.PullAlways
	case "never":
		return docker.PullNever
	case "missing", "if_not_present":
		return docker.PullMissing
	case "build":
		*warnings = append(*warnings, Warning{
			Service: service,
			Field:   "pull_policy",
			Message: "pull_policy: build only means something with a build section; the image is pulled if missing",
		})
		return docker.PullMissing
	default:
		return docker.PullMissing
	}
}

// collectServiceWarnings reports the fields Iskele reads but cannot honor.
func collectServiceWarnings(service types.ServiceConfig, warnings *[]Warning) {
	add := func(field, message string) {
		*warnings = append(*warnings, Warning{Service: service.Name, Field: field, Message: message})
	}

	if len(service.Configs) > 0 {
		add("configs", "swarm-only; the container starts without the config file")
	}
	if len(service.Secrets) > 0 {
		add("secrets", "swarm-only; the container starts without the secret file")
	}
	if len(service.Links) > 0 {
		add("links", "links are legacy; services on the same network already reach each other by name")
	}
	if len(service.VolumesFrom) > 0 {
		add("volumes_from", "not supported; mount the same volume in both services instead")
	}
	if service.Develop != nil {
		add("develop", "watch mode is a compose CLI feature and has no effect here")
	}
	if service.Deploy != nil {
		if service.Deploy.Mode == "global" {
			add("deploy.mode", "global mode is swarm-only; the service runs once")
		}
		if len(service.Deploy.Placement.Constraints) > 0 {
			add("deploy.placement", "placement constraints are swarm-only and are ignored")
		}
		if service.Deploy.UpdateConfig != nil {
			add("deploy.update_config", "rolling updates are swarm-only and are ignored")
		}
	}
	if service.Extends != nil {
		// The loader has already resolved it; saying so avoids an operator
		// wondering whether it was honored.
		add("extends", "resolved at parse time; the merged result is what deploys")
	}
	if len(service.Profiles) > 0 {
		add("profiles", "every service is deployed; Iskele does not select profiles")
	}
	if len(service.ExternalLinks) > 0 {
		add("external_links", "not supported; attach both containers to the same network instead")
	}
	if service.CredentialSpec != nil {
		add("credential_spec", "Windows-only and ignored")
	}
	if len(service.Models) > 0 {
		add("models", "model runners are not supported")
	}
	if service.Provider != nil {
		add("provider", "provider services are not supported")
	}
	if len(service.PreStart) > 0 || len(service.PostStart) > 0 || len(service.PreStop) > 0 {
		add("lifecycle hooks", "pre_start, post_start and pre_stop are compose CLI features and do not run here")
	}
}

// networkName is what the engine will call a project network.
func networkName(project, key string, network types.NetworkConfig) string {
	if network.Name != "" {
		return network.Name
	}
	if network.External {
		return key
	}
	return project + "_" + key
}

// volumeName is what the engine will call a project volume.
func volumeName(project, key string, vol types.VolumeConfig) string {
	if vol.Name != "" {
		return vol.Name
	}
	if vol.External {
		return key
	}
	return project + "_" + key
}

// resourceLabels marks a network or volume as belonging to a stack.
func resourceLabels(project string, own types.Labels) map[string]string {
	labels := make(map[string]string, len(own)+3)
	for key, value := range own {
		labels[key] = value
	}
	labels[LabelStack] = project
	labels[LabelManaged] = "true"
	labels[LabelComposeProject] = project
	return labels
}

// appendUnique adds a value only if it is not already there.
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// ContainerName is what one replica of a service is called.
//
// It matches what `docker compose` produces, so a stack deployed here looks
// the same in `docker ps` as one deployed with the CLI.
func ContainerName(project, service string, replica int) string {
	return fmt.Sprintf("%s-%s-%d", project, service, replica)
}
