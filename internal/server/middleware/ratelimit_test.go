package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsTheBurstThenThrottles(t *testing.T) {
	limiter := NewRateLimiter(60, 3)

	for i := 0; i < 3; i++ {
		if allowed, _ := limiter.Allow("192.0.2.1"); !allowed {
			t.Fatalf("request %d was refused inside the burst", i)
		}
	}

	allowed, retryAfter := limiter.Allow("192.0.2.1")
	if allowed {
		t.Fatal("the burst was not enforced")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want a positive wait", retryAfter)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	limiter := NewRateLimiter(60, 2) // one token per second
	now := time.Now()
	limiter.SetClock(func() time.Time { return now })

	limiter.Allow("192.0.2.1")
	limiter.Allow("192.0.2.1")
	if allowed, _ := limiter.Allow("192.0.2.1"); allowed {
		t.Fatal("the bucket did not empty")
	}

	now = now.Add(2 * time.Second)
	if allowed, _ := limiter.Allow("192.0.2.1"); !allowed {
		t.Error("the bucket did not refill")
	}
}

func TestRateLimiterIsPerClient(t *testing.T) {
	limiter := NewRateLimiter(60, 1)

	limiter.Allow("192.0.2.1")
	if allowed, _ := limiter.Allow("192.0.2.1"); allowed {
		t.Fatal("the first client was not throttled")
	}

	// One noisy client must not throttle everyone else.
	if allowed, _ := limiter.Allow("198.51.100.7"); !allowed {
		t.Error("a different client was throttled by someone else's traffic")
	}
}

func TestRateLimiterCleanupBoundsMemory(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	now := time.Now()
	limiter.SetClock(func() time.Time { return now })

	// A scan across many source addresses must not grow the map forever.
	for i := 0; i < 100; i++ {
		limiter.Allow(string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if limiter.Size() == 0 {
		t.Fatal("no buckets were created")
	}

	now = now.Add(time.Hour)
	removed := limiter.Cleanup(30 * time.Minute)

	if removed == 0 || limiter.Size() != 0 {
		t.Errorf("removed %d, %d buckets left, want everything idle to be dropped",
			removed, limiter.Size())
	}
}

func TestRateLimitMiddlewareRejectsWithRetryAfter(t *testing.T) {
	limiter := NewRateLimiter(60, 1)

	var denied bool
	h := RateLimit(limiter, func(w http.ResponseWriter, _ *http.Request, _ time.Duration) {
		denied = true
		w.WriteHeader(http.StatusTooManyRequests)
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "192.0.2.1:1234"

	first := httptest.NewRecorder()
	h.ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first request = %d", first.Code)
	}

	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)

	if !denied || second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, denied = %v", second.Code, denied)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}
}

func TestRateLimiterDefaultsAreSane(t *testing.T) {
	limiter := NewRateLimiter(0, 0)

	// Zero values must not produce a limiter that refuses everything.
	if allowed, _ := limiter.Allow("192.0.2.1"); !allowed {
		t.Error("a default limiter refused the first request")
	}
}
