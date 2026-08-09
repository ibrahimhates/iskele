package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/store"
)

// buildEnv is a builder over a real database and a fake engine.
type buildEnv struct {
	builder *Builder
	docker  *fake.Client
	db      *store.DB
	root    string
	logDir  string
}

func newBuildEnv(t *testing.T, files map[string]string) *buildEnv {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	root := buildTree(t, files)
	logDir := filepath.Join(t.TempDir(), "builds")
	f := fake.New()

	return &buildEnv{
		builder: NewBuilder(f, db.Builds, nil, NewPathGuard([]string{root}),
			NewTaskRegistry(), nil, logDir),
		docker: f,
		db:     db,
		root:   root,
		logDir: logDir,
	}
}

// runToCompletion starts and runs a build, draining both channels.
func (e *buildEnv) runToCompletion(t *testing.T, req BuildRequest) (store.Build, error) {
	t.Helper()
	ctx := context.Background()

	record, err := e.builder.Start(ctx, req, audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		return store.Build{}, err
	}

	events, errs := e.builder.Run(ctx, record, req)
	for range events {
	}
	var failure error
	for err := range errs {
		if err != nil && failure == nil {
			failure = err
		}
	}

	final, getErr := e.builder.Get(ctx, record.ID)
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	return final, failure
}

const simpleDockerfile = "FROM alpine:3.20\nCOPY . /app\n"

func TestBuildRecordsSuccess(t *testing.T) {
	env := newBuildEnv(t, map[string]string{
		"Dockerfile":  simpleDockerfile,
		"app/main.go": "package main\n",
	})

	record, err := env.runToCompletion(t, BuildRequest{
		ContextDir: env.root,
		Tags:       []string{"app:v1", "  ", "app:latest"},
	})
	if err != nil {
		t.Fatalf("build error = %v", err)
	}

	if record.Status != store.BuildSuccess {
		t.Errorf("status = %q, want success", record.Status)
	}
	if record.ImageID != "sha256:built1" {
		t.Errorf("image_id = %q, want the built image", record.ImageID)
	}
	if len(record.Tags) != 2 {
		t.Errorf("tags = %v, want the blank one dropped", record.Tags)
	}
	if record.FinishedAt.IsZero() {
		t.Error("finished_at was not set")
	}
	if record.ContextFiles == 0 {
		t.Error("the context stats were not recorded")
	}
}

// The engine reports a failed build inside a 200 response, so the record has
// to be driven from the stream rather than from an HTTP status.
func TestBuildRecordsFailureWithTheEnginesMessage(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})
	env.docker.SetBuildEvents([]docker.BuildEvent{
		{Stream: "Step 1/2 : FROM alpine\n", Step: 1, TotalSteps: 2},
		{Error: "The command '/bin/sh -c false' returned a non-zero code: 1"},
	})

	record, err := env.runToCompletion(t, BuildRequest{ContextDir: env.root})
	if err == nil {
		t.Fatal("build error = nil, want the engine's failure")
	}

	if record.Status != store.BuildFailed {
		t.Errorf("status = %q, want failed", record.Status)
	}
	if !strings.Contains(record.Error, "non-zero code") {
		t.Errorf("error = %q, want the engine's own message", record.Error)
	}
	if record.ImageID != "" {
		t.Errorf("image_id = %q on a failed build", record.ImageID)
	}
}

func TestBuildRefusesAContextOutsideTheWhitelist(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	_, err := env.builder.Start(context.Background(),
		BuildRequest{ContextDir: "/etc"}, audit.Actor{}, RequestMeta{})

	if !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("error = %v, want the path refusal", err)
	}
	if len(env.docker.Calls()) != 0 {
		t.Error("the engine was reached for a refused context")
	}
}

// A request that can never work is refused before a stream is opened, so the
// client gets a plain HTTP error rather than a socket that immediately dies.
func TestBuildRefusesAMissingDockerfileUpFront(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"main.go": "package main\n"})

	_, err := env.builder.Start(context.Background(),
		BuildRequest{ContextDir: env.root}, audit.Actor{}, RequestMeta{})

	if !errors.Is(err, ErrNoDockerfile) {
		t.Errorf("error = %v, want ErrNoDockerfile", err)
	}
}

func TestBuildRefusesADockerfileOutsideTheContext(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	for _, name := range []string{"../Dockerfile", "/etc/Dockerfile"} {
		_, err := env.builder.Start(context.Background(),
			BuildRequest{ContextDir: env.root, Dockerfile: name}, audit.Actor{}, RequestMeta{})

		var specErr *docker.SpecError
		if !errors.As(err, &specErr) {
			t.Errorf("dockerfile %q: error = %v, want a SpecError", name, err)
		}
	}
}

func TestBuildPacksTheContextForTheEngine(t *testing.T) {
	env := newBuildEnv(t, map[string]string{
		"Dockerfile":    simpleDockerfile,
		".dockerignore": "secret.txt\n",
		"app.js":        "console.log(1)\n",
		"secret.txt":    "shh\n",
	})

	if _, err := env.runToCompletion(t, BuildRequest{ContextDir: env.root}); err != nil {
		t.Fatalf("build error = %v", err)
	}

	contexts := env.docker.BuiltContexts()
	if len(contexts) != 1 {
		t.Fatalf("the engine received %d contexts", len(contexts))
	}
	packed := string(contexts[0])
	if !strings.Contains(packed, "app.js") {
		t.Error("app.js is missing from the context the engine received")
	}
	if strings.Contains(packed, "shh") {
		t.Error(".dockerignore was not applied to the context the engine received")
	}
}

func TestBuildArchivesItsLog(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	record, err := env.runToCompletion(t, BuildRequest{ContextDir: env.root})
	if err != nil {
		t.Fatalf("build error = %v", err)
	}

	reader, err := env.builder.OpenLog(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("OpenLog() error = %v", err)
	}
	defer func() { _ = reader.Close() }()

	buf := make([]byte, 4096)
	n, _ := reader.Read(buf)
	log := string(buf[:n])

	if !strings.Contains(log, "Step 1/3") {
		t.Errorf("the archived log is missing the build output: %q", log)
	}
	if !record.LogArchived {
		t.Error("log_archived was not set on the record")
	}
}

func TestOpenLogForAnUnknownBuildIsNotFound(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	_, err := env.builder.OpenLog(context.Background(), "nope")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Errorf("error = %v, want ErrBuildNotFound", err)
	}
}

// Retention removes the file long before the row: knowing a build happened
// stays cheap after the megabytes stop being useful.
func TestPruneLogsRemovesOldArchivesButKeepsTheRows(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	record, err := env.runToCompletion(t, BuildRequest{ContextDir: env.root})
	if err != nil {
		t.Fatalf("build error = %v", err)
	}

	// Age the archive past its retention.
	old := time.Now().Add(-BuildLogRetention - time.Hour)
	if chErr := os.Chtimes(env.builder.LogPath(record.ID), old, old); chErr != nil {
		t.Fatalf("chtimes: %v", chErr)
	}

	removed, err := env.builder.PruneLogs(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("PruneLogs() error = %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	after, err := env.builder.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("the record went with the log: %v", err)
	}
	if after.LogArchived {
		t.Error("log_archived is still set after the file was removed")
	}
	if _, err := env.builder.OpenLog(context.Background(), record.ID); !errors.Is(err, ErrLogUnavailable) {
		t.Errorf("OpenLog() = %v, want ErrLogUnavailable", err)
	}
}

func TestPruneLogsKeepsRecentArchives(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	record, err := env.runToCompletion(t, BuildRequest{ContextDir: env.root})
	if err != nil {
		t.Fatalf("build error = %v", err)
	}

	removed, err := env.builder.PruneLogs(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("PruneLogs() error = %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want a fresh log kept", removed)
	}
	if _, err := env.builder.OpenLog(context.Background(), record.ID); err != nil {
		t.Errorf("OpenLog() error = %v", err)
	}
}

// Canceling a build ends the context the engine's request is on, and the
// record has to say canceled rather than failed.
func TestCancelStopsARunningBuild(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})
	ctx := context.Background()

	record, err := env.builder.Start(ctx, BuildRequest{ContextDir: env.root},
		audit.Actor{Username: "admin"}, RequestMeta{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// The build runs under a task keyed by its own id, which is how
	// POST /builds/{id}/cancel reaches the work.
	buildCtx := env.builder.tasks.StartWithID(ctx, record.ID, "image.build", record.ID, "admin")

	events, errs := env.builder.Run(buildCtx, record, BuildRequest{ContextDir: env.root})

	if cancelErr := env.builder.Cancel(ctx, record.ID, audit.Actor{}, RequestMeta{}); cancelErr != nil {
		t.Fatalf("Cancel() error = %v", cancelErr)
	}

	for range events {
	}
	for range errs {
	}

	final, err := env.builder.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if final.Status != store.BuildCanceled {
		t.Errorf("status = %q, want canceled", final.Status)
	}
}

func TestCancelAFinishedBuildIsRefused(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	record, err := env.runToCompletion(t, BuildRequest{ContextDir: env.root})
	if err != nil {
		t.Fatalf("build error = %v", err)
	}

	err = env.builder.Cancel(context.Background(), record.ID, audit.Actor{}, RequestMeta{})
	if !errors.Is(err, ErrTaskFinished) {
		t.Errorf("error = %v, want ErrTaskFinished", err)
	}
}

func TestCancelAnUnknownBuildIsNotFound(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	err := env.builder.Cancel(context.Background(), "nope", audit.Actor{}, RequestMeta{})
	if !errors.Is(err, ErrBuildNotFound) {
		t.Errorf("error = %v, want ErrBuildNotFound", err)
	}
}

// A build is bound to the process that started it: a row still marked running
// after a restart can never finish on its own.
func TestReconcileClosesBuildsLeftRunningByAGoneProcess(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})
	ctx := context.Background()

	record, err := env.builder.Start(ctx, BuildRequest{ContextDir: env.root},
		audit.Actor{}, RequestMeta{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	closed, err := env.builder.ReconcileRunning(ctx)
	if err != nil {
		t.Fatalf("ReconcileRunning() error = %v", err)
	}
	if closed != 1 {
		t.Errorf("closed = %d, want 1", closed)
	}

	final, _ := env.builder.Get(ctx, record.ID)
	if final.Status != store.BuildCanceled {
		t.Errorf("status = %q, want canceled", final.Status)
	}
	if !strings.Contains(final.Error, "restarted") {
		t.Errorf("error = %q, want it to say why", final.Error)
	}
}

func TestBuildListIsNewestFirst(t *testing.T) {
	env := newBuildEnv(t, map[string]string{"Dockerfile": simpleDockerfile})

	first, _ := env.runToCompletion(t, BuildRequest{ContextDir: env.root, Tags: []string{"a:1"}})
	second, _ := env.runToCompletion(t, BuildRequest{ContextDir: env.root, Tags: []string{"b:1"}})

	builds, err := env.builder.List(context.Background(), store.BuildFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("got %d builds", len(builds))
	}
	if builds[0].ID != second.ID || builds[1].ID != first.ID {
		t.Error("builds are not newest first")
	}
}

func TestToBuildArgsDropsBlankKeys(t *testing.T) {
	args := toBuildArgs(map[string]string{"VERSION": "1.2.3", "  ": "dropped", "EMPTY": ""})

	if len(args) != 2 {
		t.Fatalf("args = %v", args)
	}
	if args["VERSION"] == nil || *args["VERSION"] != "1.2.3" {
		t.Errorf("VERSION = %v", args["VERSION"])
	}
	// An argument set to the empty string is set, not absent.
	if args["EMPTY"] == nil || *args["EMPTY"] != "" {
		t.Errorf("EMPTY = %v, want an explicit empty value", args["EMPTY"])
	}
}
