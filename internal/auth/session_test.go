package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-that-is-long-enough-32b"

func testSessions(t *testing.T) *SessionManager {
	t.Helper()
	m, err := NewSessionManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSessionRoundTrip(t *testing.T) {
	m := testSessions(t)
	token, err := m.Issue("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	email, err := m.Verify(token)
	if err != nil {
		t.Fatalf("verify own token: %v", err)
	}
	if email != "alice@example.com" {
		t.Fatalf("email %q", email)
	}
}

func TestSessionRejectsForgery(t *testing.T) {
	m := testSessions(t)
	valid, err := m.Issue("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid, ".")

	// Helper to mint a raw token with this manager's key.
	mintWith := func(key, alg string, claims map[string]any) string {
		header, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT"})
		payload, _ := json.Marshal(claims)
		input := encodeSegment(header) + "." + encodeSegment(payload)
		if key == "" {
			return input + "."
		}
		other := &SessionManager{key: []byte(key), ttl: time.Hour, now: time.Now}
		return input + "." + encodeSegment(other.sign(input))
	}
	goodClaims := map[string]any{"iss": sessionIssuer, "sub": "alice@example.com", "exp": time.Now().Add(time.Hour).Unix()}

	cases := map[string]string{
		"garbage":             "not-a-token",
		"two segments":        parts[0] + "." + parts[1],
		"four segments":       valid + ".extra",
		"bad base64 sig":      parts[0] + "." + parts[1] + ".%%%",
		"tampered payload":    parts[0] + "." + encodeSegment([]byte(`{"iss":"financy","sub":"mallory@example.com","exp":9999999999}`)) + "." + parts[2],
		"wrong key":           mintWith("another-secret-that-is-long-enough", "HS256", goodClaims),
		"alg none":            mintWith(testSecret, "none", goodClaims),
		"alg confusion RS256": mintWith(testSecret, "RS256", goodClaims),
		"wrong issuer":        mintWith(testSecret, "HS256", map[string]any{"iss": "evil", "sub": "alice@example.com", "exp": time.Now().Add(time.Hour).Unix()}),
		"empty subject":       mintWith(testSecret, "HS256", map[string]any{"iss": sessionIssuer, "sub": "", "exp": time.Now().Add(time.Hour).Unix()}),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Verify(token); !errors.Is(err, ErrBadSession) {
				t.Fatalf("want ErrBadSession, got %v", err)
			}
		})
	}
}

func TestSessionExpiry(t *testing.T) {
	m := testSessions(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	token, err := m.Issue("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := m.Verify(token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("want ErrSessionExpired, got %v", err)
	}
}

func TestSessionManagerValidation(t *testing.T) {
	if _, err := NewSessionManager("too-short", time.Hour); err == nil {
		t.Fatal("short secret must be rejected")
	}
	if _, err := NewSessionManager(testSecret, 0); err == nil {
		t.Fatal("zero ttl must be rejected")
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	m := testSessions(t)
	rec := httptest.NewRecorder()
	if err := m.SetCookie(rec, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	c := cookies[0]
	if c.Name != SessionCookieName || c.Value == "" {
		t.Fatalf("cookie %+v", c)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("hardening flags missing: %+v", c)
	}
	if c.Path != "/" || c.MaxAge != 3600 {
		t.Fatalf("path/maxage: %+v", c)
	}
}

func TestClearCookie(t *testing.T) {
	m := testSessions(t)
	rec := httptest.NewRecorder()
	m.ClearCookie(rec)
	set := rec.Header().Get("Set-Cookie")
	if !strings.Contains(set, SessionCookieName+"=") || !strings.Contains(set, "Max-Age=0") {
		t.Fatalf("clear cookie: %q", set)
	}
}

func TestEmailFromRequest(t *testing.T) {
	m := testSessions(t)
	if _, err := m.EmailFromRequest(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	token, err := m.Issue("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	email, err := m.EmailFromRequest(req)
	if err != nil || email != "alice@example.com" {
		t.Fatalf("email %q err %v", email, err)
	}
}

// Ensure base64 segment decoding rejects malformed payloads even with a valid MAC.
func TestVerifyValidSigBadJSON(t *testing.T) {
	m := testSessions(t)
	header := encodeSegment([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	input := header + "." + payload
	token := input + "." + encodeSegment(m.sign(input))
	if _, err := m.Verify(token); !errors.Is(err, ErrBadSession) {
		t.Fatalf("want ErrBadSession, got %v", err)
	}
}
