package handlers

import (
	"net/http"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
)

// Create serves POST /api/v1/containers and the endpoints the wizard needs
// before it can submit one.
type Create struct {
	svc   *service.Creator
	paths *service.PathGuard
}

// NewCreate builds the create handler set.
func NewCreate(svc *service.Creator, paths *service.PathGuard) *Create {
	return &Create{svc: svc, paths: paths}
}

// Container handles POST /containers.
//
// The caller's privileged permission is read here and passed to the service:
// the handler knows the identity, the service enforces the policy, and neither
// has to know both.
func (h *Create) Container(w http.ResponseWriter, r *http.Request) error {
	spec, err := decodeJSON[docker.ContainerSpec](r)
	if err != nil {
		return err
	}

	identity := middleware.IdentityFrom(r.Context())
	result, err := h.svc.Create(r.Context(), spec, service.CreateOptions{
		Privileged: middleware.RoleHas(identity.Role, middleware.PermPrivileged),
	}, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, result)
	return nil
}

// AllowedPaths handles GET /system/allowed-paths.
//
// The wizard's path picker needs the whitelist to show what may be mounted;
// without it the operator finds out only by being refused.
func (h *Create) AllowedPaths(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"paths": h.paths.Allowed(),
	})
	return nil
}
