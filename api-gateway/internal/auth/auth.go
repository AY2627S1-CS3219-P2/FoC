// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: RS256 access-token verification against user-service's JWKS, and the
//   claim set the gateway forwards. Implements D-023.
// Author review: Nigeltzy - Checked the output of this generated code, this is part of the auth package that my team had discussed extensively about before, and the generated code seems to be a reasonable implementation of the requirements. I also checked the output of the generated code and it seems to be a typical implementation of an access-token verification package.

// Package auth verifies RS256 access tokens against the public keys that
// user-service publishes at its JWKS endpoint, and exposes the claims the
// gateway forwards. It holds no private key, so it cannot mint a token
// (D-023 in ai/decisions.md). It does not check revocation.
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
	// ErrExpired covers a token past its exp or before its nbf, after clockSkew leeway.
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

// clockSkew is the leeway applied to exp and nbf, so a few seconds of drift
// between user-service and the gateway does not reject a valid token.
const clockSkew = 5 * time.Second

// minRefreshInterval throttles JWKS refetches. An unknown kid triggers a
// refresh, so without this an attacker could force unbounded outbound requests
// by sending tokens with invented kids.
const minRefreshInterval = 30 * time.Second

// Claims is the subset of the access token the gateway acts on. Other claims
// are ignored and not forwarded.
type Claims struct {
	// Subject is the user's UUID, forwarded as the user id header.
	Subject string
	// Role is STUDENT or ADMIN, forwarded as the role header.
	Role string
	// ID is the token's jti. It is parsed but not used or forwarded.
	ID string
}

// Verifier verifies access tokens against the issuer's published RSA keys.
// Keys are fetched on first use, so the gateway can start before user-service
// is reachable.
type Verifier struct {
	jwksURL string
	client  *http.Client

	// AI-generated (edited by nigeltzy).
	// fetchMu serialises refetches, so concurrent requests with an unknown
	// kid share one fetch instead of each starting their own.
	fetchMu sync.Mutex

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	lastErr     error
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
// gateway forwards. The returned error matches one of the sentinels above
// under errors.Is; ErrKeysUnavailable carries the underlying cause.
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
		// AI-generated (edited by nigeltzy).
		// A token with no exp would otherwise never expire.
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenNotValidYet):
			return Claims{}, ErrExpired
		case errors.Is(err, ErrKeysUnavailable):
			return Claims{}, err
		case errors.Is(err, jwt.ErrTokenMalformed), errors.Is(err, jwt.ErrTokenRequiredClaimMissing), errors.Is(err, ErrMalformed):
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

// AI-generated (edited by nigeltzy).
// keyFor resolves the key named by the token's kid header, refetching the key
// set once if the kid is unknown. user-service publishes an active key plus
// retired ones, so an unknown kid usually means a rotation not yet fetched.
func (v *Verifier) keyFor(ctx context.Context, token *jwt.Token) (*rsa.PublicKey, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, ErrMalformed
	}

	if key, ok := v.cachedKey(kid); ok {
		return key, nil
	}

	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()

	// Another request may have fetched while this one waited for fetchMu.
	if key, ok := v.cachedKey(kid); ok {
		return key, nil
	}

	v.mu.RLock()
	attempted, lastErr := v.lastAttempt, v.lastErr
	v.mu.RUnlock()
	if !attempted.IsZero() && time.Since(attempted) < minRefreshInterval {
		// Attempted recently, successful or not: answer from that attempt
		// rather than fetch again.
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrInvalidSignature
	}

	// The fetch is detached from the request's cancellation: its result is
	// cached for every request, so one client disconnecting must not record
	// a failure. The http.Client timeout still bounds it.
	if err := v.refresh(context.WithoutCancel(ctx)); err != nil {
		return nil, err
	}

	if key, ok := v.cachedKey(kid); ok {
		return key, nil
	}
	return nil, ErrInvalidSignature
}

// cachedKey returns the already-fetched key for kid, if there is one.
func (v *Verifier) cachedKey(kid string) (*rsa.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	key, ok := v.keys[kid]
	return key, ok
}

// refresh fetches the key set and records the attempt, successful or not, so
// minRefreshInterval also throttles fetches while the endpoint is failing. A
// failed fetch keeps the previously cached keys.
func (v *Verifier) refresh(ctx context.Context) error {
	keys, err := v.fetch(ctx)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastAttempt = time.Now()
	v.lastErr = err
	if err == nil {
		v.keys = keys
	}
	return err
}

// fetch downloads and decodes the key set from the JWKS endpoint.
func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrKeysUnavailable, err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeysUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s returned %d", ErrKeysUnavailable, v.jwksURL, resp.StatusCode)
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
		return nil, fmt.Errorf("%w: decoding key set: %v", ErrKeysUnavailable, err)
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
		return nil, fmt.Errorf("%w: %s published no usable RSA keys", ErrKeysUnavailable, v.jwksURL)
	}

	return keys, nil
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
