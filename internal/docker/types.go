// Package docker is the single access point to the Docker Engine API.
//
// Nothing outside this package imports the Docker SDK: every other layer works
// with the plain types declared here, which keeps the SDK's frequent breaking
// changes contained and makes the whole engine mockable behind [Client].
package docker

import (
	"encoding/json"
	"time"
)

// Container is the list-view projection of a container.
type Container struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	ImageID string            `json:"image_id"`
	Command string            `json:"command"`
	Created time.Time         `json:"created"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Health  string            `json:"health,omitempty"`
	Ports   []Port            `json:"ports"`
	Labels  map[string]string `json:"labels"`
	// Networks lists the networks the container is attached to.
	Networks []string `json:"networks"`
	// Mounts lists mount destinations inside the container.
	Mounts []string `json:"mounts"`
	// SizeRW and SizeRootFS are only populated when the caller asks for sizes;
	// computing them is expensive, so they are -1 when not requested.
	SizeRW     int64 `json:"size_rw"`
	SizeRootFS int64 `json:"size_root_fs"`
}

// Port is a published or exposed container port.
type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"private_port"`
	PublicPort  uint16 `json:"public_port,omitempty"`
	Type        string `json:"type"`
}

// ContainerDetail is the inspect-view projection of a container.
type ContainerDetail struct {
	Container

	RestartCount int    `json:"restart_count"`
	Platform     string `json:"platform,omitempty"`
	Driver       string `json:"driver,omitempty"`
	LogPath      string `json:"log_path,omitempty"`

	Path string   `json:"path,omitempty"`
	Args []string `json:"args,omitempty"`

	Entrypoint []string          `json:"entrypoint,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	Env        []string          `json:"env,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
	User       string            `json:"user,omitempty"`
	Hostname   string            `json:"hostname,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`

	RestartPolicy string `json:"restart_policy,omitempty"`
	Privileged    bool   `json:"privileged"`

	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	ExitCode   int       `json:"exit_code"`
	OOMKilled  bool      `json:"oom_killed"`
	Pid        int       `json:"pid"`
	// Error carries the engine's own explanation of why the container died.
	Error string `json:"error,omitempty"`

	MountPoints  []MountPoint        `json:"mount_points"`
	NetworkList  []NetworkAttachment `json:"network_list"`
	HealthCheck  *ContainerHealth    `json:"health_check,omitempty"`
	PortBindings map[string][]Port   `json:"port_bindings,omitempty"`
}

// MountPoint describes one mount attached to a container.
type MountPoint struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Driver      string `json:"driver,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RW          bool   `json:"rw"`
	Propagation string `json:"propagation,omitempty"`
}

// NetworkAttachment describes one network a container is connected to.
type NetworkAttachment struct {
	Name        string   `json:"name"`
	NetworkID   string   `json:"network_id"`
	EndpointID  string   `json:"endpoint_id,omitempty"`
	IPAddress   string   `json:"ip_address,omitempty"`
	IPPrefixLen int      `json:"ip_prefix_len,omitempty"`
	Gateway     string   `json:"gateway,omitempty"`
	MacAddress  string   `json:"mac_address,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

// ContainerHealth is the health-check state reported by the engine.
type ContainerHealth struct {
	Status        string `json:"status"`
	FailingStreak int    `json:"failing_streak"`
	// LastOutput is the output of the most recent probe, useful for diagnosing
	// an unhealthy container without opening the raw inspect payload.
	LastOutput string `json:"last_output,omitempty"`
}

// Image is the list-view projection of an image.
type Image struct {
	ID          string            `json:"id"`
	ParentID    string            `json:"parent_id,omitempty"`
	RepoTags    []string          `json:"repo_tags"`
	RepoDigests []string          `json:"repo_digests"`
	Created     time.Time         `json:"created"`
	Size        int64             `json:"size"`
	SharedSize  int64             `json:"shared_size"`
	Labels      map[string]string `json:"labels"`
	// Containers is the number of containers using this image, or -1 when the
	// engine did not compute it.
	Containers int64 `json:"containers"`
	// Dangling reports an image with no repository tag of its own.
	Dangling bool `json:"dangling"`
}

// Volume is the list-view projection of a volume.
type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Scope      string            `json:"scope"`
	CreatedAt  time.Time         `json:"created_at,omitempty"`
	Labels     map[string]string `json:"labels"`
	Options    map[string]string `json:"options"`
	// Size is -1 unless the engine reported usage data.
	Size int64 `json:"size"`
	// RefCount is -1 unless the engine reported usage data.
	RefCount int64 `json:"ref_count"`
}

// Network is the list-view projection of a network.
type Network struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Created    time.Time         `json:"created"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	Ingress    bool              `json:"ingress"`
	EnableIPv6 bool              `json:"enable_ipv6"`
	IPAM       []IPAMConfig      `json:"ipam"`
	Labels     map[string]string `json:"labels"`
	// ContainerCount is the number of attached containers. The engine only
	// fills this in on inspect, so it is 0 for plain list results.
	ContainerCount int `json:"container_count"`
}

// IPAMConfig is one subnet definition of a network.
type IPAMConfig struct {
	Subnet     string `json:"subnet,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	IPRange    string `json:"ip_range,omitempty"`
	AuxAddress string `json:"aux_address,omitempty"`
}

// SystemInfo summarizes the engine and the host it runs on.
type SystemInfo struct {
	ServerVersion   string `json:"server_version"`
	APIVersion      string `json:"api_version"`
	Name            string `json:"name"`
	OSType          string `json:"os_type"`
	OperatingSystem string `json:"operating_system"`
	Architecture    string `json:"architecture"`
	KernelVersion   string `json:"kernel_version"`
	NCPU            int    `json:"ncpu"`
	MemTotal        int64  `json:"mem_total"`
	DockerRootDir   string `json:"docker_root_dir"`
	StorageDriver   string `json:"storage_driver"`
	LoggingDriver   string `json:"logging_driver"`
	CgroupDriver    string `json:"cgroup_driver"`

	Containers        int `json:"containers"`
	ContainersRunning int `json:"containers_running"`
	ContainersPaused  int `json:"containers_paused"`
	ContainersStopped int `json:"containers_stopped"`
	Images            int `json:"images"`

	Warnings []string `json:"warnings,omitempty"`
}

// DiskUsage is the `docker system df` summary.
type DiskUsage struct {
	LayersSize int64          `json:"layers_size"`
	Images     DiskUsageEntry `json:"images"`
	Containers DiskUsageEntry `json:"containers"`
	Volumes    DiskUsageEntry `json:"volumes"`
	BuildCache DiskUsageEntry `json:"build_cache"`
}

// DiskUsageEntry is the per-resource-type part of a disk usage report.
type DiskUsageEntry struct {
	Count       int   `json:"count"`
	Size        int64 `json:"size"`
	Reclaimable int64 `json:"reclaimable"`
}

// ListContainersOptions filters a container listing.
type ListContainersOptions struct {
	// All includes stopped containers; otherwise only running ones are listed.
	All bool
	// Size requests SizeRW / SizeRootFS, which is expensive on large graphs.
	Size bool
	// Label filters on exact "key=value" label matches.
	Label []string
	// Status filters on container state (running, exited, ...).
	Status []string
	// Name filters on a substring of the container name.
	Name string
}

// ListImagesOptions filters an image listing.
type ListImagesOptions struct {
	// All includes intermediate layers.
	All bool
	// Dangling, when set, restricts the result to untagged images.
	Dangling *bool
	// Label filters on exact "key=value" label matches.
	Label []string
}

// RemoveContainerOptions controls container deletion.
type RemoveContainerOptions struct {
	Force         bool
	RemoveVolumes bool
	RemoveLinks   bool
}

// StopOptions carries the grace period for stop and restart. A nil Timeout
// leaves the engine's own default (10s) in place.
type StopOptions struct {
	Timeout *int
}

// RawInspect is the engine's unmodified inspect payload, surfaced by the UI's
// Inspect tab so nothing the engine reports is hidden from the operator.
type RawInspect = json.RawMessage
