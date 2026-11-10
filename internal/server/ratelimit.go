package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jasondostal/rill/internal/auth"
	"golang.org/x/time/rate"
)

// RateLimiter implements per-IP token-bucket rate limiting.
// Cleanup goroutine removes idle limiters periodically.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	rps      rate.Limit
	burst    int
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// NewRateLimiter creates a rate limiter with the given requests-per-second and burst.
// NewRateLimiter creates a rate limiter with the given requests-per-second
// and burst. The cleanup goroutine is tied to the supplied context so it
// exits cleanly on shutdown. Pass context.Background() if you don't need a
// shutdown hook (process-exit will reap it either way).
func NewRateLimiter(ctx context.Context, rps int, burst int) *RateLimiter {
	if ctx == nil {
		ctx = context.Background()
	}
	rl := &RateLimiter{
		limiters: make(map[string]*limiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	// Clean up idle limiters every 5 minutes. Stops when ctx is cancelled.
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rl.cleanup()
			}
		}
	}()
	return rl
}

// Allow reports whether the given IP is allowed to make a request.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	entry, ok := rl.limiters[ip]
	if !ok {
		entry = &limiterEntry{
			limiter:  rate.NewLimiter(rl.rps, rl.burst),
			lastUsed: time.Now(),
		}
		rl.limiters[ip] = entry
	}
	entry.lastUsed = time.Now()
	return entry.limiter.Allow()
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	for ip, entry := range rl.limiters {
		if entry.lastUsed.Before(cutoff) {
			delete(rl.limiters, ip)
		}
	}
}

// sourceIP extracts the client IP from the request, honoring X-Forwarded-For
// only when the request comes from a trusted proxy, and then only the part of
// it our proxies wrote (see auth.ResolveClientIP).
func sourceIP(r *http.Request, trustedProxies []string) string {
	return auth.ResolveClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), auth.ParseTrustedProxies(trustedProxies))
}

// RateLimitMiddleware returns a middleware that rate-limits requests.
// Different limiters per path prefix. Unmatched paths pass through un-throttled.
func RateLimitMiddleware(mcpRL, loginRL, restRL *RateLimiter, trustedProxies []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := sourceIP(r, trustedProxies)

			var rl *RateLimiter
			if r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/setup" {
				rl = loginRL
			} else if r.URL.Path == "/mcp" || r.URL.Path == "/api/mcp" {
				rl = mcpRL
			} else if strings.HasPrefix(r.URL.Path, "/oauth/") {
				// Unauthenticated by nature (client registration, the consent
				// page, the token endpoint) and each call writes to the DB.
				rl = restRL
			} else if strings.HasPrefix(r.URL.Path, "/api/") &&
				!strings.HasPrefix(r.URL.Path, "/api/auth/") {
				rl = restRL
			}

			if rl != nil && !rl.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
