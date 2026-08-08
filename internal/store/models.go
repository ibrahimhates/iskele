package store

import "time"

// Role is an authorization role. The set is closed and enforced by a CHECK
// constraint in the schema.
type Role string

const (
	// RoleAdmin may do everything, including builds, prunes and user management.
	RoleAdmin Role = "admin"
	// RoleOperator may run containers but not build images or change settings.
	RoleOperator Role = "operator"
	// RoleViewer may only read.
	RoleViewer Role = "viewer"
)

// ValidRole reports whether r is one of the three known roles.
func ValidRole(r Role) bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

// User is an account that can sign in.
type User struct {
	ID            string    `json:"id"`
	Username      string    `json:"username"`
	Role          Role      `json:"role"`
	TOTPEnabled   bool      `json:"totp_enabled"`
	Disabled      bool      `json:"disabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LastLoginAt   time.Time `json:"last_login_at,omitempty"`
	PasswordHash  string    `json:"-"`
	TOTPSecretEnc string    `json:"-"`
}

// Session is a refresh-token grant.
type Session struct {
	ID          string
	UserID      string
	RefreshHash string
	IP          string
	UserAgent   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   time.Time
	LastUsedAt  time.Time
}

// Active reports whether the session may still be exchanged for new tokens.
func (s Session) Active(now time.Time) bool {
	return s.RevokedAt.IsZero() && now.Before(s.ExpiresAt)
}

// APIToken is a long-lived credential for headless use.
type APIToken struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	TokenHash  string    `json:"-"`
	Scopes     []string  `json:"scopes"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitempty"`
}

// Active reports whether the token may still authenticate a request.
func (t APIToken) Active(now time.Time) bool {
	if !t.RevokedAt.IsZero() {
		return false
	}
	if !t.ExpiresAt.IsZero() && now.After(t.ExpiresAt) {
		return false
	}
	return true
}

// AuditEntry records one state-changing operation.
type AuditEntry struct {
	ID           int64     `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	Username     string    `json:"username,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	Result       string    `json:"result"`
	Detail       string    `json:"detail,omitempty"`
	IP           string    `json:"ip,omitempty"`
	UserAgent    string    `json:"user_agent,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Audit results.
const (
	ResultOK    = "ok"
	ResultError = "error"
)

// LoginAttempt is one authentication attempt, used for brute-force limiting.
type LoginAttempt struct {
	ID        int64
	IP        string
	Username  string
	Success   bool
	CreatedAt time.Time
}
