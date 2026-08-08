package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ibrahimhates/iskele/internal/config"
)

const (
	// shutdownTimeout bounds how long in-flight requests may take to finish
	// once a termination signal arrives.
	shutdownTimeout = 30 * time.Second

	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

// Server owns the TCP listener and the HTTP server lifecycle.
//
// Read and write timeouts are intentionally not set: log streaming, exec
// sessions and image pulls are long-lived by design. Per-request deadlines are
// applied by the handlers that need them.
type Server struct {
	http     *http.Server
	listener net.Listener
	log      *slog.Logger
	tlsCfg   config.TLS
}

// New creates a Server bound to cfg.Listen. The socket is opened eagerly so a
// port conflict is reported at startup rather than asynchronously. ctx bounds
// the bind itself only; use Run's context to control the serving lifetime.
func New(ctx context.Context, cfg *config.Config, handler http.Handler, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	if cfg.TLS.Enabled {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	return &Server{http: srv, listener: ln, log: log, tlsCfg: cfg.TLS}, nil
}

// Addr reports the address actually bound, which differs from the configured
// value when port 0 was requested.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Run serves until ctx is canceled, then drains in-flight requests. It
// returns nil on a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		var err error
		if s.tlsCfg.Enabled {
			err = s.http.ServeTLS(s.listener, s.tlsCfg.CertFile, s.tlsCfg.KeyFile)
		} else {
			err = s.http.Serve(s.listener)
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		// The listener died on its own (bad TLS material, socket error).
		return err
	case <-ctx.Done():
	}

	s.log.Info("shutting down", slog.Duration("timeout", shutdownTimeout))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		// Requests were still running when the grace period expired; close
		// them hard so the process can exit.
		s.log.Warn("graceful shutdown timed out, closing connections",
			slog.Any("error", err))
		if closeErr := s.http.Close(); closeErr != nil {
			return fmt.Errorf("force close: %w", closeErr)
		}
	}

	// Surface any error Serve reported while we were shutting down.
	if err := <-errCh; err != nil {
		return err
	}
	return nil
}
