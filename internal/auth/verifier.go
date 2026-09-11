package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// googleJWKSURL is Google's public signing-key set for ID tokens.
const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// jwksMinRefetch rate-limits cache-busting refetches triggered by unknown
// key IDs (Google rotates keys, so a miss may be legitimate), so a flood of
// forged tokens cannot hammer Google (or us).
const jwksMinRefetch = time.Minute

var (
	ErrBadIDToken   = errors.New("invalid id token")
	ErrTokenExpired = errors.New("id token expired")
)

// GoogleClaims is the subset of a Google ID token the application needs.
type GoogleClaims struct {
	Email         string
	EmailVerified bool
}

// TokenVerifier verifies a Google ID token. It is an interface so handlers
// can be tested without Google.
type TokenVerifier interface {
	Verify(ctx context.Context, idToken string) (GoogleClaims, error)
}

// GoogleVerifier verifies Google-issued ID tokens using stdlib crypto only:
// the algorithm is pinned to RS256, the signature is checked against Google's
// JWKS before any claim is trusted, and iss/aud/exp are all enforced.
type GoogleVerifier struct {
	clientID string
	client   *http.Client
	jwksURL  string

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	now       func() time.Time
}

// NewGoogleVerifier requires the OAuth web client ID the tokens are minted
// for; it becomes the required aud claim.
func NewGoogleVerifier(clientID string) *GoogleVerifier {
	return &GoogleVerifier{
		clientID: clientID,
		client:   &http.Client{Timeout: 10 * time.Second},
		jwksURL:  googleJWKSURL,
		keys:     map[string]*rsa.PublicKey{},
		now:      time.Now,
	}
}

// Verify validates idToken and returns its email claims.
func (v *GoogleVerifier) Verify(ctx context.Context, idToken string) (GoogleClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return GoogleClaims{}, ErrBadIDToken
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return GoogleClaims{}, ErrBadIDToken
	}
	// Pin the algorithm before touching keys: a token claiming HS256 or
	// "none" must never reach signature verification.
	if header.Alg != "RS256" || header.Kid == "" {
		return GoogleClaims{}, ErrBadIDToken
	}

	key, err := v.publicKey(ctx, header.Kid)
	if err != nil {
		return GoogleClaims{}, err
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return GoogleClaims{}, ErrBadIDToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return GoogleClaims{}, ErrBadIDToken
	}

	// The signature is valid; claims may now be trusted enough to check.
	var payload struct {
		Iss           string `json:"iss"`
		Aud           string `json:"aud"`
		Exp           int64  `json:"exp"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := decodeSegment(parts[1], &payload); err != nil {
		return GoogleClaims{}, ErrBadIDToken
	}
	if payload.Iss != "accounts.google.com" && payload.Iss != "https://accounts.google.com" {
		return GoogleClaims{}, ErrBadIDToken
	}
	if payload.Aud != v.clientID || payload.Email == "" {
		return GoogleClaims{}, ErrBadIDToken
	}
	if time.Now().Unix() >= payload.Exp {
		return GoogleClaims{}, ErrTokenExpired
	}

	return GoogleClaims{Email: payload.Email, EmailVerified: truthy(payload.EmailVerified)}, nil
}

// publicKey returns the RSA key for kid, refreshing the JWKS cache on a miss
// (rate-limited) in case Google rotated keys.
func (v *GoogleVerifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	key, ok := v.keys[kid]
	canRefetch := v.now().Sub(v.fetchedAt) > jwksMinRefetch
	v.mu.Unlock()
	if ok {
		return key, nil
	}
	if !canRefetch {
		return nil, ErrBadIDToken
	}
	if err := v.fetchKeys(ctx); err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, ErrBadIDToken
}

// fetchKeys replaces the cached key set from the JWKS endpoint.
func (v *GoogleVerifier) fetchKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		eInt := 0
		for _, b := range e {
			eInt = eInt<<8 | int(b)
		}
		if eInt < 3 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: eInt}
	}
	if len(keys) == 0 {
		return fmt.Errorf("jwks contained no usable RSA keys")
	}

	v.mu.Lock()
	v.keys = keys
	v.fetchedAt = v.now()
	v.mu.Unlock()
	return nil
}

// truthy accepts Google's boolean true and the legacy string "true".
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	default:
		return false
	}
}
