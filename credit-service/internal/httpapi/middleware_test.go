// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-14
// Scope: Added tests for credit-service JWT middleware behavior.
// Author review: PENDING - reviewer to complete

package httpapi_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/httpapi"
)

const testKeyID = "test-key-1"

type issuer struct {
	key *rsa.PrivateKey
}

type tokenOpts struct {
	subject string
	role    string
	typ     string
	expires time.Time
	kid     string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return &issuer{key: key}
}

func (i *issuer) jwksHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pub := i.key.Public().(*rsa.PublicKey)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": testKeyID,
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
}

func (i *issuer) mint(t *testing.T, opts tokenOpts) string {
	t.Helper()
	if opts.typ == "" {
		opts.typ = "access"
	}
	if opts.expires.IsZero() {
		opts.expires = time.Now().Add(15 * time.Minute)
	}
	if opts.kid == "" {
		opts.kid = testKeyID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":  "campusrun-user-service",
		"sub":  opts.subject,
		"aud":  "campusrun-api",
		"exp":  opts.expires.Unix(),
		"iat":  time.Now().Add(-time.Minute).Unix(),
		"jti":  "test-jti",
		"typ":  opts.typ,
		"role": opts.role,
	})
	token.Header["kid"] = opts.kid
	signed, err := token.SignedString(i.key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestRequireTokenRejectsMissingBearer(t *testing.T) {
	verifier := auth.NewVerifier("http://127.0.0.1/unused", nil)
	handler := httpapi.RequireToken(verifier)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	got := httptest.NewRecorder()
	handler.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/api/v1/credits/me/balance", nil))

	if got.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got.Code)
	}
}

func TestRequireTokenRejectsExpiredToken(t *testing.T) {
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)

	verifier := auth.NewVerifier(jwks.URL, nil)
	handler := httpapi.RequireToken(verifier)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/me/balance", nil)
	req.Header.Set("Authorization", "Bearer "+iss.mint(t, tokenOpts{
		subject: "uid-123",
		role:    "STUDENT",
		expires: time.Now().Add(-time.Hour),
	}))
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, req)

	if got.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got.Code)
	}
}

func TestRequireTokenRejectsRefreshToken(t *testing.T) {
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)

	verifier := auth.NewVerifier(jwks.URL, nil)
	handler := httpapi.RequireToken(verifier)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/me/balance", nil)
	req.Header.Set("Authorization", "Bearer "+iss.mint(t, tokenOpts{
		subject: "uid-123",
		role:    "STUDENT",
		typ:     "refresh",
	}))
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, req)

	if got.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got.Code)
	}
}

func TestRequireTokenStoresVerifiedIdentity(t *testing.T) {
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)

	verifier := auth.NewVerifier(jwks.URL, nil)
	handler := httpapi.RequireToken(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			t.Fatal("identity missing from request context")
		}
		if identity.UserID != "uid-123" {
			t.Errorf("UserID = %q, want uid-123", identity.UserID)
		}
		if identity.Role != "STUDENT" {
			t.Errorf("Role = %q, want STUDENT", identity.Role)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/me/balance", nil)
	req.Header.Set("Authorization", "Bearer "+iss.mint(t, tokenOpts{
		subject: "uid-123",
		role:    "STUDENT",
	}))
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, req)

	if got.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", got.Code)
	}
}
