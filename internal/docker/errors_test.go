package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
)

func TestClassifyMapsEngineErrorsToKinds(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Kind
	}{
		{"not found", fmt.Errorf("wrapped: %w", cerrdefs.ErrNotFound), KindNotFound},
		{"conflict", fmt.Errorf("wrapped: %w", cerrdefs.ErrConflict), KindConflict},
		{"invalid", fmt.Errorf("wrapped: %w", cerrdefs.ErrInvalidArgument), KindInvalid},
		{"permission", fmt.Errorf("wrapped: %w", cerrdefs.ErrPermissionDenied), KindPermission},
		{"unavailable", fmt.Errorf("wrapped: %w", cerrdefs.ErrUnavailable), KindUnavailable},
		{"canceled", context.Canceled, KindCanceled},
		{"deadline", context.DeadlineExceeded, KindCanceled},
		{"unknown", errors.New("something else"), KindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify("container.start", "container", "abc", tt.err)
			if KindOf(got) != tt.want {
				t.Errorf("KindOf() = %v, want %v", KindOf(got), tt.want)
			}
			if !errors.Is(got, tt.err) {
				t.Error("the original error is no longer reachable through errors.Is")
			}
		})
	}
}

func TestClassifyReturnsNilForNilError(t *testing.T) {
	if err := classify("container.start", "container", "abc", nil); err != nil {
		t.Errorf("classify(nil) = %v, want nil", err)
	}
}

func TestNotFoundMessageNamesTheResource(t *testing.T) {
	err := classify("container.inspect", "container", "abc", fmt.Errorf("x: %w", cerrdefs.ErrNotFound))

	if got := Message(err); got != "no such container: abc" {
		t.Errorf("Message() = %q, want %q", got, "no such container: abc")
	}
	if Resource(err) != "container" {
		t.Errorf("Resource() = %q, want %q", Resource(err), "container")
	}
}

func TestClassifyPreservesEngineMessage(t *testing.T) {
	// The engine's own wording is what the operator needs to act on, so it
	// must survive classification untouched for non-404s.
	original := "driver failed programming external connectivity on endpoint web: port is already allocated"
	err := classify("container.start", "container", "web", errors.New(original))

	if got := Message(err); got != original {
		t.Errorf("Message() = %q, want the engine text preserved", got)
	}
}

func TestPredicateHelpers(t *testing.T) {
	cases := []struct {
		kind      Kind
		predicate func(error) bool
		name      string
	}{
		{KindNotFound, IsNotFound, "IsNotFound"},
		{KindConflict, IsConflict, "IsConflict"},
		{KindUnavailable, IsUnavailable, "IsUnavailable"},
		{KindInvalid, IsInvalid, "IsInvalid"},
		{KindPermission, IsPermission, "IsPermission"},
		{KindCanceled, IsCanceled, "IsCanceled"},
	}

	for _, c := range cases {
		err := NewError(c.kind, "op", "container", "id", "msg")
		if !c.predicate(err) {
			t.Errorf("%s() = false for kind %v", c.name, c.kind)
		}
		other := NewError(KindUnknown, "op", "container", "id", "msg")
		if c.predicate(other) {
			t.Errorf("%s() = true for KindUnknown", c.name)
		}
	}
}

func TestErrorStringIncludesOpAndID(t *testing.T) {
	err := NewError(KindNotFound, "container.inspect", "container", "abc", "no such container: abc")

	s := err.Error()
	if !strings.Contains(s, "container.inspect") || !strings.Contains(s, "abc") {
		t.Errorf("Error() = %q, want it to name the operation and the id", s)
	}
}

func TestUnavailableCarriesTheDockerGroupHint(t *testing.T) {
	err := unavailable("docker.ping", "unix:///var/run/docker.sock", errors.New("permission denied"))

	msg := Message(err)
	if !strings.Contains(msg, "docker") || !strings.Contains(msg, "group") {
		t.Errorf("Message() = %q, want the docker-group hint", msg)
	}
	if !strings.Contains(msg, "unix:///var/run/docker.sock") {
		t.Errorf("Message() = %q, want it to name the endpoint", msg)
	}
	if !IsUnavailable(err) {
		t.Error("unavailable() produced an error that is not KindUnavailable")
	}
}

func TestMessageAndResourceForPlainErrors(t *testing.T) {
	plain := errors.New("boom")

	if got := Message(plain); got != "boom" {
		t.Errorf("Message() = %q, want the plain error text", got)
	}
	if got := Resource(plain); got != "" {
		t.Errorf("Resource() = %q, want empty for a non-engine error", got)
	}
	if got := Message(nil); got != "" {
		t.Errorf("Message(nil) = %q, want empty", got)
	}
	if KindOf(plain) != KindUnknown {
		t.Errorf("KindOf() = %v, want KindUnknown", KindOf(plain))
	}
}
