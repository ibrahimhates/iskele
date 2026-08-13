package docker

import (
	"errors"
	"testing"
	"time"
)

func TestBuildCreateSpecNeedsAnImage(t *testing.T) {
	_, err := BuildCreateSpec(ContainerSpec{Name: "web"})

	var specErr *SpecError
	if !errors.As(err, &specErr) || specErr.Field != "image" {
		t.Fatalf("error = %v, want a SpecError naming the image field", err)
	}
}

func TestBuildCreateSpecCarriesTheBasics(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Name:       "  web  ",
		Image:      " nginx:1.27 ",
		Command:    []string{"nginx", "-g", "daemon off;"},
		Entrypoint: []string{"/docker-entrypoint.sh"},
		WorkingDir: "/app",
		User:       "1000:1000",
		Hostname:   "web-1",
		Labels:     map[string]string{"app": "web"},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if got.Name != "web" {
		t.Errorf("Name = %q, want it trimmed", got.Name)
	}
	if got.Config.Image != "nginx:1.27" {
		t.Errorf("Image = %q, want it trimmed", got.Config.Image)
	}
	if len(got.Config.Cmd) != 3 || got.Config.Cmd[0] != "nginx" {
		t.Errorf("Cmd = %v", got.Config.Cmd)
	}
	if len(got.Config.Entrypoint) != 1 {
		t.Errorf("Entrypoint = %v", got.Config.Entrypoint)
	}
	if got.Config.WorkingDir != "/app" || got.Config.User != "1000:1000" {
		t.Errorf("working dir / user not carried: %+v", got.Config)
	}
}

// A value containing "=" is common in connection strings, and splitting on the
// first "=" at the wrong layer would corrupt it.
func TestEnvironmentKeepsEqualsSignsInValues(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		Env: []EnvVar{
			{Key: "DSN", Value: "postgres://u:p@host/db?sslmode=disable"},
			{Key: "  ", Value: "dropped"},
			{Key: "A", Value: ""},
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	want := []string{"A=", "DSN=postgres://u:p@host/db?sslmode=disable"}
	if len(got.Config.Env) != len(want) {
		t.Fatalf("Env = %v, want %v", got.Config.Env, want)
	}
	for i := range want {
		if got.Config.Env[i] != want[i] {
			t.Errorf("Env[%d] = %q, want %q", i, got.Config.Env[i], want[i])
		}
	}
}

func TestPortsBecomeExposedPortsAndBindings(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		Ports: []PortMapping{
			{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: 80},
			{ContainerPort: 53, Protocol: "udp", HostPort: "5353"},
			// No host side: exposed to other containers, not published.
			{ContainerPort: 9000},
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if len(got.Config.ExposedPorts) != 3 {
		t.Errorf("ExposedPorts = %v, want all three", got.Config.ExposedPorts)
	}
	if len(got.HostConfig.PortBindings) != 2 {
		t.Errorf("PortBindings = %v, want only the published ones", got.HostConfig.PortBindings)
	}

	bindings := got.HostConfig.PortBindings["80/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" || bindings[0].HostPort != "8080" {
		t.Errorf("80/tcp bound to %+v", bindings)
	}
	if _, ok := got.HostConfig.PortBindings["53/udp"]; !ok {
		t.Error("the udp mapping was lost")
	}
}

func TestPortsAreValidated(t *testing.T) {
	cases := map[string]PortMapping{
		"port zero":        {ContainerPort: 0},
		"port too large":   {ContainerPort: 70000},
		"unknown protocol": {ContainerPort: 80, Protocol: "icmp"},
		"host not a port":  {ContainerPort: 80, HostPort: "http"},
		"bad range":        {ContainerPort: 80, HostPort: "8000-"},
	}

	for name, port := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := BuildCreateSpec(ContainerSpec{Image: "app", Ports: []PortMapping{port}})

			var specErr *SpecError
			if !errors.As(err, &specErr) || specErr.Field != "ports" {
				t.Errorf("error = %v, want a SpecError naming ports", err)
			}
		})
	}
}

func TestHostPortRangeIsAccepted(t *testing.T) {
	_, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		Ports: []PortMapping{{ContainerPort: 80, HostPort: "8000-8010"}},
	})
	if err != nil {
		t.Errorf("a host port range was rejected: %v", err)
	}
}

func TestMountsSplitByType(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		Mounts: []MountSpec{
			{Type: MountTypeBind, Source: "/srv/data", Destination: "/data", ReadOnly: true},
			{Type: MountTypeVolume, Source: "pgdata", Destination: "/var/lib/postgresql/data"},
			{Type: MountTypeTmpfs, Destination: "/tmp", TmpfsSize: 64 << 20},
			// An empty type means a volume, which is the least surprising
			// default for a row with only a destination.
			{Destination: "/cache"},
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if len(got.HostConfig.Mounts) != 3 {
		t.Fatalf("Mounts = %d, want the two named plus the anonymous one", len(got.HostConfig.Mounts))
	}
	if got.HostConfig.Mounts[0].Type != "bind" || !got.HostConfig.Mounts[0].ReadOnly {
		t.Errorf("bind mount = %+v", got.HostConfig.Mounts[0])
	}
	if got.HostConfig.Mounts[3-1].Source != "" {
		t.Errorf("the typeless row should be an anonymous volume, got %+v", got.HostConfig.Mounts[2])
	}

	if got.HostConfig.Tmpfs["/tmp"] != "rw,size=67108864" {
		t.Errorf("tmpfs options = %q", got.HostConfig.Tmpfs["/tmp"])
	}
}

func TestMountsAreValidated(t *testing.T) {
	cases := map[string]MountSpec{
		"no destination":       {Type: MountTypeBind, Source: "/srv"},
		"relative destination": {Type: MountTypeBind, Source: "/srv", Destination: "data"},
		"bind without source":  {Type: MountTypeBind, Destination: "/data"},
		"relative bind source": {Type: MountTypeBind, Source: "srv", Destination: "/data"},
		"unknown type":         {Type: "nfs", Destination: "/data"},
	}

	for name, mount := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := BuildCreateSpec(ContainerSpec{Image: "app", Mounts: []MountSpec{mount}})

			var specErr *SpecError
			if !errors.As(err, &specErr) || specErr.Field != "mounts" {
				t.Errorf("error = %v, want a SpecError naming mounts", err)
			}
		})
	}
}

func TestRestartPolicies(t *testing.T) {
	for _, name := range []string{"", "no", "always", "unless-stopped"} {
		t.Run("accepts "+name, func(t *testing.T) {
			_, err := BuildCreateSpec(ContainerSpec{
				Image:         "app",
				RestartPolicy: RestartPolicy{Name: name},
			})
			if err != nil {
				t.Errorf("policy %q rejected: %v", name, err)
			}
		})
	}

	t.Run("on-failure carries its retry count", func(t *testing.T) {
		got, err := BuildCreateSpec(ContainerSpec{
			Image:         "app",
			RestartPolicy: RestartPolicy{Name: "on-failure", MaxRetries: 5},
		})
		if err != nil {
			t.Fatalf("BuildCreateSpec() error = %v", err)
		}
		if got.HostConfig.RestartPolicy.MaximumRetryCount != 5 {
			t.Errorf("retry count = %d", got.HostConfig.RestartPolicy.MaximumRetryCount)
		}
	})

	// The engine rejects this, and catching it here names the field.
	t.Run("a retry count without on-failure is refused", func(t *testing.T) {
		_, err := BuildCreateSpec(ContainerSpec{
			Image:         "app",
			RestartPolicy: RestartPolicy{Name: "always", MaxRetries: 3},
		})
		var specErr *SpecError
		if !errors.As(err, &specErr) || specErr.Field != "restart_policy" {
			t.Errorf("error = %v, want a SpecError naming restart_policy", err)
		}
	})

	t.Run("an unknown policy is refused", func(t *testing.T) {
		_, err := BuildCreateSpec(ContainerSpec{
			Image:         "app",
			RestartPolicy: RestartPolicy{Name: "sometimes"},
		})
		if err == nil {
			t.Error("an unknown restart policy was accepted")
		}
	})
}

func TestResourceLimitsMapToCgroupSettings(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		Resources: Resources{
			CPUs:       1.5,
			Memory:     512 << 20,
			MemorySwap: 1 << 30,
			PidsLimit:  100,
			CPUSetCPUs: "0-3",
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if got.HostConfig.NanoCPUs != 1_500_000_000 {
		t.Errorf("NanoCPUs = %d, want 1.5 cores", got.HostConfig.NanoCPUs)
	}
	if got.HostConfig.PidsLimit == nil || *got.HostConfig.PidsLimit != 100 {
		t.Errorf("PidsLimit = %v", got.HostConfig.PidsLimit)
	}
	if got.HostConfig.CpusetCpus != "0-3" {
		t.Errorf("CpusetCpus = %q", got.HostConfig.CpusetCpus)
	}
}

// A single string is what an operator types; the engine needs it wrapped so it
// runs under a shell rather than being exec'd as a binary named "curl -f ...".
func TestHealthCheckWrapsABareCommandInAShell(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		HealthCheck: &HealthSpec{
			Test:     []string{"curl -f localhost/health"},
			Interval: "30s",
			Timeout:  "5s",
			Retries:  3,
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	test := got.Config.Healthcheck.Test
	if len(test) != 2 || test[0] != "CMD-SHELL" {
		t.Errorf("Test = %v, want it wrapped in CMD-SHELL", test)
	}
	if got.Config.Healthcheck.Interval != 30*time.Second {
		t.Errorf("Interval = %v", got.Config.Healthcheck.Interval)
	}
}

func TestHealthCheckArgumentListIsExecedDirectly(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image:       "app",
		HealthCheck: &HealthSpec{Test: []string{"curl", "-f", "localhost/health"}},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	test := got.Config.Healthcheck.Test
	if len(test) != 4 || test[0] != "CMD" {
		t.Errorf("Test = %v, want a CMD prefix", test)
	}
}

func TestHealthCheckDisableTurnsOffTheImagesProbe(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image:       "app",
		HealthCheck: &HealthSpec{Disable: true},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}
	if len(got.Config.Healthcheck.Test) != 1 || got.Config.Healthcheck.Test[0] != "NONE" {
		t.Errorf("Test = %v, want NONE", got.Config.Healthcheck.Test)
	}
}

func TestHealthCheckDurationsAreValidated(t *testing.T) {
	_, err := BuildCreateSpec(ContainerSpec{
		Image:       "app",
		HealthCheck: &HealthSpec{Test: []string{"true"}, Interval: "half a minute"},
	})

	var specErr *SpecError
	if !errors.As(err, &specErr) || specErr.Field != "health_check.interval" {
		t.Errorf("error = %v, want a SpecError naming the interval", err)
	}
}

func TestDevicesAreParsedAndValidated(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image:    "app",
		Security: SecuritySpec{Devices: []string{"/dev/ttyUSB0", "/dev/sda:/dev/xvda:r"}},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if len(got.HostConfig.Devices) != 2 {
		t.Fatalf("Devices = %v", got.HostConfig.Devices)
	}
	if got.HostConfig.Devices[0].PathInContainer != "/dev/ttyUSB0" {
		t.Errorf("a device without a container path should keep the host path: %+v",
			got.HostConfig.Devices[0])
	}
	if got.HostConfig.Devices[1].CgroupPermissions != "r" {
		t.Errorf("permissions = %q", got.HostConfig.Devices[1].CgroupPermissions)
	}

	for _, bad := range []string{"ttyUSB0", "/dev/sda:/dev/xvda:x", "/a:/b:rwm:extra"} {
		if _, err := BuildCreateSpec(ContainerSpec{
			Image:    "app",
			Security: SecuritySpec{Devices: []string{bad}},
		}); err == nil {
			t.Errorf("device %q was accepted", bad)
		}
	}
}

func TestNetworkNameBecomesTheNetworkMode(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image:   "app",
		Network: NetworkSpec{Name: "backend", Aliases: []string{"api"}, IPv4Address: "172.20.0.5"},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if string(got.HostConfig.NetworkMode) != "backend" {
		t.Errorf("NetworkMode = %q", got.HostConfig.NetworkMode)
	}
	endpoint := got.NetworkingConfig.EndpointsConfig["backend"]
	if endpoint == nil {
		t.Fatal("no endpoint config for the named network")
	}
	if endpoint.IPAMConfig == nil || endpoint.IPAMConfig.IPv4Address != "172.20.0.5" {
		t.Errorf("static address lost: %+v", endpoint.IPAMConfig)
	}
}

// host and none networking reject an endpoint config, so one must not be built
// when there is nothing to put in it.
func TestPlainNetworkModeCarriesNoEndpointConfig(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image:   "app",
		Network: NetworkSpec{Name: "host"},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if string(got.HostConfig.NetworkMode) != "host" {
		t.Errorf("NetworkMode = %q", got.HostConfig.NetworkMode)
	}
	if got.NetworkingConfig != nil {
		t.Errorf("NetworkingConfig = %+v, want nil for a bare network mode", got.NetworkingConfig)
	}
}

func TestSecurityOptionsAreCarriedThrough(t *testing.T) {
	got, err := BuildCreateSpec(ContainerSpec{
		Image: "app",
		Security: SecuritySpec{
			Privileged:     true,
			CapAdd:         []string{"NET_ADMIN"},
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges:true"},
			ReadOnlyRootFS: true,
			Sysctls:        map[string]string{"net.ipv4.ip_forward": "1"},
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateSpec() error = %v", err)
	}

	if !got.HostConfig.Privileged || !got.HostConfig.ReadonlyRootfs {
		t.Errorf("flags lost: %+v", got.HostConfig)
	}
	if len(got.HostConfig.CapAdd) != 1 || got.HostConfig.CapAdd[0] != "NET_ADMIN" {
		t.Errorf("CapAdd = %v", got.HostConfig.CapAdd)
	}
	if got.HostConfig.Sysctls["net.ipv4.ip_forward"] != "1" {
		t.Errorf("Sysctls = %v", got.HostConfig.Sysctls)
	}
}

func TestBindSourcesListsOnlyBindMounts(t *testing.T) {
	sources := BindSources(ContainerSpec{
		Mounts: []MountSpec{
			{Type: MountTypeBind, Source: "/srv/data", Destination: "/data"},
			{Type: MountTypeVolume, Source: "pgdata", Destination: "/var/lib"},
			{Type: MountTypeTmpfs, Destination: "/tmp"},
			{Type: "BIND", Source: "/opt/app", Destination: "/app"},
		},
	})

	if len(sources) != 2 {
		t.Fatalf("sources = %v, want the two binds", sources)
	}
	if sources[0] != "/srv/data" || sources[1] != "/opt/app" {
		t.Errorf("sources = %v", sources)
	}
}
