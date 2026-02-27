// CLAUDE:SUMMARY HTTP middleware — security headers, no-cache static, IP-based rate limiter, CORS configuration
package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// SecurityHeaders wraps a handler with standard security headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// NoCacheStatic wraps a handler to add Cache-Control: no-cache for static files.
func NoCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

// RateLimiter tracks request counts per IP within a rolling window.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*rateBucket
	limit   int
	window  time.Duration
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a limiter with the given request limit per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*rateBucket),
		limit:   limit,
		window:  window,
	}
}

// Allow returns true if the request from ip is within the rate limit.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, ok := rl.clients[ip]
	if !ok || now.After(bucket.resetAt) {
		rl.clients[ip] = &rateBucket{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	bucket.count++
	return bucket.count <= rl.limit
}

// RateLimitMiddleware wraps a handler with rate limiting (429 Too Many Requests).
func RateLimitMiddleware(rl *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = fwd
		}
		// Strip port from RemoteAddr (e.g. "127.0.0.1:54321" -> "127.0.0.1")
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		if !rl.Allow(ip) {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
