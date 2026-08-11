package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
)

// Users serves /api/v1/users and the caller's own two-factor enrollment.
type Users struct {
	svc *service.Users
}

// NewUsers builds the account handler set.
func NewUsers(svc *service.Users) *Users { return &Users{svc: svc} }

// List handles GET /users.
//
// Accounts go out through the same view the session endpoints use, which is
// what keeps the password hash and the two-factor secret in: a raw store row
// would leak both the moment somebody added a field.
func (h *Users) List(w http.ResponseWriter, r *http.Request) error {
	users, err := h.svc.List(r.Context())
	if err != nil {
		return err
	}

	out := make([]userResponse, 0, len(users))
	for _, user := range users {
		out = append(out, toUserResponse(user))
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(out))
	return nil
}

// Get handles GET /users/{id}.
func (h *Users) Get(w http.ResponseWriter, r *http.Request) error {
	user, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, toUserResponse(user))
	return nil
}

// Create handles POST /users.
func (h *Users) Create(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.CreateInput](r)
	if err != nil {
		return err
	}

	user, err := h.svc.Create(r.Context(), req, identityOf(r), metaOf(r))
	if err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, toUserResponse(user))
	return nil
}

// Update handles PUT /users/{id}.
func (h *Users) Update(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.UpdateInput](r)
	if err != nil {
		return err
	}

	user, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), req, identityOf(r), metaOf(r))
	if err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, toUserResponse(user))
	return nil
}

// Delete handles DELETE /users/{id}.
func (h *Users) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "id"), identityOf(r), metaOf(r)); err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusNoContent, nil)
	return nil
}

// ResetTOTP handles DELETE /users/{id}/totp: an admin clearing somebody else's
// second factor after they lost the device that produced it.
func (h *Users) ResetTOTP(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.ResetTOTP(r.Context(), chi.URLParam(r, "id"), identityOf(r), metaOf(r)); err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusNoContent, nil)
	return nil
}

// totpCodeRequest is the body of the two endpoints that ask for a code.
type totpCodeRequest struct {
	Code string `json:"code"`
}

// BeginTOTP handles POST /auth/totp/setup.
func (h *Users) BeginTOTP(w http.ResponseWriter, r *http.Request) error {
	setup, err := h.svc.BeginTOTP(r.Context(), identityOf(r), metaOf(r))
	if err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, setup)
	return nil
}

// ConfirmTOTP handles POST /auth/totp/verify.
func (h *Users) ConfirmTOTP(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[totpCodeRequest](r)
	if err != nil {
		return err
	}

	if err := h.svc.ConfirmTOTP(r.Context(), req.Code, identityOf(r), metaOf(r)); err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"totp_enabled": true})
	return nil
}

// DisableTOTP handles POST /auth/totp/disable.
func (h *Users) DisableTOTP(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[totpCodeRequest](r)
	if err != nil {
		return err
	}

	if err := h.svc.DisableTOTP(r.Context(), req.Code, identityOf(r), metaOf(r)); err != nil {
		return userError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"totp_enabled": false})
	return nil
}

// userError maps account-management failures onto the HTTP vocabulary.
func userError(err error) error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, service.ErrUserNotFound):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound, "%s", err.Error())

	case errors.Is(err, service.ErrLastAdmin):
		return httpx.NewError(http.StatusConflict, httpx.CodeLastAdmin, "%s", err.Error())

	case errors.Is(err, service.ErrSelfDelete), errors.Is(err, service.ErrOwnTOTPReset):
		return httpx.NewError(http.StatusForbidden, httpx.CodeForbidden, "%s", err.Error())

	case errors.Is(err, service.ErrUsernameUnavailable):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict, "%s", err.Error())

	case errors.Is(err, service.ErrTOTPUnavailable):
		return httpx.NewError(http.StatusServiceUnavailable, httpx.CodeTOTPUnavailable, "%s", err.Error())

	case errors.Is(err, auth.ErrInvalidTOTPCode), errors.Is(err, auth.ErrInvalidTOTPSecret):
		return httpx.NewError(http.StatusUnauthorized, httpx.CodeInvalidCredentials, "%s", err.Error())

	case errors.Is(err, service.ErrTOTPEnabled),
		errors.Is(err, service.ErrTOTPNotPending):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict, "%s", err.Error())

	case errors.Is(err, service.ErrInvalidRole),
		errors.Is(err, service.ErrUsernameRequired),
		errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong),
		errors.Is(err, auth.ErrPasswordTooWeak):
		return httpx.ErrValidation("%s", err.Error())

	default:
		return err
	}
}
