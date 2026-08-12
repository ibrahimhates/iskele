// Package server wires the HTTP router and owns the listener lifecycle.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/crypto"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/hostinfo"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/handlers"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
	"github.com/ibrahimhates/iskele/internal/templates"
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
	// Recorder writes the audit trail. A nil recorder is tolerated.
	Recorder *audit.Recorder
	// Users and Sessions back account administration. Nil leaves /users
	// unmounted; two-factor enrollment goes with them, since it writes to the
	// same table.
	Users    *store.UserRepo
	Sessions *store.SessionRepo
	// Audit backs the audit screen. Nil leaves /audit unmounted; the recorder
	// is separate, so the trail is still written either way.
	Audit *store.AuditRepo
	// Tickets issues the short-lived credentials WebSocket and SSE endpoints
	// use. The router creates one when this is nil.
	Tickets *auth.TicketStore
	// SPA serves the frontend. Nil means the copy embedded in this binary,
	// which is what the daemon always wants; tests substitute their own.
	SPA http.Handler
	// Tasks tracks long-running operations. The router creates one when this
	// is nil.
	Tasks *service.TaskRegistry
	// Registries stores private registry credentials. Nil leaves every pull
	// anonymous and the registry endpoints unmounted.
	Registries *store.RegistryRepo
	// SecretBox encrypts those credentials. Required when Registries is set.
	SecretBox *crypto.SecretBox
	// Builds stores the build history. Nil leaves the build endpoints
	// unmounted rather than failing at request time.
	Builds *store.BuildRepo
	// Stacks stores compose stacks. Nil leaves the stack endpoints unmounted.
	Stacks *store.StackRepo
	// Catalog is the app catalog. Nil leaves the template endpoints unmounted,
	// which is what a test that does not care about them wants.
	Catalog *templates.Catalog
}

// isAPIPath reports whether a request belongs to the JSON API rather than the
// frontend. It matches the prefix as a path segment, so a client-side route
// named /apixyz is still handed to the SPA.
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
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

	// A nil repository leaves every pull anonymous, which is what a test that
	// only exercises the engine wants; the service tolerates it.
	var registryService *service.Registry
	if deps.Registries != nil && deps.SecretBox != nil {
		registryService = service.NewRegistry(deps.Registries, deps.SecretBox, deps.Recorder)
	}

	imageService := service.NewImage(dockerClient, registryService, deps.Recorder)
	volumeService := service.NewVolume(dockerClient, deps.Recorder)
	networkService := service.NewNetwork(dockerClient, deps.Recorder)

	images := handlers.NewImages(imageService)
	volumes := handlers.NewVolumes(volumeService)
	networks := handlers.NewNetworks(networkService)
	systemService := service.NewSystem(dockerClient, diskTargets(deps.Config))
	engine := handlers.NewEngine(systemService)

	tickets := deps.Tickets
	if tickets == nil {
		tickets = auth.NewTicketStore(auth.TicketTTL)
	}
	taskRegistry := deps.Tasks
	if taskRegistry == nil {
		taskRegistry = service.NewTaskRegistry()
	}
	tasks := handlers.NewTasks(taskRegistry)

	// Account administration and two-factor enrollment. The secret box may be
	// nil in a test that does not care; two-factor then reports itself
	// unavailable rather than running without encryption.
	var users *handlers.Users
	if deps.Users != nil {
		users = handlers.NewUsers(
			service.NewUsers(deps.Users, deps.Sessions, deps.SecretBox, deps.Recorder))
	}

	// Reading the trail needs the same table the recorder writes to; without
	// it the endpoints stay unmounted rather than answering an empty list,
	// which would read as "nothing happened" instead of "not configured".
	var audits *handlers.Audit
	if deps.Audit != nil {
		audits = handlers.NewAudit(service.NewAudit(deps.Audit))
	}
	containerService := service.NewContainer(dockerClient, deps.Recorder)
	containers := handlers.NewContainers(containerService)
	streams := handlers.NewStream(containerService, systemService, imageService, tickets, taskRegistry)

	// Bind mounts are checked against this before any of them reach the
	// engine; an unset whitelist refuses them all.
	var allowedPaths []string
	if deps.Config != nil {
		allowedPaths = deps.Config.AllowedPaths
	}
	// One guard, shared by everything that touches a host path: bind mounts,
	// the directory browser and build contexts are the same trust boundary
	// seen from three sides.
	pathGuard := service.NewPathGuard(allowedPaths)

	creator := service.NewCreator(dockerClient, registryService, pathGuard, deps.Recorder)
	create := handlers.NewCreate(creator, pathGuard)
	registries := handlers.NewRegistries(registryService)

	// Builds need a database; without one the endpoints stay unmounted rather
	// than failing at request time.
	var (
		builds  *handlers.Builds
		builder *service.Builder
	)
	if deps.Builds != nil {
		builder = service.NewBuilder(dockerClient, deps.Builds, registryService,
			pathGuard, taskRegistry, deps.Recorder, buildLogDir(deps))
		builds = handlers.NewBuilds(builder, service.NewBrowser(pathGuard), taskRegistry, tickets)
	}

	// Stacks need a database too. The builder is passed along so a compose
	// service with a `build:` section produces its image the same way a manual
	// build does — through the whitelist, with the same log archive.
	var stacks *handlers.Stacks
	if deps.Stacks != nil {
		stackService := service.NewStackService(dockerClient, deps.Stacks, registryService,
			builder, pathGuard, taskRegistry, deps.Recorder, stackStateDir(deps))
		stacks = handlers.NewStacks(stackService, tickets)
	}

	var catalog *handlers.Templates
	if deps.Catalog != nil {
		catalog = handlers.NewTemplates(
			service.NewCatalog(deps.Catalog, creator, dockerClient, deps.Recorder))
	}

	generalLimiter := middleware.NewRateLimiter(middleware.GeneralRate, middleware.GeneralBurst)
	loginLimiter := middleware.NewRateLimiter(middleware.LoginRate, middleware.LoginBurst)

	r := chi.NewRouter()

	// Order matters: RequestID first so the logger can include it, Logger
	// before Recover so a panic is still reported with request context.
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recover)
	r.Use(middleware.SecurityHeaders)

	// Anything the API does not claim belongs to the frontend: the SPA owns
	// its own routes, and the browser asks the server for them after a reload.
	// An unmatched /api path stays JSON so a client never has to parse HTML to
	// learn it called the wrong endpoint.
	spa := deps.SPA
	if spa == nil {
		spa = newSPAHandler()
	}
	apiNotFound := httpx.Handler(func(_ http.ResponseWriter, r *http.Request) error {
		return httpx.ErrNotFound("no route matches %s %s", r.Method, r.URL.Path)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			apiNotFound.ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})

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

		// Streaming endpoints authenticate with a single-use ticket rather than
		// a Bearer header, which a browser cannot set on a WebSocket handshake
		// or an EventSource request. They still enforce the same permissions,
		// and the WebSocket library rejects a cross-origin handshake.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimit(generalLimiter, denyRateLimited))
			r.Use(requireInitialized(deps.Auth))

			r.Method(http.MethodGet, "/containers/stats", httpx.Handler(streams.StatsAll))
			r.Method(http.MethodGet, "/images/pull", httpx.Handler(streams.Pull))
			r.Method(http.MethodGet, "/containers/{id}/logs", httpx.Handler(streams.Logs))
			r.Method(http.MethodGet, "/containers/{id}/exec", httpx.Handler(streams.Exec))
			r.Method(http.MethodGet, "/containers/{id}/stats", httpx.Handler(streams.Stats))
			r.Method(http.MethodGet, "/system/events", httpx.Handler(streams.Events))

			if builds != nil {
				r.Method(http.MethodGet, "/build", httpx.Handler(builds.Build))
			}
			if stacks != nil {
				r.Method(http.MethodGet, "/stacks/{id}/up", httpx.Handler(stacks.Up))
				r.Method(http.MethodGet, "/stacks/{id}/pull", httpx.Handler(stacks.PullImages))
				r.Method(http.MethodGet, "/stacks/{id}/scale", httpx.Handler(stacks.Scale))
				r.Method(http.MethodGet, "/stacks/{id}/logs", httpx.Handler(stacks.Logs))
			}
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
			r.Method(http.MethodPost, "/auth/ws-ticket", httpx.Handler(streams.Ticket))

			// The audit trail is admin-only and read-only: nothing here edits
			// or deletes a record, because a trail an admin can rewrite is not
			// a trail. Ageing records out belongs to a retention sweep, which
			// is still to come.
			if audits != nil {
				r.Route("/audit", func(r chi.Router) {
					r.Use(middleware.RequirePermission(middleware.PermAdmin, denyAuth))

					r.Method(http.MethodGet, "/", httpx.Handler(audits.List))
					r.Method(http.MethodGet, "/facets", httpx.Handler(audits.Facets))
					r.Method(http.MethodGet, "/export", httpx.Handler(audits.Export))
				})
			}

			if users != nil {
				// Two-factor enrollment is nobody's business but the account
				// holder's: no permission gate, because these three endpoints
				// only ever act on the caller's own account.
				r.Method(http.MethodPost, "/auth/totp/setup", httpx.Handler(users.BeginTOTP))
				r.Method(http.MethodPost, "/auth/totp/verify", httpx.Handler(users.ConfirmTOTP))
				r.Method(http.MethodPost, "/auth/totp/disable", httpx.Handler(users.DisableTOTP))

				// Accounts are admin-only: everything here either grants access
				// to this panel or takes it away.
				r.Route("/users", func(r chi.Router) {
					r.Use(middleware.RequirePermission(middleware.PermAdmin, denyAuth))

					r.Method(http.MethodGet, "/", httpx.Handler(users.List))
					r.Method(http.MethodPost, "/", httpx.Handler(users.Create))
					r.Method(http.MethodGet, "/{id}", httpx.Handler(users.Get))
					r.Method(http.MethodPut, "/{id}", httpx.Handler(users.Update))
					r.Method(http.MethodDelete, "/{id}", httpx.Handler(users.Delete))
					r.Method(http.MethodDelete, "/{id}/totp", httpx.Handler(users.ResetTOTP))
				})
			}

			r.Route("/containers", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/", httpx.Handler(containers.List))
				r.With(operate()).Method(http.MethodPost, "/batch", httpx.Handler(containers.Batch))
				r.With(prune()).Method(http.MethodPost, "/prune", httpx.Handler(containers.Prune))

				r.With(create_()).Method(http.MethodPost, "/", httpx.Handler(create.Container))

				r.Route("/{id}", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(containers.Get))
					r.With(read()).Method(http.MethodGet, "/inspect", httpx.Handler(containers.Inspect))

					r.With(operate()).Method(http.MethodPost, "/start", httpx.Handler(containers.Start))
					r.With(operate()).Method(http.MethodPost, "/stop", httpx.Handler(containers.Stop))
					r.With(operate()).Method(http.MethodPost, "/restart", httpx.Handler(containers.Restart))
					r.With(operate()).Method(http.MethodPost, "/pause", httpx.Handler(containers.Pause))
					r.With(operate()).Method(http.MethodPost, "/unpause", httpx.Handler(containers.Unpause))
					r.With(operate()).Method(http.MethodPost, "/kill", httpx.Handler(containers.Kill))
					r.With(operate()).Method(http.MethodPost, "/rename", httpx.Handler(containers.Rename))
					r.With(operate()).Method(http.MethodPost, "/redeploy", httpx.Handler(containers.Redeploy))

					r.With(remove()).Method(http.MethodDelete, "/", httpx.Handler(containers.Remove))
				})
			})

			r.Route("/images", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/", httpx.Handler(images.List))
				r.With(prune()).Method(http.MethodPost, "/prune", httpx.Handler(images.Prune))

				r.Route("/{id}", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/history", httpx.Handler(images.History))
					r.With(read()).Method(http.MethodGet, "/inspect", httpx.Handler(images.Inspect))
					r.With(operate()).Method(http.MethodPost, "/tag", httpx.Handler(images.Tag))
					r.With(remove()).Method(http.MethodDelete, "/", httpx.Handler(images.Remove))
				})
			})

			r.Route("/volumes", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/", httpx.Handler(volumes.List))
				r.With(create_()).Method(http.MethodPost, "/", httpx.Handler(volumes.Create))
				r.With(prune()).Method(http.MethodPost, "/prune", httpx.Handler(volumes.Prune))

				r.Route("/{name}", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(volumes.Get))
					r.With(read()).Method(http.MethodGet, "/inspect", httpx.Handler(volumes.Inspect))
					r.With(remove()).Method(http.MethodDelete, "/", httpx.Handler(volumes.Remove))
				})
			})

			r.Route("/networks", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/", httpx.Handler(networks.List))
				r.With(create_()).Method(http.MethodPost, "/", httpx.Handler(networks.Create))
				r.With(prune()).Method(http.MethodPost, "/prune", httpx.Handler(networks.Prune))

				r.Route("/{id}", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(networks.Get))
					r.With(read()).Method(http.MethodGet, "/inspect", httpx.Handler(networks.Inspect))
					r.With(operate()).Method(http.MethodPost, "/connect", httpx.Handler(networks.Connect))
					r.With(operate()).Method(http.MethodPost, "/disconnect", httpx.Handler(networks.Disconnect))
					r.With(remove()).Method(http.MethodDelete, "/", httpx.Handler(networks.Remove))
				})
			})

			// Registry credentials are admin-only: they are the one thing here
			// that grants access to something outside this host.
			if registryService != nil {
				r.Route("/registries", func(r chi.Router) {
					r.Use(middleware.RequirePermission(middleware.PermAdmin, denyAuth))

					r.Method(http.MethodGet, "/", httpx.Handler(registries.List))
					r.Method(http.MethodPost, "/", httpx.Handler(registries.Create))
					r.Method(http.MethodPut, "/{id}", httpx.Handler(registries.Update))
					r.Method(http.MethodDelete, "/{id}", httpx.Handler(registries.Delete))
				})
			}

			if builds != nil {
				// Browsing takes the build permission, not read: this enumerates
				// host directories, and the build form is the only thing that needs it.
				r.With(build_()).Method(http.MethodGet, "/fs/browse", httpx.Handler(builds.Browse))

				r.Route("/builds", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(builds.List))
					r.With(read()).Method(http.MethodGet, "/{id}", httpx.Handler(builds.Get))
					r.With(read()).Method(http.MethodGet, "/{id}/log", httpx.Handler(builds.Log))
					r.With(build_()).Method(http.MethodPost, "/{id}/cancel", httpx.Handler(builds.Cancel))
				})
			}

			r.Route("/tasks", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/", httpx.Handler(tasks.List))
				r.With(read()).Method(http.MethodGet, "/{id}", httpx.Handler(tasks.Get))
				r.With(operate()).Method(http.MethodPost, "/{id}/cancel", httpx.Handler(tasks.Cancel))
			})

			if stacks != nil {
				r.Route("/stacks", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(stacks.List))
					r.With(create_()).Method(http.MethodPost, "/", httpx.Handler(stacks.Create))
					// Validation is a read of the caller's own text; it creates
					// nothing and is what the editor calls on every keystroke.
					r.With(read()).Method(http.MethodPost, "/validate", httpx.Handler(stacks.Validate))
					r.With(read()).Method(http.MethodGet, "/discovered", httpx.Handler(stacks.Discovered))
					r.With(create_()).Method(http.MethodPost, "/import", httpx.Handler(stacks.Import))

					r.With(read()).Method(http.MethodGet, "/{id}", httpx.Handler(stacks.Get))
					r.With(create_()).Method(http.MethodPut, "/{id}", httpx.Handler(stacks.Update))
					r.With(remove()).Method(http.MethodDelete, "/{id}", httpx.Handler(stacks.Delete))
					r.With(read()).Method(http.MethodPost, "/{id}/diff", httpx.Handler(stacks.Diff))

					// Taking a stack away removes containers, so it takes the
					// delete permission rather than operate.
					r.With(remove()).Method(http.MethodPost, "/{id}/down", httpx.Handler(stacks.Down))
					r.With(operate()).Method(http.MethodPost, "/{id}/stop", httpx.Handler(stacks.Act(service.StackStop)))
					r.With(operate()).Method(http.MethodPost, "/{id}/start", httpx.Handler(stacks.Act(service.StackStart)))
					r.With(operate()).Method(http.MethodPost, "/{id}/restart", httpx.Handler(stacks.Act(service.StackRestart)))
				})
			}

			if catalog != nil {
				r.Route("/templates", func(r chi.Router) {
					r.With(read()).Method(http.MethodGet, "/", httpx.Handler(catalog.List))
					// Generating a secret creates nothing and reveals nothing;
					// it is a random string, and the operator filling in a
					// catalog form is the one who needs it.
					r.With(read()).Method(http.MethodPost, "/secret", httpx.Handler(catalog.GenerateSecret))
					r.With(read()).Method(http.MethodGet, "/{id}", httpx.Handler(catalog.Get))
					r.With(create_()).Method(http.MethodPost, "/{id}/deploy", httpx.Handler(catalog.Deploy))
				})
			}

			r.Route("/system", func(r chi.Router) {
				r.With(read()).Method(http.MethodGet, "/ping", httpx.Handler(engine.Ping))
				r.With(read()).Method(http.MethodGet, "/info", httpx.Handler(engine.Info))
				r.With(read()).Method(http.MethodGet, "/df", httpx.Handler(engine.DiskUsage))
				r.With(read()).Method(http.MethodGet, "/host", httpx.Handler(engine.Host))
				r.With(read()).Method(http.MethodGet, "/allowed-paths", httpx.Handler(create.AllowedPaths))
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

// create_ is spelled with a trailing underscore because "create" is the local
// variable holding the handler set.
func create_() func(http.Handler) http.Handler { //nolint:revive // see above
	return middleware.RequirePermission(middleware.PermCreate, denyAuth)
}

func prune() func(http.Handler) http.Handler {
	return middleware.RequirePermission(middleware.PermPrune, denyAuth)
}

// build_ carries a trailing underscore for the same reason create_ does: the
// local variable holding the handler set is called `builds`.
func build_() func(http.Handler) http.Handler { //nolint:revive // see above
	return middleware.RequirePermission(middleware.PermBuild, denyAuth)
}

// stackStateDir is where each stack's working copy lives.
func stackStateDir(deps Deps) string {
	if deps.Config == nil {
		return ""
	}
	return deps.Config.StackDir()
}

// buildLogDir is where build output is archived.
func buildLogDir(deps Deps) string {
	if deps.Config == nil {
		return ""
	}
	return deps.Config.BuildLogDir()
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

// diskTargets are the directories whose free space the dashboard reports.
//
// The daemon's own data directory is the one it can run out of: a full disk
// there stops builds, stack checkouts and the audit trail alike. The engine's
// root directory is added at read time, once the engine says where it is.
func diskTargets(cfg *config.Config) []hostinfo.Target {
	if cfg == nil || cfg.DataDir == "" {
		return nil
	}
	return []hostinfo.Target{{Label: "data", Path: cfg.DataDir}}
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
