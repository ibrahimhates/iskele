package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"time"
)

// responseRecorder captures the status code and response size for logging.
type responseRecorder struct {
	http.ResponseWriter
	status  int
	written int64
}

func (rr *responseRecorder) WriteHeader(status int) {
	if rr.status == 0 {
		rr.status = status
	}
	rr.ResponseWriter.WriteHeader(status)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	if rr.status == 0 {
		rr.status = http.StatusOK
	}
	n, err := rr.ResponseWriter.Write(b)
	rr.written += int64(n)
	return n, err
}

// Flush forwards to the underlying writer so SSE handlers keep working.
func (rr *responseRecorder) Flush() {
	if f, ok := rr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the original writer, which
// WebSocket hijacking and deadline control depend on.
func (rr *responseRecorder) Unwrap() http.ResponseWriter { return rr.ResponseWriter }

// Logger attaches a request-scoped logger to the context and writes one
// summary record per request once it completes.
func Logger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			log := base.With(
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			ctx := WithLogger(r.Context(), log)

			rec := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}

			attrs := []any{
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.written),
				slog.Duration("duration", time.Since(start).Round(time.Microsecond)),
				slog.String("remote_ip", ClientIP(r)),
			}

			switch {
			case rec.status >= http.StatusInternalServerError:
				log.Error("request failed", attrs...)
			case rec.status >= http.StatusBadRequest:
				log.Warn("request rejected", attrs...)
			default:
				log.Info("request", attrs...)
			}
		})
	}
}

// ClientIP returns the peer address of the request. Proxy headers are
// deliberately ignored: they are attacker-controlled, and rate limiting and
// brute-force protection are keyed off this value.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
