// Package httpx holds the HTTP vocabulary shared by the router and every
// handler: the standard error envelope, error codes and JSON writers.
package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// Code identifies an error class in a stable, machine-readable way. The set is
// documented in docs/openapi.yaml and mirrored by the frontend.
type Code string

// Error codes. Only the ones reachable in this milestone are wired up; the
// rest are declared here so handlers added later use the same vocabulary.
const (
	CodeBadRequest       Code = "BAD_REQUEST"
	CodeValidationFailed Code = "VALIDATION_FAILED"
	CodeUnauthorized     Code = "UNAUTHORIZED"
	CodeForbidden        Code = "FORBIDDEN"
	CodeNotFound         Code = "NOT_FOUND"
	CodeMethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	CodeConflict         Code = "CONFLICT"
	CodeRateLimited      Code = "RATE_LIMITED"
	CodePayloadTooLarge  Code = "PAYLOAD_TOO_LARGE"
	CodeInternal         Code = "INTERNAL"
	CodeUnavailable      Code = "SERVICE_UNAVAILABLE"
	CodeCanceled         Code = "REQUEST_CANCELED"
)

// Authentication and authorization codes. The frontend switches on these to
// decide whether to show the bootstrap screen, attempt a refresh, or send the
// user back to the login form.
const (
	CodeNotInitialized     Code = "NOT_INITIALIZED"
	CodeAlreadyInitialized Code = "ALREADY_INITIALIZED"
	// CodeInvalidCredentials is an error identifier, not a credential.
	CodeInvalidCredentials Code = "INVALID_CREDENTIALS" //nolint:gosec // G101 false positive
	CodeTokenExpired       Code = "TOKEN_EXPIRED"
	CodeAccountDisabled    Code = "ACCOUNT_DISABLED"
	CodeCSRFInvalid        Code = "CSRF_INVALID"
)

// Docker-specific error codes. The frontend switches on these to offer the
// right recovery action (reconnect, refresh the list, force-remove, ...).
const (
	CodeContainerNotFound Code = "CONTAINER_NOT_FOUND"
	CodeImageNotFound     Code = "IMAGE_NOT_FOUND"
	CodeVolumeNotFound    Code = "VOLUME_NOT_FOUND"
	CodeNetworkNotFound   Code = "NETWORK_NOT_FOUND"
	CodeDockerUnavailable Code = "DOCKER_UNAVAILABLE"
	CodeDockerError       Code = "DOCKER_ERROR"
	// CodePathNotAllowed marks a bind mount or build context outside the
	// configured allowed_paths. The details carry the path and the whitelist,
	// so the UI can show the operator what it may use instead.
	CodePathNotAllowed Code = "PATH_NOT_ALLOWED"
)

// APIError is the error type every handler returns. It carries both the HTTP
// status and the stable code that clients switch on.
type APIError struct {
	// Status is the HTTP status code sent to the client.
	Status int
	// Code is the stable, machine-readable error identifier.
	Code Code
	// Message is a human-readable explanation. It is shown to the user
	// verbatim, so it must never contain secrets.
	Message string
	// Details carries structured, code-specific context (e.g. field errors).
	Details map[string]any
	// cause is the wrapped error, logged but never sent to the client.
	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is / errors.As.
func (e *APIError) Unwrap() error { return e.cause }

// WithCause attaches an internal error for logging. The cause is never
// serialized into the response body.
func (e *APIError) WithCause(err error) *APIError {
	e.cause = err
	return e
}

// WithDetails attaches structured context to the error body.
func (e *APIError) WithDetails(details map[string]any) *APIError {
	e.Details = details
	return e
}

// NewError builds an APIError with an explicit status and code.
func NewError(status int, code Code, format string, args ...any) *APIError {
	return &APIError{
		Status:  status,
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Convenience constructors for the statuses handlers reach for most often.

func ErrBadRequest(format string, args ...any) *APIError {
	return NewError(http.StatusBadRequest, CodeBadRequest, format, args...)
}

func ErrValidation(format string, args ...any) *APIError {
	return NewError(http.StatusUnprocessableEntity, CodeValidationFailed, format, args...)
}

func ErrUnauthorized(format string, args ...any) *APIError {
	return NewError(http.StatusUnauthorized, CodeUnauthorized, format, args...)
}

func ErrForbidden(format string, args ...any) *APIError {
	return NewError(http.StatusForbidden, CodeForbidden, format, args...)
}

func ErrNotFound(format string, args ...any) *APIError {
	return NewError(http.StatusNotFound, CodeNotFound, format, args...)
}

func ErrConflict(format string, args ...any) *APIError {
	return NewError(http.StatusConflict, CodeConflict, format, args...)
}

func ErrInternal(format string, args ...any) *APIError {
	return NewError(http.StatusInternalServerError, CodeInternal, format, args...)
}

// AsAPIError converts any error into an APIError. Errors that are not already
// APIErrors become opaque 500s: their text is logged, not returned, so
// internal details never leak to clients.
func AsAPIError(err error) *APIError {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return ErrInternal("an internal error occurred").WithCause(err)
}
