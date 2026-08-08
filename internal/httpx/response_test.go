package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeError(t *testing.T, body []byte) errorPayload {
	t.Helper()
	var env errorBody
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("response is not a valid error envelope: %v (%q)", err, body)
	}
	return env.Error
}

func TestWriteJSONSetsContentTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	WriteJSON(rec, req, http.StatusCreated, map[string]string{"a": "b"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got["a"] != "b" {
		t.Errorf("body = %v", got)
	}
}

func TestWriteJSONOmitsBodyForNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/x", nil)

	WriteJSON(rec, req, http.StatusNoContent, map[string]string{"ignored": "yes"})

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty for 204", rec.Body.String())
	}
}

func TestWriteErrorUsesStandardEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/containers/abc", nil)

	WriteError(rec, req, ErrNotFound("no such container: %s", "abc").
		WithDetails(map[string]any{"id": "abc"}))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	payload := decodeError(t, rec.Body.Bytes())
	if payload.Code != CodeNotFound {
		t.Errorf("code = %q, want %q", payload.Code, CodeNotFound)
	}
	if payload.Message != "no such container: abc" {
		t.Errorf("message = %q", payload.Message)
	}
	if payload.Details["id"] != "abc" {
		t.Errorf("details = %v", payload.Details)
	}
}

func TestWriteErrorHidesInternalCause(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	secret := errors.New("dial tcp 10.0.0.1:5432: connection refused")
	WriteError(rec, req, secret)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	payload := decodeError(t, rec.Body.Bytes())
	if payload.Code != CodeInternal {
		t.Errorf("code = %q, want %q", payload.Code, CodeInternal)
	}
	if payload.Message == secret.Error() {
		t.Error("internal error text leaked into the response body")
	}
}

func TestWriteErrorIgnoresNil(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	WriteError(rec, req, nil)

	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want nothing written for a nil error", rec.Body.String())
	}
}

func TestAsAPIErrorUnwrapsWrappedAPIErrors(t *testing.T) {
	inner := ErrForbidden("operator role may not build images")
	wrapped := errors.Join(errors.New("context"), inner)

	got := AsAPIError(wrapped)
	if got.Code != CodeForbidden {
		t.Errorf("code = %q, want %q", got.Code, CodeForbidden)
	}
	if got.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", got.Status)
	}
}

func TestAPIErrorWrapsCause(t *testing.T) {
	cause := errors.New("boom")
	err := ErrInternal("failed").WithCause(cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is could not find the wrapped cause")
	}
	if got := err.Error(); got == "" {
		t.Error("Error() returned an empty string")
	}
}

func TestHandlerRendersReturnedError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	Handler(func(http.ResponseWriter, *http.Request) error {
		return ErrBadRequest("bad %s", "input")
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if payload := decodeError(t, rec.Body.Bytes()); payload.Code != CodeBadRequest {
		t.Errorf("code = %q", payload.Code)
	}
}

func TestHandlerLeavesSuccessfulResponsesAlone(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	Handler(func(w http.ResponseWriter, r *http.Request) error {
		WriteJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
		return nil
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() == "" {
		t.Error("body is empty")
	}
}

func TestErrorConstructorsCarryExpectedStatuses(t *testing.T) {
	tests := []struct {
		err        *APIError
		wantStatus int
		wantCode   Code
	}{
		{ErrBadRequest("x"), http.StatusBadRequest, CodeBadRequest},
		{ErrValidation("x"), http.StatusUnprocessableEntity, CodeValidationFailed},
		{ErrUnauthorized("x"), http.StatusUnauthorized, CodeUnauthorized},
		{ErrForbidden("x"), http.StatusForbidden, CodeForbidden},
		{ErrNotFound("x"), http.StatusNotFound, CodeNotFound},
		{ErrConflict("x"), http.StatusConflict, CodeConflict},
		{ErrInternal("x"), http.StatusInternalServerError, CodeInternal},
	}
	for _, tt := range tests {
		if tt.err.Status != tt.wantStatus {
			t.Errorf("%s status = %d, want %d", tt.err.Code, tt.err.Status, tt.wantStatus)
		}
		if tt.err.Code != tt.wantCode {
			t.Errorf("code = %q, want %q", tt.err.Code, tt.wantCode)
		}
	}
}
