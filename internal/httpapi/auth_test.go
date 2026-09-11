package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/auth"
)

// fakeVerifier implements auth.TokenVerifier without contacting Google.
type fakeVerifier struct {
	claims auth.GoogleClaims
	err    error
}

func (f fakeVerifier) Verify(context.Context, string) (auth.GoogleClaims, error) {
	return f.claims, f.err
}

// testAuthStack builds a complete Auth with a fake verifier and an allowlist
// containing alice@example.com.
func testAuthStack(t *testing.T, v auth.TokenVerifier) *Auth {
	t.Helper()
	if v == nil {
		v = fakeVerifier{claims: auth.GoogleClaims{Email: "alice@example.com", EmailVerified: true}}
	}
	sessions, err := auth.NewSessionManager("test-secret-that-is-long-enough-32b", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &Auth{
		Verifier:  v,
		Sessions:  sessions,
		Allowlist: auth.NewAllowlist([]string{"alice@example.com"}),
	}
}

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(`{"credential":"fake-token"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status %d body %s", rec.Code, rec.Body.String())
	}
	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %v", cookies)
	}
	return cookies[0]
}

func TestPostAuthGoogle(t *testing.T) {
	t.Run("allowlisted email gets session", func(t *testing.T) {
		h := New(nil, testAuthStack(t, nil))
		cookie := login(t, h)
		if cookie.Name != "__session" {
			t.Fatalf("cookie name %q, want __session (Firebase Hosting forwards nothing else)", cookie.Name)
		}
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("hardening flags: %+v", cookie)
		}
	})

	t.Run("non-allowlisted email is 403 without cookie", func(t *testing.T) {
		v := fakeVerifier{claims: auth.GoogleClaims{Email: "mallory@example.com", EmailVerified: true}}
		h := New(nil, testAuthStack(t, v))
		req := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(`{"credential":"fake-token"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("no cookie may be set on rejection")
		}
	})

	t.Run("unverified email is 403", func(t *testing.T) {
		v := fakeVerifier{claims: auth.GoogleClaims{Email: "alice@example.com", EmailVerified: false}}
		h := New(nil, testAuthStack(t, v))
		req := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(`{"credential":"fake-token"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("invalid token is 401", func(t *testing.T) {
		v := fakeVerifier{err: errors.New("bad token")}
		h := New(nil, testAuthStack(t, v))
		req := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(`{"credential":"fake-token"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rec.Code)
		}
	})

	t.Run("malformed body is 400", func(t *testing.T) {
		h := New(nil, testAuthStack(t, nil))
		for _, body := range []string{"", "not json", `{"credential":""}`} {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("body %q: status %d", body, rec.Code)
			}
		}
	})
}

func TestPostAuthDev(t *testing.T) {
	t.Run("disabled is 404 without cookie", func(t *testing.T) {
		h := New(nil, testAuthStack(t, nil))
		req := httptest.NewRequest(http.MethodPost, "/api/auth/dev", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404", rec.Code)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("no cookie may be set when disabled")
		}
	})

	t.Run("enabled issues a session for the primary allowlisted user", func(t *testing.T) {
		a := testAuthStack(t, nil)
		a.DevLogin = true
		h := New(nil, a)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/dev", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["email"] != "alice@example.com" {
			t.Fatalf("email %q", body["email"])
		}
		cookies := (&http.Response{Header: rec.Header()}).Cookies()
		if len(cookies) != 1 || cookies[0].Name != "__session" {
			t.Fatalf("expected session cookie, got %v", cookies)
		}
	})

	t.Run("cross-site is rejected without cookie", func(t *testing.T) {
		a := testAuthStack(t, nil)
		a.DevLogin = true
		h := New(nil, a)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/dev", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("no cookie may be set on rejection")
		}
	})
}

func TestAuthMeRoundTrip(t *testing.T) {
	h := New(nil, testAuthStack(t, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: status %d, want 401", rec.Code)
	}

	cookie := login(t, h)
	req = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with session: status %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["email"] != "alice@example.com" {
		t.Fatalf("email %q", body["email"])
	}
}

func TestLogout(t *testing.T) {
	t.Run("same-origin logout expires the cookie", func(t *testing.T) {
		h := New(nil, testAuthStack(t, nil))
		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d", rec.Code)
		}
		set := rec.Header().Get("Set-Cookie")
		if !strings.Contains(set, "__session=") || !strings.Contains(set, "Max-Age=0") {
			t.Fatalf("logout must expire the cookie: %q", set)
		}
	})

	t.Run("cross-site logout is rejected without clearing", func(t *testing.T) {
		h := New(nil, testAuthStack(t, nil))
		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
		if set := rec.Header().Get("Set-Cookie"); set != "" {
			t.Fatalf("cross-site logout must not touch the cookie: %q", set)
		}
	})
}
