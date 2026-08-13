package compose

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// convertFixture parses a fixture and converts it, which is what every test
// here starts from.
func convertFixture(t *testing.T, name, project, env string) (Conversion, string) {
	t.Helper()

	dir := t.TempDir()
	parsed, _, err := Parse(context.Background(), Input{
		Name:       project,
		Compose:    fixture(t, name),
		Env:        env,
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("Parse(%s) error = %v", name, err)
	}

	order, err := ServiceOrder(context.Background(), parsed)
	if err != nil {
		t.Fatalf("ServiceOrder() error = %v", err)
	}

	conversion, err := Convert(parsed, order)
	if err != nil {
		t.Fatalf("Convert(%s) error = %v", name, err)
	}
	return conversion, dir
}

// planFor finds one service in a conversion.
func planFor(t *testing.T, conversion Conversion, name string) ServicePlan {
	t.Helper()

	for _, plan := range conversion.Services {
		if plan.Name == name {
			return plan
		}
	}
	t.Fatalf("service %q is not in the conversion", name)
	return ServicePlan{}
}

func TestConvertLabelsEveryContainerWithItsStack(t *testing.T) {
	conversion, _ := convertFixture(t, "redis.yaml", "cache-stack", "")

	labels := planFor(t, conversion, "cache").Spec.Labels
	for key, want := range map[string]string{
		LabelStack:          "cache-stack",
		LabelService:        "cache",
		LabelManaged:        "true",
		LabelComposeProject: "cache-stack",
		LabelComposeService: "cache",
	} {
		if labels[key] != want {
			t.Errorf("label %s = %q, want %q", key, labels[key], want)
		}
	}
}

func TestConvertNamespacesVolumesAndNetworks(t *testing.T) {
	conversion, _ := convertFixture(t, "redis.yaml", "cache-stack", "")

	if len(conversion.Volumes) != 1 {
		t.Fatalf("volumes = %+v, want one", conversion.Volumes)
	}
	if conversion.Volumes[0].Name != "cache-stack_cache" {
		t.Errorf("volume name = %q, want cache-stack_cache", conversion.Volumes[0].Name)
	}

	// The mount has to point at the namespaced name, not the key in the file.
	mounts := planFor(t, conversion, "cache").Spec.Mounts
	if len(mounts) != 1 || mounts[0].Source != "cache-stack_cache" {
		t.Errorf("mounts = %+v, want the namespaced volume", mounts)
	}
}

func TestConvertKeepsDependencyOrder(t *testing.T) {
	conversion, _ := convertFixture(t, "scaled.yaml", "jobs", "")

	var names []string
	for _, plan := range conversion.Services {
		names = append(names, plan.Name)
	}

	if slices.Index(names, "queue") > slices.Index(names, "worker") {
		t.Errorf("order = %v, want queue before worker", names)
	}
}

func TestConvertReadsReplicas(t *testing.T) {
	conversion, _ := convertFixture(t, "scaled.yaml", "jobs", "")

	if got := planFor(t, conversion, "worker").Replicas; got != 3 {
		t.Errorf("worker replicas = %d, want 3", got)
	}
	if got := planFor(t, conversion, "queue").Replicas; got != 1 {
		t.Errorf("queue replicas = %d, want 1", got)
	}
}

func TestConvertMapsRestartPolicies(t *testing.T) {
	conversion, _ := convertFixture(t, "scaled.yaml", "jobs", "")

	policy := planFor(t, conversion, "scheduler").Spec.RestartPolicy
	if policy.Name != "on-failure" || policy.MaxRetries != 3 {
		t.Errorf("restart = %+v, want on-failure with 3 retries", policy)
	}
}

func TestConvertReadsDeployResourceLimits(t *testing.T) {
	conversion, _ := convertFixture(t, "wordpress.yaml", "blog",
		"DB_ROOT_PASSWORD=r\nDB_PASSWORD=p\n")

	resources := planFor(t, conversion, "wordpress").Spec.Resources
	if resources.CPUs != 1.5 {
		t.Errorf("cpus = %v, want 1.5", resources.CPUs)
	}
	if resources.Memory != 512*1024*1024 {
		t.Errorf("memory = %d, want 512 MiB", resources.Memory)
	}
}

func TestConvertReadsHealthChecks(t *testing.T) {
	conversion, _ := convertFixture(t, "wordpress.yaml", "blog",
		"DB_ROOT_PASSWORD=r\nDB_PASSWORD=p\n")

	check := planFor(t, conversion, "db").Spec.HealthCheck
	if check == nil {
		t.Fatal("health check = nil, want the fixture's probe")
	}
	if check.Interval != "10s" || check.Retries != 5 || check.StartPeriod != "30s" {
		t.Errorf("health = %+v, want 10s/5/30s", check)
	}
}

// A service must answer to its compose name on every network it joins:
// `WORDPRESS_DB_HOST: db:3306` depends on nothing else.
func TestConvertAliasesEveryServiceByName(t *testing.T) {
	conversion, _ := convertFixture(t, "networks.yaml", "net", "")

	proxy := planFor(t, conversion, "proxy")
	if len(proxy.Networks) != 2 {
		t.Fatalf("networks = %+v, want two", proxy.Networks)
	}
	for _, attachment := range proxy.Networks {
		if !slices.Contains(attachment.Aliases, "proxy") {
			t.Errorf("network %s aliases = %v, want the service name", attachment.Name, attachment.Aliases)
		}
	}
	// The declared alias survives alongside it.
	if !slices.Contains(proxy.Networks[1].Aliases, "gateway") {
		t.Errorf("edge aliases = %v, want the declared gateway", proxy.Networks[1].Aliases)
	}
}

func TestConvertCreatesTheContainerOnTheFirstNetwork(t *testing.T) {
	conversion, _ := convertFixture(t, "networks.yaml", "net", "")

	api := planFor(t, conversion, "api")
	if api.Spec.Network.Name != "net_backend" {
		t.Errorf("network = %q, want net_backend", api.Spec.Network.Name)
	}
	if api.Spec.Network.IPv4Address != "172.28.1.10" {
		t.Errorf("ipv4 = %q, want the static address", api.Spec.Network.IPv4Address)
	}
}

func TestConvertReadsNetworkIPAM(t *testing.T) {
	conversion, _ := convertFixture(t, "networks.yaml", "net", "")

	var backend NetworkPlan
	for _, network := range conversion.Networks {
		if network.Key == "backend" {
			backend = network
		}
	}

	if !backend.Options.Internal {
		t.Error("backend should be internal")
	}
	if len(backend.Options.IPAM) != 1 || backend.Options.IPAM[0].Subnet != "172.28.1.0/24" {
		t.Errorf("ipam = %+v, want the declared subnet", backend.Options.IPAM)
	}
}

func TestConvertReadsBindsTmpfsAndReadOnly(t *testing.T) {
	conversion, dir := convertFixture(t, "binds.yaml", "site", "")

	web := planFor(t, conversion, "web")
	if !web.Spec.Security.ReadOnlyRootFS {
		t.Error("read_only should reach the security spec")
	}

	var binds, tmpfs int
	for _, mount := range web.Spec.Mounts {
		switch mount.Type {
		case docker.MountTypeBind:
			binds++
			if mount.Source == filepath.Join(dir, "site") && !mount.ReadOnly {
				t.Error("the ./site bind is declared :ro and should stay read-only")
			}
		case docker.MountTypeTmpfs:
			tmpfs++
		}
	}
	if binds != 2 {
		t.Errorf("binds = %d, want 2", binds)
	}
	if tmpfs != 1 {
		t.Errorf("tmpfs mounts = %d, want 1", tmpfs)
	}
}

// The point of the warnings is that nothing is dropped silently.
func TestConvertWarnsAboutFieldsItCannotHonour(t *testing.T) {
	conversion, _ := convertFixture(t, "unsupported.yaml", "app", "")

	fields := map[string]bool{}
	for _, warning := range conversion.Warnings {
		fields[warning.Field] = true
	}

	for _, want := range []string{"configs", "secrets", "links", "develop"} {
		if !fields[want] {
			t.Errorf("no warning for %q; warnings = %+v", want, conversion.Warnings)
		}
	}
}

func TestConvertDropsPassthroughEnvironment(t *testing.T) {
	// `- FROM_HOST` with no value asks compose to copy the variable out of the
	// caller's environment. iskeled's environment is not the operator's shell.
	dir := t.TempDir()
	project, _, err := Parse(context.Background(), Input{
		Name:       "env",
		Compose:    "services:\n  app:\n    image: alpine\n    environment:\n      - FROM_HOST\n      - SET=yes\n",
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	conversion, err := Convert(project, nil)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	env := planFor(t, conversion, "app").Spec.Env
	if len(env) != 1 || env[0].Key != "SET" {
		t.Errorf("env = %+v, want only the explicitly set variable", env)
	}
}

// One name cannot cover two containers, and the loader says so before any of
// this reaches the engine.
func TestParseRefusesReplicasWithAFixedContainerName(t *testing.T) {
	_, _, err := Parse(context.Background(), Input{
		Name: "clash",
		Compose: "services:\n  app:\n    image: alpine\n" +
			"    container_name: fixed\n    deploy:\n      replicas: 2\n",
		WorkingDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want container_name and replicas to be refused")
	}
}

func TestConvertPassesSecurityOptionsThroughForTheGateToRefuse(t *testing.T) {
	dir := t.TempDir()
	project, _, err := Parse(context.Background(), Input{
		Name: "priv",
		Compose: "services:\n  app:\n    image: alpine\n    privileged: true\n" +
			"    cap_add: [SYS_ADMIN]\n    devices:\n      - /dev/kvm:/dev/kvm:rwm\n",
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	conversion, err := Convert(project, nil)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	security := planFor(t, conversion, "app").Spec.Security
	if !security.Privileged {
		t.Error("privileged should survive conversion so the permission gate can refuse it")
	}
	if !slices.Contains(security.CapAdd, "SYS_ADMIN") {
		t.Errorf("cap_add = %v, want SYS_ADMIN carried through", security.CapAdd)
	}
	if len(security.Devices) != 1 || security.Devices[0] != "/dev/kvm:/dev/kvm:rwm" {
		t.Errorf("devices = %v, want the declared mapping", security.Devices)
	}
}

func TestConvertBuildsAnImageWhenTheServiceHasNoneToPull(t *testing.T) {
	dir := t.TempDir()
	project, _, err := Parse(context.Background(), Input{
		Name: "built",
		Compose: "services:\n  app:\n    build:\n      context: ./app\n" +
			"      dockerfile: Dockerfile.prod\n      args:\n        VERSION: \"1.2\"\n",
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	conversion, err := Convert(project, nil)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	plan := planFor(t, conversion, "app")
	if plan.Build == nil {
		t.Fatal("build = nil, want a build plan")
	}
	if plan.Build.Context != filepath.Join(dir, "app") {
		t.Errorf("context = %q, want it resolved against the working dir", plan.Build.Context)
	}
	if plan.Build.Image != "built-app:latest" {
		t.Errorf("image = %q, want built-app:latest", plan.Build.Image)
	}
	if plan.Build.Args["VERSION"] != "1.2" {
		t.Errorf("args = %v, want VERSION=1.2", plan.Build.Args)
	}
	// The image is built locally, so pulling it would fail.
	if plan.Spec.PullPolicy != docker.PullNever {
		t.Errorf("pull policy = %q, want never", plan.Spec.PullPolicy)
	}
}

func TestContainerNameMatchesCompose(t *testing.T) {
	if got := ContainerName("blog", "db", 1); got != "blog-db-1" {
		t.Errorf("ContainerName() = %q, want blog-db-1", got)
	}
}

func TestConvertHonoursNetworkMode(t *testing.T) {
	dir := t.TempDir()
	project, _, err := Parse(context.Background(), Input{
		Name:       "hostnet",
		Compose:    "services:\n  app:\n    image: alpine\n    network_mode: host\n",
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	conversion, err := Convert(project, nil)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	plan := planFor(t, conversion, "app")
	if plan.Spec.Network.Name != "host" {
		t.Errorf("network = %q, want host", plan.Spec.Network.Name)
	}
	if len(plan.Networks) != 0 {
		t.Errorf("attachments = %+v, want none: host mode is not a network to join", plan.Networks)
	}
}
