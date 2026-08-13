package handlers

import (
	"testing"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// The engine reports each layer separately and never a total, so a single
// progress bar only exists if this sums them.
func TestLayerProgressSumsAcrossLayers(t *testing.T) {
	p := newLayerProgress()

	// Status lines before any size is known cannot produce a figure.
	if got := p.observe(docker.PullEvent{Status: "Pulling from library/nginx"}); got != -1 {
		t.Errorf("percent = %d, want -1 before any layer reports a size", got)
	}

	p.observe(docker.PullEvent{ID: "a", Status: "Downloading", Current: 0, Total: 100})
	if got := p.observe(docker.PullEvent{ID: "b", Status: "Downloading", Current: 0, Total: 100}); got != 0 {
		t.Errorf("percent = %d, want 0", got)
	}

	p.observe(docker.PullEvent{ID: "a", Status: "Downloading", Current: 100, Total: 100})
	if got := p.observe(docker.PullEvent{ID: "b", Status: "Downloading", Current: 50, Total: 100}); got != 75 {
		t.Errorf("percent = %d, want 75 across both layers", got)
	}
}

// A layer's last figure replaces its previous one; adding them would race past
// 100 on the first layer alone.
func TestLayerProgressReplacesRatherThanAccumulates(t *testing.T) {
	p := newLayerProgress()

	p.observe(docker.PullEvent{ID: "a", Current: 30, Total: 100})
	p.observe(docker.PullEvent{ID: "a", Current: 60, Total: 100})
	if got := p.observe(docker.PullEvent{ID: "a", Current: 90, Total: 100}); got != 90 {
		t.Errorf("percent = %d, want 90", got)
	}
}

// A layer already on disk is announced complete with no size at all. Counting
// it as zero would make the bar go backwards as more layers appear.
func TestLayerProgressCompletesALayerAnnouncedWithoutASize(t *testing.T) {
	p := newLayerProgress()

	p.observe(docker.PullEvent{ID: "a", Status: "Downloading", Current: 20, Total: 100})
	if got := p.observe(docker.PullEvent{ID: "a", Status: "Pull complete"}); got != 100 {
		t.Errorf("percent = %d, want the layer counted complete", got)
	}
}

// The engine occasionally reports a current above the total on a resumed
// layer; a bar past 100 looks broken.
func TestLayerProgressIsCappedAtComplete(t *testing.T) {
	p := newLayerProgress()

	p.observe(docker.PullEvent{ID: "a", Current: 150, Total: 100})
	if got := p.observe(docker.PullEvent{ID: "a", Current: 150, Total: 100}); got != 100 {
		t.Errorf("percent = %d, want it capped at 100", got)
	}
}

// Status lines with no layer at all must not disturb the figure.
func TestLayerProgressIgnoresOverallStatusLines(t *testing.T) {
	p := newLayerProgress()

	p.observe(docker.PullEvent{ID: "a", Current: 50, Total: 100})
	if got := p.observe(docker.PullEvent{Status: "Digest: sha256:…"}); got != 50 {
		t.Errorf("percent = %d, want the figure unchanged", got)
	}
}
