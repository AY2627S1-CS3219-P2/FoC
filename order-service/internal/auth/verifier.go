// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: RS256 access-token verification against user-service's published keys
//   (D-023, D-056). Adapted from api-gateway/internal/auth, duplicated rather
//   than shared (root AGENTS.md §4 rule 2).
// Author review: James Teo — reviewed and verified.

// Package auth verifies the access tokens user-service issues, checks them
// against the revocation keys user-service writes to the shared Redis, and
// carries a verified token on a request's context so order-service can forward
// it to credit-service (D-054, D-056). It holds no private key, so it cannot
// mint a token (D-023).
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
	"github.com/google/uuid"
)

// Errors the caller must distinguish. Anything else is reported to the client
// as an invalid token.
var (
	// ErrMalformed covers a token that is absent, not a JWT, unparseable, or
	// missing a claim order-service needs.
	ErrMalformed = errors.New("auth: malformed token")
	// AI-generated (edited by James Teo).
	// ErrInvalidSignature covers a token whose signature does not verify,
	// which names a key user-service does not publish, or whose iss or aud is
	// not the one user-service issues.
	ErrInvalidSignature = errors.New("auth: invalid signature")
	// ErrExpired covers a token past its exp or before its nbf, after clockSkew leeway.
	ErrExpired = errors.New("auth: token expired")
	// ErrWrongType covers a refresh token presented where an access token
	// belongs: both are signed by the same key, so only typ separates them.
	ErrWrongType = errors.New("auth: not an access token")
	// ErrKeysUnavailable covers being unable to load user-service's keys.
	// Nothing can be verified without them, so requests are refused (D-056).
	ErrKeysUnavailable = errors.New("auth: signing keys unavailable")
)

// clockSkew is the leeway on exp and nbf, so a few seconds of drift between
// user-service and order-service does not reject a valid token.
const clockSkew = 5 * time.Second

// AI-generated (edited by James Teo).
// The iss and aud user-service puts on every token it signs, and requires
// back in its own Verify (user-service/internal/jwt/jwt.go). Checking them
// here too means a token the same keys sign for anything else is not taken
// as a user's session.
const (
	issuer   = "campusrun-user-service"
	audience = "campusrun-api"
)

// minRefreshInterval throttles key refetches. An unknown kid triggers one, so
// without this a client could force unbounded requests to user-service by
// sending tokens with invented kids.
const minRefreshInterval = 30 * time.Second

// Claims is what order-service reads from a verified access token.
type Claims struct {
	// UserID is the token's sub: the caller's account UUID.
	UserID uuid.UUID
	// Role is the role claim: STUDENT or ADMIN (D-019). The caller checks it.
	Role string
	// TokenID is the jti user-service blocklists on logout (D-014).
	TokenID string
	// IssuedAt is the iat, compared with a suspension time (D-014, D-054).
	IssuedAt time.Time
}

// Verifier verifies access tokens against user-service's published RSA keys.
// Keys are fetched on first use, so order-service can start before
// user-service. It is safe for concurrent use.
type Verifier struct {
	jwksURL string
	client  *http.Client

	// fetchMu serializes refetches, so concurrent requests with an unknown kid
	// share one fetch instead of each starting their own.
	fetchMu sync.Mutex

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	lastErr     error
}

// NewVerifier returns a Verifier that fetches keys from jwksURL with client
// (a nil client gets a 5-second timeout).
func NewVerifier(jwksURL string, client *http.Client) *Verifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Verifier{jwksURL: jwksURL, client: client, keys: make(map[string]*rsa.PublicKey)}
}

// Verify parses and validates an access token. The error matches one of the
// sentinels above under errors.Is; ErrKeysUnavailable carries its cause.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	if token == "" {
		return Claims{}, ErrMalformed
	}
	var registered jwt.RegisteredClaims
	parsed, err := jwt.ParseWithClaims(token, &registered,
		func(t *jwt.Token) (any, error) { return v.keyFor(ctx, t) },
		// RS256 only: otherwise a token could name "none" or an HMAC algorithm
		// and be checked against the public key as if it were a shared secret.
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(clockSkew),
		// A token with no exp would never expire, and one with no iat could
		// not be compared with a suspension time.
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		// AI-generated (edited by James Teo).
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
	)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenNotValidYet), errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
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

	// typ and role are not registered claims, so they come off the payload.
	// Safe here: the signature has been verified, so the payload is exactly
	// what user-service signed.
	var custom struct {
		Typ  string `json:"typ"`
		Role string `json:"role"`
	}
	if err := decodePayload(token, &custom); err != nil {
		return Claims{}, ErrMalformed
	}
	if custom.Typ != "access" {
		return Claims{}, ErrWrongType
	}
	id, err := uuid.Parse(registered.Subject)
	if err != nil || id == uuid.Nil || registered.ID == "" || registered.IssuedAt == nil {
		return Claims{}, ErrMalformed
	}
	return Claims{UserID: id, Role: custom.Role, TokenID: registered.ID, IssuedAt: registered.IssuedAt.Time}, nil
}

// keyFor resolves the key named by the token's kid, refetching the key set
// once if the kid is unknown: user-service publishes an active key plus
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
	// Another request may have fetched while this one waited.
	if key, ok := v.cachedKey(kid); ok {
		return key, nil
	}
	v.mu.RLock()
	attempted, lastErr := v.lastAttempt, v.lastErr
	v.mu.RUnlock()
	if !attempted.IsZero() && time.Since(attempted) < minRefreshInterval {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrInvalidSignature
	}
	// Detached from the request's cancellation: the result is cached for every
	// request, so one client leaving must not record a failure. The client's
	// timeout still bounds it.
	if err := v.refresh(context.WithoutCancel(ctx)); err != nil {
		return nil, err
	}
	if key, ok := v.cachedKey(kid); ok {
		return key, nil
	}
	return nil, ErrInvalidSignature
}

func (v *Verifier) cachedKey(kid string) (*rsa.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	key, ok := v.keys[kid]
	return key, ok
}

// refresh fetches the key set and records the attempt, successful or not, so
// minRefreshInterval also throttles fetches while user-service is failing. A
// failed fetch keeps the keys already cached.
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

func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrKeysUnavailable, err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeysUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %d", ErrKeysUnavailable, v.jwksURL, resp.StatusCode)
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
		return nil, fmt.Errorf("%w: decode key set: %v", ErrKeysUnavailable, err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		key, err := rsaKey(k.N, k.E)
		if err != nil {
			continue // one malformed key does not spoil the rest
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: %s published no usable RSA key", ErrKeysUnavailable, v.jwksURL)
	}
	return keys, nil
}

// rsaKey rebuilds a public key from a key-set entry's base64url modulus and exponent.
func rsaKey(nRaw, eRaw string) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(nRaw)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	e, err := base64.RawURLEncoding.DecodeString(eRaw)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}
	if len(n) == 0 || len(e) == 0 {
		return nil, errors.New("empty modulus or exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
}

// decodePayload reads the claims straight off the token's payload segment.
func decodePayload(token string, out any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ErrMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrMalformed
	}
	return json.Unmarshal(payload, out)
}
