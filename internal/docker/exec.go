package docker

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
)

// Exec starts a command inside a container and attaches to it.
func (e *engine) Exec(ctx context.Context, id string, opts ExecOptions) (*ExecSession, error) {
	cmd := opts.Cmd
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}

	createOpts := container.ExecOptions{
		Cmd:          cmd,
		Tty:          opts.TTY,
		User:         opts.User,
		WorkingDir:   opts.WorkingDir,
		Env:          opts.Env,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}
	if opts.TTY && opts.Rows > 0 && opts.Cols > 0 {
		// Sizing at creation means the shell's first prompt is already the
		// right shape, instead of redrawing after the first resize message.
		size := [2]uint{opts.Rows, opts.Cols}
		createOpts.ConsoleSize = &size
	}

	created, err := e.api.ContainerExecCreate(ctx, id, createOpts)
	if err != nil {
		return nil, classify("container.exec", "container", id, err)
	}

	attached, err := e.api.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: opts.TTY})
	if err != nil {
		return nil, classify("container.exec", "container", id, err)
	}

	return &ExecSession{
		ID:     created.ID,
		Conn:   attached.Conn,
		Reader: attached.Reader,
		TTY:    opts.TTY,
		Close:  attached.Close,
	}, nil
}

// ResizeExec changes the pseudo-terminal size of a running exec.
func (e *engine) ResizeExec(ctx context.Context, execID string, rows, cols uint) error {
	err := e.api.ContainerExecResize(ctx, execID, container.ResizeOptions{Height: rows, Width: cols})
	return classify("container.exec_resize", "container", execID, err)
}

// ExecExitCode reports the exit status of a finished exec.
func (e *engine) ExecExitCode(ctx context.Context, execID string) (int, error) {
	inspect, err := e.api.ContainerExecInspect(ctx, execID)
	if err != nil {
		return 0, classify("container.exec_inspect", "container", execID, err)
	}
	return inspect.ExitCode, nil
}

// Events streams engine events until ctx ends.
func (e *engine) Events(ctx context.Context) (<-chan Event, <-chan error) {
	out := make(chan Event, 64)
	errs := make(chan error, 1)

	messages, sdkErrs := e.api.Events(ctx, events.ListOptions{})

	go func() {
		defer close(out)
		defer close(errs)

		for {
			select {
			case <-ctx.Done():
				return

			case msg, ok := <-messages:
				if !ok {
					return
				}
				select {
				case out <- toEvent(msg):
				case <-ctx.Done():
					return
				}

			case err, ok := <-sdkErrs:
				if !ok {
					return
				}
				if wrapped := endOfStream(err); wrapped != nil {
					select {
					case errs <- classify("system.events", "system", "", wrapped):
					default:
					}
				}
				return
			}
		}
	}()

	return out, errs
}

func toEvent(m events.Message) Event {
	e := Event{
		Type:   string(m.Type),
		Action: string(m.Action),
		Actor:  m.Actor.ID,
		Scope:  m.Scope,
		Attrs:  m.Actor.Attributes,
		Time:   time.Unix(0, m.TimeNano).UTC(),
	}
	if e.Time.IsZero() || m.TimeNano == 0 {
		e.Time = time.Unix(m.Time, 0).UTC()
	}
	// The engine puts the human-readable name in the attribute bag; lifting it
	// out saves every consumer the same lookup.
	if name, ok := m.Actor.Attributes["name"]; ok {
		e.Name = name
	}
	return e
}
