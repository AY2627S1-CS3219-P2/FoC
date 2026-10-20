// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: End-to-end tests for the router — real RS256 tokens against a real
//   JWKS endpoint, through the middleware and proxy to stub downstreams.
//   2026-09-21: three tests added for the reworked /api/users route.
//   2026-09-22: two added for /api/suppliers reaching supplier-service's
//   /suppliers prefix. The two interim-CORS tests were removed with the
//   policy when the frontend became same-origin. 2026-10-10 (Claude Code,
//   Opus 5.5): one added for the D-043 OTP routes.
// Author review: Nigeltzy - Directed and generated as part of implementation of unit tests for the router package. I checked the output and it seems to be reasonable and typical unit tests for the router package.

package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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

// issuer stands in for user-service: it signs tokens with an RSA key and
// publishes the public half as a JWKS.
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
	// issuedAt defaults to a minute ago; noIAT and noJTI leave the claim out.
	issuedAt time.Time
	noIAT    bool
	noJTI    bool
}

// testJTI is the jti every minted token carries unless noJTI is set.
const testJTI = "test-jti"

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
	if o.issuedAt.IsZero() {
		o.issuedAt = time.Now().Add(-time.Minute)
	}
	claims := jwt.MapClaims{
		"iss":  "campusrun-user-service",
		"sub":  o.subject,
		"aud":  "campusrun-api",
		"exp":  o.expires.Unix(),
		"iat":  o.issuedAt.Unix(),
		"jti":  testJTI,
		"typ":  o.typ,
		"role": o.role,
	}
	if o.noIAT {
		delete(claims, "iat")
	}
	if o.noJTI {
		delete(claims, "jti")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = o.kid
	signed, err := token.SignedString(i.key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

// testRefreshTTL stands in for config's REFRESH_TOKEN_TTL (168h, as in
// .env.example).
const testRefreshTTL = 168 * time.Hour

// recorder captures what a downstream service actually received.
type recorder struct {
	path   string
	query  string
	header http.Header
	body   string
}

func stubService(rec *recorder) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.header = r.Header.Clone()
		// Sets a CORS header, as a real callee may, so tests can check the proxy
		// strips it.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

// fakeRevocations stands in for internal/revocation's Checker. It answers
// revoked and err as set, and records each lookup.
type fakeRevocations struct {
	revoked bool
	err     error

	calls       int
	tokenID     string
	subject     string
	tokenIssued time.Time
}

func (f *fakeRevocations) Revoked(_ context.Context, tokenID, subject string, issuedAt time.Time) (bool, error) {
	f.calls++
	f.tokenID, f.subject, f.tokenIssued = tokenID, subject, issuedAt
	return f.revoked, f.err
}

// AI-generated (edited by nigeltzy).
// AI-generated (edited by James Teo): order-service and credit-service get
// their own recorders.
type harness struct {
	router      http.Handler
	issuer      *issuer
	revocations *fakeRevocations
	userRec     *recorder
	suppRec     *recorder
	orderRec    *recorder
	creditRec   *recorder
}

// AI-generated (edited by nigeltzy).
// AI-generated (edited by James Teo): every downstream gets its own stub, so a
// route pointed at the wrong service fails the test that checks its callee.
func newHarness(t *testing.T) *harness {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)

	userRec, suppRec, orderRec, creditRec := &recorder{}, &recorder{}, &recorder{}, &recorder{}
	userSvc := stubService(userRec)
	t.Cleanup(userSvc.Close)
	suppSvc := stubService(suppRec)
	t.Cleanup(suppSvc.Close)
	orderSvc := stubService(orderRec)
	t.Cleanup(orderSvc.Close)
	creditSvc := stubService(creditRec)
	t.Cleanup(creditSvc.Close)

	verifier := auth.NewVerifier(jwks.URL, nil)
	revocations := &fakeRevocations{}
	router, err := httpapi.NewRouter(config.Downstream{
		User:     userSvc.URL,
		Supplier: suppSvc.URL,
		Order:    orderSvc.URL,
		Credit:   creditSvc.URL,
	}, verifier, revocations, testRefreshTTL)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	return &harness{
		router:      router,
		issuer:      iss,
		revocations: revocations,
		userRec:     userRec,
		suppRec:     suppRec,
		orderRec:    orderRec,
		creditRec:   creditRec,
	}
}

func (h *harness) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// AI-generated (edited by James Teo).
// downstreamCalls lists every stub service that received a request, as
// "service path", so a test can check that a request stopped at the gateway.
func (h *harness) downstreamCalls() []string {
	var calls []string
	for _, d := range []struct {
		service string
		rec     *recorder
	}{
		{"user-service", h.userRec},
		{"supplier-service", h.suppRec},
		{"order-service", h.orderRec},
		{"credit-service", h.creditRec},
	} {
		if d.rec.path != "" {
			calls = append(calls, d.service+" "+d.rec.path)
		}
	}
	return calls
}

func TestHealthz(t *testing.T) {
	h := newHarness(t)

	got := h.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", got.Code)
	}
}

// AI-generated (edited by nigeltzy).
func TestServiceRouteRequiresAValidAccessToken(t *testing.T) {
	tests := []struct {
		name string
		// token returns the bearer token to send; "" sends no Authorization header.
		token func(t *testing.T, iss *issuer) string
	}{
		{"missing token", func(*testing.T, *issuer) string { return "" }},
		{"refresh token presented as bearer", func(t *testing.T, iss *issuer) string {
			return iss.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT", typ: "refresh"})
		}},
		{"expired token", func(t *testing.T, iss *issuer) string {
			return iss.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT", expires: time.Now().Add(-time.Hour)})
		}},
		{"token signed by another key", func(t *testing.T, _ *issuer) string {
			// A different issuer entirely — right shape, wrong key.
			return newIssuer(t).mint(t, tokenOpts{subject: "uid-123", role: "ADMIN"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)

			req := httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil)
			if token := tt.token(t, h.issuer); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			if got := h.do(req); got.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", got.Code)
			}
			if h.suppRec.path != "" {
				t.Errorf("request reached supplier-service at %q, want it stopped at the gateway", h.suppRec.path)
			}
		})
	}
}

func TestServiceRouteForwardsVerifiedIdentity(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	got := h.do(req)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
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

// TestClientCannotEscalateViaHeader checks that a caller with a valid STUDENT
// token who also sends X-User-Role: ADMIN and another user's X-User-Id reaches
// the service as itself, a STUDENT.
func TestClientCannotEscalateViaHeader(t *testing.T) {
	h := newHarness(t)

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

func TestAuthRouteIsPublicAndRewritten(t *testing.T) {
	h := newHarness(t)

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

// AI-generated (edited by nigeltzy).
// Covers the three /api/users tests below.

func TestUserRouteReachesUserServicePrefix(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/users/uid-123", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	// user-service mounts its profile route at /api/v1/users/{uid}.
	if want := "/api/v1/users/uid-123"; h.userRec.path != want {
		t.Errorf("user-service saw path %q, want %q", h.userRec.path, want)
	}
}

func TestUserRouteRetainsBearerAndStillAssertsIdentity(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/users/uid-123", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// A caller trying to assert its own role on the way through.
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	// user-service verifies this header itself.
	if h.userRec.header.Get("Authorization") != "Bearer "+token {
		t.Error("user-service did not receive the bearer token it authenticates on")
	}
	// The client's ADMIN claim is replaced by the verified role.
	if role := h.userRec.header.Get(proxy.ClaimHeaderRole); role != "STUDENT" {
		t.Errorf("%s = %q, want STUDENT — the client's own value must not survive",
			proxy.ClaimHeaderRole, role)
	}
	if uid := h.userRec.header.Get(proxy.ClaimHeaderUserID); uid != "uid-123" {
		t.Errorf("%s = %q, want uid-123", proxy.ClaimHeaderUserID, uid)
	}
}

// AI-generated (edited by James Teo).
// D-041 has every service verify the bearer token itself, so every service
// route forwards it, alongside the claim headers the gateway still injects
// (D-022).
func TestServiceRoutesKeepTheBearerToken(t *testing.T) {
	cases := []struct {
		path, service string
		// callee picks the stub that stands in for service.
		callee func(*harness) *recorder
	}{
		{"/api/suppliers/42", "supplier-service", func(h *harness) *recorder { return h.suppRec }},
		{"/api/orders/open", "order-service", func(h *harness) *recorder { return h.orderRec }},
		{"/api/credits/accounts/me", "credit-service", func(h *harness) *recorder { return h.creditRec }},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			h := newHarness(t)
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)

			if got := h.do(req); got.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
			}
			callee := tc.callee(h)
			if callee.path == "" {
				t.Fatalf("%s was not called; the request reached %v", tc.service, h.downstreamCalls())
			}
			if authz := callee.header.Get("Authorization"); authz != "Bearer "+token {
				t.Errorf("%s received Authorization = %q, want the caller's bearer token", tc.service, authz)
			}
			if id := callee.header.Get("X-User-Id"); id != "uid-123" {
				t.Errorf("%s received X-User-Id = %q, want uid-123", tc.service, id)
			}
		})
	}
}

// AI-generated (edited by James Teo).
// /api/credits/accounts/* is rewritten onto credit-service's account prefix.
func TestCreditAccountRouteReachesCreditServicePrefix(t *testing.T) {
	h := newHarness(t)
	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/credits/accounts/me/transactions", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	if want := "/api/v1/credits/accounts/me/transactions"; h.creditRec.path != want {
		t.Errorf("credit-service saw path %q, want %q", h.creditRec.path, want)
	}
}

// AI-generated (edited by James Teo).
// credit-service's reservation routes are for order-service alone: from the
// browser they are not routed, not even through dot segments, so a requester
// cannot lower their own hold.
func TestCreditWriteRoutesAreNotPublic(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/credits/reservations"},
		{http.MethodPatch, "/api/credits/reservations/0b6f3c1e-5f0a-4c8e-9a51-6d2f4a8b7c10"},
		{http.MethodPost, "/api/credits/api/v1/credits/reservations"},
		{http.MethodGet, "/api/credits"},
		// Dot segments, raw and percent-encoded, which credit-service could
		// resolve onto its reservation routes. httptest.NewRequest parses the
		// target as a server does, so they reach the router as sent.
		{http.MethodPost, "/api/credits/accounts/../reservations"},
		{http.MethodPost, "/api/credits/accounts/%2e%2e/reservations"},
		{http.MethodPost, "/api/credits/accounts/..%2freservations"},
		{http.MethodGet, "/api/credits/accounts/./me"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			h := newHarness(t)
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"newReward":1}`))
			req.Header.Set("Authorization", "Bearer "+token)

			got := h.do(req)
			if got.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body %s)", got.Code, got.Body.String())
			}
			if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want the gateway's JSON 404", ct)
			}
			if calls := h.downstreamCalls(); len(calls) != 0 {
				t.Errorf("the request reached %v; nothing should leave the gateway", calls)
			}
		})
	}
}

// AI-generated (edited by James Teo).
// The dot-segment guard runs before every route, not only /api/credits.
func TestDotSegmentsAreRejectedOnEveryRoute(t *testing.T) {
	for _, path := range []string{"/api/suppliers/../orders", "/auth/../api/orders", "/../healthz"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t)
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+token)

			got := h.do(req)
			if got.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body %s)", got.Code, got.Body.String())
			}
			if calls := h.downstreamCalls(); len(calls) != 0 {
				t.Errorf("the request reached %v; nothing should leave the gateway", calls)
			}
		})
	}
}

// AI-generated (edited by nigeltzy).
// Covers the two /api/suppliers prefix tests below.

func TestSupplierRouteReachesSupplierServicePrefix(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	// supplier-service mounts its routes under /suppliers.
	if want := "/suppliers/42"; h.suppRec.path != want {
		t.Errorf("supplier-service saw path %q, want %q", h.suppRec.path, want)
	}
}

func TestSupplierCollectionRouteKeepsThePrefix(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	// A bare public prefix maps to AddPrefix alone, with no trailing slash
	// (Route.RewritePath).
	if want := "/suppliers"; h.suppRec.path != want {
		t.Errorf("supplier-service saw path %q, want %q", h.suppRec.path, want)
	}
}

// AI-generated (edited by nigeltzy).

// AI-generated (edited by nigeltzy).
func TestDownstreamCORSHeadersDoNotReachTheBrowser(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "http://localhost:3001")

	got := h.do(req)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	// The gateway emits no CORS headers and strips any a downstream sets, so
	// none may reach the browser.
	if n := len(got.Header().Values("Access-Control-Allow-Origin")); n != 0 {
		t.Errorf("Access-Control-Allow-Origin appears %d times, want none — "+
			"the gateway emits no CORS policy and a callee's must be stripped", n)
	}
}

// AI-generated (edited by nigeltzy).
// Covers the refresh-token cookie tests below.

// authStub records each request and answers /logout with 204 and every other
// path with a token pair.
func authStub(rec *recorder) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.header = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		rec.body = string(body)

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/logout") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"at-value","refreshToken":"rt-value"}`))
	}))
}

// AI-generated (edited by nigeltzy).
func newAuthHarness(t *testing.T) (http.Handler, *recorder) {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)
	rec := &recorder{}
	svc := authStub(rec)
	t.Cleanup(svc.Close)

	router, err := httpapi.NewRouter(config.Downstream{
		User: svc.URL, Supplier: svc.URL, Order: svc.URL, Credit: svc.URL,
	}, auth.NewVerifier(jwks.URL, nil), &fakeRevocations{}, testRefreshTTL)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return router, rec
}

func post(h http.Handler, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func refreshCookieFrom(t *testing.T, res *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: res.Header()}).Cookies() {
		if c.Name == "foc_refresh" {
			return c
		}
	}
	return nil
}

func TestLoginPutsTheRefreshTokenInACookieAndNotTheBody(t *testing.T) {
	// AI-generated (edited by nigeltzy).
	h, _ := newAuthHarness(t)

	res := post(h, "/auth/login", `{"identifier":"a@u.nus.edu","password":"Passw0rd"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}

	// The refresh token must not be in the body, where page scripts can read it.
	if strings.Contains(res.Body.String(), "rt-value") {
		t.Errorf("the refresh token is still in the response body: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "at-value") {
		t.Errorf("the access token should still be in the body, got %s", res.Body.String())
	}

	c := refreshCookieFrom(t, res)
	if c == nil {
		t.Fatal("no foc_refresh cookie was set on login")
	}
	if c.Value != "rt-value" {
		t.Errorf("cookie value = %q, want rt-value", c.Value)
	}
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly — a page script could read the refresh token")
	}
	if !c.Secure {
		t.Error("cookie is not Secure")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict", c.SameSite)
	}
	// /auth, not /auth/refresh: logout needs it too.
	if c.Path != "/auth" {
		t.Errorf("cookie Path = %q, want /auth so logout receives it", c.Path)
	}
	if c.MaxAge != int(testRefreshTTL.Seconds()) {
		t.Errorf("cookie Max-Age = %d, want %d", c.MaxAge, int(testRefreshTTL.Seconds()))
	}
}

func TestRefreshInjectsTheCookieIntoTheBodyUserServiceRequires(t *testing.T) {
	// AI-generated (edited by nigeltzy).
	h, rec := newAuthHarness(t)

	// The browser sends no body — it cannot read the token to send one.
	res := post(h, "/auth/refresh", "", &http.Cookie{Name: "foc_refresh", Value: "rt-from-cookie"})
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	// user-service's RefreshRequest requires refreshToken, so the gateway copies
	// it from the cookie into the body.
	if !strings.Contains(rec.body, `"refreshToken":"rt-from-cookie"`) {
		t.Errorf("user-service received body %q, want it to carry the cookie's token", rec.body)
	}
	// Rotation: the reply's new token must replace the cookie, or the next
	// exchange replays a spent one and user-service revokes the session.
	if c := refreshCookieFrom(t, res); c == nil || c.Value != "rt-value" {
		t.Errorf("refresh did not replace the cookie with the rotated token: %+v", c)
	}
}

func TestLogoutReceivesTheTokenAndClearsTheCookie(t *testing.T) {
	// AI-generated (edited by nigeltzy).
	h, rec := newAuthHarness(t)

	res := post(h, "/auth/logout", "", &http.Cookie{Name: "foc_refresh", Value: "rt-from-cookie"})
	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.Code)
	}
	// LogoutRequest has required: [refreshToken]. Without this injection the
	// call 400s and nothing is ever revoked.
	if !strings.Contains(rec.body, `"refreshToken":"rt-from-cookie"`) {
		t.Errorf("user-service received body %q, want the cookie's token", rec.body)
	}
	c := refreshCookieFrom(t, res)
	if c == nil {
		t.Fatal("logout set no cookie — the old one would survive")
	}
	if c.MaxAge >= 0 {
		t.Errorf("cookie Max-Age = %d, want negative so the browser drops it", c.MaxAge)
	}
}

func TestRegisterCarriesNoCookie(t *testing.T) {
	// AI-generated (edited by nigeltzy).
	h, _ := newAuthHarness(t)

	// The stub returns a token pair here too; register must still set no cookie.
	res := post(h, "/auth/register", `{"email":"a@u.nus.edu","username":"a","password":"Passw0rd"}`)
	if c := refreshCookieFrom(t, res); c != nil {
		t.Errorf("register set a refresh cookie (%+v); it issues no tokens", c)
	}
}

// AI-generated (edited by nigeltzy).
// TestOTPRoutesArePublicPassthroughs checks the two D-043 routes: no token
// needed, rewritten onto user-service's OTP endpoints, body forwarded as sent,
// and no refresh cookie read or set.
func TestOTPRoutesArePublicPassthroughs(t *testing.T) {
	cases := []struct {
		public, upstream, body string
	}{
		{"/auth/resend-otp", "/api/v1/users/resend-otp", `{"email":"a@u.nus.edu"}`},
		{"/auth/verify-otp", "/api/v1/users/verify-otp", `{"email":"a@u.nus.edu","otp":"123456"}`},
	}
	for _, tc := range cases {
		t.Run(tc.public, func(t *testing.T) {
			h, rec := newAuthHarness(t)

			// A stale cookie must not be injected into the body the way refresh does.
			res := post(h, tc.public, tc.body, &http.Cookie{Name: "foc_refresh", Value: "rt-stale"})
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — OTP routes take no token", res.Code)
			}
			if rec.path != tc.upstream {
				t.Errorf("user-service saw path %q, want %q", rec.path, tc.upstream)
			}
			if rec.body != tc.body {
				t.Errorf("user-service saw body %q, want it unchanged: %q", rec.body, tc.body)
			}
			// The stub returns a token pair; the route must still set no cookie.
			if c := refreshCookieFrom(t, res); c != nil {
				t.Errorf("%s set a refresh cookie (%+v); it issues no tokens", tc.public, c)
			}
		})
	}
}

// AI-generated (edited by nigeltzy).
// TestUnknownAPIPathsGetJSON404 checks that a mistyped /api or /auth path gets
// a JSON 404 in the gateway's error shape.
func TestUnknownAPIPathsGetJSON404(t *testing.T) {
	// AI-generated (edited by nigeltzy).
	h := newHarness(t).router

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api"},
		{http.MethodGet, "/api/unknown"},
		{http.MethodPost, "/api/unknown"},
		{http.MethodGet, "/api/supplier/42"},
		{http.MethodGet, "/api/v1/users/login"},
		{http.MethodGet, "/auth"},
		{http.MethodPost, "/auth/typo"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusNotFound || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s %s = %d %q, want 404 application/json", tc.method, tc.path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestOtherUnmatchedPathsGetA404(t *testing.T) {
	h := newHarness(t)

	// The gateway serves no pages: a path outside its routes is a plain 404.
	if got := h.do(httptest.NewRequest(http.MethodGet, "/suppliers", nil)); got.Code != http.StatusNotFound {
		t.Errorf("GET /suppliers = %d, want 404", got.Code)
	}
}

// A revoked token stops at the gateway on every service route, with the same
// message as a bad signature.
func TestRevokedTokenNeverReachesAService(t *testing.T) {
	for _, path := range []string{"/api/users/me", "/api/suppliers/42", "/api/orders/open", "/api/credits/accounts/me"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t)
			h.revocations.revoked = true
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+token)

			got := h.do(req)

			if got.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", got.Code, got.Body.String())
			}
			if body := strings.TrimSpace(got.Body.String()); body != `{"error":"invalid token"}` {
				t.Errorf("body = %s, want the bad-signature message", body)
			}
			if calls := h.downstreamCalls(); len(calls) != 0 {
				t.Errorf("a revoked token reached %v", calls)
			}
		})
	}
}

// The check is asked about the token's own jti, sub and iat.
func TestRevocationCheckGetsTheTokensIDSubjectAndIssueTime(t *testing.T) {
	h := newHarness(t)
	issued := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "ADMIN", issuedAt: issued})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
	}
	f := h.revocations
	if f.calls != 1 || f.tokenID != testJTI || f.subject != "uid-123" || !f.tokenIssued.Equal(issued) {
		t.Errorf("lookup = %d call(s) for jti %q, sub %q, iat %v; want 1 for %q, uid-123, %v",
			f.calls, f.tokenID, f.subject, f.tokenIssued, testJTI, issued)
	}
}

// When the check cannot be made, the gateway fails closed with 503, not 401:
// the frontend answers a 401 by refreshing, which would sign users out.
func TestRevocationCheckUnavailableAnswers503(t *testing.T) {
	h := newHarness(t)
	h.revocations.err = errors.New("read revocation keys: dial tcp: connection refused")
	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/orders/open", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	got := h.do(req)

	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", got.Code, got.Body.String())
	}
	if body := strings.TrimSpace(got.Body.String()); body != `{"error":"cannot check tokens right now"}` {
		t.Errorf("body = %s", body)
	}
	if calls := h.downstreamCalls(); len(calls) != 0 {
		t.Errorf("an unchecked token reached %v", calls)
	}
}

// Only a token that has passed verification reaches Redis.
func TestUnverifiedTokenSkipsRevocationLookup(t *testing.T) {
	tests := []struct {
		name  string
		token func(t *testing.T, iss *issuer) string
	}{
		{"missing token", func(*testing.T, *issuer) string { return "" }},
		{"expired token", func(t *testing.T, iss *issuer) string {
			return iss.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT", expires: time.Now().Add(-time.Hour)})
		}},
		{"token signed by another key", func(t *testing.T, _ *issuer) string {
			return newIssuer(t).mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
		}},
		{"refresh token presented as bearer", func(t *testing.T, iss *issuer) string {
			return iss.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT", typ: "refresh"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
			if token := tt.token(t, h.issuer); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			if got := h.do(req); got.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", got.Code)
			}
			if h.revocations.calls != 0 {
				t.Errorf("revocation lookups = %d, want 0 for a token that failed verification", h.revocations.calls)
			}
		})
	}
}

// A token without a jti or an iat can't be checked, so it is refused before
// any lookup.
func TestTokenWithoutJTIOrIATIsRefused(t *testing.T) {
	for name, opts := range map[string]tokenOpts{
		"no jti": {subject: "uid-123", role: "STUDENT", noJTI: true},
		"no iat": {subject: "uid-123", role: "STUDENT", noIAT: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
			req.Header.Set("Authorization", "Bearer "+h.issuer.mint(t, opts))

			if got := h.do(req); got.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", got.Code)
			}
			if h.revocations.calls != 0 {
				t.Errorf("revocation lookups = %d, want 0", h.revocations.calls)
			}
			if calls := h.downstreamCalls(); len(calls) != 0 {
				t.Errorf("the token reached %v", calls)
			}
		})
	}
}

// /api/orders/* forwards the rest of the path to order-service's root, keeps
// the query, and passes X-Request-Id through unchanged: it is the
// duplicate-post key (D-047), so the gateway must never replace it.
func TestOrderRouteReachesOrderServiceRoot(t *testing.T) {
	cases := []struct {
		method, path, wantPath, wantQuery string
	}{
		{http.MethodPost, "/api/orders", "/", ""},
		{http.MethodGet, "/api/orders/requested?scope=active", "/requested", "scope=active"},
		{http.MethodPost, "/api/orders/6f1c2b9e-0d4a-4c3b-9a8e-1f2d3c4b5a69/accept", "/6f1c2b9e-0d4a-4c3b-9a8e-1f2d3c4b5a69/accept", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			h := newHarness(t)
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Request-Id", "create-form-key-1")

			if got := h.do(req); got.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
			}
			if h.orderRec.path != tc.wantPath || h.orderRec.query != tc.wantQuery {
				t.Errorf("order-service received %q?%q, want %q?%q", h.orderRec.path, h.orderRec.query, tc.wantPath, tc.wantQuery)
			}
			if id := h.orderRec.header.Get("X-Request-Id"); id != "create-form-key-1" {
				t.Errorf("order-service received X-Request-Id = %q, want it unchanged", id)
			}
		})
	}
}

// The public /auth routes carry no access token to check, so a revoked or
// unanswerable check must never stop them: logout in particular has to reach
// user-service with a revoked token.
func TestAuthRoutesSkipTheRevocationCheck(t *testing.T) {
	for _, route := range []string{"/auth/register", "/auth/login", "/auth/logout"} {
		t.Run(route, func(t *testing.T) {
			h := newHarness(t)
			h.revocations.revoked = true
			h.revocations.err = errors.New("read revocation keys: dial tcp: connection refused")
			req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))

			h.do(req)

			if h.userRec.path == "" {
				t.Errorf("%s did not reach user-service", route)
			}
			if h.revocations.calls != 0 {
				t.Errorf("revocation lookups = %d, want 0 on a public auth route", h.revocations.calls)
			}
		})
	}
}

func TestNewRouterNeedsARevocationChecker(t *testing.T) {
	_, err := httpapi.NewRouter(config.Downstream{
		User: "http://user", Supplier: "http://supplier", Order: "http://order", Credit: "http://credit",
	}, auth.NewVerifier("http://jwks", nil), nil, testRefreshTTL)
	if err == nil {
		t.Fatal("NewRouter with a nil checker: expected an error, got nil")
	}
}
