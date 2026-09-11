package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SessionCookieName is the only cookie name Firebase Hosting forwards to
// Cloud Run on rewrite paths. Do not rename without checking the edge
// behaviour of whatever fronts this service.
const SessionCookieName = "__session"

const sessionIssuer = "financy"

var (
	// ErrNoSession means the request carried no session cookie.
	ErrNoSession = errors.New("no session cookie")
	// ErrBadSession means the cookie failed validation.
	ErrBadSession = errors.New("invalid session")
	// ErrSessionExpired means the session was valid but out of date.
	ErrSessionExpired = errors.New("session expired")
)

// SessionManager issues and verifies HS256 session JWTs. Sessions are
// stateless: there is no server-side revocation, so the TTL is deliberately
// short.
type SessionManager struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

// NewSessionManager requires a secret of at least 32 bytes and a positive TTL.
func NewSessionManager(secret string, ttl time.Duration) (*SessionManager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("session secret must be at least 32 bytes, got %d", len(secret))
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("session ttl must be positive")
	}
	return &SessionManager{key: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// Issue signs a session token for email.
func (m *SessionManager) Issue(email string) (string, error) {
	now := m.now().Unix()
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{
		"iss": sessionIssuer,
		"sub": email,
		"iat": now,
		"exp": now + int64(m.ttl.Seconds()),
	})
	if err != nil {
		return "", err
	}
	input := encodeSegment(header) + "." + encodeSegment(payload)
	return input + "." + encodeSegment(m.sign(input)), nil
}

// Verify validates a session token and returns the email it belongs to.
// The signature is checked before any claim is trusted; the algorithm is
// pinned to HS256 so header tampering cannot downgrade verification.
func (m *SessionManager) Verify(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ErrBadSession
	}
	input := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, m.sign(input)) {
		return "", ErrBadSession
	}

	var header struct {
		Alg string `json:"alg"`
	}
	if err := decodeSegment(parts[0], &header); err != nil || header.Alg != "HS256" {
		return "", ErrBadSession
	}

	var payload struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := decodeSegment(parts[1], &payload); err != nil {
		return "", ErrBadSession
	}
	if payload.Iss != sessionIssuer || payload.Sub == "" {
		return "", ErrBadSession
	}
	if m.now().Unix() >= payload.Exp {
		return "", ErrSessionExpired
	}
	return payload.Sub, nil
}

// SetCookie issues a session and writes it as a hardened cookie.
func (m *SessionManager) SetCookie(w http.ResponseWriter, email string) error {
	token, err := m.Issue(email)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// ClearCookie expires the session cookie.
func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// EmailFromRequest extracts and verifies the session cookie.
func (m *SessionManager) EmailFromRequest(r *http.Request) (string, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return "", ErrNoSession
	}
	return m.Verify(cookie.Value)
}

func (m *SessionManager) sign(input string) []byte {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(input))
	return mac.Sum(nil)
}

func encodeSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeSegment(s string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
