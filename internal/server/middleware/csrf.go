package middleware

import (
	"net/http"
	"strings"
)

// CSRF protection for a Bearer-token API.
//
// Iskele's browser client keeps its access token in memory and sends it in the
// Authorization header, which a cross-site form or image cannot set. That
// alone defeats classic CSRF. What is still reachable cross-origin is a simple
// request (GET/POST with a form content type) that relies on ambient
// credentials — so the rule enforced here is: every state-changing request
// must carry a Bearer token, and any request that carries an Origin header
// must have an allowed one.
//
// PROMPT §6.6 asks for either a double-submit cookie or Bearer-only
// acceptance; this is the second option, made explicit.

// safeMethods never change state, so they are exempt.
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// CSRFGuard rejects cross-origin state-changing requests.
//
// allowedOrigin, when non-empty, is the only Origin accepted besides
// same-origin requests; it exists for a deployment served from a different
// hostname than the API.
func CSRFGuard(allowedOrigin string, deny func(w http.ResponseWriter, r *http.Request, reason string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if safeMethods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}

			// A browser sends Origin on every cross-origin request and on
			// same-origin POSTs; a CLI sends none, which is fine because a CLI
			// is not subject to CSRF.
			origin := r.Header.Get("Origin")
			if origin != "" && !OriginAllowed(r, origin, allowedOrigin) {
				deny(w, r, "request origin "+origin+" is not allowed")
				return
			}

			// Ambient-credential requests cannot set this header.
			if BearerToken(r) == "" {
				deny(w, r, "state-changing requests must authenticate with a Bearer token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// OriginAllowed reports whether an Origin header may act on this server.
//
// It is also used for the WebSocket handshake in M4, where origin checking is
// the only defense available.
func OriginAllowed(r *http.Request, origin, allowedOrigin string) bool {
	if origin == "" {
		return true
	}
	if allowedOrigin != "" && strings.EqualFold(origin, allowedOrigin) {
		return true
	}

	host := r.Host
	if host == "" {
		return false
	}

	// Compare host:port, ignoring the scheme: a plaintext and a TLS listener
	// on the same host are the same origin for this purpose, and iskeled is
	// commonly reached through a proxy that terminates TLS.
	originHost := origin
	if idx := strings.Index(originHost, "://"); idx >= 0 {
		originHost = originHost[idx+3:]
	}
	return strings.EqualFold(originHost, host)
}
