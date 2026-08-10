package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/compose"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/store"
)

// stackEnv is a stack service over a real database and a fake engine.
type stackEnv struct {
	svc    *StackService
	docker *fake.Client
	db     *store.DB
	root   string
}

func newStackEnv(t *testing.T) *stackEnv {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	f := fake.New()
	// The fixture containers belong to nobody's stack; leaving them in only
	// makes every assertion below count around them.
	f.Containers = nil

	svc := NewStackService(f, db.Stacks, nil, nil, NewPathGuard([]string{root}),
		NewTaskRegistry(), nil, filepath.Join(root, "stacks"))

	return &stackEnv{svc: svc, docker: f, db: db, root: root}
}

// save creates a stack from an editor-typed compose file.
func (e *stackEnv) save(t *testing.T, name, composeYAML, env string) store.Stack {
	t.Helper()

	stack, err := e.svc.Create(context.Background(), StackInput{
		Name:    name,
		Source:  store.StackSourceEditor,
		Compose: composeYAML,
		Env:     env,
	}, audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return stack
}

// deploy runs Up to completion and returns the events it produced.
func (e *stackEnv) deploy(t *testing.T, stack store.Stack, opts UpOptions) ([]StackEvent, error) {
	t.Helper()

	events, errs, err := e.svc.Up(context.Background(), stack, opts,
		audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		return nil, err
	}

	var (
		collected []StackEvent
		failure   error
	)
	for events != nil || errs != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			collected = append(collected, event)
		case upErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if upErr != nil {
				failure = upErr
			}
		}
	}
	return collected, failure
}

const twoServices = `
services:
  db:
    image: postgres:16
    volumes:
      - data:/var/lib/postgresql/data
  api:
    image: ghcr.io/example/api:1
    depends_on:
      - db
    ports:
      - "8080:8080"

volumes:
  data:

networks:
  default:
    driver: bridge
`

func TestStackUpCreatesResourcesThenContainersInOrder(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	events, err := env.deploy(t, stack, UpOptions{})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	var messages []string
	for _, event := range events {
		messages = append(messages, event.Message)
	}
	joined := strings.Join(messages, "\n")

	if !strings.Contains(joined, "shop-db-1 started") || !strings.Contains(joined, "shop-api-1 started") {
		t.Fatalf("events =\n%s", joined)
	}
	// db is a dependency of api, so it has to come first.
	if slices.IndexFunc(events, hasMessage("shop-db-1 started")) >
		slices.IndexFunc(events, hasMessage("shop-api-1 started")) {
		t.Errorf("api was started before its database:\n%s", joined)
	}

	// The volume and the network are namespaced and created before anything
	// mounts or joins them.
	if !strings.Contains(joined, "volume shop_data created") {
		t.Errorf("the declared volume was not created:\n%s", joined)
	}

	if got, err := env.db.Stacks.ByID(context.Background(), stack.ID); err != nil {
		t.Fatalf("ByID() error = %v", err)
	} else if got.Status != store.StackDeployed {
		t.Errorf("status = %q, want deployed", got.Status)
	}
}

func TestStackUpLabelsContainersForTheStack(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	containers, err := env.svc.containersOf(context.Background(), "shop")
	if err != nil {
		t.Fatalf("containersOf() error = %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("containers = %d, want 2", len(containers))
	}

	for _, container := range containers {
		if container.Labels[compose.LabelStack] != "shop" {
			t.Errorf("%s is not labeled with its stack: %v", container.Name, container.Labels)
		}
		if container.Labels[LabelConfigHash] == "" {
			t.Errorf("%s has no config hash; a redeploy could not tell it apart", container.Name)
		}
	}
}

// A second deploy of an unchanged file should leave a running container alone.
// Recreating everything on every deploy would restart a database because its
// neighbour's image tag moved.
func TestStackUpLeavesAnUnchangedServiceRunning(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("first Up() error = %v", err)
	}

	events, err := env.deploy(t, stack, UpOptions{})
	if err != nil {
		t.Fatalf("second Up() error = %v", err)
	}

	var upToDate int
	for _, event := range events {
		if strings.HasSuffix(event.Message, "is up to date") {
			upToDate++
		}
		if strings.Contains(event.Message, "removed for replacement") {
			t.Errorf("an unchanged service was replaced: %s", event.Message)
		}
	}
	if upToDate != 2 {
		t.Errorf("up-to-date services = %d, want 2", upToDate)
	}
}

func TestStackUpReplacesAServiceWhoseDefinitionChanged(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("first Up() error = %v", err)
	}

	changed := strings.Replace(twoServices, "ghcr.io/example/api:1", "ghcr.io/example/api:2", 1)
	updated, err := env.svc.Update(context.Background(), stack.ID, StackInput{
		Source:  store.StackSourceEditor,
		Compose: changed,
	}, audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	events, err := env.deploy(t, updated, UpOptions{})
	if err != nil {
		t.Fatalf("second Up() error = %v", err)
	}

	var replaced, untouched bool
	for _, event := range events {
		if event.Message == "shop-api-1 removed for replacement" {
			replaced = true
		}
		if event.Message == "shop-db-1 is up to date" {
			untouched = true
		}
	}
	if !replaced {
		t.Error("the changed service was not replaced")
	}
	if !untouched {
		t.Error("the unchanged service was restarted for no reason")
	}
}

// A compose file must not be a way around the whitelist that the create wizard
// enforces: it is the same trust boundary.
func TestStackUpRefusesABindOutsideTheWhitelist(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "leak", `
services:
  app:
    image: alpine
    volumes:
      - /etc:/host-etc:ro
`, "")

	_, err := env.deploy(t, stack, UpOptions{})
	if err == nil {
		t.Fatal("Up() error = nil, want a refusal")
	}

	var refused *StackRefused
	if !errors.As(err, &refused) {
		t.Fatalf("error type = %T, want *StackRefused", err)
	}
	if len(refused.Problems) == 0 || refused.Problems[0].Service != "app" {
		t.Errorf("problems = %+v, want one naming the service", refused.Problems)
	}

	// Nothing was created: a refused deploy leaves no half-built remains.
	if len(env.docker.Containers) != 0 {
		t.Errorf("containers = %+v, want none", env.docker.Containers)
	}
}

// `privileged: true` in YAML is the same request as ticking the box, and takes
// the same permission.
func TestStackUpRefusesPrivilegedOptionsWithoutThePermission(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "priv", `
services:
  app:
    image: alpine
    privileged: true
`, "")

	if _, err := env.deploy(t, stack, UpOptions{Privileged: false}); err == nil {
		t.Fatal("Up() error = nil, want the privileged gate to refuse it")
	}

	if _, err := env.deploy(t, stack, UpOptions{Privileged: true}); err != nil {
		t.Fatalf("Up() with the permission error = %v", err)
	}
}

func TestStackUpRefusesASecondConcurrentDeploy(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	// Claim the stack the way a running deploy would, and leave it running.
	if _, _, err := env.svc.claim(context.Background(), stack, "stack.up", "someone"); err != nil {
		t.Fatalf("claim() error = %v", err)
	}

	_, _, err := env.svc.Up(context.Background(), stack, UpOptions{},
		audit.Actor{Username: "admin"}, RequestMeta{})
	if !errors.Is(err, ErrStackBusy) {
		t.Fatalf("Up() error = %v, want ErrStackBusy", err)
	}
}

func TestStackDownRemovesContainersAndKeepsVolumesByDefault(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	result, err := env.svc.Down(context.Background(), stack, DownOptions{Networks: true},
		audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Down() error = %v", err)
	}

	if len(result.Containers) != 2 {
		t.Errorf("removed containers = %v, want both", result.Containers)
	}
	// The volume is the one part of a stack that cannot be recreated.
	if len(result.Volumes) != 0 {
		t.Errorf("removed volumes = %v, want none without being asked", result.Volumes)
	}

	if got, err := env.db.Stacks.ByID(context.Background(), stack.ID); err != nil {
		t.Fatalf("ByID() error = %v", err)
	} else if got.Status != store.StackStopped {
		t.Errorf("status = %q, want stopped", got.Status)
	}
}

func TestStackDownRemovesVolumesWhenAsked(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	result, err := env.svc.Down(context.Background(), stack,
		DownOptions{Networks: true, Volumes: true}, audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Down() error = %v", err)
	}

	if !slices.Contains(result.Volumes, "shop_data") {
		t.Errorf("removed volumes = %v, want shop_data", result.Volumes)
	}
}

func TestStackScaleAddsAndRemovesReplicas(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "jobs", `
services:
  worker:
    image: alpine
`, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	scaleTo := func(replicas int) []StackEvent {
		t.Helper()
		events, errs, err := env.svc.Scale(context.Background(), stack, "worker", replicas,
			UpOptions{}, audit.Actor{Username: "admin"}, RequestMeta{})
		if err != nil {
			t.Fatalf("Scale() error = %v", err)
		}

		var collected []StackEvent
		for events != nil || errs != nil {
			select {
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				collected = append(collected, event)
			case scaleErr, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if scaleErr != nil {
					t.Fatalf("scale failed: %v", scaleErr)
				}
			}
		}
		return collected
	}

	scaleTo(3)
	containers, _ := env.svc.containersOf(context.Background(), "jobs")
	if len(containers) != 3 {
		t.Fatalf("containers after scaling up = %d, want 3", len(containers))
	}

	events := scaleTo(1)
	containers, _ = env.svc.containersOf(context.Background(), "jobs")
	if len(containers) != 1 {
		t.Errorf("containers after scaling down = %d, want 1", len(containers))
	}

	var removed bool
	for _, event := range events {
		if strings.Contains(event.Message, "asks for fewer replicas") {
			removed = true
		}
	}
	if !removed {
		t.Errorf("scaling down said nothing about the surplus: %+v", events)
	}
}

func TestStackScaleRejectsAnUnknownService(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "jobs", "services:\n  worker:\n    image: alpine\n", "")

	events, errs, err := env.svc.Scale(context.Background(), stack, "nope", 2,
		UpOptions{}, audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Scale() error = %v", err)
	}

	var failure error
	for events != nil || errs != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case scaleErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			failure = scaleErr
		}
	}
	if !errors.Is(failure, ErrNoSuchService) {
		t.Errorf("error = %v, want ErrNoSuchService", failure)
	}
}

func TestStackValidateReportsProblemsWithoutDeploying(t *testing.T) {
	env := newStackEnv(t)

	report, err := env.svc.Validate(context.Background(), StackInput{
		Name:   "leak",
		Source: store.StackSourceEditor,
		Compose: "services:\n  app:\n    image: alpine\n    volumes:\n      - /etc:/x\n" +
			"  other:\n    image: alpine\n    privileged: true\n",
	}, false)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if report.Valid {
		t.Error("valid = true, want false")
	}
	// Both problems at once: an operator should not have to fix one, redeploy,
	// and discover the next.
	if len(report.Problems) < 2 {
		t.Errorf("problems = %+v, want one per service", report.Problems)
	}
	if len(env.docker.Containers) != 0 {
		t.Error("validation created something")
	}
}

func TestStackValidateReportsAFileThatWillNotParse(t *testing.T) {
	env := newStackEnv(t)

	report, err := env.svc.Validate(context.Background(), StackInput{
		Name:    "broken",
		Source:  store.StackSourceEditor,
		Compose: "services:\n  app:\n  image: [",
	}, true)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if report.Valid || report.Error == "" {
		t.Errorf("report = %+v, want an invalid file with an explanation", report)
	}
}

func TestStackDetailCountsRunningContainers(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	// A deploy starts every container; stopping one is what makes the count
	// worth asserting.
	if err := env.docker.StopContainer(context.Background(), "shop-api-1", docker.StopOptions{}); err != nil {
		t.Fatalf("StopContainer() error = %v", err)
	}

	detail, err := env.svc.Detail(context.Background(), stack.ID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if len(detail.Services) != 2 {
		t.Fatalf("services = %+v, want two", detail.Services)
	}

	for _, service := range detail.Services {
		switch service.Name {
		case "db":
			if service.Running != 1 {
				t.Errorf("db running = %d, want 1", service.Running)
			}
		case "api":
			if service.Running != 0 {
				t.Errorf("api running = %d, want 0 after being stopped", service.Running)
			}
		}
	}
}

// A stack whose file stopped parsing is still a stack: it has containers, and
// the operator needs to be told why it will not deploy.
func TestStackDetailSurvivesAnUnparseableFile(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	broken := stack
	broken.Compose = "services:\n  app:\n  image: ["
	if err := env.db.Stacks.Update(context.Background(), &broken); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	detail, err := env.svc.Detail(context.Background(), stack.ID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if detail.ParseError == "" {
		t.Error("parse_error is empty; the operator has no idea why nothing works")
	}
}

func TestStackListWithholdsEnvContent(t *testing.T) {
	env := newStackEnv(t)
	env.save(t, "shop", twoServices, "DB_PASSWORD=hunter2\n")

	stacks, err := env.svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(stacks) != 1 {
		t.Fatalf("stacks = %d, want 1", len(stacks))
	}
	if stacks[0].Env != "" {
		t.Error("a listing carried the .env, which routinely holds passwords")
	}
}

func TestStackCreateRefusesADuplicateName(t *testing.T) {
	env := newStackEnv(t)
	env.save(t, "shop", twoServices, "")

	_, err := env.svc.Create(context.Background(), StackInput{
		Name:    "shop",
		Source:  store.StackSourceEditor,
		Compose: twoServices,
	}, audit.Actor{Username: "admin"}, RequestMeta{})
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("error = %v, want a conflict: the name labels every container", err)
	}
}

func TestStackFileSourceReadsFromTheWhitelist(t *testing.T) {
	env := newStackEnv(t)

	dir := filepath.Join(env.root, "project")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, []byte("services:\n  app:\n    image: alpine:${TAG}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TAG=3.20\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	stack, err := env.svc.Create(context.Background(), StackInput{
		Name:   "fromfile",
		Source: store.StackSourceFile,
		Path:   path,
	}, audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if !strings.Contains(stack.Compose, "alpine:${TAG}") {
		t.Errorf("compose = %q, want the file's content", stack.Compose)
	}
	// The .env beside the compose file is compose's own convention.
	if !strings.Contains(stack.Env, "TAG=3.20") {
		t.Errorf("env = %q, want the neighboring .env", stack.Env)
	}
}

func TestStackFileSourceRefusesAPathOutsideTheWhitelist(t *testing.T) {
	env := newStackEnv(t)

	_, err := env.svc.Create(context.Background(), StackInput{
		Name:   "outside",
		Source: store.StackSourceFile,
		Path:   "/etc/compose.yaml",
	}, audit.Actor{Username: "admin"}, RequestMeta{})

	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error = %v (%T), want a whitelist refusal", err, err)
	}
}

func TestStackActStopsInReverseDependencyOrder(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	if _, err := env.deploy(t, stack, UpOptions{}); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	result, err := env.svc.Act(context.Background(), stack, StackStop,
		audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Act() error = %v", err)
	}

	// api uses db, so db goes down last.
	if got := result.Containers; len(got) != 2 || got[0] != "shop-api-1" || got[1] != "shop-db-1" {
		t.Errorf("stopped = %v, want the dependent first", got)
	}
}

func TestNormalizeStackName(t *testing.T) {
	for input, want := range map[string]string{
		"My Shop":     "my-shop",
		"  spaced  ":  "spaced",
		"UPPER_case":  "upper_case",
		"we!!ird//":   "weird",
		"dots.in.it":  "dots-in-it",
		"--trimmed--": "trimmed",
	} {
		if got := normalizeStackName(input); got != want {
			t.Errorf("normalizeStackName(%q) = %q, want %q", input, got, want)
		}
	}
}

// configHash must ignore what differs between replicas of one service,
// or every deploy would replace containers that had not changed.
func TestConfigHashIgnoresNameAndReplicaLabels(t *testing.T) {
	base := docker.ContainerSpec{
		Image:  "alpine",
		Labels: map[string]string{compose.LabelStack: "s"},
	}

	first := base
	first.Name = "s-app-1"
	first.Labels = replicaLabels(base.Labels, 1, "irrelevant")

	second := base
	second.Name = "s-app-2"
	second.Labels = replicaLabels(base.Labels, 2, "irrelevant")

	if configHash(first) != configHash(second) {
		t.Error("two replicas of one service hash differently; every deploy would replace them")
	}

	changed := base
	changed.Image = "alpine:3.20"
	if configHash(changed) == configHash(first) {
		t.Error("a changed image did not change the hash; a redeploy would skip it")
	}
}

// hasMessage matches an event by its message, for ordering assertions.
func hasMessage(message string) func(StackEvent) bool {
	return func(event StackEvent) bool { return event.Message == message }
}

// `docker compose up` on the command line produces the same labeled
// containers Iskele does, so a stack list that only showed this panel's own
// work would be lying about what is running.
func TestDiscoverFindsComposeProjectsStartedElsewhere(t *testing.T) {
	env := newStackEnv(t)

	path := filepath.Join(env.root, "compose.yaml")
	if err := os.WriteFile(path, []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	env.docker.Containers = []docker.Container{{
		ID: "x1", Name: "cli-app-1", State: "running",
		Labels: map[string]string{
			compose.LabelComposeProject: "cli",
			compose.LabelComposeService: "app",
			LabelComposeConfigFiles:     path,
		},
	}}

	found, err := env.svc.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("discovered = %+v, want one", found)
	}
	if found[0].Name != "cli" || found[0].Running != 1 || !found[0].Importable {
		t.Errorf("discovered = %+v, want an importable running project", found[0])
	}
}

func TestDiscoverSkipsStacksAlreadyRecorded(t *testing.T) {
	env := newStackEnv(t)
	env.save(t, "shop", twoServices, "")

	env.docker.Containers = []docker.Container{{
		ID: "x1", Name: "shop-db-1", State: "running",
		Labels: map[string]string{compose.LabelComposeProject: "shop"},
	}}

	found, err := env.svc.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(found) != 0 {
		t.Errorf("discovered = %+v, want none: the stack is already recorded", found)
	}
}

// A compose file outside allowed_paths cannot be read, and the reason belongs
// in the listing rather than in a failure after the operator clicks import.
func TestDiscoverExplainsAnUnimportableStack(t *testing.T) {
	env := newStackEnv(t)

	env.docker.Containers = []docker.Container{{
		ID: "x1", Name: "other-app-1", State: "running",
		Labels: map[string]string{
			compose.LabelComposeProject: "other",
			LabelComposeConfigFiles:     "/etc/compose.yaml",
		},
	}}

	found, err := env.svc.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(found) != 1 || found[0].Importable {
		t.Fatalf("discovered = %+v, want one that cannot be imported", found)
	}
	if found[0].Reason == "" {
		t.Error("no reason given; the operator has nothing to act on")
	}
}

func TestImportAdoptsADiscoveredStackWithoutTouchingIt(t *testing.T) {
	env := newStackEnv(t)

	path := filepath.Join(env.root, "compose.yaml")
	if err := os.WriteFile(path, []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	env.docker.Containers = []docker.Container{{
		ID: "x1", Name: "cli-app-1", State: "running",
		Labels: map[string]string{
			compose.LabelComposeProject: "cli",
			compose.LabelComposeService: "app",
			LabelComposeConfigFiles:     path,
		},
	}}

	stack, err := env.svc.Import(context.Background(), "cli",
		audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if stack.Source != store.StackSourceFile || stack.Path != path {
		t.Errorf("stack = %+v, want a file stack pointing at the discovered file", stack)
	}

	// Adoption is a record, not a redeploy.
	if len(env.docker.Containers) != 1 || env.docker.Containers[0].ID != "x1" {
		t.Errorf("containers = %+v, want the running one untouched", env.docker.Containers)
	}
}

func TestImportRefusesAnUnknownProject(t *testing.T) {
	env := newStackEnv(t)

	if _, err := env.svc.Import(context.Background(), "nope",
		audit.Actor{Username: "admin"}, RequestMeta{}); !errors.Is(err, ErrStackNotFound) {
		t.Errorf("error = %v, want ErrStackNotFound", err)
	}
}

// A stack's compose file, its services and its warnings are all knowable
// without the engine. An operator whose Docker is down still needs to read the
// file they are about to fix.
func TestStackDetailStillReadsWithoutTheEngine(t *testing.T) {
	env := newStackEnv(t)
	stack := env.save(t, "shop", twoServices, "")

	env.docker.Fail(fake.OpListContainers, errors.New("no docker here"))

	detail, err := env.svc.Detail(context.Background(), stack.ID)
	if err != nil {
		t.Fatalf("Detail() error = %v, want the definition anyway", err)
	}
	if len(detail.Services) != 2 {
		t.Errorf("services = %+v, want both from the file", detail.Services)
	}
	if detail.EngineError == "" {
		t.Error("engine_error is empty; the operator has no idea why nothing is listed")
	}
}
