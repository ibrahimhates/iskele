package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ibrahimhates/iskele/internal/server/middleware"
)

// errorBody is the wire format of every error response:
//
//	{"error": {"code": "...", "message": "...", "details": {}}}
type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// WriteJSON serializes v as the response body. Encoding failures are logged
// rather than returned, because the status line has already been sent by then.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil || status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		middleware.LoggerFrom(r.Context()).Error("write json response failed",
			slog.String("path", r.URL.Path), slog.Any("error", err))
	}
}

// WriteError renders err using the standard error envelope. Non-APIErrors are
// reported to the client as an opaque 500 and logged in full.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := AsAPIError(err)
	if apiErr == nil {
		return
	}

	log := middleware.LoggerFrom(r.Context())
	attrs := []any{
		slog.String("code", string(apiErr.Code)),
		slog.Int("status", apiErr.Status),
		slog.String("path", r.URL.Path),
	}
	if cause := apiErr.Unwrap(); cause != nil {
		attrs = append(attrs, slog.Any("cause", cause))
	}
	if apiErr.Status >= http.StatusInternalServerError {
		log.Error(apiErr.Message, attrs...)
	} else {
		log.Debug(apiErr.Message, attrs...)
	}

	WriteJSON(w, r, apiErr.Status, errorBody{Error: errorPayload{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Details: apiErr.Details,
	}})
}

// Handler is a http.HandlerFunc that may return an error. Returning an error
// keeps handlers free of repeated write-and-return boilerplate.
type Handler func(w http.ResponseWriter, r *http.Request) error

// ServeHTTP makes Handler satisfy http.Handler, rendering any returned error
// through WriteError.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}
