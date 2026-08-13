package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
)

// ErrEmptyName is returned when a caller passes a blank resource reference.
var ErrEmptyName = errors.New("a name is required")

// Pull streams an image pull, applying the stored credential for its registry.
//
// Progress is forwarded to the caller rather than buffered: the UI's progress
// bar is the reason this endpoint exists at all.
func (s *Image) Pull(ctx context.Context, ref string, actor audit.Actor, meta RequestMeta) (<-chan docker.PullEvent, <-chan error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return closedPull(&docker.SpecError{Field: "image", Message: "an image reference is required"})
	}

	auth, err := s.registries.AuthFor(ctx, ref)
	if err != nil {
		return closedPull(err)
	}

	events, engineErrs := s.docker.PullImageProgress(ctx, docker.PullOptions{Ref: ref, Auth: auth})

	// The audit entry is written when the pull finishes, since only then is
	// its outcome known. The pull itself is not held up by it.
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		var failure error
		for err := range engineErrs {
			if err != nil && failure == nil {
				failure = err
			}
		}
		s.recorder.Record(ctx, audit.Event{
			Actor:        actor,
			Action:       "image.pull",
			ResourceType: "image",
			ResourceID:   ref,
			Err:          failure,
			Detail:       map[string]any{"reference": ref, "authenticated": auth != nil},
			IP:           meta.IP,
			UserAgent:    meta.UserAgent,
		})
		if failure != nil {
			errs <- failure
		}
	}()

	return events, errs
}

// closedPull returns an already-failed pull.
func closedPull(err error) (<-chan docker.PullEvent, <-chan error) {
	events := make(chan docker.PullEvent)
	errs := make(chan error, 1)
	errs <- err
	close(events)
	close(errs)
	return events, errs
}

// Remove deletes an image and reports what went.
func (s *Image) Remove(ctx context.Context, id string, opts docker.RemoveImageOptions,
	actor audit.Actor, meta RequestMeta,
) ([]docker.ImageDeleted, error) {
	id, err := normalizeName(id)
	if err != nil {
		return nil, err
	}

	deleted, err := s.docker.RemoveImage(ctx, id, opts)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "image.remove",
		ResourceType: "image",
		ResourceID:   id,
		Err:          err,
		Detail:       map[string]any{"force": opts.Force, "deleted": len(deleted)},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return deleted, err
}

// Prune removes unused images. all=false keeps every tagged image.
func (s *Image) Prune(ctx context.Context, all bool, actor audit.Actor, meta RequestMeta) (docker.PruneReport, error) {
	report, err := s.docker.PruneImages(ctx, all)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "image.prune",
		ResourceType: "image",
		Err:          err,
		Detail: map[string]any{
			"all":             all,
			"deleted":         len(report.Deleted),
			"space_reclaimed": report.SpaceReclaimed,
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return report, err
}

// Tag adds a reference to an existing image.
func (s *Image) Tag(ctx context.Context, id, ref string, actor audit.Actor, meta RequestMeta) error {
	id, err := normalizeName(id)
	if err != nil {
		return err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return &docker.SpecError{Field: "tag", Message: "a target reference is required"}
	}

	err = s.docker.TagImage(ctx, id, ref)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "image.tag",
		ResourceType: "image",
		ResourceID:   id,
		Err:          err,
		Detail:       map[string]any{"tag": ref},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return err
}

// History returns an image's layers.
func (s *Image) History(ctx context.Context, id string) ([]docker.ImageHistoryEntry, error) {
	id, err := normalizeName(id)
	if err != nil {
		return nil, err
	}
	return s.docker.ImageHistory(ctx, id)
}

// Inspect returns the engine's raw image payload.
func (s *Image) Inspect(ctx context.Context, id string) (docker.RawInspect, error) {
	id, err := normalizeName(id)
	if err != nil {
		return nil, err
	}
	return s.docker.InspectImageRaw(ctx, id)
}

// Create makes a volume.
func (s *Volume) Create(ctx context.Context, opts docker.CreateVolumeOptions,
	actor audit.Actor, meta RequestMeta,
) (docker.Volume, error) {
	opts.Name = strings.TrimSpace(opts.Name)

	created, err := s.docker.CreateVolume(ctx, opts)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "volume.create",
		ResourceType: "volume",
		ResourceID:   opts.Name,
		Err:          err,
		Detail:       map[string]any{"driver": opts.Driver, "options": opts.DriverOpts},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return created, err
}

// Get returns one volume.
func (s *Volume) Get(ctx context.Context, name string) (docker.Volume, error) {
	name, err := normalizeName(name)
	if err != nil {
		return docker.Volume{}, err
	}
	return s.docker.InspectVolume(ctx, name)
}

// Inspect returns the engine's raw volume payload.
func (s *Volume) Inspect(ctx context.Context, name string) (docker.RawInspect, error) {
	name, err := normalizeName(name)
	if err != nil {
		return nil, err
	}
	return s.docker.InspectVolumeRaw(ctx, name)
}

// Remove deletes a volume, with the data in it.
func (s *Volume) Remove(ctx context.Context, name string, force bool, actor audit.Actor, meta RequestMeta) error {
	name, err := normalizeName(name)
	if err != nil {
		return err
	}

	err = s.docker.RemoveVolume(ctx, name, force)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "volume.remove",
		ResourceType: "volume",
		ResourceID:   name,
		Err:          err,
		Detail:       map[string]any{"force": force},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return err
}

// Prune removes every volume no container references.
func (s *Volume) Prune(ctx context.Context, actor audit.Actor, meta RequestMeta) (docker.PruneReport, error) {
	report, err := s.docker.PruneVolumes(ctx)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "volume.prune",
		ResourceType: "volume",
		Err:          err,
		Detail: map[string]any{
			"deleted":         report.Deleted,
			"space_reclaimed": report.SpaceReclaimed,
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return report, err
}

// Create makes a network.
func (s *Network) Create(ctx context.Context, opts docker.CreateNetworkOptions,
	actor audit.Actor, meta RequestMeta,
) (docker.Network, error) {
	opts.Name = strings.TrimSpace(opts.Name)
	if opts.Name == "" {
		return docker.Network{}, ErrEmptyName
	}

	created, err := s.docker.CreateNetwork(ctx, opts)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "network.create",
		ResourceType: "network",
		ResourceID:   opts.Name,
		Err:          err,
		Detail: map[string]any{
			"driver":   opts.Driver,
			"internal": opts.Internal,
			"subnets":  len(opts.IPAM),
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	return created, err
}

// Get returns one network.
func (s *Network) Get(ctx context.Context, id string) (docker.Network, error) {
	id, err := normalizeName(id)
	if err != nil {
		return docker.Network{}, err
	}
	return s.docker.InspectNetwork(ctx, id)
}

// Inspect returns the engine's raw network payload.
func (s *Network) Inspect(ctx context.Context, id string) (docker.RawInspect, error) {
	id, err := normalizeName(id)
	if err != nil {
		return nil, err
	}
	return s.docker.InspectNetworkRaw(ctx, id)
}

// Remove deletes a network.
func (s *Network) Remove(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	id, err := normalizeName(id)
	if err != nil {
		return err
	}

	err = s.docker.RemoveNetwork(ctx, id)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "network.remove",
		ResourceType: "network",
		ResourceID:   id,
		Err:          err,
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return err
}

// Prune removes user-defined networks with nothing attached.
func (s *Network) Prune(ctx context.Context, actor audit.Actor, meta RequestMeta) (docker.PruneReport, error) {
	report, err := s.docker.PruneNetworks(ctx)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "network.prune",
		ResourceType: "network",
		Err:          err,
		Detail:       map[string]any{"deleted": report.Deleted},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return report, err
}

// Connect attaches a container to a network.
func (s *Network) Connect(ctx context.Context, networkID, containerID string,
	opts docker.ConnectOptions, actor audit.Actor, meta RequestMeta,
) error {
	networkID, err := normalizeName(networkID)
	if err != nil {
		return err
	}
	containerID, err = normalizeID(containerID)
	if err != nil {
		return err
	}

	err = s.docker.ConnectNetwork(ctx, networkID, containerID, opts)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "network.connect",
		ResourceType: "network",
		ResourceID:   networkID,
		Err:          err,
		Detail:       map[string]any{"container": containerID, "aliases": opts.Aliases},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return err
}

// Disconnect detaches a container from a network.
func (s *Network) Disconnect(ctx context.Context, networkID, containerID string, force bool,
	actor audit.Actor, meta RequestMeta,
) error {
	networkID, err := normalizeName(networkID)
	if err != nil {
		return err
	}
	containerID, err = normalizeID(containerID)
	if err != nil {
		return err
	}

	err = s.docker.DisconnectNetwork(ctx, networkID, containerID, force)
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "network.disconnect",
		ResourceType: "network",
		ResourceID:   networkID,
		Err:          err,
		Detail:       map[string]any{"container": containerID, "force": force},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
	return err
}

// normalizeName trims a resource reference and refuses a blank one, so an
// empty path parameter does not reach the engine as a wildcard.
func normalizeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrEmptyName
	}
	return trimmed, nil
}
