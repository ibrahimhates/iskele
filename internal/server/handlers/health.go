// Package handlers contains the HTTP handlers for every API resource.
package handlers

import (
	"net/http"
	"time"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/version"
)

// System serves the two unauthenticated endpoints: health and version.
type System struct {
	startedAt time.Time
}

// NewSystem returns a System handler set that reports uptime relative to now.
func NewSystem() *System {
	return &System{startedAt: time.Now()}
}

// HealthResponse is the body of GET /api/v1/health.
//
// It deliberately exposes nothing about internal state: the endpoint is
// reachable without authentication so a load balancer or systemd watchdog can
// poll it.
type HealthResponse struct {
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

// Health reports that the daemon is up and serving.
func (s *System) Health(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, r, http.StatusOK, HealthResponse{
		Status: "ok",
		Uptime: time.Since(s.startedAt).Round(time.Second).String(),
	})
	return nil
}

// Version reports the build metadata of the running binary.
func (s *System) Version(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, r, http.StatusOK, version.Get())
	return nil
}
