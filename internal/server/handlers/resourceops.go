package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/server/middleware"
	"github.com/ibrahimhates/iskele/internal/service"
)

// writeRaw sends an engine payload through unmodified.
func writeRaw(w http.ResponseWriter, raw docker.RawInspect) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(raw)
	return err
}

// Pull handles SSE GET /images/pull.
//
// It is a stream rather than a request/response because a pull of a large
// image takes minutes and the operator needs to see it moving. Authentication
// is by ticket, like the other streaming endpoints.
func (h *Stream) Pull(w http.ResponseWriter, r *http.Request) error {
	ticket, err := h.redeemTicket(r, middleware.PermOperate)
	if err != nil {
		return err
	}

	ref := r.URL.Query().Get("ref")
	if ref == "" {
		return httpx.ErrBadRequest("a ?ref image reference is required")
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpx.ErrInternal("this connection does not support streaming")
	}

	// The pull is registered as a task so the drawer can show it even after
	// the operator navigates away from the page that started it, and so it can
	// be canceled from there.
	taskID, ctx, err := h.tasks.Start(r.Context(), "image.pull", ref, ticket.Username)
	if err != nil {
		return err
	}
	defer func() { h.tasks.Finish(taskID, ctx.Err()) }()

	events, errs := h.images.Pull(ctx, ref, ticketActor(ticket), metaOf(r))

	writeSSEHeaders(w)
	// The task ID goes out first so the client can cancel before any layer has
	// moved.
	writeSSEEvent(w, "task", mustJSON(map[string]string{"id": taskID}))
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	layers := newLayerProgress()

	// Both channels have to be drained to the end before the pull can be
	// called successful: the engine reports a failed pull inside a 200
	// response, so the failure arrives on errs after — or alongside — the last
	// progress line. Declaring success the moment events closes would race it.
	for events != nil || errs != nil {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			flusher.Flush()

		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}

			// A layer-level failure comes through as an event with Error set.
			if event.Error != "" {
				h.tasks.Finish(taskID, errors.New(event.Error))
				writeSSEEvent(w, "error", mustJSON(map[string]string{
					"code":    string(httpx.CodeDockerError),
					"message": event.Error,
				}))
				flusher.Flush()
				return nil
			}

			percent := layers.observe(event)
			h.tasks.Progress(taskID, percent, event.Status)

			payload, marshalErr := json.Marshal(pullProgress{PullEvent: event, Percent: percent})
			if marshalErr != nil {
				continue
			}
			if !writeSSEEvent(w, "progress", payload) {
				return nil
			}
			flusher.Flush()

		case pullErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if pullErr == nil {
				continue
			}
			h.tasks.Finish(taskID, pullErr)
			writeSSEEvent(w, "error", mustJSON(map[string]string{
				"code":    kindCodeOf(pullErr),
				"message": docker.Message(pullErr),
			}))
			flusher.Flush()
			return nil
		}
	}

	h.tasks.Finish(taskID, nil)
	writeSSEEvent(w, "done", json.RawMessage(`{}`))
	flusher.Flush()
	return nil
}

// pullProgress is one progress line plus the overall percentage, which the
// engine does not compute.
type pullProgress struct {
	docker.PullEvent
	// Percent is 0..100 across every layer, or -1 while no layer has reported
	// a size — the engine sends several status lines before that.
	Percent int `json:"percent"`
}

// layerProgress turns per-layer byte counts into one figure.
//
// The engine reports each layer separately and never a total, so the only way
// to show a single bar is to keep the last figure for every layer and sum
// them. Layers that report no size at all are ignored rather than counted as
// zero, which would make the bar go backwards as new ones appear.
type layerProgress struct {
	current map[string]int64
	total   map[string]int64
}

func newLayerProgress() *layerProgress {
	return &layerProgress{current: map[string]int64{}, total: map[string]int64{}}
}

// observe records one event and returns the overall percentage.
func (p *layerProgress) observe(event docker.PullEvent) int {
	if event.ID != "" && event.Total > 0 {
		p.current[event.ID] = event.Current
		p.total[event.ID] = event.Total
	}
	// A layer that is already present reports completion without a size.
	if event.ID != "" && event.Total == 0 &&
		(event.Status == "Already exists" || event.Status == "Pull complete") {
		if total, ok := p.total[event.ID]; ok {
			p.current[event.ID] = total
		}
	}

	var current, total int64
	for id, t := range p.total {
		total += t
		current += p.current[id]
	}
	if total == 0 {
		return -1
	}

	percent := int(current * 100 / total)
	if percent > 100 {
		percent = 100
	}
	return percent
}

// mustJSON encodes a value that cannot fail to encode.
func mustJSON(v any) []byte {
	payload, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return payload
}

// Remove handles DELETE /images/{id}. Query parameters: force, noprune.
func (h *Images) Remove(w http.ResponseWriter, r *http.Request) error {
	force, err := boolParam(r, "force")
	if err != nil {
		return err
	}
	noPrune, err := boolParam(r, "noprune")
	if err != nil {
		return err
	}

	deleted, err := h.svc.Remove(r.Context(), chi.URLParam(r, "id"),
		docker.RemoveImageOptions{Force: force, NoPrune: noPrune}, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": deleted})
	return nil
}

// Prune handles POST /images/prune. Query parameter: all.
func (h *Images) Prune(w http.ResponseWriter, r *http.Request) error {
	all, err := boolParam(r, "all")
	if err != nil {
		return err
	}

	report, err := h.svc.Prune(r.Context(), all, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, report)
	return nil
}

// tagRequest is the body of POST /images/{id}/tag.
type tagRequest struct {
	Tag string `json:"tag"`
}

// Tag handles POST /images/{id}/tag.
func (h *Images) Tag(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[tagRequest](r)
	if err != nil {
		return err
	}

	if err := h.svc.Tag(r.Context(), chi.URLParam(r, "id"), req.Tag, actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"id":     chi.URLParam(r, "id"),
		"tag":    req.Tag,
		"status": "ok",
	})
	return nil
}

// History handles GET /images/{id}/history.
func (h *Images) History(w http.ResponseWriter, r *http.Request) error {
	layers, err := h.svc.History(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(layers))
	return nil
}

// Inspect handles GET /images/{id}/inspect.
func (h *Images) Inspect(w http.ResponseWriter, r *http.Request) error {
	raw, err := h.svc.Inspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return engineError(err)
	}

	return writeRaw(w, raw)
}

// createVolumeRequest is the body of POST /volumes.
type createVolumeRequest struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	DriverOpts map[string]string `json:"driver_opts"`
	Labels     map[string]string `json:"labels"`
}

// Create handles POST /volumes.
func (h *Volumes) Create(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[createVolumeRequest](r)
	if err != nil {
		return err
	}

	created, err := h.svc.Create(r.Context(), docker.CreateVolumeOptions{
		Name:       req.Name,
		Driver:     req.Driver,
		DriverOpts: req.DriverOpts,
		Labels:     req.Labels,
	}, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, created)
	return nil
}

// Get handles GET /volumes/{name}.
func (h *Volumes) Get(w http.ResponseWriter, r *http.Request) error {
	found, err := h.svc.Get(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, found)
	return nil
}

// Inspect handles GET /volumes/{name}/inspect.
func (h *Volumes) Inspect(w http.ResponseWriter, r *http.Request) error {
	raw, err := h.svc.Inspect(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		return engineError(err)
	}

	return writeRaw(w, raw)
}

// Remove handles DELETE /volumes/{name}. Query parameter: force.
func (h *Volumes) Remove(w http.ResponseWriter, r *http.Request) error {
	force, err := boolParam(r, "force")
	if err != nil {
		return err
	}

	if err := h.svc.Remove(r.Context(), chi.URLParam(r, "name"), force, actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Prune handles POST /volumes/prune.
func (h *Volumes) Prune(w http.ResponseWriter, r *http.Request) error {
	report, err := h.svc.Prune(r.Context(), actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, report)
	return nil
}

// createNetworkRequest is the body of POST /networks.
type createNetworkRequest struct {
	Name       string              `json:"name"`
	Driver     string              `json:"driver"`
	Internal   bool                `json:"internal"`
	Attachable bool                `json:"attachable"`
	EnableIPv6 bool                `json:"enable_ipv6"`
	IPAM       []docker.IPAMConfig `json:"ipam"`
	Options    map[string]string   `json:"options"`
	Labels     map[string]string   `json:"labels"`
}

// Create handles POST /networks.
func (h *Networks) Create(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[createNetworkRequest](r)
	if err != nil {
		return err
	}

	created, err := h.svc.Create(r.Context(), docker.CreateNetworkOptions{
		Name:       req.Name,
		Driver:     req.Driver,
		Internal:   req.Internal,
		Attachable: req.Attachable,
		EnableIPv6: req.EnableIPv6,
		IPAM:       req.IPAM,
		Options:    req.Options,
		Labels:     req.Labels,
	}, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, created)
	return nil
}

// Get handles GET /networks/{id}.
func (h *Networks) Get(w http.ResponseWriter, r *http.Request) error {
	found, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, found)
	return nil
}

// Inspect handles GET /networks/{id}/inspect.
func (h *Networks) Inspect(w http.ResponseWriter, r *http.Request) error {
	raw, err := h.svc.Inspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return engineError(err)
	}

	return writeRaw(w, raw)
}

// Remove handles DELETE /networks/{id}.
func (h *Networks) Remove(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Remove(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Prune handles POST /networks/prune.
func (h *Networks) Prune(w http.ResponseWriter, r *http.Request) error {
	report, err := h.svc.Prune(r.Context(), actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, report)
	return nil
}

// connectRequest is the body of POST /networks/{id}/connect.
type connectRequest struct {
	Container   string   `json:"container"`
	Aliases     []string `json:"aliases"`
	IPv4Address string   `json:"ipv4_address"`
	IPv6Address string   `json:"ipv6_address"`
}

// Connect handles POST /networks/{id}/connect.
func (h *Networks) Connect(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[connectRequest](r)
	if err != nil {
		return err
	}

	err = h.svc.Connect(r.Context(), chi.URLParam(r, "id"), req.Container, docker.ConnectOptions{
		Aliases:     req.Aliases,
		IPv4Address: req.IPv4Address,
		IPv6Address: req.IPv6Address,
	}, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"network":   chi.URLParam(r, "id"),
		"container": req.Container,
		"status":    "connected",
	})
	return nil
}

// disconnectRequest is the body of POST /networks/{id}/disconnect.
type disconnectRequest struct {
	Container string `json:"container"`
	Force     bool   `json:"force"`
}

// Disconnect handles POST /networks/{id}/disconnect.
func (h *Networks) Disconnect(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[disconnectRequest](r)
	if err != nil {
		return err
	}

	err = h.svc.Disconnect(r.Context(), chi.URLParam(r, "id"), req.Container, req.Force,
		actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"network":   chi.URLParam(r, "id"),
		"container": req.Container,
		"status":    "disconnected",
	})
	return nil
}

// Registries serves /api/v1/registries.
type Registries struct {
	svc *service.Registry
}

// NewRegistries builds the registry handler set.
func NewRegistries(svc *service.Registry) *Registries { return &Registries{svc: svc} }

// List handles GET /registries.
func (h *Registries) List(w http.ResponseWriter, r *http.Request) error {
	entries, err := h.svc.List(r.Context())
	if err != nil {
		return err
	}

	httpx.WriteJSON(w, r, http.StatusOK, newList(entries))
	return nil
}

// Create handles POST /registries.
func (h *Registries) Create(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.RegistryInput](r)
	if err != nil {
		return err
	}

	created, err := h.svc.Create(r.Context(), req, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusCreated, created)
	return nil
}

// Update handles PUT /registries/{id}.
func (h *Registries) Update(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeJSON[service.RegistryInput](r)
	if err != nil {
		return err
	}

	updated, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), req, actorOf(r), metaOf(r))
	if err != nil {
		return engineError(err)
	}

	httpx.WriteJSON(w, r, http.StatusOK, updated)
	return nil
}

// Delete handles DELETE /registries/{id}.
func (h *Registries) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "id"), actorOf(r), metaOf(r)); err != nil {
		return engineError(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
