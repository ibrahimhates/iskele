package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/ibrahimhates/iskele/internal/store"
)

// Brute-force defaults. PROMPT §6.7 asks for a strict limit on login; these
// are the numbers the login handler enforces.
const (
	// DefaultMaxFailures is how many failed attempts from one IP are tolerated
	// inside the window before the lockout starts.
	DefaultMaxFailures = 10
	// DefaultWindow is how far back failures are counted.
	DefaultWindow = 15 * time.Minute
	// DefaultLockout is how long an IP stays locked after crossing the limit.
	DefaultLockout = 15 * time.Minute
)

// Limiter throttles repeated failed logins per source IP.
//
// It is keyed on IP rather than username on purpose: keying on the username
// would let anyone lock a known account out by failing its login repeatedly.
type Limiter struct {
	attempts    *store.LoginAttemptRepo
	maxFailures int
	window      time.Duration
	lockout     time.Duration
	now         func() time.Time
}

// LimiterOptions configures a Limiter. Zero fields take the defaults.
type LimiterOptions struct {
	MaxFailures int
	Window      time.Duration
	Lockout     time.Duration
}

// NewLimiter builds a brute-force limiter over the attempt log.
func NewLimiter(attempts *store.LoginAttemptRepo, opts LimiterOptions) *Limiter {
	l := &Limiter{
		attempts:    attempts,
		maxFailures: opts.MaxFailures,
		window:      opts.Window,
		lockout:     opts.Lockout,
		now:         time.Now,
	}
	if l.maxFailures <= 0 {
		l.maxFailures = DefaultMaxFailures
	}
	if l.window <= 0 {
		l.window = DefaultWindow
	}
	if l.lockout <= 0 {
		l.lockout = DefaultLockout
	}
	return l
}

// SetClock replaces the time source, so tests can advance past a lockout.
func (l *Limiter) SetClock(now func() time.Time) { l.now = now }

// LockStatus describes whether an IP may attempt a login.
type LockStatus struct {
	// Locked reports whether attempts are currently refused.
	Locked bool
	// RetryAfter is how long until the lock lifts.
	RetryAfter time.Duration
	// Failures is the number of failures counted in the window.
	Failures int
	// Remaining is how many attempts are left before locking.
	Remaining int
}

// Check reports whether the IP is currently locked out.
func (l *Limiter) Check(ctx context.Context, ip string) (LockStatus, error) {
	now := l.now().UTC()

	failures, err := l.attempts.FailuresSince(ctx, ip, now.Add(-l.window))
	if err != nil {
		return LockStatus{}, err
	}

	status := LockStatus{Failures: failures, Remaining: l.maxFailures - failures}
	if status.Remaining < 0 {
		status.Remaining = 0
	}
	if failures < l.maxFailures {
		return status, nil
	}

	lastFailure, err := l.attempts.LastFailureAt(ctx, ip)
	if err != nil {
		return LockStatus{}, err
	}
	if lastFailure.IsZero() {
		return status, nil
	}

	unlockAt := lastFailure.Add(l.lockout)
	if now.Before(unlockAt) {
		status.Locked = true
		status.RetryAfter = unlockAt.Sub(now).Round(time.Second)
	}
	return status, nil
}

// RecordFailure logs a failed attempt.
func (l *Limiter) RecordFailure(ctx context.Context, ip, username string) error {
	if err := l.attempts.Record(ctx, ip, username, false); err != nil {
		return fmt.Errorf("record failed login: %w", err)
	}
	return nil
}

// RecordSuccess logs a successful attempt, which clears the IP's failure
// count for subsequent checks.
func (l *Limiter) RecordSuccess(ctx context.Context, ip, username string) error {
	if err := l.attempts.Record(ctx, ip, username, true); err != nil {
		return fmt.Errorf("record successful login: %w", err)
	}
	return nil
}

// Prune deletes attempt records that can no longer affect any decision.
func (l *Limiter) Prune(ctx context.Context) (int64, error) {
	cutoff := l.now().UTC().Add(-(l.window + l.lockout))
	return l.attempts.DeleteBefore(ctx, cutoff)
}
