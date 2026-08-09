package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Build errors.
var (
	ErrBuildNotFound  = errors.New("build not found")
	ErrLogUnavailable = errors.New("this build's log is no longer archived")
)

// BuildLogRetention is how long an archived build log is kept.
//
// The row outlives the file: knowing that a build happened, and what it
// produced, stays cheap long after the megabytes of output stop being useful.
const BuildLogRetention = 30 * 24 * time.Hour

// BuildRowRetention is how long the record itself is kept.
const BuildRowRetention = 180 * 24 * time.Hour

// BuildRequest is what the operator asked to build.
type BuildRequest struct {
	// ContextDir is the host directory to build from. It is checked against
	// the whitelist before anything is read.
	ContextDir string `json:"context_dir"`
	// Dockerfile is relative to ContextDir. Empty means "Dockerfile".
	Dockerfile string            `json:"dockerfile"`
	Tags       []string          `json:"tags"`
	BuildArgs  map[string]string `json:"build_args"`
	Target     string            `json:"target"`
	NoCache    bool              `json:"no_cache"`
	Pull       bool              `json:"pull"`
	Platform   string            `json:"platform"`
	Labels     map[string]string `json:"labels"`
}

// Builder runs image builds and keeps their history.
type Builder struct {
	docker     docker.Client
	builds     *store.BuildRepo
	registries *Registry
	paths      *PathGuard
	tasks      *TaskRegistry
	recorder   *audit.Recorder
	// logDir is where build output is archived.
	logDir string
	// maxContextBytes caps a build context; zero means the default.
	maxContextBytes int64
}

// NewBuilder builds the build service.
func NewBuilder(client docker.Client, builds *store.BuildRepo, registries *Registry,
	paths *PathGuard, tasks *TaskRegistry, recorder *audit.Recorder, logDir string,
) *Builder {
	return &Builder{
		docker:     client,
		builds:     builds,
		registries: registries,
		paths:      paths,
		tasks:      tasks,
		recorder:   recorder,
		logDir:     logDir,
	}
}

// SetMaxContextBytes overrides the context size cap, for tests.
func (s *Builder) SetMaxContextBytes(n int64) { s.maxContextBytes = n }

// Start validates a build request and records it, without running it.
//
// Splitting this from Run means a request that will never work — a directory
// outside the whitelist, a missing Dockerfile — is refused with a plain HTTP
// error before a WebSocket is accepted, rather than as the first frame of a
// stream the client then has to interpret.
func (s *Builder) Start(ctx context.Context, req BuildRequest, actor audit.Actor, meta RequestMeta) (store.Build, error) {
	dir := strings.TrimSpace(req.ContextDir)
	if err := s.paths.Check(dir); err != nil {
		s.auditBuild(ctx, actor, meta, "build.start", store.Build{ContextDir: dir}, err)
		return store.Build{}, err
	}

	dockerfile := strings.TrimSpace(req.Dockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if filepath.IsAbs(dockerfile) || strings.Contains(filepath.ToSlash(dockerfile), "../") {
		return store.Build{}, &docker.SpecError{
			Field:   "dockerfile",
			Message: fmt.Sprintf("%q must be inside the build context", req.Dockerfile),
		}
	}
	if _, err := os.Stat(filepath.Join(dir, dockerfile)); err != nil {
		return store.Build{}, fmt.Errorf("%w: %s", ErrNoDockerfile, dockerfile)
	}

	id, err := auth.NewID()
	if err != nil {
		return store.Build{}, err
	}

	record := store.Build{
		ID:         id,
		UserID:     actor.UserID,
		Username:   actor.Username,
		ContextDir: dir,
		Dockerfile: dockerfile,
		Tags:       normalizeTags(req.Tags),
		Target:     strings.TrimSpace(req.Target),
		Platform:   strings.TrimSpace(req.Platform),
		NoCache:    req.NoCache,
		Pull:       req.Pull,
		Status:     store.BuildRunning,
	}

	if err := s.builds.Create(ctx, &record); err != nil {
		return store.Build{}, err
	}
	s.auditBuild(ctx, actor, meta, "build.start", record, nil)

	return record, nil
}

// Run executes a recorded build and streams the engine's output.
//
// The returned channels close when the build ends, whichever way it ends. The
// record's final status is written before they do, so a client that re-reads
// the build after the stream closes sees the outcome.
func (s *Builder) Run(ctx context.Context, record store.Build, req BuildRequest) (<-chan docker.BuildEvent, <-chan error) {
	events := make(chan docker.BuildEvent, 128)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		if err := s.run(ctx, record, req, events); err != nil {
			select {
			case errs <- err:
			default:
			}
		}
	}()

	return events, errs
}

func (s *Builder) run(ctx context.Context, record store.Build, req BuildRequest, out chan<- docker.BuildEvent) error {
	archive, archiveErr := s.openLog(record.ID)
	if archiveErr != nil {
		// A build is still worth running without its archive; the operator
		// just cannot re-read the log later.
		archive = nil
	}
	var logWriter *bufio.Writer
	if archive != nil {
		logWriter = bufio.NewWriter(archive)
		defer func() {
			_ = logWriter.Flush()
			_ = archive.Close()
		}()
	}

	// The context is packed into a pipe and streamed to the engine, so a large
	// tree never exists twice: once on disk and once in memory.
	pipeReader, pipeWriter := io.Pipe()
	statsCh := make(chan ContextStats, 1)

	go func() {
		stats, err := WriteBuildContext(pipeWriter, ContextOptions{
			Dir:        record.ContextDir,
			Dockerfile: record.Dockerfile,
			MaxBytes:   s.maxContextBytes,
		})
		statsCh <- stats
		_ = pipeWriter.CloseWithError(err)
	}()

	buildEvents, buildErrs := s.docker.BuildImage(ctx, docker.BuildOptions{
		Context:     pipeReader,
		Dockerfile:  record.Dockerfile,
		Tags:        record.Tags,
		BuildArgs:   toBuildArgs(req.BuildArgs),
		Target:      record.Target,
		NoCache:     record.NoCache,
		Pull:        record.Pull,
		Platform:    record.Platform,
		Labels:      req.Labels,
		Remove:      true,
		ForceRemove: true,
		Auth:        s.authForBuild(ctx),
	})

	var (
		imageID   string
		failure   error
		remaining = 2
	)

	// Both channels are drained to the end: the engine reports a failed build
	// inside a 200 response, so the failure can arrive alongside — or just
	// after — the last output line.
	for remaining > 0 {
		select {
		case <-ctx.Done():
			// The caller canceled. The engine stops when its context does;
			// the record is finished as canceled below.
			remaining = 0

		case event, ok := <-buildEvents:
			if !ok {
				buildEvents = nil
				remaining--
				continue
			}
			if event.ImageID != "" {
				imageID = event.ImageID
			}
			writeBuildLog(logWriter, event)

			select {
			case out <- event:
			case <-ctx.Done():
			}

		case err, ok := <-buildErrs:
			if !ok {
				buildErrs = nil
				remaining--
				continue
			}
			if err != nil && failure == nil {
				failure = err
			}
		}
	}

	// Draining the pipe's stats channel also reports a context that was too
	// large, which the engine would otherwise report as a truncated tar.
	select {
	case stats := <-statsCh:
		if stats.Files > 0 {
			_ = s.builds.SetContextStats(context.WithoutCancel(ctx), record.ID,
				stats.Files, stats.Bytes)
		}
	default:
	}
	_ = pipeReader.Close()

	// A finished build must be recorded even when the request that started it
	// is gone, so the write uses a context that outlives the cancellation.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	switch {
	case ctx.Err() != nil:
		_ = s.builds.Finish(finishCtx, record.ID, store.BuildCanceled, "", "canceled by the operator")
		return nil
	case failure != nil:
		_ = s.builds.Finish(finishCtx, record.ID, store.BuildFailed, "", docker.Message(failure))
		return failure
	default:
		_ = s.builds.Finish(finishCtx, record.ID, store.BuildSuccess, imageID, "")
		return nil
	}
}

// authForBuild collects the credentials for the registries a build might pull
// its base images from.
//
// Every configured registry is offered rather than guessing from the
// Dockerfile: deciding which one a build needs would mean parsing FROM lines —
// including the ARG interpolation in them — and getting it wrong produces an
// authentication failure the operator cannot explain.
func (s *Builder) authForBuild(ctx context.Context) map[string]docker.RegistryAuth {
	if s.registries == nil {
		return nil
	}

	entries, err := s.registries.repo.List(ctx)
	if err != nil || len(entries) == 0 {
		return nil
	}

	out := make(map[string]docker.RegistryAuth, len(entries))
	for _, entry := range entries {
		if entry.Username == "" && entry.Password == "" {
			continue
		}
		password := ""
		if entry.Password != "" {
			decrypted, decryptErr := s.registries.box.Decrypt(entry.Password)
			if decryptErr != nil {
				// One unreadable credential must not fail the whole build; the
				// registry it belongs to simply stays anonymous.
				continue
			}
			password = decrypted
		}
		out[entry.Server] = docker.RegistryAuth{
			Username:      entry.Username,
			Password:      password,
			ServerAddress: entry.Server,
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// Get returns one build record.
func (s *Builder) Get(ctx context.Context, id string) (store.Build, error) {
	record, err := s.builds.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Build{}, ErrBuildNotFound
	}
	return record, err
}

// List returns the build history, newest first.
func (s *Builder) List(ctx context.Context, filter store.BuildFilter) ([]store.Build, error) {
	return s.builds.List(ctx, filter)
}

// Cancel stops a running build.
//
// The cancellation reaches the engine through the task registry: the build's
// context is the task's, so ending the task ends the HTTP request to the
// daemon, which is what actually stops the work.
func (s *Builder) Cancel(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if record.Status.Terminal() {
		return fmt.Errorf("build %s already %s: %w", id, record.Status, ErrTaskFinished)
	}

	err = s.tasks.Cancel(id)
	if errors.Is(err, ErrTaskNotFound) {
		// The build is recorded as running but nothing is running it — the
		// daemon restarted mid-build. Closing the record is the honest
		// outcome; leaving it "running" forever is not.
		err = s.builds.Finish(ctx, id, store.BuildCanceled, "",
			"the daemon restarted while this build was running")
	}

	s.auditBuild(ctx, actor, meta, "build.cancel", record, err)
	return err
}

// LogPath is where a build's archived output lives.
func (s *Builder) LogPath(id string) string {
	return filepath.Join(s.logDir, id+".log")
}

// OpenLog opens a build's archived output for reading.
func (s *Builder) OpenLog(ctx context.Context, id string) (io.ReadCloser, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(s.LogPath(record.ID)) //nolint:gosec // the id is a generated hex string
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrLogUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("read build log: %w", err)
	}
	return file, nil
}

// openLog creates a build's archive file.
func (s *Builder) openLog(id string) (*os.File, error) {
	if s.logDir == "" {
		return nil, errors.New("no build log directory is configured")
	}
	if err := os.MkdirAll(s.logDir, 0o750); err != nil {
		return nil, fmt.Errorf("create build log dir: %w", err)
	}
	return os.OpenFile(s.LogPath(id), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640) //nolint:gosec // the id is a generated hex string
}

// writeBuildLog appends one event to the archive in the form the operator
// would have seen on a terminal.
func writeBuildLog(w *bufio.Writer, event docker.BuildEvent) {
	if w == nil {
		return
	}

	switch {
	case event.Stream != "":
		_, _ = w.WriteString(event.Stream)
	case event.Error != "":
		_, _ = w.WriteString("\nERROR: " + event.Error + "\n")
	case event.Status != "":
		line := event.Status
		if event.ID != "" {
			line = event.ID + ": " + line
		}
		_, _ = w.WriteString(line + "\n")
	}
}

// PruneLogs deletes archived logs past their retention and reports how many
// went. The rows stay: knowing a build happened is cheap.
func (s *Builder) PruneLogs(ctx context.Context, now time.Time) (int, error) {
	if s.logDir == "" {
		return 0, nil
	}

	entries, err := os.ReadDir(s.logDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read build log dir: %w", err)
	}

	cutoff := now.Add(-BuildLogRetention)
	removed := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			continue
		}

		path := filepath.Join(s.logDir, entry.Name())
		if rmErr := os.Remove(path); rmErr != nil {
			continue
		}
		removed++
		_ = s.builds.MarkLogRemoved(ctx, strings.TrimSuffix(entry.Name(), ".log"))
	}

	return removed, nil
}

// ReconcileRunning closes out builds left running by a process that is gone.
//
// A build is bound to the daemon that started it: the engine's build request
// dies with the process, so a row still marked running after a restart can
// never finish on its own.
func (s *Builder) ReconcileRunning(ctx context.Context) (int, error) {
	running, err := s.builds.Running(ctx)
	if err != nil {
		return 0, err
	}

	closed := 0
	for _, record := range running {
		if err := s.builds.Finish(ctx, record.ID, store.BuildCanceled, "",
			"iskeled restarted while this build was running"); err == nil {
			closed++
		}
	}
	return closed, nil
}

// auditBuild records a build lifecycle event.
func (s *Builder) auditBuild(ctx context.Context, actor audit.Actor, meta RequestMeta,
	action string, record store.Build, err error,
) {
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       action,
		ResourceType: "build",
		ResourceID:   record.ID,
		Err:          err,
		Detail: map[string]any{
			"context_dir": record.ContextDir,
			"dockerfile":  record.Dockerfile,
			"tags":        record.Tags,
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
}

// normalizeTags trims and drops the blanks a form produces.
func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// toBuildArgs converts the request's plain map into the engine's pointer form.
//
// The engine distinguishes an argument set to the empty string from one only
// declared, and a map of values cannot express that; every value here is set,
// which is what a form can actually produce.
func toBuildArgs(args map[string]string) map[string]*string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]*string, len(args))
	for key, value := range args {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		v := value
		out[key] = &v
	}
	return out
}
