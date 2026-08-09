package docker

import (
	"context"
	"strings"
	"testing"
)

// collectBuild drains a build stream over the given engine output.
func collectBuild(t *testing.T, body string) ([]BuildEvent, error) {
	t.Helper()

	out := make(chan BuildEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		errCh <- readBuildStream(context.Background(), strings.NewReader(body), out)
		close(out)
	}()

	var events []BuildEvent
	for event := range out {
		events = append(events, event)
	}
	return events, <-errCh
}

func TestReadBuildStreamCarriesOutput(t *testing.T) {
	body := strings.Join([]string{
		`{"stream":"Step 1/3 : FROM alpine:3.20"}`,
		`{"stream":"\n"}`,
		`{"stream":" ---> a1b2c3\n"}`,
		`{"stream":"Successfully built a1b2c3\n"}`,
		`{"aux":{"ID":"sha256:a1b2c3"}}`,
	}, "\n")

	events, err := collectBuild(t, body)
	if err != nil {
		t.Fatalf("readBuildStream() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	if events[4].ImageID != "sha256:a1b2c3" {
		t.Errorf("ImageID = %q, want the built image", events[4].ImageID)
	}
}

// The engine writes progress as text; parsing it here means the UI does not
// have to, and cannot get it subtly different.
func TestReadBuildStreamParsesTheStepPrefix(t *testing.T) {
	events, err := collectBuild(t, strings.Join([]string{
		`{"stream":"Step 3/12 : RUN go build ./..."}`,
		`{"stream":"go: downloading example.com/x v1.2.3\n"}`,
	}, "\n"))
	if err != nil {
		t.Fatalf("readBuildStream() error = %v", err)
	}

	if events[0].Step != 3 || events[0].TotalSteps != 12 {
		t.Errorf("step = %d/%d, want 3/12", events[0].Step, events[0].TotalSteps)
	}
	// A line that is not a step prefix must not carry stale numbers.
	if events[1].Step != 0 {
		t.Errorf("Step = %d on a plain output line", events[1].Step)
	}
}

// A base-image pull happens inside a build and reports as pull progress.
func TestReadBuildStreamCarriesTheBaseImagePull(t *testing.T) {
	events, err := collectBuild(t,
		`{"status":"Downloading","id":"a1","progressDetail":{"current":512,"total":1024}}`)
	if err != nil {
		t.Fatalf("readBuildStream() error = %v", err)
	}

	if events[0].Status != "Downloading" || events[0].Current != 512 || events[0].Total != 1024 {
		t.Errorf("event = %+v", events[0])
	}
}

// A failed build arrives inside a 200 response, so the JSON body is the only
// place the failure appears.
func TestReadBuildStreamReportsAFailureInsideTheStream(t *testing.T) {
	body := strings.Join([]string{
		`{"stream":"Step 2/2 : RUN false"}`,
		`{"errorDetail":{"code":1,"message":"The command '/bin/sh -c false' returned a non-zero code: 1"},"error":"The command '/bin/sh -c false' returned a non-zero code: 1"}`,
	}, "\n")

	events, err := collectBuild(t, body)
	if err == nil {
		t.Fatal("readBuildStream() error = nil, want the build failure")
	}
	if !strings.Contains(err.Error(), "non-zero code") {
		t.Errorf("error = %v, want the engine's own message", err)
	}
	// The error event reaches the caller too, so the log shows where it died.
	if len(events) != 2 || events[1].Error == "" {
		t.Errorf("events = %+v", events)
	}
}

func TestReadBuildStreamSkipsUnparseableLines(t *testing.T) {
	events, err := collectBuild(t, "\n{\"stream\":\"a\"}\nnot json\n{\"stream\":\"b\"}\n")
	if err != nil {
		t.Fatalf("readBuildStream() error = %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want the two parseable ones", len(events))
	}
}

func TestReadBuildStreamStopsWhenTheCallerGoesAway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan BuildEvent, 1)
	if err := readBuildStream(ctx, strings.NewReader(`{"stream":"x"}`), out); err != nil {
		t.Errorf("readBuildStream() error = %v, want a clean stop", err)
	}
}

// A build pulls its own base images, so it needs credentials keyed by
// registry rather than the single header a pull takes.
func TestToAuthConfigsKeysByRegistry(t *testing.T) {
	configs := toAuthConfigs(map[string]RegistryAuth{
		"ghcr.io":   {Username: "deploy", Password: "a", ServerAddress: "ghcr.io"},
		"docker.io": {Username: "hub", Password: "b", ServerAddress: "docker.io"},
	})

	if len(configs) != 2 {
		t.Fatalf("configs = %v", configs)
	}
	if configs["ghcr.io"].Username != "deploy" || configs["docker.io"].Password != "b" {
		t.Errorf("configs = %+v", configs)
	}
}

func TestBuildWithoutAContextIsRejected(t *testing.T) {
	e := &engine{}

	_, errs := e.BuildImage(context.Background(), BuildOptions{})

	err := <-errs
	if err == nil || !IsInvalid(err) {
		t.Errorf("error = %v, want an invalid-argument failure", err)
	}
}
