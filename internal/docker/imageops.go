package docker

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
)

// pullChannelBuffer lets the engine run ahead of a slow browser without
// stalling the pull itself.
const pullChannelBuffer = 64

// RegistryAuth is the credential a pull may need. Empty means anonymous.
type RegistryAuth struct {
	Username      string
	Password      string
	ServerAddress string
}

// PullOptions controls an image pull.
type PullOptions struct {
	// Ref is the image reference, e.g. "nginx:1.27" or "ghcr.io/org/app@sha256:…".
	Ref string
	// Platform requests a specific architecture, e.g. "linux/arm64". Empty
	// takes the daemon's own.
	Platform string
	// Auth is used when the registry needs it.
	Auth *RegistryAuth
}

// PullEvent is one progress line from the engine.
type PullEvent struct {
	// ID is the layer the line concerns, empty for overall status lines.
	ID     string `json:"id,omitempty"`
	Status string `json:"status"`
	// Current and Total are byte counts for the layer, both 0 when the engine
	// reported no progress figures.
	Current int64 `json:"current,omitempty"`
	Total   int64 `json:"total,omitempty"`
	// Error carries a failure the engine reported mid-stream. A pull can fail
	// after it has started, and the HTTP status will already have been 200.
	Error string `json:"error,omitempty"`
}

// PullImageProgress pulls an image and reports the engine's progress lines.
//
// The plain PullImage drains the same stream and returns only success or
// failure; this one exists for the UI's progress bar.
func (e *engine) PullImageProgress(ctx context.Context, opts PullOptions) (<-chan PullEvent, <-chan error) {
	events := make(chan PullEvent, pullChannelBuffer)
	errs := make(chan error, 1)

	ref := strings.TrimSpace(opts.Ref)
	if ref == "" {
		errs <- NewError(KindInvalid, "image.pull", "image", "", "an image reference is required")
		close(events)
		close(errs)
		return events, errs
	}

	sdkOpts := image.PullOptions{Platform: opts.Platform}
	if opts.Auth != nil {
		encoded, err := encodeRegistryAuth(*opts.Auth)
		if err != nil {
			errs <- classify("image.pull", "image", ref, err)
			close(events)
			close(errs)
			return events, errs
		}
		sdkOpts.RegistryAuth = encoded
	}

	body, err := e.api.ImagePull(ctx, ref, sdkOpts)
	if err != nil {
		errs <- classify("image.pull", "image", ref, err)
		close(events)
		close(errs)
		return events, errs
	}

	go func() {
		defer close(events)
		defer close(errs)
		defer func() { _ = body.Close() }()

		if pullErr := readPullStream(ctx, body, events); pullErr != nil {
			select {
			case errs <- classify("image.pull", "image", ref, pullErr):
			default:
			}
		}
	}()

	return events, errs
}

// pullLine is the engine's own JSON progress record.
type pullLine struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Error          string `json:"error"`
	ProgressDetail *struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
	ErrorDetail *struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// readPullStream decodes the engine's newline-delimited JSON progress.
func readPullStream(ctx context.Context, r interface{ Read([]byte) (int, error) }, out chan<- PullEvent) error {
	scanner := bufio.NewScanner(r)
	// Progress lines are short, but an error message with a registry's HTML
	// response in it is not; a small buffer would truncate the explanation.
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}

		var line pullLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			// A line we cannot parse is not worth failing the pull over; the
			// engine occasionally emits keep-alive whitespace.
			continue
		}

		event := PullEvent{ID: line.ID, Status: line.Status}
		if line.ProgressDetail != nil {
			event.Current = line.ProgressDetail.Current
			event.Total = line.ProgressDetail.Total
		}
		if line.ErrorDetail != nil {
			event.Error = line.ErrorDetail.Message
		} else if line.Error != "" {
			event.Error = line.Error
		}

		select {
		case out <- event:
		case <-ctx.Done():
			return nil
		}

		// The engine reports a failed pull inside a 200 response, so this is
		// the only place the failure appears.
		if event.Error != "" {
			return NewError(KindUnknown, "image.pull", "image", line.ID, event.Error)
		}
	}

	return endOfStream(scanner.Err())
}

// encodeRegistryAuth renders a credential the way the Engine API expects it:
// base64url of the JSON auth config, in a header.
func encodeRegistryAuth(auth RegistryAuth) (string, error) {
	payload, err := json.Marshal(registry.AuthConfig{
		Username:      auth.Username,
		Password:      auth.Password,
		ServerAddress: auth.ServerAddress,
	})
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(payload), nil
}

// RemoveImageOptions controls image deletion.
type RemoveImageOptions struct {
	// Force removes an image still referenced by a stopped container or by
	// several tags.
	Force bool
	// NoPrune keeps untagged parent layers.
	NoPrune bool
}

// ImageDeleted is one entry of a removal result.
type ImageDeleted struct {
	Deleted  string `json:"deleted,omitempty"`
	Untagged string `json:"untagged,omitempty"`
}

// RemoveImage deletes an image and reports what the engine actually removed.
func (e *engine) RemoveImage(ctx context.Context, id string, opts RemoveImageOptions) ([]ImageDeleted, error) {
	responses, err := e.api.ImageRemove(ctx, id, image.RemoveOptions{
		Force:         opts.Force,
		PruneChildren: !opts.NoPrune,
	})
	if err != nil {
		return nil, classify("image.remove", "image", id, err)
	}

	out := make([]ImageDeleted, 0, len(responses))
	for _, r := range responses {
		out = append(out, ImageDeleted{Deleted: r.Deleted, Untagged: r.Untagged})
	}
	return out, nil
}

// PruneReport is what a prune reclaimed.
type PruneReport struct {
	// Deleted names the objects removed: image IDs, volume names, network names.
	Deleted []string `json:"deleted"`
	// SpaceReclaimed is in bytes. The engine reports 0 for networks, which
	// occupy no space.
	SpaceReclaimed int64 `json:"space_reclaimed"`
}

// PruneImages removes unused images. When all is false only dangling images
// go, which is the safe default; all removes every image no container uses.
func (e *engine) PruneImages(ctx context.Context, all bool) (PruneReport, error) {
	args := filters.NewArgs()
	// The engine's filter is inverted: dangling=false means "everything
	// unused", dangling=true means "untagged only".
	args.Add("dangling", boolFilterValue(!all))

	report, err := e.api.ImagesPrune(ctx, args)
	if err != nil {
		return PruneReport{}, classify("image.prune", "image", "", err)
	}

	deleted := make([]string, 0, len(report.ImagesDeleted))
	for _, d := range report.ImagesDeleted {
		if d.Deleted != "" {
			deleted = append(deleted, d.Deleted)
			continue
		}
		if d.Untagged != "" {
			deleted = append(deleted, d.Untagged)
		}
	}
	// SpaceReclaimed is uint64 in the SDK and always well inside int64 for a
	// real disk.
	return PruneReport{Deleted: deleted, SpaceReclaimed: int64(report.SpaceReclaimed)}, nil //nolint:gosec // a reclaimed byte count cannot overflow int64
}

// boolFilterValue renders a filter flag the way the engine parses it.
func boolFilterValue(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// TagImage adds a new reference to an existing image.
func (e *engine) TagImage(ctx context.Context, id, ref string) error {
	if err := e.api.ImageTag(ctx, id, ref); err != nil {
		return classify("image.tag", "image", id, err)
	}
	return nil
}

// ImageHistoryEntry is one layer in an image's build history.
type ImageHistoryEntry struct {
	ID        string    `json:"id"`
	Created   time.Time `json:"created"`
	CreatedBy string    `json:"created_by"`
	Size      int64     `json:"size"`
	Comment   string    `json:"comment,omitempty"`
	Tags      []string  `json:"tags"`
}

// ImageHistory returns an image's layers, newest first.
func (e *engine) ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
	layers, err := e.api.ImageHistory(ctx, id)
	if err != nil {
		return nil, classify("image.history", "image", id, err)
	}

	out := make([]ImageHistoryEntry, 0, len(layers))
	for _, l := range layers {
		tags := l.Tags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, ImageHistoryEntry{
			ID:        l.ID,
			Created:   time.Unix(l.Created, 0).UTC(),
			CreatedBy: l.CreatedBy,
			Size:      l.Size,
			Comment:   l.Comment,
			Tags:      tags,
		})
	}
	return out, nil
}

// InspectImageRaw returns the engine's unmodified image inspect payload.
func (e *engine) InspectImageRaw(ctx context.Context, id string) (RawInspect, error) {
	inspect, err := e.api.ImageInspect(ctx, id)
	if err != nil {
		return nil, classify("image.inspect", "image", id, err)
	}
	payload, err := json.Marshal(inspect)
	if err != nil {
		return nil, NewError(KindUnknown, "image.inspect", "image", id,
			"could not encode the engine's response: "+err.Error())
	}
	return payload, nil
}

// sortedStrings returns a copy in a stable order, so two identical results
// compare equal.
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
