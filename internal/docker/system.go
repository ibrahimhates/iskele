package docker

import (
	"context"

	"github.com/docker/docker/api/types"
)

// Info summarizes the engine and the host it runs on.
func (e *engine) Info(ctx context.Context) (SystemInfo, error) {
	info, err := e.api.Info(ctx)
	if err != nil {
		return SystemInfo{}, classify("system.info", "system", "", err)
	}

	out := SystemInfo{
		ServerVersion:     info.ServerVersion,
		Name:              info.Name,
		OSType:            info.OSType,
		OperatingSystem:   info.OperatingSystem,
		Architecture:      info.Architecture,
		KernelVersion:     info.KernelVersion,
		NCPU:              info.NCPU,
		MemTotal:          info.MemTotal,
		DockerRootDir:     info.DockerRootDir,
		StorageDriver:     info.Driver,
		LoggingDriver:     info.LoggingDriver,
		CgroupDriver:      info.CgroupDriver,
		Containers:        info.Containers,
		ContainersRunning: info.ContainersRunning,
		ContainersPaused:  info.ContainersPaused,
		ContainersStopped: info.ContainersStopped,
		Images:            info.Images,
		Warnings:          info.Warnings,
	}

	// The API version is not part of Info; ask the negotiated client. A ping
	// failure here is not worth failing the whole call over.
	if p, err := e.Ping(ctx); err == nil {
		out.APIVersion = p.APIVersion
	}
	return out, nil
}

// DiskUsage reports what `docker system df` shows.
func (e *engine) DiskUsage(ctx context.Context) (DiskUsage, error) {
	du, err := e.api.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return DiskUsage{}, classify("system.df", "system", "", err)
	}

	out := DiskUsage{LayersSize: du.LayersSize}

	// Images: an image is reclaimable when nothing uses it.
	for _, img := range du.Images {
		if img == nil {
			continue
		}
		out.Images.Count++
		out.Images.Size += img.Size
		if img.Containers == 0 {
			out.Images.Reclaimable += img.Size
		}
	}

	// Containers: writable layers of stopped containers can be reclaimed.
	for _, c := range du.Containers {
		if c == nil {
			continue
		}
		out.Containers.Count++
		out.Containers.Size += c.SizeRw
		if c.State != "running" {
			out.Containers.Reclaimable += c.SizeRw
		}
	}

	// Volumes: a volume with no referencing container can be reclaimed.
	for _, v := range du.Volumes {
		if v == nil || v.UsageData == nil {
			if v != nil {
				out.Volumes.Count++
			}
			continue
		}
		out.Volumes.Count++
		out.Volumes.Size += v.UsageData.Size
		if v.UsageData.RefCount == 0 {
			out.Volumes.Reclaimable += v.UsageData.Size
		}
	}

	// Build cache: everything not in use is reclaimable.
	for _, b := range du.BuildCache {
		if b == nil {
			continue
		}
		out.BuildCache.Count++
		out.BuildCache.Size += b.Size
		if !b.InUse {
			out.BuildCache.Reclaimable += b.Size
		}
	}

	return out, nil
}
