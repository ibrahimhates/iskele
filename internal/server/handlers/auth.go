package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
	"github.com/ibrahimhates/iskele/internal/store"
)

// maxAuthBodyBytes bounds credential payloads. Passwords are capped at 1 KiB
// by the policy, so anything near this is abuse.
const maxAuthBodyBytes = 8 << 10

// Auth serves /api/v1/auth.
type Auth struct {
	svc *service.Auth
}

// NewAuth builds the authentication handler set.
func NewAuth(svc *service.Auth) *Auth { return &Auth{svc: svc} }

// credentialsRequest is the body of bootstrap and login.
type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// refreshRequest is the body of refresh and logout.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// sessionResponse is what a successful bootstrap, login or refresh returns.
type sessionResponse struct {
	AccessToken      string       `json:"access_token"`
	TokenType        string       `json:"token_type"`
	ExpiresAt        string       `json:"expires_at"`
	RefreshToken     string       `json:"refresh_token"`
	RefreshExpiresAt string       `json:"refresh_expires_at"`
	User             userResponse `json:"user"`
}

// userResponse is the account view returned to clients. It deliberately omits
// the password hash and the TOTP secret.
type userResponse struct {
	ID          string                  `json:"id"`
	Username    string                  `json:"username"`
	Role        store.Role              `json:"role"`
	TOTPEnabled bool                    `json:"totp_enabled"`
	Disabled    bool                    `json:"disabled"`
	CreatedAt   string                  `json:"created_at"`
	LastLoginAt string                  `json:"last_login_at,omitempty"`
	Permissions []middleware.Permission `json:"permissions"`
}

func toUserResponse(u store.User) userResponse {
	out := userResponse{
		ID:          u.ID,
		Username:    u.Username,
		Role:        u.Role,
		TOTPEnabled: u.TOTPEnabled,
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt.Format(timeFormat),
		Permissions: middleware.PermissionsOf(u.Role),
	}
	if !u.LastLoginAt.IsZero() {
		out.LastLoginAt = u.LastLoginAt.Format(timeFormat)
	}
	return out
}

// Status handles GET /auth/status, the only call a client can make before
// signing in: it says whether the installation still needs bootstrapping.
func (h *Auth) Status(w http.ResponseWriter, r *http.Request) error {
	initialized, err := h.svc.Initialized(r.Context())
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"initialized": initialized})
	return nil
}

// Bootstrap handles POST /auth/bootstrap.
func (h *Auth) Bootstrap(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[credentialsRequest](r)
	if err != nil {
		return err
	}

	pair, err := h.svc.Bootstrap(r.Context(), req.Username, req.Password, metaOf(r))
	if err != nil {
		return authError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, toSessionResponse(pair))
	return nil
}

// Login handles POST /auth/login.
func (h *Auth) Login(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.LoginInput](r)
	if err != nil {
		return err
	}

	pair, err := h.svc.Login(r.Context(), req, metaOf(r))
	if err != nil {
		return authError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, toSessionResponse(pair))
	return nil
}

// Refresh handles POST /auth/refresh.
func (h *Auth) Refresh(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[refreshRequest](r)
	if err != nil {
		return err
	}

	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken, metaOf(r))
	if err != nil {
		return authError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, toSessionResponse(pair))
	return nil
}

// Logout handles POST /auth/logout.
func (h *Auth) Logout(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[refreshRequest](r)
	if err != nil {
		return err
	}

	identity := identityOf(r)
	if err := h.svc.Logout(r.Context(), req.RefreshToken, identity, metaOf(r)); err != nil {
		return authError(err)
	}

	httpx.WriteJSON(w, r, http.StatusNoContent, nil)
	return nil
}

// Me handles GET /auth/me.
func (h *Auth) Me(w http.ResponseWriter, r *http.Request) error {
	identity := middleware.IdentityFrom(r.Context())

	user, err := h.svc.User(r.Context(), identity.UserID)
	if err != nil {
		return authError(err)
	}

	response := toUserResponse(user)
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		userResponse
		TokenID string   `json:"token_id,omitempty"`
		Scopes  []string `json:"scopes,omitempty"`
	}{
		userResponse: response,
		TokenID:      identity.TokenID,
		Scopes:       identity.Scopes,
	})
	return nil
}

func toSessionResponse(p service.TokenPair) sessionResponse {
	return sessionResponse{
		AccessToken:      p.AccessToken,
		TokenType:        "Bearer",
		ExpiresAt:        p.ExpiresAt.Format(timeFormat),
		RefreshToken:     p.RefreshToken,
		RefreshExpiresAt: p.RefreshExpiresAt.Format(timeFormat),
		User:             toUserResponse(p.User),
	}
}

// metaOf collects the request context an audit record needs.
func metaOf(r *http.Request) service.RequestMeta {
	return service.RequestMeta{
		IP:        middleware.ClientIP(r),
		UserAgent: truncate(r.UserAgent(), 256),
	}
}

// identityOf adapts the middleware identity to the service one.
func identityOf(r *http.Request) service.Identity {
	id := middleware.IdentityFrom(r.Context())
	return service.Identity{
		UserID:   id.UserID,
		Username: id.Username,
		Role:     id.Role,
		TokenID:  id.TokenID,
		Scopes:   id.Scopes,
	}
}

// decodeJSON reads a bounded JSON body, rejecting unknown fields so a typo in
// a client is reported rather than silently ignored.
func decodeJSON[T any](r *http.Request) (T, error) {
	var out T

	if r.Body == nil {
		return out, httpx.ErrBadRequest("a JSON request body is required")
	}

	dec := json.NewDecoder(io.LimitReader(r.Body, maxAuthBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&out); err != nil {
		if errors.Is(err, io.EOF) {
			return out, httpx.ErrBadRequest("a JSON request body is required")
		}
		return out, httpx.ErrBadRequest("malformed JSON body: %s", err.Error())
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// authError maps a service-layer authentication error onto an APIError.
func authError(err error) error {
	if err == nil {
		return nil
	}

	var locked *service.LockedOutError
	if errors.As(err, &locked) {
		return httpx.NewError(http.StatusTooManyRequests, httpx.CodeRateLimited, "%s", locked.Error()).
			WithDetails(map[string]any{"retry_after_seconds": int(locked.RetryAfter.Seconds())}).
			WithCause(err)
	}

	switch {
	case errors.Is(err, service.ErrNotInitialized):
		return httpx.NewError(http.StatusConflict, httpx.CodeNotInitialized, "%s", err.Error())

	case errors.Is(err, service.ErrAlreadyInitialized):
		return httpx.NewError(http.StatusConflict, httpx.CodeAlreadyInitialized, "%s", err.Error())

	case errors.Is(err, service.ErrTOTPRequired):
		// Deliberately distinguishable from a wrong password: the form has to
		// know to ask for a code, and by this point the password was correct.
		return httpx.NewError(http.StatusUnauthorized, httpx.CodeTOTPRequired, "%s", err.Error())

	case errors.Is(err, service.ErrTOTPUnavailable):
		return httpx.NewError(http.StatusServiceUnavailable, httpx.CodeTOTPUnavailable, "%s", err.Error())

	case errors.Is(err, service.ErrInvalidCredentials):
		return httpx.NewError(http.StatusUnauthorized, httpx.CodeInvalidCredentials, "%s", err.Error())

	case errors.Is(err, service.ErrAccountDisabled):
		return httpx.NewError(http.StatusForbidden, httpx.CodeAccountDisabled, "%s", err.Error())

	case errors.Is(err, service.ErrSessionInvalid), errors.Is(err, auth.ErrTokenInvalid):
		return httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthorized, "%s", err.Error())

	case errors.Is(err, auth.ErrTokenExpired):
		return httpx.NewError(http.StatusUnauthorized, httpx.CodeTokenExpired, "%s", err.Error())

	case errors.Is(err, service.ErrUsernameUnavailable):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict, "%s", err.Error())

	case errors.Is(err, service.ErrUsernameRequired),
		errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong),
		errors.Is(err, auth.ErrPasswordTooWeak):
		return httpx.ErrValidation("%s", err.Error())

	default:
		return err
	}
}

// timeFormat is the single timestamp format the API emits.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"
