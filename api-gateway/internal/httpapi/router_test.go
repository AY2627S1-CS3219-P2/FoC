// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: End-to-end tests for the router — real RS256 tokens against a real
//   JWKS endpoint, through the middleware and proxy to stub downstreams.
//   2026-09-21: three tests added for the reworked /api/users route.
//   2026-09-22: two added for /api/suppliers reaching supplier-service's
//   /suppliers prefix. The two interim-CORS tests were removed with the
//   policy when the frontend became same-origin.
// Author review: Nigeltzy - Directed and generated as part of implementation of unit tests for the router package. I checked the output and it seems to be reasonable and typical unit tests for the router package.

package httpapi_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
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

// testRefreshTTL stands in for config's REFRESH_TOKEN_TTL (168h, as in
// .env.example).
const testRefreshTTL = 168 * time.Hour

// recorder captures what a downstream service actually received.
type recorder struct {
	path   string
	header http.Header
	body   string
}

func stubService(rec *recorder) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.header = r.Header.Clone()
		// Sets a CORS header, as a real callee may, so tests can check the proxy
		// strips it.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

// AI-generated (edited by nigeltzy).
type harness struct {
	router  http.Handler
	issuer  *issuer
	userRec *recorder
	suppRec *recorder
}

// AI-generated (edited by nigeltzy).
func newHarness(t *testing.T) *harness {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)

	userRec, suppRec := &recorder{}, &recorder{}
	userSvc := stubService(userRec)
	t.Cleanup(userSvc.Close)
	suppSvc := stubService(suppRec)
	t.Cleanup(suppSvc.Close)

	verifier := auth.NewVerifier(jwks.URL, nil)
	router, err := httpapi.NewRouter(config.Downstream{
		User:     userSvc.URL,
		Supplier: suppSvc.URL,
		Order:    suppSvc.URL,
		Credit:   suppSvc.URL,
	}, verifier, testRefreshTTL)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	return &harness{
		router:  router,
		issuer:  iss,
		userRec: userRec,
		suppRec: suppRec,
	}
}

func (h *harness) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
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

func TestOtherServicesNeverReceiveTheBearerToken(t *testing.T) {
	h := newHarness(t)

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if got := h.do(req); got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	// Only /api/users keeps Authorization; the other /api/ service routes strip it.
	if authz := h.suppRec.header.Get("Authorization"); authz != "" {
		t.Errorf("supplier-service received Authorization = %q, want it stripped", authz)
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
	}, auth.NewVerifier(jwks.URL, nil), testRefreshTTL)
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
