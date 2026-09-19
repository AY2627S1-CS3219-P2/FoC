// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: RS256 access-token verification against user-service's JWKS, and the
//   claim set the gateway forwards. Implements D-023.
// Author review: PENDING — <reviewer to complete>

// Package auth verifies access-token signatures and exposes the claims the
// gateway forwards downstream.
//
// It verifies with a PUBLIC key only (D-023). The gateway holds no private key
// and cannot mint a token, which is what keeps user-service the sole issuer
// (D-012). It does NOT check revocation: under D-024 the gateway never talks
// to Redis, so a token is good until its exp (D-025 records what that costs).
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Errors the caller must distinguish. Anything else is an internal failure and
// should not be reported to the client in detail.
var (
	// ErrMalformed covers a token that is absent, not a JWT, or unparseable.
	ErrMalformed = errors.New("auth: malformed token")
	// ErrInvalidSignature covers a token whose signature does not verify, or
	// which names a key the issuer does not publish.
	ErrInvalidSignature = errors.New("auth: invalid signature")
	// ErrExpired covers a token past its exp, allowing for clock skew.
	ErrExpired = errors.New("auth: token expired")
	// ErrWrongType covers a refresh token presented where an access token
	// belongs. They are both JWTs signed by the same key, so nothing but the
	// typ claim separates them.
	ErrWrongType = errors.New("auth: not an access token")
	// ErrKeysUnavailable covers being unable to reach the JWKS endpoint. The
	// gateway cannot verify anything without it, so requests are refused
	// rather than let through.
	ErrKeysUnavailable = errors.New("auth: signing keys unavailable")
)

// clockSkew is the tolerance applied to exp, so a few seconds of drift between
// user-service and the gateway does not reject a valid token.
const clockSkew = 5 * time.Second

// minRefreshInterval throttles JWKS refetches. An unknown kid triggers a
// refresh, so without this an attacker could force unbounded outbound requests
// by sending tokens with invented kids.
const minRefreshInterval = 30 * time.Second

// Claims is the subset of the access token the gateway acts on. user-service
// mints more than this; anything not listed here is deliberately ignored
// rather than forwarded.
type Claims struct {
	// Subject is the user's UUID, forwarded as the user id header.
	Subject string
	// Role is STUDENT or ADMIN (D-019), forwarded as the role header.
	Role string
	// ID is the jti. The gateway does not use it — nothing here reads a
	// blocklist (D-024) — but it is parsed so downstream logging can
	// correlate a request with a session.
	ID string
}

// Verifier verifies access tokens against the issuer's published RSA keys.
//
// New returns a ready-to-use value. Keys are fetched lazily on first use, not
// in the constructor, because the gateway must not assume user-service is
// already up (root AGENTS.md §5, temporal coupling).
type Verifier struct {
	jwksURL string
	client  *http.Client

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastFetched time.Time
}

// NewVerifier returns a Verifier that fetches keys from jwksURL.
func NewVerifier(jwksURL string, client *http.Client) *Verifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Verifier{
		jwksURL: jwksURL,
		client:  client,
		keys:    make(map[string]*rsa.PublicKey),
	}
}

// Verify parses and validates an access token, returning the claims the
// gateway forwards. The returned error is one of the sentinels above.
func (v *Verifier) Verify(ctx context.Context, tokenString string) (Claims, error) {
	if tokenString == "" {
		return Claims{}, ErrMalformed
	}

	var registered jwt.RegisteredClaims
	var typ struct {
		Typ  string `json:"typ"`
		Role string `json:"role"`
	}

	parsed, err := jwt.ParseWithClaims(
		tokenString,
		&registered,
		func(token *jwt.Token) (any, error) { return v.keyFor(ctx, token) },
		// RS256 only. Without this, a token could name "none" or an HMAC
		// algorithm and be verified against the public key as if it were a
		// shared secret — the classic JWT algorithm-confusion attack.
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(clockSkew),
	)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenNotValidYet):
			return Claims{}, ErrExpired
		case errors.Is(err, ErrKeysUnavailable):
			return Claims{}, ErrKeysUnavailable
		case errors.Is(err, jwt.ErrTokenMalformed):
			return Claims{}, ErrMalformed
		default:
			return Claims{}, ErrInvalidSignature
		}
	}
	if !parsed.Valid {
		return Claims{}, ErrInvalidSignature
	}

	// typ and role are not registered claims, so they come off the payload
	// segment. Safe to read at this point: the signature has been verified,
	// so the payload is exactly what user-service signed.
	if err := decodeCustom(tokenString, &typ); err != nil {
		return Claims{}, ErrMalformed
	}
	if typ.Typ != "access" {
		return Claims{}, ErrWrongType
	}

	return Claims{
		Subject: registered.Subject,
		Role:    typ.Role,
		ID:      registered.ID,
	}, nil
}

// keyFor resolves the key a token names in its kid header, refetching the key
// set once if the kid is unknown. user-service publishes an active key plus
// retired ones (D-023), so an unknown kid normally means a rotation the
// gateway has not picked up yet.
func (v *Verifier) keyFor(ctx context.Context, token *jwt.Token) (*rsa.PublicKey, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, ErrMalformed
	}

	v.mu.RLock()
	key, ok := v.keys[kid]
	fetched := v.lastFetched
	v.mu.RUnlock()
	if ok {
		return key, nil
	}

	if !fetched.IsZero() && time.Since(fetched) < minRefreshInterval {
		// Recently refreshed and the kid still is not there: treat it as a
		// key the issuer does not publish rather than refetch on demand.
		return nil, ErrInvalidSignature
	}

	if err := v.refresh(ctx); err != nil {
		return nil, err
	}

	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, ErrInvalidSignature
	}
	return key, nil
}

// refresh replaces the cached key set from the JWKS endpoint.
func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("%w: building request: %v", ErrKeysUnavailable, err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeysUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s returned %d", ErrKeysUnavailable, v.jwksURL, resp.StatusCode)
	}

	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("%w: decoding key set: %v", ErrKeysUnavailable, err)
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		key, err := rsaKeyFrom(k.N, k.E)
		if err != nil {
			// One malformed key does not invalidate the rest of the set.
			continue
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: %s published no usable RSA keys", ErrKeysUnavailable, v.jwksURL)
	}

	v.mu.Lock()
	v.keys = keys
	v.lastFetched = time.Now()
	v.mu.Unlock()
	return nil
}

// rsaKeyFrom rebuilds a public key from the base64url modulus and exponent a
// JWKS entry carries.
func rsaKeyFrom(nRaw, eRaw string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nRaw)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eRaw)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("empty modulus or exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

// decodeCustom reads the non-registered claims straight off the payload
// segment. The signature has already been verified by the time this runs.
func decodeCustom(tokenString string, out any) error {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return ErrMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrMalformed
	}
	return json.Unmarshal(payload, out)
}
