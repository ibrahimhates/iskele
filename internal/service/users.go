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
	"github.com/ibrahimhates/iskele/internal/store"
)

// User management errors.
var (
	ErrUserNotFound = errors.New("no such user")
	// ErrLastAdmin guards the one mistake that locks everyone out of the
	// panel: removing, demoting or disabling the last account that can
	// administer it. It covers the caller's own account too — stepping down is
	// allowed, but only once somebody else can take over.
	ErrLastAdmin = errors.New("this is the last admin account; promote another account first")
	// ErrSelfDelete refuses deleting the account the request came from. Unlike
	// a demotion, this is not a step anybody takes on purpose, and there is
	// always another admin who can do it.
	ErrSelfDelete = errors.New("you cannot delete the account you are signed in with")
	// ErrOwnTOTPReset points the caller at the endpoint that asks for a code.
	// Clearing one's own second factor without proving possession of it would
	// make the factor worth nothing.
	ErrOwnTOTPReset    = errors.New("to turn off your own two-factor authentication, confirm it with a code")
	ErrInvalidRole     = errors.New("role must be admin, operator or viewer")
	ErrTOTPUnavailable = errors.New("two-factor authentication needs the secret key, which is not configured")
	ErrTOTPNotPending  = errors.New("no two-factor setup is in progress for this account")
	ErrTOTPEnabled     = errors.New("two-factor authentication is already enabled for this account")
)

// Users implements account administration and two-factor enrollment.
//
// Account writes are an admin's business, with one deliberate exception:
// enrolling and disabling two-factor is the account holder's own, because a
// second factor nobody but its owner controls is the only kind worth having.
type Users struct {
	users    *store.UserRepo
	sessions *store.SessionRepo
	// secrets encrypts TOTP secrets at rest, so a stolen database is not a
	// stolen second factor.
	secrets  *crypto.SecretBox
	recorder *audit.Recorder
	now      func() time.Time
}

// NewUsers builds the account service. secrets may be nil, which leaves
// two-factor unavailable rather than silently unprotected.
func NewUsers(users *store.UserRepo, sessions *store.SessionRepo, secrets *crypto.SecretBox,
	recorder *audit.Recorder,
) *Users {
	return &Users{users: users, sessions: sessions, secrets: secrets, recorder: recorder, now: time.Now}
}

// SetClock replaces the time source, for tests.
func (s *Users) SetClock(now func() time.Time) { s.now = now }

// List returns every account.
func (s *Users) List(ctx context.Context) ([]store.User, error) {
	return s.users.List(ctx)
}

// Get returns one account.
func (s *Users) Get(ctx context.Context, id string) (store.User, error) {
	user, err := s.users.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, ErrUserNotFound
	}
	return user, err
}

// CreateInput is the body of POST /users.
type CreateInput struct {
	Username string     `json:"username"`
	Password string     `json:"password"`
	Role     store.Role `json:"role"`
}

// Create adds an account.
func (s *Users) Create(ctx context.Context, in CreateInput, actor Identity, meta RequestMeta) (store.User, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return store.User{}, ErrUsernameRequired
	}
	if !validRole(in.Role) {
		return store.User{}, ErrInvalidRole
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		return store.User{}, err
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return store.User{}, err
	}
	id, err := auth.NewID()
	if err != nil {
		return store.User{}, err
	}

	user := store.User{
		ID:           id,
		Username:     username,
		Role:         in.Role,
		PasswordHash: hash,
		CreatedAt:    s.now().UTC(),
	}
	if createErr := s.users.Create(ctx, user); createErr != nil {
		if errors.Is(createErr, store.ErrConflict) {
			return store.User{}, ErrUsernameUnavailable
		}
		return store.User{}, createErr
	}

	s.record(ctx, actor, meta, "user.create", user, map[string]any{"role": string(user.Role)}, nil)
	return user, nil
}

// UpdateInput is the body of PUT /users/{id}. Every field is optional: an
// absent field is left alone, which is what lets one form change a role
// without also resetting a password.
type UpdateInput struct {
	Role     *store.Role `json:"role,omitempty"`
	Password *string     `json:"password,omitempty"`
	Disabled *bool       `json:"disabled,omitempty"`
}

// Update changes a role, a password, or an account's disabled state.
//
// Resetting a password ends that account's sessions: an admin resetting it is
// either handing the account back to its owner or taking it away from someone,
// and both cases mean the tokens issued before now must stop working.
func (s *Users) Update(ctx context.Context, id string, in UpdateInput, actor Identity, meta RequestMeta) (store.User, error) {
	user, err := s.Get(ctx, id)
	if err != nil {
		return store.User{}, err
	}

	changed := map[string]any{}

	if in.Role != nil && *in.Role != user.Role {
		if !validRole(*in.Role) {
			return store.User{}, ErrInvalidRole
		}
		if user.Role == store.RoleAdmin {
			if lastErr := s.refuseIfLastAdmin(ctx, user.ID); lastErr != nil {
				return store.User{}, lastErr
			}
		}
		if updateErr := s.users.UpdateRole(ctx, user.ID, *in.Role); updateErr != nil {
			return store.User{}, updateErr
		}
		changed["role"] = string(*in.Role)
		user.Role = *in.Role
	}

	if in.Disabled != nil && *in.Disabled != user.Disabled {
		if *in.Disabled && user.Role == store.RoleAdmin {
			if lastErr := s.refuseIfLastAdmin(ctx, user.ID); lastErr != nil {
				return store.User{}, lastErr
			}
		}
		if updateErr := s.users.SetDisabled(ctx, user.ID, *in.Disabled); updateErr != nil {
			return store.User{}, updateErr
		}
		changed["disabled"] = *in.Disabled
		user.Disabled = *in.Disabled

		// A disabled account whose browser still holds a valid access token is
		// not disabled.
		if *in.Disabled {
			if revokeErr := s.revokeSessions(ctx, user.ID); revokeErr != nil {
				return store.User{}, revokeErr
			}
		}
	}

	if in.Password != nil {
		if validateErr := auth.ValidatePassword(*in.Password); validateErr != nil {
			return store.User{}, validateErr
		}
		hash, hashErr := auth.HashPassword(*in.Password)
		if hashErr != nil {
			return store.User{}, hashErr
		}
		if updateErr := s.users.UpdatePassword(ctx, user.ID, hash); updateErr != nil {
			return store.User{}, updateErr
		}
		if revokeErr := s.revokeSessions(ctx, user.ID); revokeErr != nil {
			return store.User{}, revokeErr
		}
		changed["password"] = "reset"
	}

	if len(changed) == 0 {
		return user, nil
	}

	s.record(ctx, actor, meta, "user.update", user, changed, nil)
	return user, nil
}

// Delete removes an account and every session it holds.
func (s *Users) Delete(ctx context.Context, id string, actor Identity, meta RequestMeta) error {
	user, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if user.ID == actor.UserID {
		return ErrSelfDelete
	}
	if user.Role == store.RoleAdmin {
		if lastErr := s.refuseIfLastAdmin(ctx, user.ID); lastErr != nil {
			return lastErr
		}
	}

	if err := s.revokeSessions(ctx, user.ID); err != nil {
		return err
	}
	if err := s.users.Delete(ctx, user.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	s.record(ctx, actor, meta, "user.delete", user, nil, nil)
	return nil
}

// TOTPSetup is what the enrollment screen needs: a URI to scan, and the same
// secret in text for a device that cannot scan.
type TOTPSetup struct {
	// Secret is the base32 secret, grouped for manual entry.
	Secret string `json:"secret"`
	// URI is the otpauth:// URI the QR code encodes.
	URI string `json:"uri"`
}

// BeginTOTP generates a secret and stores it, disabled.
//
// The secret is written before it is confirmed so that the code the browser is
// about to send can be checked against it — but with the enabled flag off, so
// an enrollment abandoned halfway leaves the account exactly as it was, able to
// sign in with a password alone.
func (s *Users) BeginTOTP(ctx context.Context, actor Identity, meta RequestMeta) (TOTPSetup, error) {
	if s.secrets == nil {
		return TOTPSetup{}, ErrTOTPUnavailable
	}

	user, err := s.Get(ctx, actor.UserID)
	if err != nil {
		return TOTPSetup{}, err
	}
	if user.TOTPEnabled {
		return TOTPSetup{}, ErrTOTPEnabled
	}

	secret, err := auth.NewTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	encrypted, err := s.secrets.Encrypt(secret)
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("encrypt two-factor secret: %w", err)
	}
	if err := s.users.SetTOTP(ctx, user.ID, encrypted, false); err != nil {
		return TOTPSetup{}, err
	}

	s.record(ctx, actor, meta, "user.totp_begin", user, nil, nil)

	return TOTPSetup{
		Secret: auth.FormatTOTPSecret(secret),
		URI:    auth.TOTPURI(totpIssuer, user.Username, secret),
	}, nil
}

// totpIssuer is the name authenticator apps show beside the account.
const totpIssuer = "iskele"

// ConfirmTOTP enables two-factor once the account holder proves they can
// produce a code.
func (s *Users) ConfirmTOTP(ctx context.Context, code string, actor Identity, meta RequestMeta) error {
	if s.secrets == nil {
		return ErrTOTPUnavailable
	}

	user, err := s.Get(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if user.TOTPEnabled {
		return ErrTOTPEnabled
	}
	if user.TOTPSecretEnc == "" {
		return ErrTOTPNotPending
	}

	secret, err := s.secrets.Decrypt(user.TOTPSecretEnc)
	if err != nil {
		return fmt.Errorf("decrypt two-factor secret: %w", err)
	}
	if err := auth.VerifyTOTP(secret, code, s.now()); err != nil {
		return err
	}

	if err := s.users.SetTOTP(ctx, user.ID, user.TOTPSecretEnc, true); err != nil {
		return err
	}

	s.record(ctx, actor, meta, "user.totp_enable", user, nil, nil)
	return nil
}

// DisableTOTP turns two-factor off for the caller's own account.
//
// A current code is required: an unattended browser must not be enough to
// remove the factor that protects the account from an unattended browser.
func (s *Users) DisableTOTP(ctx context.Context, code string, actor Identity, meta RequestMeta) error {
	if s.secrets == nil {
		return ErrTOTPUnavailable
	}

	user, err := s.Get(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if !user.TOTPEnabled {
		return nil
	}

	secret, err := s.secrets.Decrypt(user.TOTPSecretEnc)
	if err != nil {
		return fmt.Errorf("decrypt two-factor secret: %w", err)
	}
	if err := auth.VerifyTOTP(secret, code, s.now()); err != nil {
		return err
	}

	if err := s.users.SetTOTP(ctx, user.ID, "", false); err != nil {
		return err
	}

	s.record(ctx, actor, meta, "user.totp_disable", user, nil, nil)
	return nil
}

// ResetTOTP clears another account's second factor.
//
// This is the locked-out case: a phone was lost, and somebody with admin
// rights has to let its owner back in. It cannot be used on one's own account
// — that path is DisableTOTP, which asks for a code.
func (s *Users) ResetTOTP(ctx context.Context, id string, actor Identity, meta RequestMeta) error {
	user, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if user.ID == actor.UserID {
		return ErrOwnTOTPReset
	}

	if err := s.users.SetTOTP(ctx, user.ID, "", false); err != nil {
		return err
	}
	// The sessions this account already holds were opened with the factor that
	// is being removed; they should not outlive it.
	if err := s.revokeSessions(ctx, user.ID); err != nil {
		return err
	}

	s.record(ctx, actor, meta, "user.totp_reset", user, nil, nil)
	return nil
}

// refuseIfLastAdmin reports ErrLastAdmin when id is the only enabled admin
// left.
//
// It is checked for the caller's own account as well as anybody else's: an
// admin stepping down is a legitimate thing to do, and leaving the panel with
// nobody able to administer it is not.
func (s *Users) refuseIfLastAdmin(ctx context.Context, id string) error {
	all, err := s.users.List(ctx)
	if err != nil {
		return err
	}
	for _, other := range all {
		if other.ID != id && other.Role == store.RoleAdmin && !other.Disabled {
			return nil
		}
	}
	return ErrLastAdmin
}

// revokeSessions ends every session an account holds.
func (s *Users) revokeSessions(ctx context.Context, userID string) error {
	if s.sessions == nil {
		return nil
	}
	return s.sessions.RevokeAllForUser(ctx, userID)
}

func (s *Users) record(ctx context.Context, actor Identity, meta RequestMeta, action string,
	subject store.User, detail map[string]any, err error,
) {
	if s.recorder == nil {
		return
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["username"] = subject.Username

	s.recorder.Record(ctx, audit.Event{
		Actor:        actor.Actor(),
		Action:       action,
		ResourceType: "user",
		ResourceID:   subject.ID,
		Detail:       detail,
		Err:          err,
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
	})
}

func validRole(role store.Role) bool {
	switch role {
	case store.RoleAdmin, store.RoleOperator, store.RoleViewer:
		return true
	default:
		return false
	}
}
