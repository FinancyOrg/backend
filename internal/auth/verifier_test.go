package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testClientID = "test-client-id.apps.googleusercontent.com"

// jwksServer serves the public half of key as a Google-style JWKS and counts
// requests so caching behaviour can be asserted.
type jwksServer struct {
	*httptest.Server
	requests *atomic.Int32
}

func newJWKSServer(t *testing.T, key *rsa.PublicKey, kid string) *jwksServer {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"keys":[{"kid":%q,"kty":"RSA","alg":"RS256","n":%q,"e":%q}]}`,
			kid,
			base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}), // 65537
		)
	}))
	t.Cleanup(srv.Close)
	return &jwksServer{srv, &hits}
}

func testVerifier(t *testing.T, jwks *jwksServer) *GoogleVerifier {
	t.Helper()
	v := NewGoogleVerifier(testClientID)
	v.jwksURL = jwks.URL
	return v
}

func mintGoogleToken(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid})
	payload, _ := json.Marshal(claims)
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func goodClaims() map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"email":          "alice@example.com",
		"email_verified": true,
	}
}

func TestGoogleVerifierValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := newJWKSServer(t, &key.PublicKey, "kid-1")
	v := testVerifier(t, jwks)

	token := mintGoogleToken(t, key, "kid-1", goodClaims())
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Email != "alice@example.com" || !claims.EmailVerified {
		t.Fatalf("claims %+v", claims)
	}

	// Second verify must be served from the JWKS cache.
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if got := jwks.requests.Load(); got != 1 {
		t.Fatalf("jwks requests = %d, want 1 (cached)", got)
	}
}

func TestGoogleVerifierIssuerVariants(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := newJWKSServer(t, &key.PublicKey, "kid-1")
	v := testVerifier(t, jwks)

	claims := goodClaims()
	claims["iss"] = "accounts.google.com" // legacy issuer form must also pass
	if _, err := v.Verify(context.Background(), mintGoogleToken(t, key, "kid-1", claims)); err != nil {
		t.Fatalf("legacy iss: %v", err)
	}
}

func TestGoogleVerifierRejections(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := newJWKSServer(t, &key.PublicKey, "kid-1")

	wrongAud := goodClaims()
	wrongAud["aud"] = "someone-else.apps.googleusercontent.com"
	wrongIss := goodClaims()
	wrongIss["iss"] = "https://evil.example.com"
	expired := goodClaims()
	expired["exp"] = time.Now().Add(-time.Hour).Unix()
	unverifiedEmail := goodClaims()
	unverifiedEmail["email_verified"] = false
	stringVerified := goodClaims()
	stringVerified["email_verified"] = "true"

	cases := map[string]struct {
		token string
		want  error
	}{
		"two segments":      {"a.b", ErrBadIDToken},
		"four segments":     {"a.b.c.d", ErrBadIDToken},
		"bad header json":   {"e30.e30.c2ln", ErrBadIDToken}, // {} {} sig
		"wrong audience":    {mintGoogleToken(t, key, "kid-1", wrongAud), ErrBadIDToken},
		"wrong issuer":      {mintGoogleToken(t, key, "kid-1", wrongIss), ErrBadIDToken},
		"expired":           {mintGoogleToken(t, key, "kid-1", expired), ErrTokenExpired},
		"wrong signing key": {mintGoogleToken(t, otherKey, "kid-1", goodClaims()), ErrBadIDToken},
		"unknown kid":       {mintGoogleToken(t, key, "kid-2", goodClaims()), ErrBadIDToken},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v := testVerifier(t, jwks)
			if _, err := v.Verify(context.Background(), tc.token); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}

	t.Run("algorithm pinned to RS256", func(t *testing.T) {
		v := testVerifier(t, jwks)
		header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT", "kid": "kid-1"})
		payload, _ := json.Marshal(goodClaims())
		token := base64.RawURLEncoding.EncodeToString(header) + "." +
			base64.RawURLEncoding.EncodeToString(payload) + ".c2ln"
		if _, err := v.Verify(context.Background(), token); !errors.Is(err, ErrBadIDToken) {
			t.Fatalf("want ErrBadIDToken, got %v", err)
		}
	})

	t.Run("email_verified surfaced, not enforced", func(t *testing.T) {
		v := testVerifier(t, jwks)
		claims, err := v.Verify(context.Background(), mintGoogleToken(t, key, "kid-1", unverifiedEmail))
		if err != nil {
			t.Fatal(err)
		}
		if claims.EmailVerified {
			t.Fatal("expected EmailVerified=false")
		}
	})

	t.Run("string email_verified accepted", func(t *testing.T) {
		v := testVerifier(t, jwks)
		claims, err := v.Verify(context.Background(), mintGoogleToken(t, key, "kid-1", stringVerified))
		if err != nil || !claims.EmailVerified {
			t.Fatalf("claims %+v err %v", claims, err)
		}
	})
}

func TestGoogleVerifierUnknownKidRefetchesOnce(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := newJWKSServer(t, &key.PublicKey, "kid-1")
	v := testVerifier(t, jwks)

	now := time.Now()
	v.now = func() time.Time { return now }

	// Prime the cache.
	if _, err := v.Verify(context.Background(), mintGoogleToken(t, key, "kid-1", goodClaims())); err != nil {
		t.Fatal(err)
	}
	// Within the refetch window an unknown kid must NOT trigger a refetch.
	if _, err := v.Verify(context.Background(), mintGoogleToken(t, key, "kid-2", goodClaims())); !errors.Is(err, ErrBadIDToken) {
		t.Fatalf("want ErrBadIDToken, got %v", err)
	}
	if got := jwks.requests.Load(); got != 1 {
		t.Fatalf("jwks requests = %d, want 1 (refetch rate-limited)", got)
	}

	// Past the window (key rotation), an unknown kid triggers exactly one
	// refetch, then fails because the kid is still unknown.
	now = now.Add(2 * jwksMinRefetch)
	if _, err := v.Verify(context.Background(), mintGoogleToken(t, key, "kid-2", goodClaims())); !errors.Is(err, ErrBadIDToken) {
		t.Fatalf("want ErrBadIDToken, got %v", err)
	}
	if got := jwks.requests.Load(); got != 2 {
		t.Fatalf("jwks requests = %d, want 2 (prime + one refetch)", got)
	}
}
