// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-09-26
// Scope: Unit tests for Verify's accept and reject paths and for the JWKS
//   fetch throttle, including while the endpoint is failing. Written against
//   PR #4 review findings (no-exp tokens accepted, refetch on every request).
// Author review: nigeltzy - Checked the implementation of this test code, all seems normal and valid.

package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"foc/api-gateway/internal/auth"
)

const testKID = "k1"

// jwksFor serves key under kid in the shape user-service publishes.
func jwksFor(t *testing.T, key *rsa.PrivateKey, kid string) []byte {
	t.Helper()
	b64 := base64.RawURLEncoding.EncodeToString
	body, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": kid,
		"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// jwksServer answers every request with status and body, and counts requests.
func jwksServer(t *testing.T, status int, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// sign mints a token with the given method, key and header kid.
func sign(t *testing.T, method jwt.SigningMethod, key any, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// accessClaims are the claims user-service puts on an access token, plus extra.
func accessClaims(extra jwt.MapClaims) jwt.MapClaims {
	c := jwt.MapClaims{
		"sub": "u1", "typ": "access", "role": "STUDENT", "jti": "j1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range extra {
		if v == nil {
			delete(c, k)
			continue
		}
		c[k] = v
	}
	return c
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestVerify(t *testing.T) {
	key := newKey(t)
	otherKey := newKey(t)
	srv, _ := jwksServer(t, http.StatusOK, jwksFor(t, key, testKID))

	tests := []struct {
		name    string
		token   string
		wantErr error
	}{
		{
			name:  "valid access token",
			token: sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(nil)),
		},
		{
			name:    "no exp is rejected",
			token:   sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(jwt.MapClaims{"exp": nil})),
			wantErr: auth.ErrMalformed,
		},
		{
			name:    "expired",
			token:   sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()})),
			wantErr: auth.ErrExpired,
		},
		{
			name:    "not valid yet",
			token:   sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(jwt.MapClaims{"nbf": time.Now().Add(time.Hour).Unix()})),
			wantErr: auth.ErrExpired,
		},
		{
			name:    "refresh token",
			token:   sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(jwt.MapClaims{"typ": "refresh"})),
			wantErr: auth.ErrWrongType,
		},
		{
			name:    "signed by another key under the same kid",
			token:   sign(t, jwt.SigningMethodRS256, otherKey, testKID, accessClaims(nil)),
			wantErr: auth.ErrInvalidSignature,
		},
		{
			name:    "HMAC algorithm",
			token:   sign(t, jwt.SigningMethodHS256, []byte("secret"), testKID, accessClaims(nil)),
			wantErr: auth.ErrInvalidSignature,
		},
		{
			name:    "no kid",
			token:   sign(t, jwt.SigningMethodRS256, key, "", accessClaims(nil)),
			wantErr: auth.ErrMalformed,
		},
		{
			name:    "garbage",
			token:   "not.a.jwt",
			wantErr: auth.ErrMalformed,
		},
		{
			name:    "empty",
			token:   "",
			wantErr: auth.ErrMalformed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := auth.NewVerifier(srv.URL, nil)
			claims, err := v.Verify(context.Background(), tt.token)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Verify err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if claims.Subject != "u1" || claims.Role != "STUDENT" || claims.ID != "j1" {
				t.Errorf("claims = %+v", claims)
			}
		})
	}
}

// TestVerifyKeysUnavailable checks that an unusable JWKS endpoint is reported
// as ErrKeysUnavailable with its cause, not as a bad token.
func TestVerifyKeysUnavailable(t *testing.T) {
	key := newKey(t)
	token := sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(nil))

	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()

	tests := []struct {
		name    string
		jwksURL string
	}{
		{"unreachable", downURL},
		{"500", mustServer(t, http.StatusInternalServerError, nil)},
		{"not JSON", mustServer(t, http.StatusOK, []byte("<html>"))},
		{"no usable keys", mustServer(t, http.StatusOK, []byte(`{"keys":[]}`))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewVerifier(tt.jwksURL, nil).Verify(context.Background(), token)
			if !errors.Is(err, auth.ErrKeysUnavailable) {
				t.Fatalf("Verify err = %v, want ErrKeysUnavailable", err)
			}
			if err.Error() == auth.ErrKeysUnavailable.Error() {
				t.Errorf("err %q carries no cause", err)
			}
		})
	}
}

func mustServer(t *testing.T, status int, body []byte) string {
	t.Helper()
	srv, _ := jwksServer(t, status, body)
	return srv.URL
}

// TestJWKSFetchThrottle checks that repeated verifications within the refetch
// interval fetch the key set once, whether the endpoint is healthy, failing,
// or healthy but missing the token's kid.
func TestJWKSFetchThrottle(t *testing.T) {
	key := newKey(t)
	token := sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(nil))

	tests := []struct {
		name    string
		status  int
		body    []byte
		wantErr error
	}{
		{"healthy: keys are reused", http.StatusOK, jwksFor(t, key, testKID), nil},
		{"failing", http.StatusInternalServerError, nil, auth.ErrKeysUnavailable},
		{"kid not published", http.StatusOK, jwksFor(t, key, "other"), auth.ErrInvalidSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, hits := jwksServer(t, tt.status, tt.body)
			v := auth.NewVerifier(srv.URL, nil)

			for i := range 10 {
				_, err := v.Verify(context.Background(), token)
				if tt.wantErr == nil && err != nil {
					t.Fatalf("call %d: Verify: %v", i, err)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Fatalf("call %d: Verify err = %v, want %v", i, err, tt.wantErr)
				}
			}
			if got := hits.Load(); got != 1 {
				t.Errorf("10 Verify calls fetched the key set %d times, want 1", got)
			}
		})
	}
}

// TestJWKSFetchIgnoresCallerCancellation checks that a request cancelled while
// the key set is being fetched does not record a failure that the next
// request would then be answered from.
func TestJWKSFetchIgnoresCallerCancellation(t *testing.T) {
	key := newKey(t)
	token := sign(t, jwt.SigningMethodRS256, key, testKID, accessClaims(nil))
	srv, _ := jwksServer(t, http.StatusOK, jwksFor(t, key, testKID))
	v := auth.NewVerifier(srv.URL, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = v.Verify(ctx, token)

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify after a cancelled first call: %v", err)
	}
}
