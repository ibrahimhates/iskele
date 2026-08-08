package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/store"
)

func newLimiter(t *testing.T, opts LimiterOptions) (*Limiter, *store.DB) {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{
		Path: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return NewLimiter(db.Logins, opts), db
}

func TestLimiterAllowsUntilTheThreshold(t *testing.T) {
	limiter, _ := newLimiter(t, LimiterOptions{MaxFailures: 3, Window: time.Hour, Lockout: time.Hour})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		status, err := limiter.Check(ctx, "192.0.2.1")
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if status.Locked {
			t.Fatalf("locked after %d failures, want the threshold to be 3", i)
		}
		if err := limiter.RecordFailure(ctx, "192.0.2.1", "alice"); err != nil {
			t.Fatalf("RecordFailure() error = %v", err)
		}
	}

	status, err := limiter.Check(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !status.Locked {
		t.Fatal("not locked after crossing the threshold")
	}
	if status.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want a positive duration", status.RetryAfter)
	}
}

func TestLockoutExpires(t *testing.T) {
	limiter, _ := newLimiter(t, LimiterOptions{MaxFailures: 2, Window: time.Hour, Lockout: 15 * time.Minute})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_ = limiter.RecordFailure(ctx, "192.0.2.1", "alice")
	}
	if status, _ := limiter.Check(ctx, "192.0.2.1"); !status.Locked {
		t.Fatal("not locked after the threshold")
	}

	// Move past the lockout window.
	limiter.SetClock(func() time.Time { return time.Now().Add(16 * time.Minute) })

	status, err := limiter.Check(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if status.Locked {
		t.Errorf("still locked %v after the lockout expired", status.RetryAfter)
	}
}

func TestFailuresOutsideTheWindowAreIgnored(t *testing.T) {
	limiter, _ := newLimiter(t, LimiterOptions{MaxFailures: 2, Window: 5 * time.Minute, Lockout: time.Minute})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = limiter.RecordFailure(ctx, "192.0.2.1", "alice")
	}

	// An hour later, those failures are outside the counting window.
	limiter.SetClock(func() time.Time { return time.Now().Add(time.Hour) })

	status, err := limiter.Check(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if status.Locked || status.Failures != 0 {
		t.Errorf("status = %+v, want the old failures to have aged out", status)
	}
}

func TestSuccessResetsTheCounter(t *testing.T) {
	limiter, _ := newLimiter(t, LimiterOptions{MaxFailures: 3, Window: time.Hour, Lockout: time.Hour})
	ctx := context.Background()

	_ = limiter.RecordFailure(ctx, "192.0.2.1", "alice")
	_ = limiter.RecordFailure(ctx, "192.0.2.1", "alice")
	if err := limiter.RecordSuccess(ctx, "192.0.2.1", "alice"); err != nil {
		t.Fatalf("RecordSuccess() error = %v", err)
	}

	status, err := limiter.Check(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if status.Failures != 0 {
		t.Errorf("Failures = %d, want the successful login to clear them", status.Failures)
	}
	if status.Remaining != 3 {
		t.Errorf("Remaining = %d, want the full allowance back", status.Remaining)
	}
}

func TestLockoutIsPerIP(t *testing.T) {
	limiter, _ := newLimiter(t, LimiterOptions{MaxFailures: 2, Window: time.Hour, Lockout: time.Hour})
	ctx := context.Background()

	// Keying on the username instead would let anyone lock out a known
	// account by failing its login on purpose.
	for i := 0; i < 5; i++ {
		_ = limiter.RecordFailure(ctx, "198.51.100.7", "alice")
	}

	if status, _ := limiter.Check(ctx, "198.51.100.7"); !status.Locked {
		t.Fatal("the offending IP is not locked")
	}

	status, err := limiter.Check(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if status.Locked {
		t.Error("a different IP was locked out by someone else's failures")
	}
}

func TestLimiterDefaults(t *testing.T) {
	limiter, _ := newLimiter(t, LimiterOptions{})

	if limiter.maxFailures != DefaultMaxFailures ||
		limiter.window != DefaultWindow ||
		limiter.lockout != DefaultLockout {
		t.Errorf("defaults not applied: %d / %v / %v",
			limiter.maxFailures, limiter.window, limiter.lockout)
	}
}

func TestPruneRemovesOldAttempts(t *testing.T) {
	limiter, db := newLimiter(t, LimiterOptions{MaxFailures: 3, Window: time.Minute, Lockout: time.Minute})
	ctx := context.Background()

	_ = limiter.RecordFailure(ctx, "192.0.2.1", "alice")

	// Well past window+lockout, the record cannot affect any decision.
	limiter.SetClock(func() time.Time { return time.Now().Add(time.Hour) })

	n, err := limiter.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}

	count, err := db.Logins.FailuresSince(ctx, "192.0.2.1", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince() error = %v", err)
	}
	if count != 0 {
		t.Errorf("failures = %d after prune, want 0", count)
	}
}
