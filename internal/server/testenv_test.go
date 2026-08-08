package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
)

// testPassword satisfies the password policy used across the suite.
const testPassword = "correct-horse-battery-Staple-1"

// testEnv is a fully wired server: real store, real auth, fake Docker.
//
// Building the real thing rather than stubbing the auth service is deliberate:
// these tests are the only place the whole chain — token issue, middleware
// resolution, RBAC — is exercised together.
type testEnv struct {
	raw    http.Handler
	db     *store.DB
	tokens map[store.Role]string
}

// newEnv builds a server backed by dockerClient, with one account per role.
// A nil dockerClient exercises the daemon-unreachable path.
func newEnv(t *testing.T, dockerClient docker.Client) *testEnv {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authService := newAuthService(db, log)

	cfg := config.Default()
	env := &testEnv{
		db: db,
		raw: NewRouter(Deps{
			Config:   &cfg,
			Logger:   log,
			Docker:   dockerClient,
			Auth:     authService,
			Recorder: audit.New(db.Audit, log),
		}),
		tokens: map[store.Role]string{},
	}

	meta := service.RequestMeta{IP: "192.0.2.1", UserAgent: "test"}

	// The first account must come through bootstrap, which is the only path
	// that may create a user before anyone is signed in.
	pair, err := authService.Bootstrap(ctx, "admin", testPassword, meta)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	env.tokens[store.RoleAdmin] = pair.AccessToken

	for _, role := range []store.Role{store.RoleOperator, store.RoleViewer} {
		env.tokens[role] = env.addUser(t, string(role), role)
	}

	return env
}

// addUser creates an account with the given role and returns its access token.
func (e *testEnv) addUser(t *testing.T, username string, role store.Role) string {
	t.Helper()
	ctx := context.Background()

	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	err = e.db.Users.Create(ctx, store.User{
		ID: id, Username: username, Role: role, PasswordHash: testPasswordHash(t),
	})
	if err != nil {
		t.Fatalf("Users.Create(%s) error = %v", username, err)
	}

	return e.login(t, username, testPassword)
}

// login signs in and returns the access token.
func (e *testEnv) login(t *testing.T, username, password string) string {
	t.Helper()

	svc := newAuthService(e.db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	pair, err := svc.Login(context.Background(), username, password,
		service.RequestMeta{IP: "192.0.2.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("Login(%s) error = %v", username, err)
	}
	return pair.AccessToken
}

// as returns a handler that authenticates every request with role's token.
func (e *testEnv) as(role store.Role) http.Handler {
	return &authedHandler{next: e.raw, token: e.tokens[role]}
}

// anonymous returns the handler without any credential.
func (e *testEnv) anonymous() http.Handler { return e.raw }

// withToken returns a handler using an arbitrary credential.
func (e *testEnv) withToken(token string) http.Handler {
	return &authedHandler{next: e.raw, token: token}
}

// authedHandler attaches a bearer token to every request it forwards.
type authedHandler struct {
	next  http.Handler
	token string
}

func (h *authedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.token != "" {
		r.Header.Set("Authorization", "Bearer "+h.token)
	}
	h.next.ServeHTTP(w, r)
}

// newAuthService builds an auth service over db with the test signing key.
func newAuthService(db *store.DB, log *slog.Logger) *service.Auth {
	return service.NewAuth(service.AuthDeps{
		Users:      db.Users,
		Sessions:   db.Sessions,
		Tokens:     db.Tokens,
		Limiter:    auth.NewLimiter(db.Logins, auth.LimiterOptions{}),
		Issuer:     auth.NewTokenIssuer(testSigningKey, 15*time.Minute),
		Recorder:   audit.New(db.Audit, log),
		RefreshTTL: 168 * time.Hour,
	})
}

// testSigningKey is a fixed key so tokens minted by one service instance are
// accepted by another in the same test.
var testSigningKey = []byte("iskele-test-signing-key-32-bytes!")

// testPasswordHash computes the hash of testPassword once for the whole
// package. argon2id is deliberately expensive, and re-deriving the same hash
// for every fixture account would dominate the suite's runtime.
var testPasswordHash = func() func(*testing.T) string {
	var (
		once sync.Once
		hash string
		err  error
	)
	return func(t *testing.T) string {
		t.Helper()
		once.Do(func() { hash, err = auth.HashPassword(testPassword) })
		if err != nil {
			t.Fatalf("HashPassword() error = %v", err)
		}
		return hash
	}
}()

// newAPITokenFor mints an API token for a test fixture.
func newAPITokenFor(t *testing.T) (token, prefix, hash string, err error) {
	t.Helper()
	return auth.NewAPIToken()
}

// newTokenID mints a database identifier for a test fixture.
func newTokenID(t *testing.T) (string, error) {
	t.Helper()
	return auth.NewID()
}
