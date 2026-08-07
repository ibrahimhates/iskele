// Command iskeled is the Iskele daemon: a Docker management panel that runs
// as a native systemd service on the host.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/logging"
	"github.com/ibrahimhates/iskele/internal/server"
	"github.com/ibrahimhates/iskele/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "iskeled: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(args, os.LookupEnv, os.Stderr)
	switch {
	case errors.Is(err, config.ErrVersionRequested):
		fmt.Println(version.String())
		return nil
	case errors.Is(err, flag.ErrHelp):
		// flag already printed the usage text.
		return nil
	case err != nil:
		return err
	}

	log := logging.New(logging.Options{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
		Output: os.Stderr,
	})
	slog.SetDefault(log)

	log.Info("starting iskeled",
		slog.String("version", version.Get().Version),
		slog.String("commit", version.Get().Commit),
		slog.String("listen", cfg.Listen),
		slog.String("docker_host", cfg.DockerHost),
		slog.String("data_dir", cfg.DataDir),
		slog.String("config_file", configSource(cfg)),
	)

	// Docker socket access is root-equivalent, so an interface reachable from
	// outside the host must not be a silent default.
	if cfg.PubliclyBound() {
		log.Warn("listening on a non-loopback address; put iskeled behind TLS or a reverse proxy",
			slog.String("listen", cfg.Listen),
			slog.Bool("tls_enabled", cfg.TLS.Enabled),
		)
	}

	if mkErr := os.MkdirAll(cfg.DataDir, 0o750); mkErr != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, mkErr)
	}

	// Canceled on SIGINT/SIGTERM; a second signal aborts immediately via the
	// default handler that signal.NotifyContext restores.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	router := server.NewRouter(server.Deps{Config: cfg, Logger: log})

	srv, err := server.New(ctx, cfg, router, log)
	if err != nil {
		return err
	}

	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	log.Info("listening", slog.String("url", fmt.Sprintf("%s://%s", scheme, srv.Addr())))

	if err := srv.Run(ctx); err != nil {
		return err
	}

	log.Info("stopped")
	return nil
}

func configSource(cfg *config.Config) string {
	if cfg.ConfigFile == "" {
		return "(defaults)"
	}
	return cfg.ConfigFile
}
