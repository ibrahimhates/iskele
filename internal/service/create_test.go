package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
)

func newCreator(t *testing.T, allowed []string) (*Creator, *fake.Client) {
	t.Helper()
	f := fake.New()
	return NewCreator(f, nil, NewPathGuard(allowed), nil), f
}

func createSpec(image string) docker.ContainerSpec {
	return docker.ContainerSpec{Name: "app", Image: image}
}

func TestCreateReturnsTheEnginesID(t *testing.T) {
	svc, _ := newCreator(t, []string{"/srv"})

	result, err := svc.Create(context.Background(), createSpec("nginx:1.27"),
		CreateOptions{}, audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ID == "" || result.Name != "app" || result.Image != "nginx:1.27" {
		t.Errorf("result = %+v", result)
	}
	if result.Started {
		t.Error("the container was started without being asked to")
	}
}

func TestCreateStartsWhenAsked(t *testing.T) {
	svc, f := newCreator(t, []string{"/srv"})

	spec := createSpec("nginx")
	spec.Start = true

	result, err := svc.Create(context.Background(), spec, CreateOptions{}, audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !result.Started {
		t.Error("start was requested but not reported")
	}
	if len(f.CallsFor(fake.OpStartContainer)) != 1 {
		t.Error("the engine was not asked to start it")
	}
}

// A container that exists but would not start is the operator's to inspect;
// removing it would throw away the evidence.
func TestAContainerThatFailsToStartIsLeftInPlace(t *testing.T) {
	svc, f := newCreator(t, nil)
	f.Fail(fake.OpStartContainer, docker.NewError(docker.KindConflict, "container.start",
		"container", "app", "port is already allocated"))

	spec := createSpec("nginx")
	spec.Start = true

	result, err := svc.Create(context.Background(), spec, CreateOptions{}, audit.Actor{}, RequestMeta{})
	if err == nil {
		t.Fatal("Create() error = nil, want the start failure")
	}
	if result.ID == "" {
		t.Error("the created container's id was not reported")
	}
	if len(f.CallsFor(fake.OpRemoveContainer)) != 0 {
		t.Error("the container was removed after failing to start")
	}
}

func TestCreateRefusesABindOutsideTheWhitelist(t *testing.T) {
	svc, f := newCreator(t, []string{"/srv"})

	spec := createSpec("alpine")
	spec.Mounts = []docker.MountSpec{
		{Type: docker.MountTypeBind, Source: "/etc", Destination: "/host"},
	}

	_, err := svc.Create(context.Background(), spec, CreateOptions{}, audit.Actor{}, RequestMeta{})
	if !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("error = %v, want the path refusal", err)
	}
	if len(f.Calls()) != 0 {
		t.Error("the engine was reached after a refused mount")
	}
}

func TestCreateEnforcesThePrivilegedGate(t *testing.T) {
	cases := map[string]docker.SecuritySpec{
		"privileged":   {Privileged: true},
		"cap_add":      {CapAdd: []string{"SYS_ADMIN"}},
		"devices":      {Devices: []string{"/dev/sda"}},
		"security_opt": {SecurityOpt: []string{"apparmor=unconfined"}},
		"sysctls":      {Sysctls: map[string]string{"net.ipv4.ip_forward": "1"}},
	}

	for name, security := range cases {
		t.Run(name, func(t *testing.T) {
			svc, _ := newCreator(t, nil)

			spec := createSpec("alpine")
			spec.Security = security

			_, err := svc.Create(context.Background(), spec, CreateOptions{},
				audit.Actor{}, RequestMeta{})

			var privErr *PrivilegedError
			if !errors.As(err, &privErr) {
				t.Fatalf("error = %v, want a PrivilegedError", err)
			}
			if len(privErr.Options) != 1 || privErr.Options[0] != name {
				t.Errorf("options = %v, want %q named", privErr.Options, name)
			}

			// The same spec from a caller who holds the permission goes through.
			if _, err := svc.Create(context.Background(), spec,
				CreateOptions{Privileged: true}, audit.Actor{}, RequestMeta{}); err != nil {
				t.Errorf("privileged caller was refused: %v", err)
			}
		})
	}
}

// Dropping capabilities narrows the container, so it must not be gated.
func TestCapDropNeedsNoPermission(t *testing.T) {
	svc, _ := newCreator(t, nil)

	spec := createSpec("alpine")
	spec.Security = docker.SecuritySpec{CapDrop: []string{"ALL"}}

	if _, err := svc.Create(context.Background(), spec, CreateOptions{},
		audit.Actor{}, RequestMeta{}); err != nil {
		t.Errorf("cap_drop was refused: %v", err)
	}
}

func TestHostNetworkingIsGated(t *testing.T) {
	svc, _ := newCreator(t, nil)

	spec := createSpec("alpine")
	spec.Network = docker.NetworkSpec{Name: "host"}

	_, err := svc.Create(context.Background(), spec, CreateOptions{}, audit.Actor{}, RequestMeta{})
	if !errors.Is(err, ErrPrivilegedDenied) {
		t.Errorf("error = %v, want host networking refused", err)
	}
}

// A named network is not a namespace escape and must stay ungated.
func TestANamedNetworkIsNotGated(t *testing.T) {
	svc, _ := newCreator(t, nil)

	spec := createSpec("alpine")
	spec.Network = docker.NetworkSpec{Name: "backend"}

	if _, err := svc.Create(context.Background(), spec, CreateOptions{},
		audit.Actor{}, RequestMeta{}); err != nil {
		t.Errorf("a named network was refused: %v", err)
	}
}

func TestPullPolicyAlwaysPullsFirst(t *testing.T) {
	svc, f := newCreator(t, nil)

	spec := createSpec("nginx:1.27")
	spec.PullPolicy = docker.PullAlways

	if _, err := svc.Create(context.Background(), spec, CreateOptions{},
		audit.Actor{}, RequestMeta{}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(f.CallsFor(fake.OpPullImageProgress)) != 1 {
		t.Error("pull_policy=always did not pull")
	}
}

// The engine pulls a missing image on create by itself; pulling anyway would
// defeat the point of the policy on a slow link.
func TestPullPolicyMissingLeavesItToTheEngine(t *testing.T) {
	for _, policy := range []string{"", docker.PullMissing, docker.PullNever} {
		t.Run(policy, func(t *testing.T) {
			svc, f := newCreator(t, nil)

			spec := createSpec("nginx")
			spec.PullPolicy = policy

			if _, err := svc.Create(context.Background(), spec, CreateOptions{},
				audit.Actor{}, RequestMeta{}); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if len(f.CallsFor(fake.OpPullImageProgress)) != 0 {
				t.Errorf("policy %q pulled anyway", policy)
			}
		})
	}
}

func TestAnUnknownPullPolicyIsRejected(t *testing.T) {
	svc, _ := newCreator(t, nil)

	spec := createSpec("nginx")
	spec.PullPolicy = "sometimes"

	_, err := svc.Create(context.Background(), spec, CreateOptions{}, audit.Actor{}, RequestMeta{})

	var specErr *docker.SpecError
	if !errors.As(err, &specErr) || specErr.Field != "pull_policy" {
		t.Errorf("error = %v, want a SpecError naming the policy", err)
	}
}

// A pull that fails must stop the create: a container from the previous image
// is not what was asked for.
func TestAFailedPullStopsTheCreate(t *testing.T) {
	svc, f := newCreator(t, nil)
	f.SetPullEvents([]docker.PullEvent{{Error: "manifest unknown"}})

	spec := createSpec("nope")
	spec.PullPolicy = docker.PullAlways

	if _, err := svc.Create(context.Background(), spec, CreateOptions{},
		audit.Actor{}, RequestMeta{}); err == nil {
		t.Fatal("Create() error = nil, want the pull failure")
	}
	if len(f.CallsFor(fake.OpCreateContainer)) != 0 {
		t.Error("a container was created after the pull failed")
	}
}

func TestCreateRejectsAMalformedSpecBeforeTheEngine(t *testing.T) {
	svc, f := newCreator(t, nil)

	spec := createSpec("nginx")
	spec.Ports = []docker.PortMapping{{ContainerPort: 70000}}

	_, err := svc.Create(context.Background(), spec, CreateOptions{}, audit.Actor{}, RequestMeta{})

	var specErr *docker.SpecError
	if !errors.As(err, &specErr) {
		t.Fatalf("error = %v, want a SpecError", err)
	}
	if len(f.CallsFor(fake.OpCreateContainer)) != 0 {
		t.Error("the engine was reached with a malformed spec")
	}
}
