package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
)

// StatsHistorySize is how many samples the server keeps per container, so a
// client that connects late still gets a chart instead of an empty axis.
const StatsHistorySize = 60

// Logs streams a container's output.
func (s *Container) Logs(ctx context.Context, id string, opts docker.LogOptions) (<-chan docker.LogLine, <-chan error) {
	id, err := normalizeID(id)
	if err != nil {
		return closedLines(err)
	}
	return s.docker.ContainerLogs(ctx, id, opts)
}

func closedLines(err error) (<-chan docker.LogLine, <-chan error) {
	lines := make(chan docker.LogLine)
	errs := make(chan error, 1)
	errs <- err
	close(lines)
	close(errs)
	return lines, errs
}

// Stats streams a container's resource usage.
func (s *Container) Stats(ctx context.Context, id string) (<-chan docker.Stats, <-chan error) {
	id, err := normalizeID(id)
	if err != nil {
		stats := make(chan docker.Stats)
		errs := make(chan error, 1)
		errs <- err
		close(stats)
		close(errs)
		return stats, errs
	}
	return s.docker.ContainerStats(ctx, id)
}

// Exec starts a command inside a container.
func (s *Container) Exec(ctx context.Context, id string, opts docker.ExecOptions, actor audit.Actor, meta RequestMeta) (*docker.ExecSession, error) {
	id, err := normalizeID(id)
	if err != nil {
		return nil, err
	}

	session, err := s.docker.Exec(ctx, id, opts)

	// An exec is a shell on the host's container: it is the single most
	// sensitive action in the panel, so it is audited whether it succeeds
	// or not.
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "container.exec",
		ResourceType: "container",
		ResourceID:   id,
		Err:          err,
		Detail:       map[string]any{"cmd": strings.Join(opts.Cmd, " "), "tty": opts.TTY, "user": opts.User},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})

	if err != nil {
		return nil, err
	}
	return session, nil
}

// ResizeExec changes a running exec's terminal size.
func (s *Container) ResizeExec(ctx context.Context, execID string, rows, cols uint) error {
	return s.docker.ResizeExec(ctx, execID, rows, cols)
}

// ExecExitCode reports a finished exec's status.
func (s *Container) ExecExitCode(ctx context.Context, execID string) (int, error) {
	return s.docker.ExecExitCode(ctx, execID)
}

// Events streams engine events.
func (s *System) Events(ctx context.Context) (<-chan docker.Event, <-chan error) {
	return s.docker.Events(ctx)
}

// BatchResult is one container's outcome in a bulk action.
type BatchResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// Batch applies one action to several containers.
//
// It never stops at the first failure: an operator who selected twenty
// containers wants the eighteen that can stop to stop, and a precise list of
// the two that could not.
func (s *Container) Batch(ctx context.Context, ids []string, action string, actor audit.Actor, meta RequestMeta) ([]BatchResult, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: no containers selected", ErrEmptyID)
	}

	apply, err := s.actionFunc(action)
	if err != nil {
		return nil, err
	}

	results := make([]BatchResult, 0, len(ids))
	for _, id := range ids {
		result := BatchResult{ID: id, OK: true}

		if actionErr := apply(ctx, id); actionErr != nil {
			result.OK = false
			result.Error = docker.Message(actionErr)
			result.Code = kindCode(actionErr)
		}

		s.recorder.Record(ctx, audit.Event{
			Actor:        actor,
			Action:       "container." + action,
			ResourceType: "container",
			ResourceID:   id,
			Err:          batchError(result),
			Detail:       map[string]any{"batch": true},
			IP:           meta.IP,
			UserAgent:    meta.UserAgent,
		})

		results = append(results, result)
	}
	return results, nil
}

// batchError turns a failed result back into an error for the audit record.
func batchError(r BatchResult) error {
	if r.OK {
		return nil
	}
	return fmt.Errorf("%s", r.Error)
}

// kindCode names the engine failure class, so the UI can offer the right
// recovery (force-remove, refresh the list, reconnect).
func kindCode(err error) string {
	switch docker.KindOf(err) {
	case docker.KindNotFound:
		return "NOT_FOUND"
	case docker.KindConflict:
		return "CONFLICT"
	case docker.KindUnavailable:
		return "DOCKER_UNAVAILABLE"
	case docker.KindPermission:
		return "FORBIDDEN"
	case docker.KindInvalid:
		return "BAD_REQUEST"
	default:
		return "DOCKER_ERROR"
	}
}

// actionFunc maps an action name onto the operation that performs it.
func (s *Container) actionFunc(action string) (func(context.Context, string) error, error) {
	// These are the unrecorded primitives: Batch writes its own audit entry
	// per container, and going through the recorded methods would put every
	// bulk action in the trail twice.
	switch action {
	case "start":
		return s.start, nil
	case "stop":
		return func(ctx context.Context, id string) error { return s.stop(ctx, id, nil) }, nil
	case "restart":
		return func(ctx context.Context, id string) error { return s.restart(ctx, id, nil) }, nil
	case "pause":
		return s.pause, nil
	case "unpause":
		return s.unpause, nil
	case "kill":
		return func(ctx context.Context, id string) error { return s.kill(ctx, id, "") }, nil
	case "remove":
		return func(ctx context.Context, id string) error {
			return s.remove(ctx, id, RemoveOptions{})
		}, nil
	default:
		return nil, fmt.Errorf("unknown batch action %q", action)
	}
}

// Pause freezes a running container's processes.
func (s *Container) Pause(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	return s.record(ctx, "container.pause", id, nil, actor, meta, s.pause)
}

// Unpause resumes a paused container.
func (s *Container) Unpause(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	return s.record(ctx, "container.unpause", id, nil, actor, meta, s.unpause)
}

// Kill sends a signal to a container's main process.
func (s *Container) Kill(ctx context.Context, id, signal string, actor audit.Actor, meta RequestMeta) error {
	detail := map[string]any(nil)
	if signal != "" {
		detail = map[string]any{"signal": signal}
	}
	return s.record(ctx, "container.kill", id, detail, actor, meta,
		func(ctx context.Context, id string) error { return s.kill(ctx, id, signal) })
}

// Rename changes a container's name.
func (s *Container) Rename(ctx context.Context, id, newName string, actor audit.Actor, meta RequestMeta) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("%w: the new name is required", ErrEmptyID)
	}
	return s.record(ctx, "container.rename", id, map[string]any{"name": newName}, actor, meta,
		func(ctx context.Context, id string) error { return s.docker.RenameContainer(ctx, id, newName) })
}

func (s *Container) pause(ctx context.Context, id string) error {
	return s.docker.PauseContainer(ctx, id)
}

func (s *Container) unpause(ctx context.Context, id string) error {
	return s.docker.UnpauseContainer(ctx, id)
}

func (s *Container) kill(ctx context.Context, id, signal string) error {
	return s.docker.KillContainer(ctx, id, signal)
}

// RedeployResult reports what a redeploy did.
type RedeployResult struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
	Image string `json:"image"`
	// RolledBack reports that the new container failed and the old one was
	// restored.
	RolledBack bool `json:"rolled_back"`
}

// Redeploy pulls a fresh image and recreates the container from its own
// definition (D-009).
//
// The old container is renamed rather than removed until the new one is
// running, so a failure leaves the operator with a working container instead
// of nothing at all.
func (s *Container) Redeploy(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) (RedeployResult, error) {
	id, err := normalizeID(id)
	if err != nil {
		return RedeployResult{}, err
	}

	spec, err := s.docker.RawInspectConfig(ctx, id)
	if err != nil {
		return RedeployResult{}, err
	}

	result := RedeployResult{OldID: id, Image: specImage(spec)}

	if result.Image != "" {
		if pullErr := s.docker.PullImage(ctx, result.Image); pullErr != nil {
			// A pull failure is worth reporting, but the recreate can still
			// succeed from the local image, which is what the operator asked
			// for when the registry is unreachable.
			s.recorder.Record(ctx, audit.Event{
				Actor: actor, Action: "image.pull", ResourceType: "image",
				ResourceID: result.Image, Err: pullErr, IP: meta.IP, UserAgent: meta.UserAgent,
			})
		}
	}

	// Free the name before recreating: two containers cannot share one.
	parked := spec.Name + "_old_" + time.Now().UTC().Format("20060102150405")
	if spec.Name != "" {
		if stopErr := s.docker.StopContainer(ctx, id, docker.StopOptions{}); stopErr != nil && !docker.IsConflict(stopErr) {
			return result, stopErr
		}
		if renameErr := s.docker.RenameContainer(ctx, id, parked); renameErr != nil {
			return result, renameErr
		}
	}

	newID, createErr := s.docker.CreateContainer(ctx, spec)
	if createErr != nil {
		err = createErr
		result.RolledBack = s.rollback(ctx, id, spec.Name)
		s.auditRedeploy(ctx, actor, meta, id, result, err)
		return result, err
	}
	result.NewID = newID

	if startErr := s.docker.StartContainer(ctx, newID); startErr != nil {
		err = startErr
		// Clean up the half-built replacement before restoring the original,
		// or the name will still be taken.
		_ = s.docker.RemoveContainer(ctx, newID, docker.RemoveContainerOptions{Force: true})
		result.NewID = ""
		result.RolledBack = s.rollback(ctx, id, spec.Name)
		s.auditRedeploy(ctx, actor, meta, id, result, err)
		return result, err
	}

	// The replacement is up; the parked original is no longer needed.
	if err := s.docker.RemoveContainer(ctx, id, docker.RemoveContainerOptions{Force: true}); err != nil {
		s.recorder.Record(ctx, audit.Event{
			Actor: actor, Action: "container.remove", ResourceType: "container",
			ResourceID: id, Err: err,
			Detail: map[string]any{"note": "the replaced container could not be removed", "parked_as": parked},
			IP:     meta.IP, UserAgent: meta.UserAgent,
		})
	}

	s.auditRedeploy(ctx, actor, meta, id, result, nil)
	return result, nil
}

// rollback restores the original container's name and starts it again.
func (s *Container) rollback(ctx context.Context, id, originalName string) bool {
	if originalName == "" {
		return false
	}
	if err := s.docker.RenameContainer(ctx, id, originalName); err != nil {
		return false
	}
	return s.docker.StartContainer(ctx, id) == nil
}

func (s *Container) auditRedeploy(ctx context.Context, actor audit.Actor, meta RequestMeta, id string, result RedeployResult, err error) {
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "container.redeploy",
		ResourceType: "container",
		ResourceID:   id,
		Err:          err,
		Detail: map[string]any{
			"image":       result.Image,
			"new_id":      result.NewID,
			"rolled_back": result.RolledBack,
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
}

// specImage reports the image a spec will run, or "" when unknown.
func specImage(spec docker.CreateSpec) string {
	if spec.Config == nil {
		return ""
	}
	return spec.Config.Image
}
