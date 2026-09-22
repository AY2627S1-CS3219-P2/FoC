// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: End-to-end tests for the router — real RS256 tokens against a real
//   JWKS endpoint, through the middleware and proxy to stub downstreams.
//   2026-09-21: three tests added for the reworked /api/users route.
//   2026-09-22: two added for /api/suppliers reaching supplier-service's
//   /suppliers prefix. The two interim-CORS tests were removed with the
//   policy when the frontend became same-origin.
// Author review: PENDING — <reviewer to complete>

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
	"os"
	"path/filepath"
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

// testRefreshTTL stands in for config's REFRESH_TOKEN_TTL, which must match
// user-service's JWT_REFRESH_TOKEN_TTL (168h in .env.example).
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
		// supplier-service runs its own permissive cors.Handler, from when the
		// browser reached it directly. The stub does the same so the proxy is
		// tested against what a real callee actually sends back.
		w.Header().Set("Access-Control-Allow-Origin", "*")
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
	}, verifier, testRefreshTTL, "")
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

// AI-generated (edited by <name>).
func TestDownstreamCORSHeadersDoNotReachTheBrowser(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/42", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "http://localhost:3001")

	got := h.do(req)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.Code)
	}
	// NONE. The browser is same-origin with the gateway now, so there is no
	// CORS policy here to emit and nothing legitimate for the browser to
	// check. supplier-service still sets its own header, and letting a
	// callee's CORS opinion reach a browser is how the duplicate-header bug
	// happened before the frontend was proxied; the strip stays.
	if n := len(got.Header().Values("Access-Control-Allow-Origin")); n != 0 {
		t.Errorf("Access-Control-Allow-Origin appears %d times, want none — "+
			"the gateway emits no CORS policy and a callee's must be stripped", n)
	}
}

// AI-generated (edited by <name>).
// The refresh token's cookie. These are the tests that matter for the
// credential: if the token leaks into a response body, or the cookie loses
// HttpOnly, an XSS gets a seven-day credential instead of a fifteen-minute one.

// authStub answers the four auth routes the way user-service does: login and
// refresh return an AuthResponse pair, logout returns 204.
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

func newAuthHarness(t *testing.T) (http.Handler, *recorder, func()) {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	rec := &recorder{}
	svc := authStub(rec)

	router, err := httpapi.NewRouter(config.Downstream{
		User: svc.URL, Supplier: svc.URL, Order: svc.URL, Credit: svc.URL,
	}, auth.NewVerifier(jwks.URL, nil), testRefreshTTL, "")
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return router, rec, func() { jwks.Close(); svc.Close() }
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
	h, _, done := newAuthHarness(t)
	defer done()

	res := post(h, "/auth/login", `{"identifier":"a@u.nus.edu","password":"Passw0rd"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}

	// THE point of the whole change: a page script must not be able to read
	// the durable credential.
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
	h, rec, done := newAuthHarness(t)
	defer done()

	// The browser sends no body — it cannot read the token to send one.
	res := post(h, "/auth/refresh", "", &http.Cookie{Name: "foc_refresh", Value: "rt-from-cookie"})
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	// user-service's RefreshRequest requires the field; its contract did not
	// change, so the gateway has to put it back.
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
	h, rec, done := newAuthHarness(t)
	defer done()

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
	h, _, done := newAuthHarness(t)
	defer done()

	// user-service answers 201 with no body; there is no token to translate.
	res := post(h, "/auth/register", `{"email":"a@u.nus.edu","username":"a","password":"Passw0rd"}`)
	if c := refreshCookieFrom(t, res); c != nil {
		t.Errorf("register set a refresh cookie (%+v); it issues no tokens", c)
	}
}

// AI-generated (edited by <name>).
// Serving the built frontend is what makes the browser same-origin with the
// API (D-033), so these check it does not shadow the API.

func newStaticHarness(t *testing.T) (http.Handler, string, func()) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>APP"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	rec := &recorder{}
	svc := stubService(rec)
	router, err := httpapi.NewRouter(config.Downstream{
		User: svc.URL, Supplier: svc.URL, Order: svc.URL, Credit: svc.URL,
	}, auth.NewVerifier(jwks.URL, nil), testRefreshTTL, dir)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return router, dir, func() { jwks.Close(); svc.Close() }
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestStaticFilesAndSPAFallback(t *testing.T) {
	h, _, done := newStaticHarness(t)
	defer done()

	if got := get(h, "/app.js"); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), "console.log") {
		t.Errorf("GET /app.js = %d %q, want the asset", got.Code, got.Body.String())
	}
	// A client-side route is not a file. 404ing it would break every deep
	// link into the app.
	if got := get(h, "/suppliers"); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), "APP") {
		t.Errorf("GET /suppliers = %d %q, want index.html", got.Code, got.Body.String())
	}
}

func TestStaticServingDoesNotShadowTheAPI(t *testing.T) {
	h, _, done := newStaticHarness(t)
	defer done()

	// The catch-all is registered last, so an API path with no token must
	// still get the API's 401 -- not a page with status 200, which would make
	// every unauthenticated call look successful to the client.
	if got := get(h, "/api/suppliers/42"); got.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/suppliers/42 = %d, want 401 from the API", got.Code)
	}
	if got := get(h, "/healthz"); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), `"status":"ok"`) {
		t.Errorf("GET /healthz = %d %q, want the gateway's own", got.Code, got.Body.String())
	}
}

func TestNoStaticDirLeavesTheDefault404(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	// `go run ./cmd/api` sets no STATIC_DIR: Vite serves the app and proxies
	// here, so the gateway must not invent a page.
	if got := h.do(httptest.NewRequest(http.MethodGet, "/suppliers", nil)); got.Code != http.StatusNotFound {
		t.Errorf("GET /suppliers = %d, want 404 when no static dir is configured", got.Code)
	}
}
