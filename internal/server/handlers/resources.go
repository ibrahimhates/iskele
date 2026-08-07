package handlers

import (
	"net/http"

	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/service"
)

// Images serves /api/v1/images.
type Images struct {
	svc *service.Image
}

// NewImages builds the image handler set.
func NewImages(svc *service.Image) *Images { return &Images{svc: svc} }

// List handles GET /images. Query parameters: all, dangling, label.
func (h *Images) List(w http.ResponseWriter, r *http.Request) error {
	all, err := boolParam(r, "all")
	if err != nil {
		return err
	}
	dangling, err := triStateParam(r, "dangling")
	if err != nil {
		return err
	}

	images, err := h.svc.List(r.Context(), service.ImageListOptions{
		All:      all,
		Dangling: dangling,
		Label:    listParam(r, "label"),
	})
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(images))
	return nil
}

// Volumes serves /api/v1/volumes.
type Volumes struct {
	svc *service.Volume
}

// NewVolumes builds the volume handler set.
func NewVolumes(svc *service.Volume) *Volumes { return &Volumes{svc: svc} }

// List handles GET /volumes.
func (h *Volumes) List(w http.ResponseWriter, r *http.Request) error {
	volumes, err := h.svc.List(r.Context())
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(volumes))
	return nil
}

// Networks serves /api/v1/networks.
type Networks struct {
	svc *service.Network
}

// NewNetworks builds the network handler set.
func NewNetworks(svc *service.Network) *Networks { return &Networks{svc: svc} }

// List handles GET /networks.
func (h *Networks) List(w http.ResponseWriter, r *http.Request) error {
	networks, err := h.svc.List(r.Context())
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(networks))
	return nil
}

// Engine serves /api/v1/system, reporting on the Docker engine itself.
type Engine struct {
	svc *service.System
}

// NewEngine builds the engine information handler set.
func NewEngine(svc *service.System) *Engine { return &Engine{svc: svc} }

// Info handles GET /system/info.
func (h *Engine) Info(w http.ResponseWriter, r *http.Request) error {
	info, err := h.svc.Info(r.Context())
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, info)
	return nil
}

// DiskUsage handles GET /system/df.
func (h *Engine) DiskUsage(w http.ResponseWriter, r *http.Request) error {
	usage, err := h.svc.DiskUsage(r.Context())
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, usage)
	return nil
}

// EngineStatus is the body of GET /system/ping: whether the daemon is
// reachable right now, which the UI polls to drive its connection banner.
type EngineStatus struct {
	Reachable  bool   `json:"reachable"`
	APIVersion string `json:"api_version,omitempty"`
	OSType     string `json:"os_type,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Ping handles GET /system/ping.
//
// Unlike the other engine endpoints this always answers 200: an unreachable
// daemon is the answer, not a failure of the request.
func (h *Engine) Ping(w http.ResponseWriter, r *http.Request) error {
	pong, err := h.svc.Ping(r.Context())
	if err != nil {
		httpx.WriteJSON(w, r, http.StatusOK, EngineStatus{
			Reachable: false,
			Error:     engineMessage(err),
		})
		return nil
	}

	httpx.WriteJSON(w, r, http.StatusOK, EngineStatus{
		Reachable:  true,
		APIVersion: pong.APIVersion,
		OSType:     pong.OSType,
	})
	return nil
}
