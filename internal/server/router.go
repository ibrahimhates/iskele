// Package server wires the HTTP router and owns the listener lifecycle.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/handlers"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
)

// APIPrefix is the version prefix every API route lives under.
const APIPrefix = "/api/v1"

// Deps are the collaborators the router needs. Later milestones extend this
// struct (store, auth) rather than reaching for globals.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
	// Docker may be nil when the daemon was unreachable at startup. The
	// server still serves, and Docker-backed routes answer DOCKER_UNAVAILABLE
	// so the UI can explain the problem instead of showing a blank page.
	Docker docker.Client
}

// NewRouter builds the HTTP handler for the whole daemon.
func NewRouter(deps Deps) http.Handler {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}

	dockerClient := deps.Docker
	if dockerClient == nil {
		dockerClient = docker.Offline(offlineReason(deps))
	}

	system := handlers.NewSystem()
	containers := handlers.NewContainers(service.NewContainer(dockerClient))
	images := handlers.NewImages(service.NewImage(dockerClient))
	volumes := handlers.NewVolumes(service.NewVolume(dockerClient))
	networks := handlers.NewNetworks(service.NewNetwork(dockerClient))
	engine := handlers.NewEngine(service.NewSystem(dockerClient))

	r := chi.NewRouter()

	// Order matters: RequestID first so the logger can include it, Logger
	// before Recover so a panic is still reported with request context.
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recover)
	r.Use(middleware.SecurityHeaders)

	r.NotFound(httpx.Handler(func(_ http.ResponseWriter, r *http.Request) error {
		return httpx.ErrNotFound("no route matches %s %s", r.Method, r.URL.Path)
	}).ServeHTTP)

	r.MethodNotAllowed(httpx.Handler(func(_ http.ResponseWriter, r *http.Request) error {
		return httpx.NewError(http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed,
			"method %s is not allowed on %s", r.Method, r.URL.Path)
	}).ServeHTTP)

	r.Route(APIPrefix, func(r chi.Router) {
		// Unauthenticated: liveness probing and version discovery.
		r.Method(http.MethodGet, "/health", httpx.Handler(system.Health))
		r.Method(http.MethodGet, "/version", httpx.Handler(system.Version))

		r.Route("/containers", func(r chi.Router) {
			r.Method(http.MethodGet, "/", httpx.Handler(containers.List))
			r.Route("/{id}", func(r chi.Router) {
				r.Method(http.MethodGet, "/", httpx.Handler(containers.Get))
				r.Method(http.MethodDelete, "/", httpx.Handler(containers.Remove))
				r.Method(http.MethodGet, "/inspect", httpx.Handler(containers.Inspect))
				r.Method(http.MethodPost, "/start", httpx.Handler(containers.Start))
				r.Method(http.MethodPost, "/stop", httpx.Handler(containers.Stop))
				r.Method(http.MethodPost, "/restart", httpx.Handler(containers.Restart))
			})
		})

		r.Method(http.MethodGet, "/images", httpx.Handler(images.List))
		r.Method(http.MethodGet, "/volumes", httpx.Handler(volumes.List))
		r.Method(http.MethodGet, "/networks", httpx.Handler(networks.List))

		r.Route("/system", func(r chi.Router) {
			r.Method(http.MethodGet, "/ping", httpx.Handler(engine.Ping))
			r.Method(http.MethodGet, "/info", httpx.Handler(engine.Info))
			r.Method(http.MethodGet, "/df", httpx.Handler(engine.DiskUsage))
		})
	})

	return r
}

// offlineReason explains, in the error every Docker route returns, which
// endpoint could not be reached.
func offlineReason(deps Deps) string {
	host := "the Docker daemon"
	if deps.Config != nil && deps.Config.DockerHost != "" {
		host = deps.Config.DockerHost
	}
	return "not connected to " + host +
		"; check that Docker is running and that iskeled's user is in the 'docker' group"
}
