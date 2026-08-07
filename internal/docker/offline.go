package docker

import "context"

// offline is a Client that reports every call as KindUnavailable.
//
// iskeled must start even when Docker is down — otherwise an operator whose
// daemon crashed cannot reach the panel that would tell them so. The router
// installs this client in that case, and every Docker-backed route answers
// DOCKER_UNAVAILABLE with the reason.
type offline struct {
	reason string
}

var _ Client = (*offline)(nil)

// Offline returns a Client that fails every call with the given reason.
func Offline(reason string) Client {
	if reason == "" {
		reason = "not connected to the Docker daemon"
	}
	return &offline{reason: reason}
}

// err builds the uniform unavailable error for an operation.
func (o *offline) err(op, resource string) error {
	return &Error{Kind: KindUnavailable, Op: op, Resource: resource, Message: o.reason}
}

func (o *offline) Ping(context.Context) (Pong, error) {
	return Pong{}, o.err("docker.ping", "system")
}

func (o *offline) Info(context.Context) (SystemInfo, error) {
	return SystemInfo{}, o.err("system.info", "system")
}

func (o *offline) DiskUsage(context.Context) (DiskUsage, error) {
	return DiskUsage{}, o.err("system.df", "system")
}

func (o *offline) ListContainers(context.Context, ListContainersOptions) ([]Container, error) {
	return nil, o.err("container.list", "container")
}

func (o *offline) InspectContainer(context.Context, string) (ContainerDetail, error) {
	return ContainerDetail{}, o.err("container.inspect", "container")
}

func (o *offline) InspectContainerRaw(context.Context, string) (RawInspect, error) {
	return nil, o.err("container.inspect", "container")
}

func (o *offline) StartContainer(context.Context, string) error {
	return o.err("container.start", "container")
}

func (o *offline) StopContainer(context.Context, string, StopOptions) error {
	return o.err("container.stop", "container")
}

func (o *offline) RestartContainer(context.Context, string, StopOptions) error {
	return o.err("container.restart", "container")
}

func (o *offline) RemoveContainer(context.Context, string, RemoveContainerOptions) error {
	return o.err("container.remove", "container")
}

func (o *offline) ListImages(context.Context, ListImagesOptions) ([]Image, error) {
	return nil, o.err("image.list", "image")
}

func (o *offline) ListVolumes(context.Context) ([]Volume, error) {
	return nil, o.err("volume.list", "volume")
}

func (o *offline) ListNetworks(context.Context) ([]Network, error) {
	return nil, o.err("network.list", "network")
}

// Close is a no-op: there is no connection to release.
func (o *offline) Close() error { return nil }
