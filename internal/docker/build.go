package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/registry"
)

// buildChannelBuffer lets the engine run ahead of a slow browser without
// stalling the build itself.
const buildChannelBuffer = 128

// maxBuildLineBytes bounds one log line. A build that prints a megabyte
// without a newline must not be able to exhaust the daemon's memory.
const maxBuildLineBytes = 64 << 10

// BuildOptions is one image build.
type BuildOptions struct {
	// Context is the tar stream of the build context. The caller owns it and
	// is responsible for closing it.
	Context io.Reader
	// Dockerfile is the build file's path relative to the context root.
	Dockerfile string
	// Tags are the references to apply, e.g. "app:v2".
	Tags []string
	// BuildArgs are --build-arg values. A nil value passes the variable
	// through from the daemon's environment, which is the CLI's behavior.
	BuildArgs map[string]*string
	// Target selects a stage in a multi-stage Dockerfile.
	Target string
	// NoCache forces every layer to be rebuilt.
	NoCache bool
	// Pull re-fetches the base image even when a local copy exists.
	Pull bool
	// Platform requests a specific architecture, e.g. "linux/arm64".
	Platform string
	// Labels are applied to the resulting image.
	Labels map[string]string
	// Remove and ForceRemove control intermediate container cleanup. Remove
	// defaults to true in the engine; ForceRemove also removes them after a
	// failed build, which otherwise leaves debris behind.
	Remove      bool
	ForceRemove bool
	// Auth carries credentials for the registries the base images come from.
	Auth map[string]RegistryAuth
}

// BuildEvent is one line from the engine's build output.
type BuildEvent struct {
	// Stream is a chunk of build output, usually ending in a newline.
	Stream string `json:"stream,omitempty"`
	// Status and Progress carry the base-image pull that precedes a build.
	Status  string `json:"status,omitempty"`
	ID      string `json:"id,omitempty"`
	Current int64  `json:"current,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Error   string `json:"error,omitempty"`
	// Step and TotalSteps are parsed out of the "Step 3/12 :" prefix the
	// engine writes, so a UI can show progress without re-parsing text.
	Step       int `json:"step,omitempty"`
	TotalSteps int `json:"total_steps,omitempty"`
	// ImageID is set on the final line of a successful build.
	ImageID string `json:"image_id,omitempty"`
}

// BuildImage builds an image and reports the engine's output as it arrives.
func (e *engine) BuildImage(ctx context.Context, opts BuildOptions) (<-chan BuildEvent, <-chan error) {
	events := make(chan BuildEvent, buildChannelBuffer)
	errs := make(chan error, 1)

	if opts.Context == nil {
		errs <- NewError(KindInvalid, "image.build", "image", "", "a build context is required")
		close(events)
		close(errs)
		return events, errs
	}

	sdkOpts := build.ImageBuildOptions{
		Dockerfile:  opts.Dockerfile,
		Tags:        opts.Tags,
		BuildArgs:   opts.BuildArgs,
		Target:      opts.Target,
		NoCache:     opts.NoCache,
		PullParent:  opts.Pull,
		Platform:    opts.Platform,
		Labels:      opts.Labels,
		Remove:      opts.Remove,
		ForceRemove: opts.ForceRemove,
	}
	if len(opts.Auth) > 0 {
		sdkOpts.AuthConfigs = toAuthConfigs(opts.Auth)
	}

	resp, err := e.api.ImageBuild(ctx, opts.Context, sdkOpts)
	if err != nil {
		errs <- classify("image.build", "image", "", err)
		close(events)
		close(errs)
		return events, errs
	}

	go func() {
		defer close(events)
		defer close(errs)
		defer func() { _ = resp.Body.Close() }()

		if buildErr := readBuildStream(ctx, resp.Body, events); buildErr != nil {
			select {
			case errs <- classify("image.build", "image", "", buildErr):
			default:
			}
		}
	}()

	return events, errs
}

// buildLine is the engine's own JSON build record.
type buildLine struct {
	Stream         string `json:"stream"`
	Status         string `json:"status"`
	ID             string `json:"id"`
	Error          string `json:"error"`
	ProgressDetail *struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
	ErrorDetail *struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
	Aux *struct {
		ID string `json:"ID"`
	} `json:"aux"`
}

// stepPattern matches the "Step 3/12 :" prefix the engine writes.
var stepPattern = regexp.MustCompile(`^Step (\d+)/(\d+) :`)

// readBuildStream decodes the engine's newline-delimited JSON build output.
func readBuildStream(ctx context.Context, r io.Reader, out chan<- BuildEvent) error {
	scanner := bufio.NewScanner(r)
	// A build line can carry a compiler's whole error output, and a small
	// buffer would truncate exactly the text the operator needs.
	scanner.Buffer(make([]byte, 0, maxBuildLineBytes), 1<<20)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}

		var line buildLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			// The engine occasionally emits keep-alive whitespace; losing a
			// whole build over it would be absurd.
			continue
		}

		event := BuildEvent{
			Stream: line.Stream,
			Status: line.Status,
			ID:     line.ID,
		}
		if line.ProgressDetail != nil {
			event.Current = line.ProgressDetail.Current
			event.Total = line.ProgressDetail.Total
		}
		if line.Aux != nil {
			event.ImageID = line.Aux.ID
		}
		if line.ErrorDetail != nil {
			event.Error = line.ErrorDetail.Message
		} else if line.Error != "" {
			event.Error = line.Error
		}

		if match := stepPattern.FindStringSubmatch(strings.TrimSpace(line.Stream)); match != nil {
			event.Step, _ = strconv.Atoi(match[1])
			event.TotalSteps, _ = strconv.Atoi(match[2])
		}

		select {
		case out <- event:
		case <-ctx.Done():
			return nil
		}

		// A failed build arrives inside a 200 response, so this is the only
		// place the failure appears.
		if event.Error != "" {
			return NewError(KindUnknown, "image.build", "image", "", event.Error)
		}
	}

	return endOfStream(scanner.Err())
}

// toAuthConfigs renders the per-registry credentials the build API expects.
//
// A build pulls its own base images, so it needs the credentials keyed by
// registry rather than the single encoded header a pull takes.
func toAuthConfigs(auth map[string]RegistryAuth) map[string]registry.AuthConfig {
	out := make(map[string]registry.AuthConfig, len(auth))
	for server, cred := range auth {
		out[server] = registry.AuthConfig{
			Username:      cred.Username,
			Password:      cred.Password,
			ServerAddress: cred.ServerAddress,
		}
	}
	return out
}
