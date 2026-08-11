package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/templates"
)

// Catalog deploys templates from the app catalog.
//
// It renders a template into the same container definition the create wizard
// produces and hands it to [Creator], so a catalog entry goes through the path
// whitelist and the privileged gate like anything else. Deploying from the
// catalog is a shortcut through the form, not around the checks.
type Catalog struct {
	catalog  *templates.Catalog
	creator  *Creator
	docker   docker.Client
	recorder *audit.Recorder
}

// NewCatalog builds the catalog service.
func NewCatalog(catalog *templates.Catalog, creator *Creator, client docker.Client,
	recorder *audit.Recorder,
) *Catalog {
	return &Catalog{catalog: catalog, creator: creator, docker: client, recorder: recorder}
}

// TemplateSummary is one catalog entry plus what this installation knows about
// it.
type TemplateSummary struct {
	templates.Template
	// Deployed counts the containers this template has produced here, so the
	// catalog can say "you already have one of these".
	Deployed int `json:"deployed"`
	// NeedsPrivileged warns before the operator fills in a form they will not
	// be allowed to submit.
	NeedsPrivileged bool `json:"needs_privileged"`
}

// MarshalJSON flattens the template and its local state into one object.
//
// Written by hand because the embedded [templates.Template] would otherwise
// decide the shape on its own if it ever grew a MarshalJSON, and because the
// two extra fields belong at the top level where a client can read them.
func (s TemplateSummary) MarshalJSON() ([]byte, error) {
	type plain templates.Template

	return marshalMerged(plain(s.Template), map[string]any{
		"deployed":         s.Deployed,
		"needs_privileged": s.NeedsPrivileged,
	})
}

// List returns the catalog, with the deployment count filled in.
func (s *Catalog) List(ctx context.Context) ([]TemplateSummary, []templates.LoadProblem) {
	entries := s.catalog.List()
	counts := s.deploymentCounts(ctx)

	out := make([]TemplateSummary, 0, len(entries))
	for _, template := range entries {
		out = append(out, TemplateSummary{
			Template:        template,
			Deployed:        counts[template.ID],
			NeedsPrivileged: template.NeedsPrivileged(),
		})
	}
	return out, s.catalog.Problems()
}

// Get returns one template.
func (s *Catalog) Get(ctx context.Context, id string) (TemplateSummary, error) {
	template, err := s.catalog.Get(id)
	if err != nil {
		return TemplateSummary{}, err
	}

	return TemplateSummary{
		Template:        template,
		Deployed:        s.deploymentCounts(ctx)[id],
		NeedsPrivileged: template.NeedsPrivileged(),
	}, nil
}

// Categories returns the categories in use.
func (s *Catalog) Categories() []string { return s.catalog.Categories() }

// deploymentCounts counts the containers each template has produced.
//
// A missing engine is not an error here: the catalog is worth browsing even
// when Docker is down, and the count is a convenience.
func (s *Catalog) deploymentCounts(ctx context.Context) map[string]int {
	counts := map[string]int{}

	containers, err := s.docker.ListContainers(ctx, docker.ListContainersOptions{All: true})
	if err != nil {
		return counts
	}

	for _, container := range containers {
		if id := container.Labels[templates.LabelTemplate]; id != "" {
			counts[id]++
		}
	}
	return counts
}

// DeployRequest is what the catalog form submits.
type DeployRequest struct {
	// Name is the container's name. Empty falls back to the template's id.
	Name string `json:"name"`
	// Values are the answers, keyed by field name.
	Values map[string]string `json:"values"`
	// Network attaches the container to an existing network, which is how a
	// deployed database is reached by the application that needs it.
	Network string `json:"network,omitempty"`
	// Start runs the container immediately. Off means create it and leave it
	// stopped, which is occasionally what an operator wants.
	Start *bool `json:"start,omitempty"`
}

// DeployResult is what the operator gets back.
type DeployResult struct {
	CreateResult
	// Template records which entry produced it.
	Template string `json:"template"`
	// Notes are the template's after-deploy instructions, with the operator's
	// own answers substituted.
	Notes string `json:"notes,omitempty"`
}

// Deploy renders a template and creates the container.
func (s *Catalog) Deploy(ctx context.Context, id string, req DeployRequest, opts CreateOptions,
	actor audit.Actor, meta RequestMeta,
) (DeployResult, error) {
	template, err := s.catalog.Get(id)
	if err != nil {
		return DeployResult{}, err
	}

	spec, err := template.Render(req.Name, req.Values)
	if err != nil {
		return DeployResult{}, err
	}
	if req.Network != "" {
		spec.Network.Name = req.Network
	}
	if req.Start != nil {
		spec.Start = *req.Start
	}

	// Create audits the container itself; this records which catalog entry
	// asked for it, which the container's own audit line cannot say.
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       "template.deploy",
		ResourceType: "template",
		ResourceID:   template.ID,
		Detail:       map[string]any{"name": spec.Name, "image": spec.Image},
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})

	result, err := s.creator.Create(ctx, spec, opts, actor, meta)
	if err != nil {
		return DeployResult{}, err
	}

	return DeployResult{
		CreateResult: result,
		Template:     template.ID,
		Notes:        template.RenderNotes(req.Values),
	}, nil
}

// generatedAlphabet is what a generated secret is drawn from.
//
// Letters and digits only: a password that travels through a shell, a YAML
// file and a connection string on its way to the application is a password
// whose punctuation will eventually be mangled by one of them.
const generatedAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Secret lengths.
const (
	minSecretLength     = 12
	maxSecretLength     = 128
	defaultSecretLength = 32
)

// GenerateSecret returns a random value for a password field.
//
// It is generated on the server rather than in the browser because the browser
// is where an operator's extension, or a machine without a good entropy
// source, would quietly produce something guessable.
func GenerateSecret(length int) (string, error) {
	if length <= 0 {
		length = defaultSecretLength
	}
	if length < minSecretLength {
		length = minSecretLength
	}
	if length > maxSecretLength {
		length = maxSecretLength
	}

	limit := big.NewInt(int64(len(generatedAlphabet)))
	var b strings.Builder
	b.Grow(length)

	for range length {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate a secret: %w", err)
		}
		b.WriteByte(generatedAlphabet[index.Int64()])
	}
	return b.String(), nil
}

// ErrNoTemplate reports a template the catalog does not have.
var ErrNoTemplate = templates.ErrNotFound

// IsTemplateValueError reports whether err is a template rejecting its answers,
// so the handler can answer 422 with the per-field list.
func IsTemplateValueError(err error) (*templates.ValueErrors, bool) {
	var values *templates.ValueErrors
	if errors.As(err, &values) {
		return values, true
	}
	return nil, false
}
