package docker

import (
	"context"
	"strings"
	"testing"
)

func TestOfflineClientFailsEveryCallAsUnavailable(t *testing.T) {
	c := Offline("docker is down")
	ctx := context.Background()

	calls := map[string]func() error{
		"Ping":                func() error { _, err := c.Ping(ctx); return err },
		"Info":                func() error { _, err := c.Info(ctx); return err },
		"DiskUsage":           func() error { _, err := c.DiskUsage(ctx); return err },
		"ListContainers":      func() error { _, err := c.ListContainers(ctx, ListContainersOptions{}); return err },
		"InspectContainer":    func() error { _, err := c.InspectContainer(ctx, "x"); return err },
		"InspectContainerRaw": func() error { _, err := c.InspectContainerRaw(ctx, "x"); return err },
		"StartContainer":      func() error { return c.StartContainer(ctx, "x") },
		"StopContainer":       func() error { return c.StopContainer(ctx, "x", StopOptions{}) },
		"RestartContainer":    func() error { return c.RestartContainer(ctx, "x", StopOptions{}) },
		"RemoveContainer":     func() error { return c.RemoveContainer(ctx, "x", RemoveContainerOptions{}) },
		"ListImages":          func() error { _, err := c.ListImages(ctx, ListImagesOptions{}); return err },
		"ListVolumes":         func() error { _, err := c.ListVolumes(ctx); return err },
		"ListNetworks":        func() error { _, err := c.ListNetworks(ctx); return err },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !IsUnavailable(err) {
				t.Errorf("error = %v, want KindUnavailable", err)
			}
			if !strings.Contains(Message(err), "docker is down") {
				t.Errorf("message = %q, want the configured reason", Message(err))
			}
		})
	}
}

func TestOfflineCloseIsANoOp(t *testing.T) {
	if err := Offline("x").Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestOfflineUsesADefaultReason(t *testing.T) {
	_, err := Offline("").Ping(context.Background())

	if Message(err) == "" {
		t.Error("Message() is empty, want a default explanation")
	}
}

func TestOfflineErrorsNameTheResource(t *testing.T) {
	c := Offline("down")

	_, err := c.ListImages(context.Background(), ListImagesOptions{})
	if got := Resource(err); got != "image" {
		t.Errorf("Resource() = %q, want %q", got, "image")
	}
}
