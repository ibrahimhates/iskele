package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ibrahimhates/iskele/internal/store"
)

// Identity is the authenticated caller, as seen by the middleware chain.
//
// It mirrors service.Identity; duplicating the four fields here keeps the
// middleware package free of a dependency on the service layer, which would
// otherwise make the import graph cyclic.
type Identity struct {
	UserID   string
	Username string
	Role     store.Role
	TokenID  string
	Scopes   []string
}

// Authenticated reports whether a request carried a valid credential.
func (i Identity) Authenticated() bool { return i.UserID != "" }

const identityKey contextKey = 100

// WithIdentity returns a context carrying the authenticated caller.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// IdentityFrom returns the authenticated caller, or a zero Identity when the
// request was not authenticated.
func IdentityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey).(Identity)
	return id
}

// BearerToken extracts the credential from the Authorization header.
//
// Only the Bearer scheme is accepted; anything else is treated as absent.
func BearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}
