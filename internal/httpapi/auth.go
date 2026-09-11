package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/FinancyOrg/backend/internal/auth"
)

// postAuthDev issues a session for the primary allowlisted user. It exists
// only when Auth.DevLogin is set (local make targets) so the UI can start
// without Google Identity Services.
func (s *Server) postAuthDev(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil || !s.Auth.DevLogin {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	email := s.Auth.Allowlist.Primary()
	if email == "" {
		http.Error(w, "no allowlisted user", http.StatusInternalServerError)
		return
	}
	if err := s.Auth.Sessions.SetCookie(w, email); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": email})
}

// postAuthGoogle exchanges a Google ID token (from the Sign In With Google
// button) for a session cookie. The token is verified against Google's JWKS,
// then the email is gated against the embedded allowlist.
func (s *Server) postAuthGoogle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.Credential == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	claims, err := s.Auth.Verifier.Verify(r.Context(), body.Credential)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "InvalidToken"})
		return
	}
	if !claims.EmailVerified || !s.Auth.Allowlist.Allowed(claims.Email) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "NotAllowed"})
		return
	}

	if err := s.Auth.Sessions.SetCookie(w, claims.Email); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": claims.Email})
}

// postAuthLogout clears the session cookie. It is public so an expired or
// malformed session can always be reset client-side. Clearing does not
// require a valid cookie, so a cross-site form POST could otherwise
// force-logout a victim: reject requests the browser marks as cross-site.
func (s *Server) postAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.Auth.Sessions.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// getAuthMe returns the current identity. It sits behind session auth, so an
// unauthenticated PWA boot gets a 401 and knows to show the sign-in button.
func (s *Server) getAuthMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"email": auth.EmailFromContext(r.Context())})
}
