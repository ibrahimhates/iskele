package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// collectPull drains a pull stream started over the given engine output.
func collectPull(t *testing.T, body string) ([]PullEvent, error) {
	t.Helper()

	out := make(chan PullEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		errCh <- readPullStream(context.Background(), strings.NewReader(body), out)
		close(out)
	}()

	var events []PullEvent
	for event := range out {
		events = append(events, event)
	}
	return events, <-errCh
}

func TestReadPullStreamParsesProgress(t *testing.T) {
	body := strings.Join([]string{
		`{"status":"Pulling from library/nginx","id":"1.27"}`,
		`{"status":"Pulling fs layer","progressDetail":{},"id":"a1"}`,
		`{"status":"Downloading","progressDetail":{"current":512,"total":1024},"id":"a1"}`,
		`{"status":"Pull complete","progressDetail":{},"id":"a1"}`,
		`{"status":"Status: Downloaded newer image for nginx:1.27"}`,
	}, "\n")

	events, err := collectPull(t, body)
	if err != nil {
		t.Fatalf("readPullStream() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}

	if events[2].ID != "a1" || events[2].Current != 512 || events[2].Total != 1024 {
		t.Errorf("progress event = %+v", events[2])
	}
	if events[4].Status != "Status: Downloaded newer image for nginx:1.27" {
		t.Errorf("final status = %q", events[4].Status)
	}
}

// The engine reports a failed pull inside a 200 response, so the JSON body is
// the only place the failure appears.
func TestReadPullStreamReportsAnErrorInsideTheStream(t *testing.T) {
	body := strings.Join([]string{
		`{"status":"Pulling from library/nope"}`,
		`{"errorDetail":{"message":"manifest for nope:latest not found"},"error":"manifest unknown"}`,
	}, "\n")

	events, err := collectPull(t, body)
	if err == nil {
		t.Fatal("readPullStream() error = nil, want the engine's failure")
	}
	if !strings.Contains(err.Error(), "manifest for nope") {
		t.Errorf("error = %v, want the detailed message", err)
	}
	// The error event still reaches the caller, so a UI can show it.
	if len(events) != 2 || events[1].Error == "" {
		t.Errorf("events = %+v", events)
	}
}

// errorDetail carries the useful text; the bare error field is often a
// summary. When only the summary is present it has to be used.
func TestReadPullStreamFallsBackToTheBareErrorField(t *testing.T) {
	_, err := collectPull(t, `{"error":"unauthorized: authentication required"}`)

	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("error = %v", err)
	}
}

// The engine occasionally emits blank or non-JSON keep-alive lines, and losing
// a whole pull over one would be absurd.
func TestReadPullStreamSkipsUnparseableLines(t *testing.T) {
	body := "\n{\"status\":\"Pulling\"}\nnot json\n{\"status\":\"Done\"}\n"

	events, err := collectPull(t, body)
	if err != nil {
		t.Fatalf("readPullStream() error = %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want the two parseable ones", len(events))
	}
}

func TestReadPullStreamStopsWhenTheCallerGoesAway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan PullEvent, 1)
	if err := readPullStream(ctx, strings.NewReader(`{"status":"Pulling"}`), out); err != nil {
		t.Errorf("readPullStream() error = %v, want a clean stop", err)
	}
}

func TestEncodeRegistryAuthIsBase64JSON(t *testing.T) {
	encoded, err := encodeRegistryAuth(RegistryAuth{
		Username:      "deploy",
		Password:      "s3cret",
		ServerAddress: "ghcr.io",
	})
	if err != nil {
		t.Fatalf("encodeRegistryAuth() error = %v", err)
	}

	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the header is not base64url: %v", err)
	}

	var decoded struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		ServerAddress string `json:"serveraddress"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}
	if decoded.Username != "deploy" || decoded.Password != "s3cret" {
		t.Errorf("decoded = %+v", decoded)
	}
	if decoded.ServerAddress != "ghcr.io" {
		t.Errorf("server address = %q", decoded.ServerAddress)
	}
}

// The engine's dangling filter is inverted from how the option reads, and
// getting it backwards would make "prune everything" remove nothing.
func TestBoolFilterValue(t *testing.T) {
	if boolFilterValue(true) != "true" || boolFilterValue(false) != "false" {
		t.Error("the filter values are not the engine's spelling")
	}
}

func TestSortedStringsDoesNotMutateItsInput(t *testing.T) {
	input := []string{"c", "a", "b"}
	sorted := sortedStrings(input)

	if input[0] != "c" {
		t.Errorf("the input was reordered: %v", input)
	}
	if sorted[0] != "a" || sorted[2] != "c" {
		t.Errorf("sorted = %v", sorted)
	}
}
