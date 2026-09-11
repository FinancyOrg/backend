package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	version "github.com/FinancyOrg/backend"
)

func TestHealth(t *testing.T) {
	h := New(nil, testAuthStack(t, nil))
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatal(body)
	}
	if body["version"] != version.String() || body["version"] == "" {
		t.Fatalf("version %q", body["version"])
	}
}

type fakeFirestore struct{ err error }

func (f fakeFirestore) Ping(context.Context) error { return f.err }

func TestFirestore(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		h := (&Server{Auth: testAuthStack(t, nil), Firestore: fakeFirestore{}}).Handler()
		req := httptest.NewRequest(http.MethodGet, "/api/db/firestore", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["status"] != "ok" {
			t.Fatal(body)
		}
	})

	t.Run("ping error", func(t *testing.T) {
		h := (&Server{Auth: testAuthStack(t, nil), Firestore: fakeFirestore{err: fmt.Errorf("down")}}).Handler()
		req := httptest.NewRequest(http.MethodGet, "/api/db/firestore", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("unconfigured", func(t *testing.T) {
		h := New(nil, testAuthStack(t, nil))
		req := httptest.NewRequest(http.MethodGet, "/api/db/firestore", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

var pathParamRe = regexp.MustCompile(`\{[^}]+\}`)

// TestAllExposedEndpointsRequireAuth walks the route registry and proves that
// every non-public route rejects requests without a session and with a
// garbage session. A route added without a public/protected decision fails
// here by construction.
func TestAllExposedEndpointsRequireAuth(t *testing.T) {
	h := New(nil, testAuthStack(t, nil))
	s := &Server{}

	for i, rt := range s.routes() {
		path := pathParamRe.ReplaceAllString(rt.pattern, "x")
		label := fmt.Sprintf("%s %s", rt.method, rt.pattern)

		if rt.public {
			continue
		}

		// Unique client IP per route so the rate limiter never interferes.
		ip := fmt.Sprintf("10.0.0.%d", i+1)

		req := httptest.NewRequest(rt.method, path, nil)
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: no cookie → status %d, want 401", label, rec.Code)
		}

		req = httptest.NewRequest(rt.method, path, strings.NewReader("{}"))
		req.Header.Set("X-Forwarded-For", ip)
		req.AddCookie(&http.Cookie{Name: "__session", Value: "garbage"})
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: garbage cookie → status %d, want 401", label, rec.Code)
		}
	}
}

// TestPublicRoutesAreExact guards the middleware's exact-match public lookup:
// a wildcard public pattern would silently never match.
func TestExchangeDisabled(t *testing.T) {
	h := New(nil, testAuthStack(t, nil))
	req := httptest.NewRequest(http.MethodPost, "/api/exchanges", strings.NewReader("{}"))
	req.AddCookie(login(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("status %d, want 410", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "Gone" {
		t.Fatal(body)
	}
}

func TestPublicRoutesAreExact(t *testing.T) {
	s := &Server{}
	for _, rt := range s.routes() {
		if rt.public && strings.Contains(rt.pattern, "{") {
			t.Errorf("public route %s %s must be an exact path", rt.method, rt.pattern)
		}
	}
}

// TestPublicEndpointsReachable proves the allowlist is not over-broad: the
// health and auth entry points must answer without a session.
func TestPublicEndpointsReachable(t *testing.T) {
	h := New(nil, testAuthStack(t, nil))

	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/health", "", http.StatusOK},
		{"GET", "/api/db/firestore", "", http.StatusServiceUnavailable},
		{"POST", "/api/auth/google", "{}", http.StatusBadRequest}, // reachable, rejects empty body
		{"POST", "/api/auth/logout", "", http.StatusNoContent},
		{"POST", "/api/auth/dev", "", http.StatusNotFound}, // public, disabled unless DevLogin
	}
	for _, c := range cases {
		var r *strings.Reader
		if c.body != "" {
			r = strings.NewReader(c.body)
		} else {
			r = strings.NewReader("")
		}
		req := httptest.NewRequest(c.method, c.path, r)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s %s must be public, got 401", c.method, c.path)
		}
		if rec.Code != c.want {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}
