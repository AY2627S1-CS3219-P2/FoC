// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: RS256 access-token verification against user-service's published JWKS keys (D-023, D-056).
//   Adapted from order-service/internal/auth per D-041/D-056.
// Author review: PENDING - reviewer to complete

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

var (
	// ErrMalformed covers a token that is absent, not a JWT, unparseable, or missing a required claim.
	ErrMalformed = errors.New("auth: malformed token")
	// ErrInvalidSignature covers a token whose signature does not verify, which names an unknown key, or whose iss/aud is invalid.
	ErrInvalidSignature = errors.New("auth: invalid signature")
	// ErrExpired covers a token past its exp or before its nbf, after clockSkew leeway.
	ErrExpired = errors.New("auth: token expired")
	// ErrWrongType covers a non-access token (e.g. refresh token) presented where an access token belongs.
	ErrWrongType = errors.New("auth: not an access token")
	// ErrKeysUnavailable covers being unable to load user-service's keys.
	ErrKeysUnavailable = errors.New("auth: signing keys unavailable")
)

const clockSkew = 5 * time.Second

const (
	issuer   = "campusrun-user-service"
	audience = "campusrun-api"
)

const minRefreshInterval = 30 * time.Second

// Claims represents what credit-service reads from a verified access token.
type Claims struct {
	UserID   uuid.UUID
	Role     string
	TokenID  string
	IssuedAt time.Time
}

// Verifier verifies access tokens against user-service's published RSA keys.
type Verifier struct {
	jwksURL string
	client  *http.Client

	fetchMu sync.Mutex

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	lastErr     error
}

// NewVerifier returns a Verifier that fetches keys from jwksURL with client.
func NewVerifier(jwksURL string, client *http.Client) *Verifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Verifier{jwksURL: jwksURL, client: client, keys: make(map[string]*rsa.PublicKey)}
}

// Verify parses and validates an access token.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	if token == "" {
		return Claims{}, ErrMalformed
	}
	var registered jwt.RegisteredClaims
	parsed, err := jwt.ParseWithClaims(token, &registered,
		func(t *jwt.Token) (any, error) { return v.keyFor(ctx, t) },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(clockSkew),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
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
			continue
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: %s published no usable RSA key", ErrKeysUnavailable, v.jwksURL)
	}
	return keys, nil
}

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
