package service

import (
	"context"
	"errors"
	"testing"
)

func TestTaskStartsRunningAndCancelable(t *testing.T) {
	registry := NewTaskRegistry()

	id, ctx, err := registry.Start(context.Background(), "image.pull", "nginx:1.27", "admin")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	task, err := registry.Get(id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if task.State != TaskRunning || !task.Cancelable {
		t.Errorf("task = %+v, want a running, cancelable task", task)
	}
	if task.Progress != -1 {
		t.Errorf("Progress = %d, want -1 before anything is known", task.Progress)
	}
	if task.Target != "nginx:1.27" || task.Username != "admin" {
		t.Errorf("task = %+v", task)
	}
	if ctx.Err() != nil {
		t.Errorf("the task context is already done: %v", ctx.Err())
	}
}

func TestCancelEndsTheTaskAndItsContext(t *testing.T) {
	registry := NewTaskRegistry()
	id, ctx, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")

	if err := registry.Cancel(id); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v, want the work to have been told to stop", ctx.Err())
	}

	task, _ := registry.Get(id)
	if task.State != TaskCanceled || task.Cancelable {
		t.Errorf("task = %+v, want a canceled task that can no longer be canceled", task)
	}
}

func TestCancelingAFinishedTaskIsAConflict(t *testing.T) {
	registry := NewTaskRegistry()
	id, _, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")
	registry.Finish(id, nil)

	if err := registry.Cancel(id); !errors.Is(err, ErrTaskFinished) {
		t.Errorf("Cancel() = %v, want ErrTaskFinished", err)
	}
}

func TestCancelingAnUnknownTaskIsNotFound(t *testing.T) {
	registry := NewTaskRegistry()

	if err := registry.Cancel("nope"); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("Cancel() = %v, want ErrTaskNotFound", err)
	}
}

func TestFinishRecordsTheOutcome(t *testing.T) {
	cases := map[string]struct {
		err   error
		state TaskState
	}{
		"success":      {nil, TaskSucceeded},
		"failure":      {errors.New("manifest unknown"), TaskFailed},
		"cancellation": {context.Canceled, TaskCanceled},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			registry := NewTaskRegistry()
			id, _, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")

			registry.Finish(id, tc.err)

			task, _ := registry.Get(id)
			if task.State != tc.state {
				t.Errorf("State = %q, want %q", task.State, tc.state)
			}
			if task.FinishedAt.IsZero() {
				t.Error("FinishedAt was not set")
			}
			if tc.err != nil && !errors.Is(tc.err, context.Canceled) && task.Error == "" {
				t.Error("the failure message was not recorded")
			}
		})
	}
}

// A late progress line from a goroutine that has not noticed the cancellation
// must not make a finished task look alive again.
func TestProgressAfterFinishIsIgnored(t *testing.T) {
	registry := NewTaskRegistry()
	id, _, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")

	registry.Finish(id, errors.New("failed"))
	registry.Progress(id, 50, "Downloading")

	task, _ := registry.Get(id)
	if task.State != TaskFailed {
		t.Errorf("State = %q, want it to stay failed", task.State)
	}
	if task.Progress == 50 {
		t.Error("progress from after the failure was applied")
	}
}

// The same reasoning for Finish: the first terminal state is the real one.
func TestFinishIsIdempotent(t *testing.T) {
	registry := NewTaskRegistry()
	id, _, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")

	registry.Finish(id, errors.New("first failure"))
	registry.Finish(id, nil)

	task, _ := registry.Get(id)
	if task.State != TaskFailed || task.Error != "first failure" {
		t.Errorf("task = %+v, want the first outcome to stand", task)
	}
}

func TestListIsNewestFirst(t *testing.T) {
	registry := NewTaskRegistry()

	first, _, _ := registry.Start(context.Background(), "image.pull", "a", "admin")
	second, _, _ := registry.Start(context.Background(), "image.pull", "b", "admin")

	tasks := registry.List()
	if len(tasks) != 2 {
		t.Fatalf("List() returned %d tasks", len(tasks))
	}
	if tasks[0].ID != second || tasks[1].ID != first {
		t.Errorf("order = %q, %q; want the newest first", tasks[0].Target, tasks[1].Target)
	}
}

func TestProgressIsReported(t *testing.T) {
	registry := NewTaskRegistry()
	id, _, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")

	registry.Progress(id, 42, "Downloading")

	task, _ := registry.Get(id)
	if task.Progress != 42 || task.Message != "Downloading" {
		t.Errorf("task = %+v", task)
	}
}

func TestSucceedingSetsProgressToComplete(t *testing.T) {
	registry := NewTaskRegistry()
	id, _, _ := registry.Start(context.Background(), "image.pull", "nginx", "admin")

	registry.Progress(id, 80, "Extracting")
	registry.Finish(id, nil)

	task, _ := registry.Get(id)
	if task.Progress != 100 {
		t.Errorf("Progress = %d, want a finished task to read 100", task.Progress)
	}
}

// A canceled parent context must reach the task's own, so an HTTP request that
// goes away stops the work it started.
func TestTaskContextInheritsCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	registry := NewTaskRegistry()

	_, ctx, _ := registry.Start(parent, "image.pull", "nginx", "admin")
	cancelParent()

	<-ctx.Done()
}
