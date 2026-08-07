// Package server wires the HTTP router and owns the listener lifecycle.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/handlers"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
)

// APIPrefix is the version prefix every API route lives under.
const APIPrefix = "/api/v1"

// Deps are the collaborators the router needs. Later milestones extend this
// struct (docker client, store, auth) rather than reaching for globals.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
}

// NewRouter builds the HTTP handler for the whole daemon.
func NewRouter(deps Deps) http.Handler {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}

	system := handlers.NewSystem()

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
	})

	return r
}
