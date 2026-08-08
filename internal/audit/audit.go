package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ibrahimhates/iskele/internal/store"
)

// Actor is who performed an action.
type Actor struct {
	UserID   string
	Username string
	Role     store.Role
	// TokenID is set when the request authenticated with an API token, so a
	// leaked token can be traced to the actions it took.
	TokenID string
}

// Event is one auditable operation.
type Event struct {
	Actor        Actor
	Action       string
	ResourceType string
	ResourceID   string
	// Err, when set, records the event as a failure with its message.
	Err error
	// Detail is arbitrary structured context. It is masked before storage.
	Detail    map[string]any
	IP        string
	UserAgent string
}

// Recorder writes audit entries.
//
// Recording never fails a request: an audit write that errors is logged and
// the operation continues, because refusing to start a container because the
// audit table is full would be worse than the missing record.
type Recorder struct {
	repo *store.AuditRepo
	log  *slog.Logger
	now  func() time.Time
}

// New builds a Recorder.
func New(repo *store.AuditRepo, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	return &Recorder{repo: repo, log: log, now: time.Now}
}

// SetClock replaces the time source, for tests.
func (r *Recorder) SetClock(now func() time.Time) { r.now = now }

// Record writes an audit entry.
func (r *Recorder) Record(ctx context.Context, e Event) {
	if r == nil || r.repo == nil {
		return
	}

	result := store.ResultOK
	detail := e.Detail
	if e.Err != nil {
		result = store.ResultError
		if detail == nil {
			detail = map[string]any{}
		}
		detail["error"] = e.Err.Error()
	}

	// Added after masking: these are server-generated, and the token ID is
	// exactly what an operator needs to trace a leaked credential's actions.
	// Masking it (the key contains "token") would defeat the purpose.
	var trusted map[string]any
	if e.Actor.TokenID != "" {
		trusted = map[string]any{"api_token_id": e.Actor.TokenID}
	}

	entry := store.AuditEntry{
		UserID:       e.Actor.UserID,
		Username:     e.Actor.Username,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Result:       result,
		Detail:       encodeDetail(r.log, detail, trusted),
		IP:           e.IP,
		UserAgent:    e.UserAgent,
		CreatedAt:    r.now().UTC(),
	}

	// A canceled request context must not lose the record of what already
	// happened, so the write gets its own short-lived context.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if _, err := r.repo.Append(writeCtx, entry); err != nil {
		r.log.Error("audit write failed",
			slog.String("action", e.Action),
			slog.String("actor", e.Actor.Username),
			slog.Any("error", err),
		)
	}
}

// encodeDetail masks the caller-supplied detail, then merges the trusted,
// server-generated fields on top, and serializes the result.
func encodeDetail(log *slog.Logger, detail, trusted map[string]any) string {
	if len(detail) == 0 && len(trusted) == 0 {
		return "{}"
	}

	masked, ok := MaskAny(detail).(map[string]any)
	if !ok || masked == nil {
		masked = map[string]any{}
	}
	for k, v := range trusted {
		masked[k] = v
	}

	encoded, err := json.Marshal(masked)
	if err != nil {
		// Losing the context is acceptable; losing the whole record is not.
		log.Warn("audit detail could not be encoded", slog.Any("error", err))
		return "{}"
	}
	return string(encoded)
}

// Common audit actions, so the strings are not retyped at each call site.
const (
	ActionBootstrap        = "auth.bootstrap"
	ActionLogin            = "auth.login"
	ActionLoginFailed      = "auth.login_failed"
	ActionLogout           = "auth.logout"
	ActionRefresh          = "auth.refresh"
	ActionContainerStart   = "container.start"
	ActionContainerStop    = "container.stop"
	ActionContainerRestart = "container.restart"
	ActionContainerRemove  = "container.remove"
)
