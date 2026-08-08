package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Authentication errors. Login maps several distinct causes onto
// ErrInvalidCredentials on purpose: telling a caller whether the username
// exists turns the login form into an account enumeration oracle.
var (
	ErrNotInitialized      = errors.New("this installation has not been set up yet")
	ErrAlreadyInitialized  = errors.New("this installation has already been set up")
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrAccountDisabled     = errors.New("this account is disabled")
	ErrSessionInvalid      = errors.New("invalid or expired session")
	ErrUsernameRequired    = errors.New("username is required")
	ErrUsernameUnavailable = errors.New("that username is already taken")
)

// LockedOutError reports that an IP is temporarily refused after too many
// failed logins.
type LockedOutError struct {
	RetryAfter time.Duration
}

func (e *LockedOutError) Error() string {
	return fmt.Sprintf("too many failed login attempts; try again in %s", e.RetryAfter)
}

// RequestMeta is the request context an audit record needs.
type RequestMeta struct {
	IP        string
	UserAgent string
}

// Identity is the authenticated caller of a request.
type Identity struct {
	UserID   string     `json:"user_id"`
	Username string     `json:"username"`
	Role     store.Role `json:"role"`
	// TokenID is set when the caller authenticated with an API token.
	TokenID string `json:"token_id,omitempty"`
	// Scopes are the API token's scopes; empty for interactive sessions.
	Scopes []string `json:"scopes,omitempty"`
}

// Actor converts the identity into an audit actor.
func (i Identity) Actor() audit.Actor {
	return audit.Actor{UserID: i.UserID, Username: i.Username, Role: i.Role, TokenID: i.TokenID}
}

// TokenPair is what a successful login, bootstrap or refresh returns.
type TokenPair struct {
	AccessToken      string     `json:"access_token"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RefreshToken     string     `json:"refresh_token"`
	RefreshExpiresAt time.Time  `json:"refresh_expires_at"`
	User             store.User `json:"user"`
}

// Auth implements the authentication flows.
type Auth struct {
	users      *store.UserRepo
	sessions   *store.SessionRepo
	tokens     *store.TokenRepo
	limiter    *auth.Limiter
	issuer     *auth.TokenIssuer
	recorder   *audit.Recorder
	refreshTTL time.Duration
	now        func() time.Time
}

// AuthDeps are the collaborators Auth needs.
type AuthDeps struct {
	Users      *store.UserRepo
	Sessions   *store.SessionRepo
	Tokens     *store.TokenRepo
	Limiter    *auth.Limiter
	Issuer     *auth.TokenIssuer
	Recorder   *audit.Recorder
	RefreshTTL time.Duration
}

// NewAuth builds the authentication service.
func NewAuth(deps AuthDeps) *Auth {
	return &Auth{
		users:      deps.Users,
		sessions:   deps.Sessions,
		tokens:     deps.Tokens,
		limiter:    deps.Limiter,
		issuer:     deps.Issuer,
		recorder:   deps.Recorder,
		refreshTTL: deps.RefreshTTL,
		now:        time.Now,
	}
}

// SetClock replaces the time source, for tests.
func (s *Auth) SetClock(now func() time.Time) { s.now = now }

// Initialized reports whether the installation has an admin account yet.
func (s *Auth) Initialized(ctx context.Context) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Bootstrap creates the first admin account and signs it in.
//
// It is the only write allowed before initialization, and it refuses once any
// account exists — otherwise anyone who reached the port could claim the
// installation.
func (s *Auth) Bootstrap(ctx context.Context, username, password string, meta RequestMeta) (TokenPair, error) {
	initialized, err := s.Initialized(ctx)
	if err != nil {
		return TokenPair{}, err
	}
	if initialized {
		return TokenPair{}, ErrAlreadyInitialized
	}

	user, err := s.createUser(ctx, username, password, store.RoleAdmin)
	if err != nil {
		return TokenPair{}, err
	}

	pair, err := s.issuePair(ctx, user, meta)
	if err != nil {
		return TokenPair{}, err
	}

	s.recorder.Record(ctx, audit.Event{
		Actor:        audit.Actor{UserID: user.ID, Username: user.Username, Role: user.Role},
		Action:       audit.ActionBootstrap,
		ResourceType: "user",
		ResourceID:   user.ID,
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return pair, nil
}

// CreateUser adds an account. Used by bootstrap and, from M8, by admins.
func (s *Auth) createUser(ctx context.Context, username, password string, role store.Role) (store.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return store.User{}, ErrUsernameRequired
	}
	if err := auth.ValidatePassword(password); err != nil {
		return store.User{}, err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return store.User{}, err
	}
	id, err := auth.NewID()
	if err != nil {
		return store.User{}, err
	}

	user := store.User{
		ID:           id,
		Username:     username,
		Role:         role,
		PasswordHash: hash,
		CreatedAt:    s.now().UTC(),
	}
	if err := s.users.Create(ctx, user); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return store.User{}, ErrUsernameUnavailable
		}
		return store.User{}, err
	}
	return user, nil
}

// Login verifies credentials and starts a session.
func (s *Auth) Login(ctx context.Context, username, password string, meta RequestMeta) (TokenPair, error) {
	initialized, err := s.Initialized(ctx)
	if err != nil {
		return TokenPair{}, err
	}
	if !initialized {
		return TokenPair{}, ErrNotInitialized
	}

	status, err := s.limiter.Check(ctx, meta.IP)
	if err != nil {
		return TokenPair{}, err
	}
	if status.Locked {
		return TokenPair{}, &LockedOutError{RetryAfter: status.RetryAfter}
	}

	user, err := s.users.ByUsername(ctx, username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return TokenPair{}, err
	}

	// Verify even when the user does not exist, against a hash of the same
	// shape, so a missing account and a wrong password take the same time.
	storedHash := user.PasswordHash
	if storedHash == "" {
		storedHash = dummyHash
	}
	ok, verifyErr := auth.VerifyPassword(storedHash, password)
	if verifyErr != nil && user.ID != "" {
		// A malformed stored hash is a data problem the operator must see.
		return TokenPair{}, fmt.Errorf("verify password for %s: %w", user.ID, verifyErr)
	}

	if !ok || user.ID == "" {
		return TokenPair{}, s.failLogin(ctx, username, meta, ErrInvalidCredentials)
	}
	if user.Disabled {
		return TokenPair{}, s.failLogin(ctx, username, meta, ErrAccountDisabled)
	}

	if recordErr := s.limiter.RecordSuccess(ctx, meta.IP, username); recordErr != nil {
		return TokenPair{}, recordErr
	}

	// Upgrade a hash made with older parameters, now that the password is
	// known to be correct.
	if auth.NeedsRehash(user.PasswordHash) {
		if newHash, hashErr := auth.HashPassword(password); hashErr == nil {
			_ = s.users.UpdatePassword(ctx, user.ID, newHash)
		}
	}

	now := s.now().UTC()
	if touchErr := s.users.TouchLogin(ctx, user.ID, now); touchErr != nil {
		return TokenPair{}, touchErr
	}

	pair, err := s.issuePair(ctx, user, meta)
	if err != nil {
		return TokenPair{}, err
	}

	s.recorder.Record(ctx, audit.Event{
		Actor:     audit.Actor{UserID: user.ID, Username: user.Username, Role: user.Role},
		Action:    audit.ActionLogin,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return pair, nil
}

// dummyHash is a valid argon2id hash of an unguessable value. Verifying
// against it makes a login for a missing account cost the same as a real one.
var dummyHash = mustHash()

func mustHash() string {
	h, err := auth.HashPassword("iskele-timing-equalizer-not-a-real-password")
	if err != nil {
		// Only possible if crypto/rand is broken, in which case nothing else
		// in this daemon can be trusted either.
		panic("auth: cannot hash the timing equalizer: " + err.Error())
	}
	return h
}

// failLogin records the failure and returns the caller-facing error.
func (s *Auth) failLogin(ctx context.Context, username string, meta RequestMeta, cause error) error {
	if err := s.limiter.RecordFailure(ctx, meta.IP, username); err != nil {
		return err
	}
	s.recorder.Record(ctx, audit.Event{
		Actor:     audit.Actor{Username: username},
		Action:    audit.ActionLoginFailed,
		Err:       cause,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
		Detail:    map[string]any{"username": username},
	})
	return cause
}

// Refresh exchanges a refresh token for a new pair, rotating the old one.
//
// Rotation means a stolen refresh token stops working as soon as the real user
// refreshes, and the theft leaves a trace.
func (s *Auth) Refresh(ctx context.Context, refreshToken string, meta RequestMeta) (TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return TokenPair{}, ErrSessionInvalid
	}

	session, err := s.sessions.ByRefreshHash(ctx, auth.HashToken(refreshToken))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TokenPair{}, ErrSessionInvalid
		}
		return TokenPair{}, err
	}

	now := s.now().UTC()
	if !session.Active(now) {
		return TokenPair{}, ErrSessionInvalid
	}

	user, err := s.users.ByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TokenPair{}, ErrSessionInvalid
		}
		return TokenPair{}, err
	}
	if user.Disabled {
		// Disabling an account must end its sessions immediately.
		_ = s.sessions.RevokeAllForUser(ctx, user.ID)
		return TokenPair{}, ErrAccountDisabled
	}

	// Revoke first: if issuing the new pair fails, the old token is already
	// dead, which is the safe direction to fail in.
	if revokeErr := s.sessions.Revoke(ctx, session.ID); revokeErr != nil {
		return TokenPair{}, revokeErr
	}

	pair, err := s.issuePair(ctx, user, meta)
	if err != nil {
		return TokenPair{}, err
	}

	s.recorder.Record(ctx, audit.Event{
		Actor:     audit.Actor{UserID: user.ID, Username: user.Username, Role: user.Role},
		Action:    audit.ActionRefresh,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return pair, nil
}

// Logout revokes the session behind a refresh token.
//
// An unknown token is not an error: the caller wanted to be signed out, and
// they are.
func (s *Auth) Logout(ctx context.Context, refreshToken string, identity Identity, meta RequestMeta) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}

	session, err := s.sessions.ByRefreshHash(ctx, auth.HashToken(refreshToken))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.sessions.Revoke(ctx, session.ID); err != nil {
		return err
	}

	s.recorder.Record(ctx, audit.Event{
		Actor:     identity.Actor(),
		Action:    audit.ActionLogout,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return nil
}

// Authenticate resolves a bearer credential — a JWT access token or an API
// token — into an Identity.
func (s *Auth) Authenticate(ctx context.Context, credential string) (Identity, error) {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return Identity{}, ErrSessionInvalid
	}

	if auth.IsAPIToken(credential) {
		return s.authenticateAPIToken(ctx, credential)
	}
	return s.authenticateJWT(ctx, credential)
}

func (s *Auth) authenticateJWT(ctx context.Context, token string) (Identity, error) {
	claims, err := s.issuer.Parse(token)
	if err != nil {
		return Identity{}, err
	}

	// The token is only a claim about the past; the account may have been
	// disabled or deleted since it was issued.
	user, err := s.users.ByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Identity{}, auth.ErrTokenInvalid
		}
		return Identity{}, err
	}
	if user.Disabled {
		return Identity{}, ErrAccountDisabled
	}

	return Identity{UserID: user.ID, Username: user.Username, Role: user.Role}, nil
}

func (s *Auth) authenticateAPIToken(ctx context.Context, token string) (Identity, error) {
	record, err := s.tokens.ByHash(ctx, auth.HashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Identity{}, auth.ErrTokenInvalid
		}
		return Identity{}, err
	}

	now := s.now().UTC()
	if !record.Active(now) {
		if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
			return Identity{}, auth.ErrTokenExpired
		}
		return Identity{}, auth.ErrTokenInvalid
	}

	user, err := s.users.ByID(ctx, record.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Identity{}, auth.ErrTokenInvalid
		}
		return Identity{}, err
	}
	if user.Disabled {
		return Identity{}, ErrAccountDisabled
	}

	_ = s.tokens.Touch(ctx, record.ID, now)

	return Identity{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		TokenID:  record.ID,
		Scopes:   record.Scopes,
	}, nil
}

// User returns the account behind an identity, for GET /auth/me.
func (s *Auth) User(ctx context.Context, userID string) (store.User, error) {
	user, err := s.users.ByID(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, ErrSessionInvalid
	}
	return user, err
}

// issuePair mints an access token and a fresh session.
func (s *Auth) issuePair(ctx context.Context, user store.User, meta RequestMeta) (TokenPair, error) {
	accessToken, expiresAt, err := s.issuer.Issue(user)
	if err != nil {
		return TokenPair{}, err
	}

	refreshToken, refreshHash, err := auth.NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	sessionID, err := auth.NewID()
	if err != nil {
		return TokenPair{}, err
	}

	now := s.now().UTC()
	refreshExpiresAt := now.Add(s.refreshTTL)

	err = s.sessions.Create(ctx, store.Session{
		ID:          sessionID,
		UserID:      user.ID,
		RefreshHash: refreshHash,
		IP:          meta.IP,
		UserAgent:   meta.UserAgent,
		CreatedAt:   now,
		ExpiresAt:   refreshExpiresAt,
	})
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:      accessToken,
		ExpiresAt:        expiresAt,
		RefreshToken:     refreshToken,
		RefreshExpiresAt: refreshExpiresAt,
		User:             user,
	}, nil
}
