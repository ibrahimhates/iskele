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
	"time"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/logging"
	"github.com/ibrahimhates/iskele/internal/server"
	"github.com/ibrahimhates/iskele/internal/version"
)

// dockerConnectTimeout bounds the startup handshake with the daemon, so a
// hung socket delays the listener by seconds rather than forever.
const dockerConnectTimeout = 5 * time.Second

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

	// A missing daemon must not stop iskeled from starting: the operator needs
	// the panel to tell them Docker is down. Docker-backed routes answer
	// DOCKER_UNAVAILABLE until the connection is established.
	dockerClient, err := docker.Connect(ctx, cfg.DockerHost, dockerConnectTimeout)
	if err != nil {
		log.Warn("docker daemon is not reachable; container features are disabled until it returns",
			slog.String("docker_host", cfg.DockerHost),
			slog.String("reason", docker.Message(err)),
		)
	} else {
		defer func() {
			if closeErr := dockerClient.Close(); closeErr != nil {
				log.Warn("closing the docker client failed", slog.Any("error", closeErr))
			}
		}()

		if pong, pingErr := dockerClient.Ping(ctx); pingErr == nil {
			log.Info("connected to docker",
				slog.String("docker_host", cfg.DockerHost),
				slog.String("api_version", pong.APIVersion),
			)
		}
	}

	router := server.NewRouter(server.Deps{Config: cfg, Logger: log, Docker: dockerClient})

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
