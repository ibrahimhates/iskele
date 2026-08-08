package middleware

import (
	"context"
	"net/http"
)

// ResolveIdentity turns a bearer credential into an Identity.
//
// It is a function rather than an interface so this package stays independent
// of the service layer.
type ResolveIdentity func(ctx context.Context, credential string) (Identity, error)

// DenyFunc writes a rejection using the caller's error format.
type DenyFunc func(w http.ResponseWriter, r *http.Request, err error)

// Authenticate resolves the Authorization header, if present.
//
// A missing credential is not an error here: the decision to require one
// belongs to RequireAuth, so unauthenticated routes can share the chain.
// A credential that is present but invalid is always rejected — silently
// downgrading a bad token to "anonymous" would turn a 401 into a confusing
// 403 or, worse, a success on a public route.
func Authenticate(resolve ResolveIdentity, deny DenyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			credential := BearerToken(r)
			if credential == "" {
				next.ServeHTTP(w, r)
				return
			}

			identity, err := resolve(r.Context(), credential)
			if err != nil {
				deny(w, r, err)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

// RequireAuth rejects requests that carry no valid identity.
func RequireAuth(deny DenyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IdentityFrom(r.Context()).Authenticated() {
				deny(w, r, ErrUnauthenticated)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission rejects a caller whose role lacks perm.
func RequirePermission(perm Permission, deny DenyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity := IdentityFrom(r.Context())
			if !identity.Authenticated() {
				deny(w, r, ErrUnauthenticated)
				return
			}
			if !RoleHas(identity.Role, perm) {
				deny(w, r, &PermissionError{Role: identity.Role, Permission: perm})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
