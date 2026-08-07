package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/config"
)

func testConfig(listen string) *config.Config {
	cfg := config.Default()
	cfg.Listen = listen
	return &cfg
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startServer boots a server on an ephemeral port and returns it plus a cancel
// func that shuts it down and waits for Run to return.
func startServer(t *testing.T, handler http.Handler) (*Server, func()) {
	t.Helper()

	srv, err := New(context.Background(), testConfig("127.0.0.1:0"), handler, quietLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	return srv, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() error = %v, want a clean shutdown", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Run() did not return within 10s of cancellation")
		}
	}
}

func TestServerServesRequests(t *testing.T) {
	srv, stop := startServer(t, NewRouter(Deps{Config: testConfig("127.0.0.1:0"), Logger: quietLogger()}))
	defer stop()

	resp, err := http.Get("http://" + srv.Addr() + APIPrefix + "/health") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Errorf("body = %q", body)
	}
}

func TestAddrReportsBoundPort(t *testing.T) {
	srv, stop := startServer(t, http.NotFoundHandler())
	defer stop()

	if srv.Addr() == "127.0.0.1:0" || !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Errorf("Addr() = %q, want the actually bound port", srv.Addr())
	}
}

func TestGracefulShutdownWaitsForInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("finished"))
	})

	srv, err := New(context.Background(), testConfig("127.0.0.1:0"), handler, quietLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		// The body is closed by the receiver below, once the response has been
		// handed over the channel.
		resp, err := http.Get("http://" + srv.Addr() + "/slow") //nolint:noctx,bodyclose
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("handler never started")
	}

	// Shutdown begins while the request is still in flight.
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case resp := <-respCh:
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "finished" {
			t.Errorf("body = %q, want the in-flight request to complete", body)
		}
	case err := <-errCh:
		t.Fatalf("in-flight request failed during shutdown: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight request never completed")
	}

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after shutdown")
	}
}

func TestServerStopsAcceptingAfterShutdown(t *testing.T) {
	srv, stop := startServer(t, http.NotFoundHandler())
	addr := srv.Addr()
	stop()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/x") //nolint:noctx
	if err == nil {
		_ = resp.Body.Close()
		t.Error("server still accepted a connection after shutdown")
	}
}

func TestNewReportsPortConflict(t *testing.T) {
	first, err := New(context.Background(), testConfig("127.0.0.1:0"), http.NotFoundHandler(), quietLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = first.listener.Close() }()

	_, err = New(context.Background(), testConfig(first.Addr()), http.NotFoundHandler(), quietLogger())
	if err == nil {
		t.Fatal("New() error = nil, want a bind conflict to be reported eagerly")
	}
	if !strings.Contains(err.Error(), "listen on") {
		t.Errorf("error = %v, want it to mention the listen address", err)
	}
}

func TestNewFailsForInvalidAddress(t *testing.T) {
	if _, err := New(context.Background(), testConfig("not-an-address"), http.NotFoundHandler(), quietLogger()); err == nil {
		t.Fatal("New() error = nil, want an error for a malformed address")
	}
}
