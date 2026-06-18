package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

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

// sourceIP extracts the client IP from the request, respecting X-Forwarded-For
// when the request comes from a trusted proxy. When not behind a proxy, uses
// RemoteAddr directly.
func sourceIP(r *http.Request, trustedProxies []string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Only trust X-Forwarded-For from configured proxy IPs.
	isTrusted := false
	if len(trustedProxies) > 0 {
		srcIP := net.ParseIP(host)
		for _, cidr := range trustedProxies {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				// Skip blank entries — an unset env var with a comma-split
				// upstream can leave [""] in the trust list, which would
				// otherwise crash net.ParseCIDR with no diagnostic.
				continue
			}
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				if ip := net.ParseIP(cidr); ip != nil {
					ipnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
				} else {
					continue
				}
			}
			if ipnet.Contains(srcIP) {
				isTrusted = true
				break
			}
		}
	}

	if isTrusted {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
	}
	return host
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
