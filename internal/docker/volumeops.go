package docker

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
)

// CreateVolumeOptions is a new volume's definition.
type CreateVolumeOptions struct {
	// Name empty asks the engine for an anonymous volume with a random name.
	Name   string
	Driver string
	// DriverOpts are driver-specific, e.g. type/device/o for the local driver
	// mounting an NFS share.
	DriverOpts map[string]string
	Labels     map[string]string
}

// CreateVolume creates a volume and returns it as the engine reports it back.
func (e *engine) CreateVolume(ctx context.Context, opts CreateVolumeOptions) (Volume, error) {
	created, err := e.api.VolumeCreate(ctx, volume.CreateOptions{
		Name:       strings.TrimSpace(opts.Name),
		Driver:     strings.TrimSpace(opts.Driver),
		DriverOpts: opts.DriverOpts,
		Labels:     opts.Labels,
	})
	if err != nil {
		return Volume{}, classify("volume.create", "volume", opts.Name, err)
	}
	return toVolume(created), nil
}

// InspectVolume returns one volume, including usage data when the engine has it.
func (e *engine) InspectVolume(ctx context.Context, name string) (Volume, error) {
	found, err := e.api.VolumeInspect(ctx, name)
	if err != nil {
		return Volume{}, classify("volume.inspect", "volume", name, err)
	}
	return toVolume(found), nil
}

// RemoveVolume deletes a volume. Force removes one the engine still considers
// in use, which is how a volume left behind by a removed container is cleared.
func (e *engine) RemoveVolume(ctx context.Context, name string, force bool) error {
	if err := e.api.VolumeRemove(ctx, name, force); err != nil {
		return classify("volume.remove", "volume", name, err)
	}
	return nil
}

// PruneVolumes removes volumes no container references.
//
// This is the most destructive prune Iskele offers: an unused volume may still
// be the only copy of a database. The UI says so before calling it.
func (e *engine) PruneVolumes(ctx context.Context) (PruneReport, error) {
	report, err := e.api.VolumesPrune(ctx, filters.NewArgs())
	if err != nil {
		return PruneReport{}, classify("volume.prune", "volume", "", err)
	}
	return PruneReport{
		Deleted:        sortedStrings(report.VolumesDeleted),
		SpaceReclaimed: int64(report.SpaceReclaimed), //nolint:gosec // a reclaimed byte count cannot overflow int64
	}, nil
}

// InspectVolumeRaw returns the engine's unmodified volume payload.
func (e *engine) InspectVolumeRaw(ctx context.Context, name string) (RawInspect, error) {
	found, err := e.api.VolumeInspect(ctx, name)
	if err != nil {
		return nil, classify("volume.inspect", "volume", name, err)
	}
	payload, err := json.Marshal(found)
	if err != nil {
		return nil, NewError(KindUnknown, "volume.inspect", "volume", name,
			"could not encode the engine's response: "+err.Error())
	}
	return payload, nil
}
