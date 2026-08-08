package docker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectRejectsAMalformedHost(t *testing.T) {
	_, err := Connect(context.Background(), "://not a url", time.Second)

	if err == nil {
		t.Fatal("Connect() error = nil, want a failure for a malformed host")
	}
	if !IsUnavailable(err) {
		t.Errorf("error = %v, want KindUnavailable", err)
	}
}

func TestConnectFailsWithAHelpfulMessageWhenTheSocketIsMissing(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")

	_, err := Connect(context.Background(), "unix://"+socket, time.Second)
	if err == nil {
		t.Fatal("Connect() error = nil, want a failure for a missing socket")
	}
	if !IsUnavailable(err) {
		t.Fatalf("error = %v, want KindUnavailable", err)
	}

	msg := Message(err)
	// The operator's most likely fix is group membership; say so.
	if !strings.Contains(msg, "docker") || !strings.Contains(msg, "group") {
		t.Errorf("message = %q, want the docker-group hint", msg)
	}
	if !strings.Contains(msg, socket) {
		t.Errorf("message = %q, want the socket path named", msg)
	}
}

func TestConnectHonoursTheTimeout(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")

	start := time.Now()
	if _, err := Connect(context.Background(), "unix://"+socket, 200*time.Millisecond); err == nil {
		t.Fatal("Connect() error = nil, want a failure")
	}

	// A missing socket fails immediately; the assertion is that we never sit
	// on the connect for materially longer than the caller allowed.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Connect() took %s, want it bounded by the timeout", elapsed)
	}
}

func TestConnectRespectsAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	socket := filepath.Join(t.TempDir(), "docker.sock")
	if _, err := Connect(ctx, "unix://"+socket, time.Second); err == nil {
		t.Fatal("Connect() error = nil, want a failure on a canceled context")
	}
}
