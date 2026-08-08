package handlers

import (
	"errors"
	"net/http"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
)

// notFoundCodes maps an engine resource kind onto its specific error code, so
// the UI can tell "this container is gone" from "this image is gone".
var notFoundCodes = map[string]httpx.Code{
	"container": httpx.CodeContainerNotFound,
	"image":     httpx.CodeImageNotFound,
	"volume":    httpx.CodeVolumeNotFound,
	"network":   httpx.CodeNetworkNotFound,
}

// engineMessage returns the engine's own text for an error, for the few
// responses that report a failure inside a 200 body rather than as an error.
func engineMessage(err error) string {
	return docker.Message(err)
}

// engineError converts an error from the service layer into an APIError.
//
// The engine's own message is passed through verbatim: iskeled talks to a
// trusted local daemon, and hiding its text would leave the operator guessing
// (see PROMPT §7 — error messages must not conceal technical detail).
func engineError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, service.ErrEmptyID) {
		return httpx.ErrBadRequest("%s", err.Error())
	}

	var engErr *docker.Error
	if !errors.As(err, &engErr) {
		return err
	}

	msg := engErr.Message

	switch engErr.Kind {
	case docker.KindNotFound:
		code, ok := notFoundCodes[engErr.Resource]
		if !ok {
			code = httpx.CodeNotFound
		}
		return httpx.NewError(http.StatusNotFound, code, "%s", msg).WithCause(err)

	case docker.KindConflict:
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict, "%s", msg).WithCause(err)

	case docker.KindInvalid:
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest, "%s", msg).WithCause(err)

	case docker.KindPermission:
		// The daemon refused, not Iskele's RBAC: 403 with the engine's reason.
		return httpx.NewError(http.StatusForbidden, httpx.CodeForbidden, "%s", msg).WithCause(err)

	case docker.KindUnavailable:
		return httpx.NewError(http.StatusServiceUnavailable, httpx.CodeDockerUnavailable, "%s", msg).WithCause(err)

	case docker.KindCanceled:
		// 499-style situation; the client is gone, so the body rarely arrives.
		return httpx.NewError(http.StatusRequestTimeout, httpx.CodeCanceled, "%s", msg).WithCause(err)

	default:
		return httpx.NewError(http.StatusBadGateway, httpx.CodeDockerError, "%s", msg).WithCause(err)
	}
}
