package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type contextKey struct{}

// WithEmail stores the authenticated email on the context.
func WithEmail(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, contextKey{}, email)
}

// EmailFromContext returns the authenticated email, or "".
func EmailFromContext(ctx context.Context) string {
	email, _ := ctx.Value(contextKey{}).(string)
	return email
}

// RequireSession gates every request behind a valid session cookie except
// the explicitly listed public routes. Public keys are exact "METHOD /path"
// pairs — wildcard patterns must never be public. Construction happens once
// in httpapi.New from the same route table the mux uses, so a route cannot
// exist without a deliberate public/protected decision.
func RequireSession(sessions *SessionManager, public map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if public[r.Method+" "+r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			email, err := sessions.EmailFromRequest(r)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithEmail(r.Context(), email)))
		})
	}
}

// SecureHeaders sets baseline response headers on every response, including
// error and rate-limited ones. The CSP is the API-appropriate minimum; the
// frontend host sets the document CSP.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is an in-memory per-key token bucket. It is intentionally
// simple: the service runs as a single instance, so local state is exact.
type RateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows rps tokens per second with a maximum burst.
func NewRateLimiter(rps, burst float64) *RateLimiter {
	return &RateLimiter{
		rate:    rps,
		burst:   burst,
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// Allow consumes one token for key.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Bound memory: sweep idle buckets once the map grows large.
	if len(l.buckets) > 10000 {
		cutoff := l.now().Add(-time.Hour)
		for k, b := range l.buckets {
			if b.last.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
	}

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: l.now()}
		return true
	}
	b.tokens += l.now().Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = l.now()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Wrap rejects requests over the limit with 429, keyed by client IP.
func (l *RateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(clientIP(r)) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"RateLimited"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP uses the LAST X-Forwarded-For entry. The service is only reachable
// through Google Front End, which appends the real client IP after any
// client-supplied (spoofable) entries — so the last entry is the one an
// attacker cannot choose. The first entry is attacker-controlled and must
// never key a security control. Falls back to the direct peer address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
