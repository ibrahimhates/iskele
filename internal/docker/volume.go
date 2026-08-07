package docker

import (
	"context"
	"sort"

	"github.com/docker/docker/api/types/volume"
)

// ListVolumes returns every volume known to the engine, sorted by name.
func (e *engine) ListVolumes(ctx context.Context) ([]Volume, error) {
	resp, err := e.api.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, classify("volume.list", "volume", "", err)
	}

	out := make([]Volume, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil {
			continue
		}
		out = append(out, toVolume(*v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func toVolume(v volume.Volume) Volume {
	out := Volume{
		Name:       v.Name,
		Driver:     v.Driver,
		Mountpoint: v.Mountpoint,
		Scope:      v.Scope,
		CreatedAt:  parseDockerTime(v.CreatedAt),
		Labels:     nonNilMap(v.Labels),
		Options:    nonNilMap(v.Options),
		// The engine only computes usage when explicitly asked; -1 tells the
		// UI "unknown" rather than "empty".
		Size:     -1,
		RefCount: -1,
	}
	if v.UsageData != nil {
		out.Size = v.UsageData.Size
		out.RefCount = v.UsageData.RefCount
	}
	return out
}
