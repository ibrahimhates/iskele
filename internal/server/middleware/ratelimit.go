package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimit settings used by the router.
const (
	// GeneralRate is the loose limit on the API as a whole.
	GeneralRate = 120
	// GeneralBurst allows a UI page load to fire many requests at once.
	GeneralBurst = 240
	// LoginRate is the strict limit on the authentication endpoints.
	LoginRate = 5
	// LoginBurst permits a couple of quick retries before throttling.
	LoginBurst = 5
)

// bucket is a token bucket for one client.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter limits requests per client IP with a token bucket.
//
// Buckets live in memory: this is a single-host daemon, so there is no second
// instance to share state with, and a restart resetting the counters is
// acceptable for a throttle (the brute-force lockout, which must survive a
// restart, is stored in the database instead).
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	// rate is the sustained requests per minute, burst the bucket size.
	rate  float64
	burst float64
	now   func() time.Time
}

// NewRateLimiter builds a limiter allowing ratePerMinute sustained requests
// with the given burst.
func NewRateLimiter(ratePerMinute, burst int) *RateLimiter {
	if ratePerMinute <= 0 {
		ratePerMinute = GeneralRate
	}
	if burst <= 0 {
		burst = ratePerMinute
	}
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(ratePerMinute) / 60.0,
		burst:   float64(burst),
		now:     time.Now,
	}
}

// SetClock replaces the time source, for tests.
func (l *RateLimiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

// Allow reports whether a request from key may proceed, and how long to wait
// if not.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, lastSeen: now}
		l.buckets[key] = b
	}

	// Refill for the time that passed, capped at the burst size.
	elapsed := now.Sub(b.lastSeen).Seconds()
	if elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	}
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Time until one token is available again.
	wait := time.Duration((1-b.tokens)/l.rate*float64(time.Second)) + time.Second
	return false, wait.Round(time.Second)
}

// Cleanup drops buckets untouched for longer than idle, so a scan of many
// source addresses cannot grow the map without bound.
func (l *RateLimiter) Cleanup(idle time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-idle)
	removed := 0
	for key, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, key)
			removed++
		}
	}
	return removed
}

// Size reports how many buckets are tracked, for tests and diagnostics.
func (l *RateLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// RateLimit rejects requests from a client that exceeds the limiter.
//
// deny is called instead of writing directly, so the response uses the same
// error envelope as everything else.
func RateLimit(limiter *RateLimiter, deny func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retryAfter := limiter.Allow(ClientIP(r))
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				deny(w, r, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
