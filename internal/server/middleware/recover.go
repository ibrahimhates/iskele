package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover turns a panic in a handler into a 500 response instead of tearing
// down the whole daemon, logging the stack for diagnosis.
//
// http.ErrAbortHandler is re-panicked: net/http uses it to abort a response
// deliberately (hijacked connections do this), and swallowing it would leave
// the connection in an undefined state.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}

			LoggerFrom(r.Context()).Error("panic recovered",
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)

			// The handler may have written a partial response already; in that
			// case WriteHeader is a no-op and net/http logs a warning, which is
			// the best we can do without buffering every response.
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL","message":"an internal error occurred"}}`))
		}()

		next.ServeHTTP(w, r)
	})
}
