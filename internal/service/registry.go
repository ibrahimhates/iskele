package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/crypto"
	"github.com/ibrahimhates/iskele/internal/docker"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Registry errors, mapped to HTTP codes by the handlers.
var (
	ErrRegistryNotFound = errors.New("registry not found")
	ErrRegistryExists   = errors.New("a registry for this server already exists")
	ErrRegistryInvalid  = errors.New("registry definition is not valid")
)

// Registry manages private registry credentials.
//
// Passwords are encrypted with the master key before they reach the database
// and are decrypted only to authenticate a pull. Nothing reads one back to a
// client: the API exposes has_password, never the value.
type Registry struct {
	repo     *store.RegistryRepo
	box      *crypto.SecretBox
	recorder *audit.Recorder
}

// NewRegistry builds the registry service.
func NewRegistry(repo *store.RegistryRepo, box *crypto.SecretBox, recorder *audit.Recorder) *Registry {
	return &Registry{repo: repo, box: box, recorder: recorder}
}

// RegistryInput is a create or update request.
type RegistryInput struct {
	Name     string `json:"name"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// List returns every configured registry, without credentials.
func (s *Registry) List(ctx context.Context) ([]store.Registry, error) {
	entries, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Password = ""
	}
	return entries, nil
}

// Get returns one registry, without its credential.
func (s *Registry) Get(ctx context.Context, id string) (store.Registry, error) {
	reg, err := s.repo.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Registry{}, ErrRegistryNotFound
	}
	if err != nil {
		return store.Registry{}, err
	}
	reg.Password = ""
	return reg, nil
}

// Create adds a registry.
func (s *Registry) Create(ctx context.Context, in RegistryInput, actor audit.Actor, meta RequestMeta) (store.Registry, error) {
	reg, err := s.build(in)
	if err != nil {
		return store.Registry{}, err
	}

	reg.ID, err = auth.NewID()
	if err != nil {
		return store.Registry{}, err
	}

	err = s.repo.Create(ctx, &reg)
	if errors.Is(err, store.ErrConflict) {
		err = ErrRegistryExists
	}
	s.auditRegistry(ctx, actor, meta, "registry.create", reg, err)
	if err != nil {
		return store.Registry{}, err
	}

	reg.Password = ""
	reg.HasPassword = in.Password != ""
	return reg, nil
}

// Update replaces a registry. A blank password keeps the stored one, because
// the UI is never given the value it would otherwise have to send back.
func (s *Registry) Update(ctx context.Context, id string, in RegistryInput, actor audit.Actor, meta RequestMeta) (store.Registry, error) {
	existing, err := s.repo.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Registry{}, ErrRegistryNotFound
	}
	if err != nil {
		return store.Registry{}, err
	}

	reg, err := s.build(in)
	if err != nil {
		return store.Registry{}, err
	}
	reg.ID = id

	err = s.repo.Update(ctx, &reg)
	if errors.Is(err, store.ErrConflict) {
		err = ErrRegistryExists
	}
	s.auditRegistry(ctx, actor, meta, "registry.update", reg, err)
	if err != nil {
		return store.Registry{}, err
	}

	reg.Password = ""
	reg.CreatedAt = existing.CreatedAt
	reg.HasPassword = in.Password != "" || existing.HasPassword
	return reg, nil
}

// Delete removes a registry.
func (s *Registry) Delete(ctx context.Context, id string, actor audit.Actor, meta RequestMeta) error {
	reg, err := s.repo.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ErrRegistryNotFound
	}
	if err != nil {
		return err
	}

	err = s.repo.Delete(ctx, id)
	s.auditRegistry(ctx, actor, meta, "registry.delete", reg, err)
	if errors.Is(err, store.ErrNotFound) {
		return ErrRegistryNotFound
	}
	return err
}

// AuthFor returns the credential to pull this image reference with, or nil
// when no registry is configured for its host.
//
// A missing credential is not an error: most pulls are anonymous, and failing
// them because no registry row exists would break every public image.
func (s *Registry) AuthFor(ctx context.Context, imageRef string) (*docker.RegistryAuth, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}

	server := store.RegistryServerForImage(imageRef)
	reg, err := s.repo.ByServer(ctx, server)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if reg.Username == "" && reg.Password == "" {
		return nil, nil
	}

	password := ""
	if reg.Password != "" {
		password, err = s.box.Decrypt(reg.Password)
		if err != nil {
			// A credential that will not decrypt means the master key changed.
			// Saying so beats an authentication failure the operator would
			// blame on the registry.
			return nil, fmt.Errorf("registry %q: stored credential cannot be decrypted "+
				"with the current secret key: %w", reg.Name, err)
		}
	}

	// Best-effort: a pull is not worth failing because a usage timestamp did
	// not write.
	_ = s.repo.TouchLastUsed(ctx, reg.ID, time.Now())

	return &docker.RegistryAuth{
		Username:      reg.Username,
		Password:      password,
		ServerAddress: reg.Server,
	}, nil
}

// build validates the input and encrypts the password.
func (s *Registry) build(in RegistryInput) (store.Registry, error) {
	name := strings.TrimSpace(in.Name)
	server := store.NormalizeRegistryServer(in.Server)

	if name == "" {
		return store.Registry{}, fmt.Errorf("%w: a name is required", ErrRegistryInvalid)
	}
	if strings.TrimSpace(in.Server) == "" {
		return store.Registry{}, fmt.Errorf("%w: a server address is required", ErrRegistryInvalid)
	}
	if strings.ContainsAny(server, " /\\") {
		return store.Registry{}, fmt.Errorf(
			"%w: %q is not a host; use a name like ghcr.io or registry.example.com:5000",
			ErrRegistryInvalid, in.Server)
	}
	// A password without a username cannot authenticate anything, and the
	// reverse is a credential the operator will think is stored.
	if in.Password != "" && strings.TrimSpace(in.Username) == "" {
		return store.Registry{}, fmt.Errorf("%w: a password needs a username", ErrRegistryInvalid)
	}

	reg := store.Registry{
		Name:     name,
		Server:   server,
		Username: strings.TrimSpace(in.Username),
		Email:    strings.TrimSpace(in.Email),
	}

	if in.Password != "" {
		encrypted, err := s.box.Encrypt(in.Password)
		if err != nil {
			return store.Registry{}, fmt.Errorf("encrypt registry password: %w", err)
		}
		reg.Password = encrypted
	}

	return reg, nil
}

// auditRegistry records a credential change. The password is never in Detail:
// the masking in internal/audit would catch the key, but not writing it at all
// is the stronger guarantee.
func (s *Registry) auditRegistry(ctx context.Context, actor audit.Actor, meta RequestMeta,
	action string, reg store.Registry, err error,
) {
	s.recorder.Record(ctx, audit.Event{
		Actor:        actor,
		Action:       action,
		ResourceType: "registry",
		ResourceID:   reg.ID,
		Err:          err,
		Detail: map[string]any{
			"name":     reg.Name,
			"server":   reg.Server,
			"username": reg.Username,
		},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
}
