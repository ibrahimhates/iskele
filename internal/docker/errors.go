package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	dockerclient "github.com/docker/docker/client"
)

// Kind classifies an engine failure so callers can map it onto an HTTP status
// without depending on the Docker SDK.
type Kind int

const (
	// KindUnknown is any failure that does not fit the categories below.
	KindUnknown Kind = iota
	// KindNotFound means the container, image, volume or network is gone.
	KindNotFound
	// KindConflict means the engine refused because of the resource's current
	// state (a running container being removed, a volume still in use, ...).
	KindConflict
	// KindInvalid means the request itself was malformed or rejected.
	KindInvalid
	// KindPermission means the daemon denied the operation.
	KindPermission
	// KindUnavailable means the daemon could not be reached at all.
	KindUnavailable
	// KindCanceled means the caller's context ended before the engine answered.
	KindCanceled
)

// Error wraps an engine failure with enough context to render a good message.
//
// Message deliberately preserves the engine's own text: hiding it would leave
// the operator guessing, and the daemon is a trusted local component.
type Error struct {
	Kind     Kind
	Op       string
	Resource string
	ID       string
	Message  string
	err      error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Op)
	if e.ID != "" {
		b.WriteString(" ")
		b.WriteString(e.ID)
	}
	b.WriteString(": ")
	b.WriteString(e.Message)
	return b.String()
}

// Unwrap exposes the SDK error for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.err }

// unavailableHint is appended when the daemon cannot be reached, because the
// cause is almost always the service account missing from the docker group.
const unavailableHint = "cannot reach the Docker daemon at %s: %v\n" +
	"check that Docker is running and that the user iskeled runs as is a member of the 'docker' group " +
	"(usermod -aG docker iskele), or set docker_host to the right endpoint"

// classify turns an SDK error into an *Error carrying the right Kind.
func classify(op, resource, id string, err error) error {
	if err == nil {
		return nil
	}

	e := &Error{Op: op, Resource: resource, ID: id, Message: err.Error(), err: err}

	switch {
	case errors.Is(err, context.Canceled):
		e.Kind = KindCanceled
	case errors.Is(err, context.DeadlineExceeded):
		e.Kind = KindCanceled
	case dockerclient.IsErrConnectionFailed(err):
		e.Kind = KindUnavailable
	case cerrdefs.IsNotFound(err):
		e.Kind = KindNotFound
		if id != "" {
			e.Message = fmt.Sprintf("no such %s: %s", resource, id)
		}
	case cerrdefs.IsConflict(err):
		e.Kind = KindConflict
	case cerrdefs.IsInvalidArgument(err):
		e.Kind = KindInvalid
	case cerrdefs.IsPermissionDenied(err):
		e.Kind = KindPermission
	case cerrdefs.IsUnavailable(err):
		e.Kind = KindUnavailable
	default:
		e.Kind = KindUnknown
	}
	return e
}

// NewError builds an engine error directly. Production code goes through
// classify; this exists so fakes and tests can produce the same shapes.
func NewError(kind Kind, op, resource, id, message string) error {
	return &Error{Kind: kind, Op: op, Resource: resource, ID: id, Message: message}
}

// unavailable builds the daemon-unreachable error, with the actionable hint.
func unavailable(op, host string, err error) error {
	return &Error{
		Kind:    KindUnavailable,
		Op:      op,
		Message: fmt.Sprintf(unavailableHint, host, err),
		err:     err,
	}
}

// KindOf reports the classification of err, or KindUnknown when err is not an
// engine error.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

// IsNotFound reports whether err is a missing-resource failure.
func IsNotFound(err error) bool { return KindOf(err) == KindNotFound }

// IsConflict reports whether the engine refused because of resource state.
func IsConflict(err error) bool { return KindOf(err) == KindConflict }

// IsUnavailable reports whether the daemon could not be reached.
func IsUnavailable(err error) bool { return KindOf(err) == KindUnavailable }

// IsInvalid reports whether the engine rejected the request as malformed.
func IsInvalid(err error) bool { return KindOf(err) == KindInvalid }

// IsPermission reports whether the daemon denied the operation.
func IsPermission(err error) bool { return KindOf(err) == KindPermission }

// IsCanceled reports whether the caller's context ended first.
func IsCanceled(err error) bool { return KindOf(err) == KindCanceled }

// Message returns the engine's own error text, or err.Error() for non-engine
// errors. Handlers show this to the operator verbatim.
func Message(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// Resource returns the resource kind an engine error refers to ("container",
// "image", ...), or "" when unknown.
func Resource(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Resource
	}
	return ""
}
