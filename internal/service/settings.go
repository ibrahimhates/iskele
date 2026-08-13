package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ibrahimhates/iskele/internal/audit"
	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/store"
)

// Setting keys. They are strings in a key/value table rather than columns, so
// adding one later needs no migration.
const (
	SettingAuditRetentionDays = "audit_retention_days"
	SettingBindMountWarning   = "bind_mount_warning"
)

// Retention bounds. Zero means "keep forever", which is a legitimate choice
// for an audit trail on a machine with disk to spare.
const (
	// MaxRetentionDays is ten years — past the point where a limit is a limit
	// and not a bug, but not unbounded, so a typo cannot store a number that
	// overflows a duration.
	MaxRetentionDays = 3650
	// DefaultAuditRetentionDays keeps the trail indefinitely by default:
	// deleting somebody's audit history because they never opened the settings
	// page would be the wrong default to pick for them.
	DefaultAuditRetentionDays = 0
)

// Settings errors.
var (
	ErrUnknownSetting = errors.New("no such setting")
	ErrRetentionRange = fmt.Errorf("retention must be between 0 and %d days, where 0 keeps everything", MaxRetentionDays)
)

// Settings serves the settings screen: the values an admin can change, and the
// facts about this installation they need to see beside them.
type Settings struct {
	repo     *store.SettingsRepo
	cfg      *config.Config
	recorder *audit.Recorder
}

// NewSettings builds the settings service.
func NewSettings(repo *store.SettingsRepo, cfg *config.Config, recorder *audit.Recorder) *Settings {
	return &Settings{repo: repo, cfg: cfg, recorder: recorder}
}

// Editable are the settings that can be changed while the daemon runs.
type Editable struct {
	// AuditRetentionDays ages entries out of the audit trail. 0 keeps them all.
	AuditRetentionDays int `json:"audit_retention_days"`
	// BindMountWarning shows a warning in the creation wizard whenever a bind
	// mount is added. It is a nudge, not a control: the path whitelist is what
	// actually decides.
	BindMountWarning bool `json:"bind_mount_warning"`
}

// Installation is what an operator needs to see but cannot change here.
//
// The socket path and the bind-mount whitelist come from the config file on
// purpose (D-080): they are startup-time security boundaries, and an admin who
// could widen `allowed_paths` from a browser would be one request away from
// mounting the whole filesystem into a container. Changing them means editing
// the file and restarting, which is a deliberate act with an audit trail of
// its own — the file's mtime.
type Installation struct {
	// ConfigFile is where these values came from, or empty for the built-in
	// defaults. It is the answer to "where do I change this?".
	ConfigFile   string   `json:"config_file,omitempty"`
	DockerHost   string   `json:"docker_host"`
	AllowedPaths []string `json:"allowed_paths"`
	DataDir      string   `json:"data_dir"`
	TemplateDir  string   `json:"template_dir"`
	Listen       string   `json:"listen"`
	TLSEnabled   bool     `json:"tls_enabled"`
	// AccessTTL and RefreshTTL are in seconds.
	AccessTTL  int `json:"access_ttl"`
	RefreshTTL int `json:"refresh_ttl"`
}

// View is the body of GET /settings.
type View struct {
	Editable
	Installation Installation `json:"installation"`
}

// Get returns the current settings and the installation's fixed facts.
func (s *Settings) Get(ctx context.Context) (View, error) {
	editable, err := s.editable(ctx)
	if err != nil {
		return View{}, err
	}

	view := View{Editable: editable}
	if s.cfg != nil {
		view.Installation = Installation{
			ConfigFile:   s.cfg.ConfigFile,
			DockerHost:   s.cfg.DockerHost,
			AllowedPaths: s.cfg.AllowedPaths,
			DataDir:      s.cfg.DataDir,
			TemplateDir:  s.cfg.TemplateDir,
			Listen:       s.cfg.Listen,
			TLSEnabled:   s.cfg.TLS.Enabled,
			AccessTTL:    int(s.cfg.Session.AccessTTL.Duration().Seconds()),
			RefreshTTL:   int(s.cfg.Session.RefreshTTL.Duration().Seconds()),
		}
	}
	if view.Installation.AllowedPaths == nil {
		view.Installation.AllowedPaths = []string{}
	}
	return view, nil
}

// Update is the body of PUT /settings. Every field is optional, so one form
// can change retention without also restating an unrelated preference.
type Update struct {
	AuditRetentionDays *int  `json:"audit_retention_days,omitempty"`
	BindMountWarning   *bool `json:"bind_mount_warning,omitempty"`
}

// Set applies an update and returns the resulting view.
func (s *Settings) Set(ctx context.Context, in Update, actor Identity, meta RequestMeta) (View, error) {
	changed := map[string]any{}

	if in.AuditRetentionDays != nil {
		days := *in.AuditRetentionDays
		if days < 0 || days > MaxRetentionDays {
			return View{}, ErrRetentionRange
		}
		if err := s.repo.Set(ctx, SettingAuditRetentionDays, strconv.Itoa(days)); err != nil {
			return View{}, err
		}
		changed[SettingAuditRetentionDays] = days
	}

	if in.BindMountWarning != nil {
		if err := s.repo.Set(ctx, SettingBindMountWarning, strconv.FormatBool(*in.BindMountWarning)); err != nil {
			return View{}, err
		}
		changed[SettingBindMountWarning] = *in.BindMountWarning
	}

	if len(changed) > 0 && s.recorder != nil {
		s.recorder.Record(ctx, audit.Event{
			Actor:        actor.Actor(),
			Action:       "settings.update",
			ResourceType: "settings",
			Detail:       changed,
			IP:           meta.IP,
			UserAgent:    meta.UserAgent,
		})
	}

	return s.Get(ctx)
}

// AuditRetention is how long audit entries are kept, or zero for forever.
//
// It is read from the store on every sweep rather than cached: an admin who
// sets a retention expects the next sweep to honor it, not the next restart.
func (s *Settings) AuditRetention(ctx context.Context) (time.Duration, error) {
	editable, err := s.editable(ctx)
	if err != nil {
		return 0, err
	}
	if editable.AuditRetentionDays <= 0 {
		return 0, nil
	}
	return time.Duration(editable.AuditRetentionDays) * 24 * time.Hour, nil
}

// editable reads the stored settings, falling back to the defaults for any
// that were never written.
//
// A value that will not parse is treated as unset rather than as an error: a
// settings table somebody edited by hand should not take the whole screen
// down, and the next save fixes it.
func (s *Settings) editable(ctx context.Context) (Editable, error) {
	out := Editable{
		AuditRetentionDays: DefaultAuditRetentionDays,
		BindMountWarning:   true,
	}
	if s.repo == nil {
		return out, nil
	}

	all, err := s.repo.All(ctx)
	if err != nil {
		return Editable{}, err
	}

	if raw, ok := all[SettingAuditRetentionDays]; ok {
		if days, convErr := strconv.Atoi(raw); convErr == nil && days >= 0 && days <= MaxRetentionDays {
			out.AuditRetentionDays = days
		}
	}
	if raw, ok := all[SettingBindMountWarning]; ok {
		if warn, convErr := strconv.ParseBool(raw); convErr == nil {
			out.BindMountWarning = warn
		}
	}
	return out, nil
}
