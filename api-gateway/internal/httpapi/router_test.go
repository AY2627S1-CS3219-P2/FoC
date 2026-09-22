// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: End-to-end tests for the router — real RS256 tokens against a real
//   JWKS endpoint, through the middleware and proxy to stub downstreams.
//   2026-09-21: three tests added for the reworked /api/users route.
//   2026-09-22: two added for /api/suppliers reaching supplier-service's
//   /suppliers prefix, and two for the interim CORS policy.
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
	"strings"
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
	// Was "/42" until 2026-09-22, when the route gained supplier-service's
	// own /suppliers prefix. This test is about the claim headers below;
	// the path itself is covered by TestSupplierRouteReachesSupplierServicePrefix.
	if h.suppRec.path != "/suppliers/42" {
		t.Errorf("supplier-service saw path %q, want %q", h.suppRec.path, "/suppliers/42")
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

// AI-generated (edited by <name>).
// The three tests below cover the /api/users route after it was pointed at
// user-service's real prefix and allowed to keep the bearer token.

func TestUserRouteReachesUserServicePrefix(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/users/uid-123", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	// Not "/uid-123": user-service mounts its profile route at
	// /api/v1/users/{uid}, per its committed api/openapi.yaml.
	if want := "/api/v1/users/uid-123"; h.userRec.path != want {
		t.Errorf("user-service saw path %q, want %q", h.userRec.path, want)
	}
}

func TestUserRouteRetainsBearerAndStillAssertsIdentity(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/users/uid-123", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// A caller trying to assert its own role on the way through.
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	// user-service authenticates on this header itself (RequireJWT).
	if h.userRec.header.Get("Authorization") != "Bearer "+token {
		t.Error("user-service did not receive the bearer token it authenticates on")
	}
	// D-022 still holds: the client's ADMIN claim is replaced, not honoured.
	if role := h.userRec.header.Get(proxy.ClaimHeaderRole); role != "STUDENT" {
		t.Errorf("%s = %q, want STUDENT — the client's own value must not survive",
			proxy.ClaimHeaderRole, role)
	}
	if uid := h.userRec.header.Get(proxy.ClaimHeaderUserID); uid != "uid-123" {
		t.Errorf("%s = %q, want uid-123", proxy.ClaimHeaderUserID, uid)
	}
}

func TestOtherServicesNeverReceiveTheBearerToken(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	// Retaining the token is user-service's exception alone. Every other
	// callee reads the claim headers and has no use for a credential.
	if authz := h.suppRec.header.Get("Authorization"); authz != "" {
		t.Errorf("supplier-service received Authorization = %q, want it stripped", authz)
	}
}

// AI-generated (edited by <name>).
// The two tests below cover /api/suppliers after it was pointed at
// supplier-service's real /suppliers prefix (PR #1, f779ced).

func TestSupplierRouteReachesSupplierServicePrefix(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	// Not "/42": supplier-service mounts its routes under /suppliers, so the
	// bare id 404'd there before this prefix was added.
	if want := "/suppliers/42"; h.suppRec.path != want {
		t.Errorf("supplier-service saw path %q, want %q", h.suppRec.path, want)
	}
}

func TestSupplierCollectionRouteKeepsThePrefix(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	// The bare prefix must not become "/suppliers/" — chi's r.Route mounts
	// the list handler at "/suppliers" itself. This is Route.RewritePath's
	// trailing-slash case.
	if want := "/suppliers"; h.suppRec.path != want {
		t.Errorf("supplier-service saw path %q, want %q", h.suppRec.path, want)
	}
}

// AI-generated (edited by <name>).
// The two tests below cover the interim permissive CORS policy. The first is
// the one that matters: a preflight carries no Authorization header, so it
// must be answered BEFORE RequireToken sees it, or the browser never sends the
// real request and the whole frontend is dead against the gateway.

func TestPreflightOnAuthenticatedRouteIsNotRejected(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	req := httptest.NewRequest(http.MethodOptions, "/api/suppliers/42", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization")

	got := h.do(req)
	if got.Code == http.StatusUnauthorized {
		t.Fatalf("preflight got 401 — RequireToken ran before the CORS handler, "+
			"so the browser will never send the real request (body %s)", got.Body.String())
	}
	if origin := got.Header().Get("Access-Control-Allow-Origin"); origin == "" {
		t.Error("no Access-Control-Allow-Origin on the preflight response")
	}
	// A header the preflight does not permit is not sent by the browser, and
	// every proxied route needs the bearer token.
	allowed := got.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowed), "authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q, want it to include Authorization", allowed)
	}
}

func TestCORSDoesNotAdvertiseTheClaimHeaders(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	req := httptest.NewRequest(http.MethodOptions, "/api/suppliers/42", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	req.Header.Set("Access-Control-Request-Method", "GET")

	allowed := strings.ToLower(h.do(req).Header().Get("Access-Control-Allow-Headers"))
	// D-022: the gateway deletes these on every route. Telling a browser it
	// may send one would suggest a client can assert its own identity.
	for _, banned := range []string{
		strings.ToLower(proxy.ClaimHeaderUserID),
		strings.ToLower(proxy.ClaimHeaderRole),
	} {
		if strings.Contains(allowed, banned) {
			t.Errorf("Access-Control-Allow-Headers advertises %q; the gateway strips it (D-022)", banned)
		}
	}
}
