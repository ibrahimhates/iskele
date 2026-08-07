package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader is both read from the request and echoed on the response so
// a reverse proxy's ID survives end to end.
const RequestIDHeader = "X-Request-Id"

// maxInboundRequestIDLen bounds what we are willing to echo back from a
// client-supplied header, so it cannot be used to bloat responses or logs.
const maxInboundRequestIDLen = 64

// RequestID attaches a request ID to the context and the response headers,
// reusing an inbound X-Request-Id when one is present and sane.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = newRequestID()
		}

		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

// sanitizeRequestID keeps only printable ASCII within a bounded length; any
// other input is discarded in favor of a freshly generated ID.
func sanitizeRequestID(v string) string {
	if v == "" || len(v) > maxInboundRequestIDLen {
		return ""
	}
	for _, c := range v {
		if c < '!' || c > '~' {
			return ""
		}
	}
	return v
}

func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failing is fatal for auth later on, but a request ID is
		// only a correlation aid: degrade instead of failing the request.
		return "unknown"
	}
	return hex.EncodeToString(buf[:])
}
