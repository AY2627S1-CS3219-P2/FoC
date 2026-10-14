// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-14
// Scope: Implemented local RS256 JWT verification for credit-service per D-041.
// Author review: PENDING - reviewer to complete

// Package auth verifies access tokens against user-service's JWKS endpoint.
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

var (
	// ErrMalformed covers an absent, non-JWT, or unparseable token.
	ErrMalformed = errors.New("auth: malformed token")
	// ErrInvalidSignature covers a token whose signature cannot be verified.
	ErrInvalidSignature = errors.New("auth: invalid signature")
	// ErrExpired covers an expired token or one that is not valid yet.
	ErrExpired = errors.New("auth: token expired")
	// ErrWrongType covers a refresh token used where an access token is required.
	ErrWrongType = errors.New("auth: not an access token")
	// ErrKeysUnavailable covers inability to fetch usable JWKS signing keys.
	ErrKeysUnavailable = errors.New("auth: signing keys unavailable")
)

const (
	clockSkew          = 5 * time.Second
	minRefreshInterval = 30 * time.Second
)

// Claims is the subset of verified access-token claims used by credit-service.
type Claims struct {
	Subject string
	Role    string
	ID      string
}

// Verifier verifies RS256 access tokens against cached JWKS public keys.
type Verifier struct {
	jwksURL string
	client  *http.Client

	fetchMu sync.Mutex

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	lastErr     error
}

// NewVerifier returns a verifier that fetches signing keys from jwksURL.
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

// Verify validates tokenString and returns verified access-token claims.
func (v *Verifier) Verify(ctx context.Context, tokenString string) (Claims, error) {
	if tokenString == "" {
		return Claims{}, ErrMalformed
	}

	var registered jwt.RegisteredClaims
	var custom struct {
		Typ  string `json:"typ"`
		Role string `json:"role"`
	}

	parsed, err := jwt.ParseWithClaims(
		tokenString,
		&registered,
		func(token *jwt.Token) (any, error) { return v.keyFor(ctx, token) },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(clockSkew),
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

	if err := decodeCustom(tokenString, &custom); err != nil {
		return Claims{}, ErrMalformed
	}
	if custom.Typ != "access" {
		return Claims{}, ErrWrongType
	}

	return Claims{
		Subject: registered.Subject,
		Role:    custom.Role,
		ID:      registered.ID,
	}, nil
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
	for _, candidate := range set.Keys {
		if candidate.Kty != "RSA" || candidate.Kid == "" {
			continue
		}
		key, err := rsaKeyFrom(candidate.N, candidate.E)
		if err != nil {
			continue
		}
		keys[candidate.Kid] = key
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: %s published no usable RSA keys", ErrKeysUnavailable, v.jwksURL)
	}

	return keys, nil
}

func rsaKeyFrom(nRaw string, eRaw string) (*rsa.PublicKey, error) {
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
