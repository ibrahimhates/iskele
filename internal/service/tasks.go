package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ibrahimhates/iskele/internal/auth"
)

// Task errors.
var (
	ErrTaskNotFound = errors.New("task not found")
	ErrTaskFinished = errors.New("task has already finished")
)

// TaskState is where a long-running operation has got to.
type TaskState string

// Task states.
const (
	TaskRunning   TaskState = "running"
	TaskSucceeded TaskState = "succeeded"
	TaskFailed    TaskState = "failed"
	TaskCanceled  TaskState = "canceled"
)

// Terminal reports whether a state can still change.
func (s TaskState) Terminal() bool {
	return s == TaskSucceeded || s == TaskFailed || s == TaskCanceled
}

// Task is one long-running operation, as the UI's task drawer shows it.
type Task struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Target names what the task acts on: an image reference, a container.
	Target string    `json:"target"`
	State  TaskState `json:"state"`
	// Progress is 0..100, or -1 when the operation cannot report a fraction.
	Progress int    `json:"progress"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
	// Username records who started it, so a shared installation's drawer is
	// not a mystery.
	Username   string    `json:"username,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// Cancelable reports whether POST /tasks/{id}/cancel would do anything.
	Cancelable bool `json:"cancelable"`
}

// taskRetention is how long a finished task stays visible. Long enough for an
// operator to come back to a tab and see what happened, short enough that the
// drawer is not a log file.
const taskRetention = 10 * time.Minute

// maxTasks bounds the registry regardless of retention, so a script pulling in
// a loop cannot grow it without limit.
const maxTasks = 200

// TaskRegistry tracks long-running operations in memory.
//
// In memory, not in the database: a task is only meaningful while the daemon
// that runs it is alive. Restarting iskeled cancels every pull anyway, so
// persisting them would only produce rows that can never finish.
type TaskRegistry struct {
	mu    sync.Mutex
	tasks map[string]*trackedTask
}

// trackedTask is a task plus the handle that stops it.
type trackedTask struct {
	task   Task
	cancel context.CancelFunc
}

// NewTaskRegistry builds an empty registry.
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{tasks: map[string]*trackedTask{}}
}

// Start registers a task under a generated ID and returns a context that
// Cancel ends.
func (r *TaskRegistry) Start(ctx context.Context, kind, target, username string) (string, context.Context, error) {
	id, err := auth.NewID()
	if err != nil {
		return "", nil, err
	}
	return id, r.StartWithID(ctx, id, kind, target, username), nil
}

// StartWithID registers a task under an ID the caller already has.
//
// A build is the case this exists for: its record is created before it runs,
// and keying the task by the build's own ID is what lets POST
// /builds/{id}/cancel reach the work without a second identifier to carry
// around.
func (r *TaskRegistry) StartWithID(ctx context.Context, id, kind, target, username string) context.Context {
	taskCtx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.tasks[id] = &trackedTask{
		task: Task{
			ID:         id,
			Kind:       kind,
			Target:     target,
			State:      TaskRunning,
			Progress:   -1,
			Username:   username,
			StartedAt:  time.Now().UTC(),
			Cancelable: true,
		},
		cancel: cancel,
	}
	r.evictLocked()

	return taskCtx
}

// Progress updates a running task. Updates to a finished task are ignored:
// a late progress line must not resurrect something that already failed.
func (r *TaskRegistry) Progress(id string, percent int, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tracked, ok := r.tasks[id]
	if !ok || tracked.task.State.Terminal() {
		return
	}
	tracked.task.Progress = percent
	tracked.task.Message = message
}

// Finish moves a task to its terminal state. A nil error means success; a
// context cancellation is recorded as canceled rather than failed, because the
// operator asked for it.
func (r *TaskRegistry) Finish(id string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tracked, ok := r.tasks[id]
	if !ok || tracked.task.State.Terminal() {
		return
	}

	tracked.task.FinishedAt = time.Now().UTC()
	tracked.task.Cancelable = false

	switch {
	case err == nil:
		tracked.task.State = TaskSucceeded
		tracked.task.Progress = 100
	case errors.Is(err, context.Canceled):
		tracked.task.State = TaskCanceled
	default:
		tracked.task.State = TaskFailed
		tracked.task.Error = err.Error()
	}

	tracked.cancel()
}

// Cancel stops a running task.
func (r *TaskRegistry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tracked, ok := r.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	if tracked.task.State.Terminal() {
		return ErrTaskFinished
	}

	tracked.cancel()
	tracked.task.State = TaskCanceled
	tracked.task.FinishedAt = time.Now().UTC()
	tracked.task.Cancelable = false
	return nil
}

// Get returns one task.
func (r *TaskRegistry) Get(id string) (Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tracked, ok := r.tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return tracked.task, nil
}

// List returns every visible task, newest first.
func (r *TaskRegistry) List() []Task {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepLocked(time.Now().UTC())

	out := make([]Task, 0, len(r.tasks))
	for _, tracked := range r.tasks {
		out = append(out, tracked.task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// sweepLocked drops finished tasks past their retention.
func (r *TaskRegistry) sweepLocked(now time.Time) {
	for id, tracked := range r.tasks {
		if tracked.task.State.Terminal() && now.Sub(tracked.task.FinishedAt) > taskRetention {
			delete(r.tasks, id)
		}
	}
}

// evictLocked enforces the size cap by dropping the oldest finished tasks, and
// only those: a running task is still doing something.
func (r *TaskRegistry) evictLocked() {
	if len(r.tasks) <= maxTasks {
		return
	}

	finished := make([]*trackedTask, 0, len(r.tasks))
	for _, tracked := range r.tasks {
		if tracked.task.State.Terminal() {
			finished = append(finished, tracked)
		}
	}
	sort.Slice(finished, func(i, j int) bool {
		return finished[i].task.FinishedAt.Before(finished[j].task.FinishedAt)
	})

	for _, tracked := range finished {
		if len(r.tasks) <= maxTasks {
			return
		}
		delete(r.tasks, tracked.task.ID)
	}
}
