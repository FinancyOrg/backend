package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireSession(t *testing.T) {
	sessions := testSessions(t)
	public := map[string]bool{"GET /api/health": true}

	t.Run("public route passes without cookie", func(t *testing.T) {
		h := RequireSession(sessions, public)(okHandler())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("no cookie is 401", func(t *testing.T) {
		h := RequireSession(sessions, public)(okHandler())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("garbage cookie is 401", func(t *testing.T) {
		h := RequireSession(sessions, public)(okHandler())
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "garbage"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("valid cookie passes with identity in context", func(t *testing.T) {
		var gotEmail string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotEmail = EmailFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})
		token, err := sessions.Issue("alice@example.com")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		RequireSession(sessions, public)(inner).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || gotEmail != "alice@example.com" {
			t.Fatalf("status %d email %q", rec.Code, gotEmail)
		}
	})

	t.Run("public match is method+path exact", func(t *testing.T) {
		h := RequireSession(sessions, public)(okHandler())
		rec := httptest.NewRecorder()
		// Same path, different method must not be public.
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/health", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func TestSecureHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecureHeaders(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, h := range []string{
		"X-Content-Type-Options",
		"Referrer-Policy",
		"Content-Security-Policy",
		"Strict-Transport-Security",
	} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("missing header %s", h)
		}
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("nosniff: %q", rec.Header().Get("X-Content-Type-Options"))
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Now()
	l := NewRateLimiter(1, 3)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("ip-1") {
			t.Fatalf("burst request %d should pass", i)
		}
	}
	if l.Allow("ip-1") {
		t.Fatal("burst exceeded should fail")
	}
	if !l.Allow("ip-2") {
		t.Fatal("other key must be independent")
	}

	now = now.Add(2 * time.Second) // refill 2 tokens at 1/s
	if !l.Allow("ip-1") || !l.Allow("ip-1") {
		t.Fatal("refill should allow 2 more")
	}
	if l.Allow("ip-1") {
		t.Fatal("refill exhausted")
	}
}

func TestRateLimiterWrap(t *testing.T) {
	l := NewRateLimiter(1, 1)
	h := l.Wrap(okHandler())

	req := func(ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		r.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	if rec := req("1.2.3.4"); rec.Code != http.StatusOK {
		t.Fatalf("first request: %d", rec.Code)
	}
	if rec := req("1.2.3.4"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: %d", rec.Code)
	}
	if rec := req("5.6.7.8"); rec.Code != http.StatusOK {
		t.Fatalf("other ip: %d", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	// GFE appends the real client IP after any client-supplied entries, so the
	// LAST entry is trusted, not the first.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(r); got != "10.0.0.1" {
		t.Fatalf("xff last (GFE-appended) entry: %q", got)
	}
	// A spoofed first entry must not become the limiter key.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 192.0.2.5")
	if got := clientIP(r); got != "192.0.2.5" {
		t.Fatalf("spoofed first entry must be ignored: %q", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("xff single entry: %q", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.1:54321"
	if got := clientIP(r); got != "192.0.2.1" {
		t.Fatalf("remote addr: %q", got)
	}
}
