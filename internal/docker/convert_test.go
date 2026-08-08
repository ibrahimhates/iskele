package docker

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
)

func TestToContainerProjectsSummary(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	s := container.Summary{
		ID:      "abc123",
		Names:   []string{"/web", "/web-alias"},
		Image:   "nginx:1.27",
		ImageID: "sha256:deadbeef",
		Command: "nginx -g 'daemon off;'",
		Created: created.Unix(),
		State:   "running",
		Status:  "Up 2 hours (healthy)",
		Ports: []container.Port{
			{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		},
		Labels: map[string]string{"app": "web"},
		Mounts: []container.MountPoint{{Destination: "/data"}},
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"proxy":  {},
				"bridge": {},
			},
		},
		SizeRw:     1234,
		SizeRootFs: 5678,
	}

	got := toContainer(s, false)

	if got.ID != "abc123" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Name != "web" {
		t.Errorf("Name = %q, want the leading slash stripped", got.Name)
	}
	if len(got.Names) != 2 || got.Names[1] != "web-alias" {
		t.Errorf("Names = %v", got.Names)
	}
	if !got.Created.Equal(created) {
		t.Errorf("Created = %v, want %v", got.Created, created)
	}
	if got.Health != "healthy" {
		t.Errorf("Health = %q, want it parsed out of the status line", got.Health)
	}
	if len(got.Ports) != 1 || got.Ports[0].PublicPort != 8080 {
		t.Errorf("Ports = %+v", got.Ports)
	}
	// Networks must be sorted so the UI does not reorder rows between polls.
	if len(got.Networks) != 2 || got.Networks[0] != "bridge" || got.Networks[1] != "proxy" {
		t.Errorf("Networks = %v, want them sorted", got.Networks)
	}
	if len(got.Mounts) != 1 || got.Mounts[0] != "/data" {
		t.Errorf("Mounts = %v", got.Mounts)
	}
	// Sizes are expensive, so they are only reported when requested.
	if got.SizeRW != -1 || got.SizeRootFS != -1 {
		t.Errorf("sizes = %d/%d, want -1 when not requested", got.SizeRW, got.SizeRootFS)
	}

	withSize := toContainer(s, true)
	if withSize.SizeRW != 1234 || withSize.SizeRootFS != 5678 {
		t.Errorf("sizes = %d/%d, want the reported values", withSize.SizeRW, withSize.SizeRootFS)
	}
}

func TestToContainerHandlesMissingOptionalFields(t *testing.T) {
	got := toContainer(container.Summary{ID: "x", Created: 0}, false)

	if got.Labels == nil {
		t.Error("Labels = nil, want an empty map so JSON carries {}")
	}
	if got.Networks == nil || got.Ports == nil || got.Mounts == nil {
		t.Error("slice fields must be empty, not nil, so JSON carries []")
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty when the engine reported no names", got.Name)
	}
}

func TestHealthFromStatus(t *testing.T) {
	tests := map[string]string{
		"Up 2 hours (healthy)":            "healthy",
		"Up 5 seconds (health: starting)": "starting",
		"Up 3 days (unhealthy)":           "unhealthy",
		"Up 3 days":                       "",
		"Exited (0) 5 minutes ago":        "",
		"":                                "",
	}
	for status, want := range tests {
		if got := healthFromStatus(status); got != want {
			t.Errorf("healthFromStatus(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestToContainerDetailProjectsInspect(t *testing.T) {
	started := "2026-03-01T12:00:00.5Z"

	r := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:           "abc123",
			Name:         "/web",
			Created:      "2026-02-01T09:00:00Z",
			RestartCount: 3,
			Platform:     "linux",
			Driver:       "overlay2",
			LogPath:      "/var/lib/docker/containers/abc/abc-json.log",
			Path:         "nginx",
			Args:         []string{"-g", "daemon off;"},
			State: &container.State{
				Status:     "running",
				Pid:        4242,
				ExitCode:   0,
				StartedAt:  started,
				FinishedAt: "0001-01-01T00:00:00Z",
				Health: &container.Health{
					Status:        "healthy",
					FailingStreak: 0,
					Log: []*container.HealthcheckResult{
						{Output: "  ok  \n"},
					},
				},
			},
			HostConfig: &container.HostConfig{
				RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
				Privileged:    true,
			},
		},
		Config: &container.Config{
			Image:      "nginx:1.27",
			Cmd:        []string{"nginx"},
			Env:        []string{"TZ=UTC"},
			WorkingDir: "/",
			User:       "nginx",
			Hostname:   "web",
			Labels:     map[string]string{"app": "web"},
		},
		Mounts: []container.MountPoint{
			{Type: "volume", Name: "web-data", Source: "/var/lib/docker/volumes/web-data/_data",
				Destination: "/usr/share/nginx/html", RW: true},
		},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {NetworkID: "net1", IPAddress: "172.17.0.2", Gateway: "172.17.0.1"},
			},
			// The SDK marks this struct deprecated but still exposes Ports only
			// through it; there is no other way to build the fixture today.
			NetworkSettingsBase: container.NetworkSettingsBase{ //nolint:staticcheck
				Ports: nat.PortMap{
					"80/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "8080"}},
				},
			},
		},
	}

	got := toContainerDetail(r)

	if got.Name != "web" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.RestartCount != 3 {
		t.Errorf("RestartCount = %d", got.RestartCount)
	}
	if got.RestartPolicy != "unless-stopped" {
		t.Errorf("RestartPolicy = %q", got.RestartPolicy)
	}
	if !got.Privileged {
		t.Error("Privileged = false, want it carried through")
	}
	if got.Pid != 4242 {
		t.Errorf("Pid = %d", got.Pid)
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt is zero, want the parsed timestamp")
	}
	// The engine's "never happened" sentinel must become the zero time.
	if !got.FinishedAt.IsZero() {
		t.Errorf("FinishedAt = %v, want zero for the 0001-01-01 sentinel", got.FinishedAt)
	}
	if got.HealthCheck == nil || got.HealthCheck.Status != "healthy" {
		t.Errorf("HealthCheck = %+v", got.HealthCheck)
	}
	if got.HealthCheck != nil && got.HealthCheck.LastOutput != "ok" {
		t.Errorf("LastOutput = %q, want it trimmed", got.HealthCheck.LastOutput)
	}
	if len(got.MountPoints) != 1 || got.MountPoints[0].Destination != "/usr/share/nginx/html" {
		t.Errorf("MountPoints = %+v", got.MountPoints)
	}
	if len(got.Mounts) != 1 || got.Mounts[0] != "/usr/share/nginx/html" {
		t.Errorf("Mounts = %v", got.Mounts)
	}
	if len(got.NetworkList) != 1 || got.NetworkList[0].IPAddress != "172.17.0.2" {
		t.Errorf("NetworkList = %+v", got.NetworkList)
	}
	bindings := got.PortBindings["80/tcp"]
	if len(bindings) != 1 || bindings[0].PublicPort != 8080 || bindings[0].PrivatePort != 80 {
		t.Errorf("PortBindings = %+v", got.PortBindings)
	}
}

func TestToContainerDetailToleratesNilBase(t *testing.T) {
	// A malformed engine response must not panic the daemon.
	if got := toContainerDetail(container.InspectResponse{}); got.ID != "" {
		t.Errorf("ID = %q, want the zero value", got.ID)
	}
}

func TestParseDockerTime(t *testing.T) {
	tests := map[string]bool{
		"":                               false,
		"0001-01-01T00:00:00Z":           false,
		"not a timestamp":                false,
		"2026-03-01T12:00:00.123456789Z": true,
		"2026-03-01T12:00:00Z":           true,
	}
	for in, wantSet := range tests {
		got := parseDockerTime(in)
		if got.IsZero() == wantSet {
			t.Errorf("parseDockerTime(%q).IsZero() = %v, want %v", in, got.IsZero(), !wantSet)
		}
	}
}

func TestToImage(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got := toImage(image.Summary{
		ID:         "sha256:abc",
		ParentID:   "sha256:parent",
		RepoTags:   []string{"nginx:1.27"},
		Created:    created.Unix(),
		Size:       142,
		SharedSize: 12,
		Containers: 2,
	})

	if got.ID != "sha256:abc" || got.ParentID != "sha256:parent" {
		t.Errorf("ids = %q / %q", got.ID, got.ParentID)
	}
	if !got.Created.Equal(created) {
		t.Errorf("Created = %v", got.Created)
	}
	if got.Dangling {
		t.Error("Dangling = true for a tagged image")
	}
	if got.Labels == nil || got.RepoDigests == nil {
		t.Error("map and slice fields must be non-nil for clean JSON")
	}
}

func TestIsDangling(t *testing.T) {
	tests := []struct {
		tags []string
		want bool
	}{
		{nil, true},
		{[]string{}, true},
		{[]string{danglingTag}, true},
		{[]string{"nginx:1.27"}, false},
		{[]string{danglingTag, "nginx:1.27"}, false},
	}
	for _, tt := range tests {
		if got := isDangling(tt.tags); got != tt.want {
			t.Errorf("isDangling(%v) = %v, want %v", tt.tags, got, tt.want)
		}
	}
}

func TestToVolume(t *testing.T) {
	got := toVolume(volume.Volume{
		Name:       "web-data",
		Driver:     "local",
		Mountpoint: "/var/lib/docker/volumes/web-data/_data",
		Scope:      "local",
		CreatedAt:  "2026-01-01T00:00:00Z",
	})

	if got.Name != "web-data" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want the parsed timestamp")
	}
	// Usage is not computed by a plain list, so it must read as unknown.
	if got.Size != -1 || got.RefCount != -1 {
		t.Errorf("Size/RefCount = %d/%d, want -1 for unknown", got.Size, got.RefCount)
	}

	withUsage := toVolume(volume.Volume{
		Name:      "web-data",
		UsageData: &volume.UsageData{Size: 999, RefCount: 2},
	})
	if withUsage.Size != 999 || withUsage.RefCount != 2 {
		t.Errorf("Size/RefCount = %d/%d, want the reported usage", withUsage.Size, withUsage.RefCount)
	}
}

func TestToNetwork(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got := toNetwork(network.Summary{
		ID:      "net1",
		Name:    "bridge",
		Driver:  "bridge",
		Scope:   "local",
		Created: created,
		IPAM: network.IPAM{
			Config: []network.IPAMConfig{{Subnet: "172.17.0.0/16", Gateway: "172.17.0.1"}},
		},
		Containers: map[string]network.EndpointResource{"a": {}, "b": {}},
	})

	if got.Name != "bridge" {
		t.Errorf("Name = %q", got.Name)
	}
	if len(got.IPAM) != 1 || got.IPAM[0].Subnet != "172.17.0.0/16" {
		t.Errorf("IPAM = %+v", got.IPAM)
	}
	if got.ContainerCount != 2 {
		t.Errorf("ContainerCount = %d, want 2", got.ContainerCount)
	}
	if got.Labels == nil {
		t.Error("Labels = nil, want an empty map")
	}
}

func TestContainerFiltersTranslateOptions(t *testing.T) {
	args := containerFilters(ListContainersOptions{
		Label:  []string{"app=web", "tier=front"},
		Status: []string{"running"},
		Name:   "web",
	})

	if !args.ExactMatch("label", "app=web") || !args.ExactMatch("label", "tier=front") {
		t.Errorf("label filters missing: %v", args)
	}
	if !args.ExactMatch("status", "running") {
		t.Error("status filter missing")
	}
	if !args.ExactMatch("name", "web") {
		t.Error("name filter missing")
	}
}

func TestParsePort(t *testing.T) {
	tests := map[string]uint16{
		"":      0,
		"8080":  8080,
		"abc":   0,
		"99999": 0,
		"65535": 65535,
	}
	for in, want := range tests {
		if got := parsePort(in); got != want {
			t.Errorf("parsePort(%q) = %d, want %d", in, got, want)
		}
	}
}
