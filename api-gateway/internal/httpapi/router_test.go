// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: End-to-end tests for the router — real RS256 tokens against a real
//   JWKS endpoint, through the middleware and proxy to stub downstreams.
// Author review: PENDING — <reviewer to complete>

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

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/config"
	"foc/api-gateway/internal/httpapi"
	"foc/api-gateway/internal/proxy"
)

const testKeyID = "test-key-1"

// issuer stands in for user-service: it holds the private key and publishes
// the matching public key as a JWKS, exactly as D-023 describes.
type issuer struct {
	key *rsa.PrivateKey
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return &issuer{key: key}
}

// jwksHandler publishes the public half, base64url-encoded as a JWKS entry.
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

type tokenOpts struct {
	subject string
	role    string
	typ     string
	expires time.Time
	kid     string
}

func (i *issuer) mint(t *testing.T, o tokenOpts) string {
	t.Helper()
	if o.typ == "" {
		o.typ = "access"
	}
	if o.expires.IsZero() {
		o.expires = time.Now().Add(15 * time.Minute)
	}
	if o.kid == "" {
		o.kid = testKeyID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":  "campusrun-user-service",
		"sub":  o.subject,
		"aud":  "campusrun-api",
		"exp":  o.expires.Unix(),
		"iat":  time.Now().Add(-time.Minute).Unix(),
		"jti":  "test-jti",
		"typ":  o.typ,
		"role": o.role,
	})
	token.Header["kid"] = o.kid
	signed, err := token.SignedString(i.key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

// recorder captures what a downstream service actually received.
type recorder struct {
	path   string
	header http.Header
}

func stubService(rec *recorder) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.header = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

type harness struct {
	router   http.Handler
	issuer   *issuer
	userRec  *recorder
	suppRec  *recorder
	teardown func()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())

	userRec, suppRec := &recorder{}, &recorder{}
	userSvc := stubService(userRec)
	suppSvc := stubService(suppRec)

	verifier := auth.NewVerifier(jwks.URL, nil)
	router, err := httpapi.NewRouter(config.Downstream{
		User:     userSvc.URL,
		Supplier: suppSvc.URL,
		Order:    suppSvc.URL,
		Credit:   suppSvc.URL,
	}, verifier)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	return &harness{
		router:  router,
		issuer:  iss,
		userRec: userRec,
		suppRec: suppRec,
		teardown: func() {
			jwks.Close()
			userSvc.Close()
			suppSvc.Close()
		},
	}
}

func (h *harness) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	got := h.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", got.Code)
	}
}

func TestServiceRouteRejectsMissingToken(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	got := h.do(httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil))
	if got.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", got.Code)
	}
	if h.suppRec.path != "" {
		t.Errorf("request reached supplier-service at %q despite having no token", h.suppRec.path)
	}
}

func TestServiceRouteForwardsVerifiedIdentity(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	got := h.do(req)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	if h.suppRec.path != "/42" {
		t.Errorf("supplier-service saw path %q, want %q", h.suppRec.path, "/42")
	}
	if uid := h.suppRec.header.Get(proxy.ClaimHeaderUserID); uid != "uid-123" {
		t.Errorf("user id header = %q, want %q", uid, "uid-123")
	}
	if role := h.suppRec.header.Get(proxy.ClaimHeaderRole); role != "STUDENT" {
		t.Errorf("role header = %q, want %q", role, "STUDENT")
	}
}

// The whole of D-022 in one test: a caller with a legitimate STUDENT token
// claiming ADMIN in a header must reach the service as a STUDENT.
func TestClientCannotEscalateViaHeader(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")
	req.Header.Set(proxy.ClaimHeaderUserID, "somebody-else")

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	if role := h.suppRec.header.Get(proxy.ClaimHeaderRole); role != "STUDENT" {
		t.Errorf("PRIVILEGE ESCALATION: role header = %q, want %q", role, "STUDENT")
	}
	if uid := h.suppRec.header.Get(proxy.ClaimHeaderUserID); uid != "uid-123" {
		t.Errorf("IDENTITY SPOOF: user id header = %q, want %q", uid, "uid-123")
	}
}

func TestRejectsRefreshTokenOnServiceRoute(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT", typ: "refresh"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a refresh token", got.Code)
	}
}

func TestRejectsExpiredToken(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{
		subject: "uid-123",
		role:    "STUDENT",
		expires: time.Now().Add(-time.Hour),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an expired token", got.Code)
	}
}

func TestRejectsTokenSignedByAnotherKey(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	// A different issuer entirely — right shape, wrong key.
	attacker := newIssuer(t)
	token := attacker.mint(t, tokenOpts{subject: "uid-123", role: "ADMIN"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a foreign signature", got.Code)
	}
}

func TestAuthRouteIsPublicAndRewritten(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	got := h.do(req)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — auth routes take no token", got.Code)
	}
	if h.userRec.path != "/api/v1/users/login" {
		t.Errorf("user-service saw path %q, want %q", h.userRec.path, "/api/v1/users/login")
	}
}

func TestAuthRoutePreservesBearerForLogout(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	if h.userRec.header.Get("Authorization") == "" {
		t.Error("logout reached user-service without the bearer token; it cannot blocklist the jti")
	}
	if h.userRec.path != "/api/v1/users/logout" {
		t.Errorf("user-service saw path %q, want %q", h.userRec.path, "/api/v1/users/logout")
	}
}
