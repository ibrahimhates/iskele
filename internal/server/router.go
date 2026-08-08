// Package server wires the HTTP router and owns the listener lifecycle.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/handlers"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
)

// APIPrefix is the version prefix every API route lives under.
const APIPrefix = "/api/v1"

// Deps are the collaborators the router needs.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
	// Docker may be nil when the daemon was unreachable at startup. The
	// server still serves, and Docker-backed routes answer DOCKER_UNAVAILABLE
	// so the UI can explain the problem instead of showing a blank page.
	Docker docker.Client
	// Auth may be nil in tests that only exercise unauthenticated routes; the
	// router then leaves the protected subtree unmounted.
	Auth *service.Auth
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

	generalLimiter := middleware.NewRateLimiter(middleware.GeneralRate, middleware.GeneralBurst)
	loginLimiter := middleware.NewRateLimiter(middleware.LoginRate, middleware.LoginBurst)

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
		// Unauthenticated: liveness probing and version discovery. These stay
		// open so a watchdog can poll the daemon before anyone signs in.
		r.Method(http.MethodGet, "/health", httpx.Handler(system.Health))
		r.Method(http.MethodGet, "/version", httpx.Handler(system.Version))

		if deps.Auth == nil {
			return
		}

		authHandlers := handlers.NewAuth(deps.Auth)

		// Credential-guessing targets: strictly rate limited, and never behind
		// the auth requirement they exist to satisfy.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimit(loginLimiter, denyRateLimited))

			r.Method(http.MethodPost, "/auth/bootstrap", httpx.Handler(authHandlers.Bootstrap))
			r.Method(http.MethodPost, "/auth/login", httpx.Handler(authHandlers.Login))
		})

		// Also unauthenticated, but not guessable: status reveals nothing but
		// whether setup is done, and refresh needs a 32-byte random token that
		// brute force cannot reach. Throttling these at login rates would make
		// a browser with several tabs hit 429 on an ordinary token renewal.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimit(generalLimiter, denyRateLimited))

			r.Method(http.MethodGet, "/auth/status", httpx.Handler(authHandlers.Status))
			r.Method(http.MethodPost, "/auth/refresh", httpx.Handler(authHandlers.Refresh))
		})

		// Everything else requires a valid credential.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimit(generalLimiter, denyRateLimited))
			r.Use(requireInitialized(deps.Auth))
			r.Use(middleware.Authenticate(resolveIdentity(deps.Auth), denyAuth))
			r.Use(middleware.RequireAuth(denyAuth))
			r.Use(middleware.CSRFGuard("", denyCSRF))

			r.Method(http.MethodGet, "/auth/me", httpx.Handler(authHandlers.Me))
			r.Method(http.MethodPost, "/auth/logout", httpx.Handler(authHandlers.Logout))

			r.Route("/containers", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/", httpx.Handler(containers.List))

				r.Route("/{id}", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(containers.Get))
					r.With(read()).Method(http.MethodGet, "/inspect", httpx.Handler(containers.Inspect))

					r.With(operate()).Method(http.MethodPost, "/start", httpx.Handler(containers.Start))
					r.With(operate()).Method(http.MethodPost, "/stop", httpx.Handler(containers.Stop))
					r.With(operate()).Method(http.MethodPost, "/restart", httpx.Handler(containers.Restart))

					r.With(remove()).Method(http.MethodDelete, "/", httpx.Handler(containers.Remove))
				})
			})

			r.With(read()).Method(http.MethodGet, "/images", httpx.Handler(images.List))
			r.With(read()).Method(http.MethodGet, "/volumes", httpx.Handler(volumes.List))
			r.With(read()).Method(http.MethodGet, "/networks", httpx.Handler(networks.List))

			r.Route("/system", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/ping", httpx.Handler(engine.Ping))
				r.With(read()).Method(http.MethodGet, "/info", httpx.Handler(engine.Info))
				r.With(read()).Method(http.MethodGet, "/df", httpx.Handler(engine.DiskUsage))
			})
		})
	})

	return r
}

// Permission shorthands, so route declarations read as a permission table.
func read() func(http.Handler) http.Handler {
	return middleware.RequirePermission(middleware.PermRead, denyAuth)
}

func operate() func(http.Handler) http.Handler {
	return middleware.RequirePermission(middleware.PermOperate, denyAuth)
}

func remove() func(http.Handler) http.Handler {
	return middleware.RequirePermission(middleware.PermDelete, denyAuth)
}

// resolveIdentity adapts the auth service to the middleware's expectation.
func resolveIdentity(svc *service.Auth) middleware.ResolveIdentity {
	return func(ctx context.Context, credential string) (middleware.Identity, error) {
		id, err := svc.Authenticate(ctx, credential)
		if err != nil {
			return middleware.Identity{}, err
		}
		return middleware.Identity{
			UserID:   id.UserID,
			Username: id.Username,
			Role:     id.Role,
			TokenID:  id.TokenID,
			Scopes:   id.Scopes,
		}, nil
	}
}

// requireInitialized closes the API until the first admin account exists.
//
// Without this, an installation that has been started but not set up would
// expose Docker to anyone who reached the port.
func requireInitialized(svc *service.Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			initialized, err := svc.Initialized(r.Context())
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			if !initialized {
				httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeNotInitialized,
					"this installation has not been set up yet; POST /api/v1/auth/bootstrap first"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// denyAuth renders authentication and authorization failures.
func denyAuth(w http.ResponseWriter, r *http.Request, err error) {
	var permErr *middleware.PermissionError
	switch {
	case errors.As(err, &permErr):
		httpx.WriteError(w, r, httpx.NewError(http.StatusForbidden, httpx.CodeForbidden,
			"%s", permErr.Error()).WithDetails(map[string]any{
			"role":                string(permErr.Role),
			"required_permission": string(permErr.Permission),
		}))

	case errors.Is(err, auth.ErrTokenExpired):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, httpx.CodeTokenExpired,
			"the access token has expired"))

	case errors.Is(err, service.ErrAccountDisabled):
		httpx.WriteError(w, r, httpx.NewError(http.StatusForbidden, httpx.CodeAccountDisabled,
			"this account is disabled"))

	case errors.Is(err, middleware.ErrUnauthenticated), errors.Is(err, auth.ErrTokenInvalid),
		errors.Is(err, service.ErrSessionInvalid):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthorized,
			"authentication required"))

	default:
		// An unexpected failure here (a database error, say) is a 500, not a
		// 401: telling the caller "unauthorized" would send them to re-login
		// over and over against a broken backend.
		httpx.WriteError(w, r, err)
	}
}

// denyRateLimited renders a throttled request.
func denyRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	httpx.WriteError(w, r, httpx.NewError(http.StatusTooManyRequests, httpx.CodeRateLimited,
		"too many requests; retry in %s", retryAfter).
		WithDetails(map[string]any{"retry_after_seconds": int(retryAfter.Seconds())}))
}

// denyCSRF renders a rejected cross-origin write.
func denyCSRF(w http.ResponseWriter, r *http.Request, reason string) {
	httpx.WriteError(w, r, httpx.NewError(http.StatusForbidden, httpx.CodeCSRFInvalid, "%s", reason))
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
