// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests for the request id, the access line, the cause lines of the
//   gateway's own 5xx answers and panic recovery, read back from the JSON
//   lines the router logs.
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/config"
	"foc/api-gateway/internal/httpapi"
)

// The messages of the lines these tests look for.
const (
	msgAccess          = "http request"
	msgProxyCause      = "cannot proxy the request; answering 502"
	msgKeysCause       = "cannot load user-service's signing keys; refusing the request"
	msgRevocationCause = "cannot read the revocation keys in Redis; refusing the request"
	msgPanic           = "panic serving request"
	msgCookieNotCopied = "refresh cookie not copied: the request body is not a JSON object"
	msgBrowserError    = "browser error"
)

// uuidV4 matches a version 4 UUID in its canonical lower-case form.
var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// logBuffer holds the JSON lines a router logs, for a test to read back.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// logger returns a logger writing to b in the gateway's own format.
func (b *logBuffer) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(b, nil))
}

// String returns everything logged so far, as written.
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// lines decodes every line logged so far.
func (b *logBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, raw := range strings.Split(b.String(), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line is not one JSON object: %q: %v", raw, err)
		}
		out = append(out, line)
	}
	return out
}

// withMsg returns the lines logged with msg.
func (b *logBuffer) withMsg(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range b.lines(t) {
		if line["msg"] == msg {
			out = append(out, line)
		}
	}
	return out
}

// only returns the one line logged with msg, failing the test unless there is
// exactly one.
func (b *logBuffer) only(t *testing.T, msg string) map[string]any {
	t.Helper()
	lines := b.withMsg(t, msg)
	if len(lines) != 1 {
		t.Fatalf("%d lines with msg %q, want exactly 1; the log:\n%s", len(lines), msg, b.String())
	}
	return lines[0]
}

// gatewaySetup is what a test changes about the router newLoggedGateway
// builds.
type gatewaySetup struct {
	// upstream is every service's base URL; "" starts a stub that answers 200.
	upstream string
	// jwks is the signing-key endpoint; "" serves the issuer's own keys.
	jwks string
	// revocations defaults to a checker that revokes nothing.
	revocations httpapi.RevocationChecker
}

// loggedGateway is a router built from a gatewaySetup, and what it logged.
type loggedGateway struct {
	router http.Handler
	issuer *issuer
	logs   *logBuffer
}

func newLoggedGateway(t *testing.T, s gatewaySetup) *loggedGateway {
	t.Helper()
	iss := newIssuer(t)
	if s.jwks == "" {
		jwks := httptest.NewServer(iss.jwksHandler())
		t.Cleanup(jwks.Close)
		s.jwks = jwks.URL
	}
	if s.upstream == "" {
		svc := stubService(&recorder{})
		t.Cleanup(svc.Close)
		s.upstream = svc.URL
	}
	if s.revocations == nil {
		s.revocations = &fakeRevocations{}
	}
	logs := &logBuffer{}
	router, err := httpapi.NewRouter(config.Downstream{
		User: s.upstream, Supplier: s.upstream, Order: s.upstream, Credit: s.upstream, Chat: s.upstream,
	}, auth.NewVerifier(s.jwks, nil), s.revocations, testRefreshTTL, logs.logger())
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return &loggedGateway{router: router, issuer: iss, logs: logs}
}

// get sends GET path on ctx, with a valid STUDENT token for uid-123.
func (g *loggedGateway) get(t *testing.T, ctx context.Context, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+g.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))
	w := httptest.NewRecorder()
	g.router.ServeHTTP(w, req)
	return w
}

// requestContext is the context of a request whose client is still there, or,
// with hungUp, of one whose client went away: net/http cancels that context.
func requestContext(hungUp bool) context.Context {
	if !hungUp {
		return context.Background()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// unreachable returns the base URL of a service that has stopped.
func unreachable(t *testing.T) string {
	t.Helper()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	return down.URL
}

// alwaysAnswers returns the base URL of a service answering every request
// with status.
func alwaysAnswers(t *testing.T, status int) string {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(svc.Close)
	return svc.URL
}

// hostOf returns the host:port of a base URL.
func hostOf(t *testing.T, baseURL string) string {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parsing %q: %v", baseURL, err)
	}
	return u.Host
}

// recovered runs f and returns what it panicked with, or nil.
func recovered(f func()) (p any) {
	defer func() { p = recover() }()
	f()
	return nil
}

// A request without an X-Request-Id gets one random UUID: forwarded to the
// service, returned once in the response and logged as its request_id.
func TestMintedRequestIDReachesTheServiceAndTheResponseOnce(t *testing.T) {
	cases := []struct {
		name, method, path string
		withToken          bool
		// sent is the X-Request-Id values the client sends.
		sent []string
		// callee is the stub the request reaches; nil when the gateway answers.
		callee func(*harness) *recorder
	}{
		{"service route", http.MethodGet, "/api/orders/open", true, nil, func(h *harness) *recorder { return h.orderRec }},
		{"auth route", http.MethodPost, "/auth/register", false, nil, func(h *harness) *recorder { return h.userRec }},
		{"an empty X-Request-Id counts as none", http.MethodGet, "/api/orders/open", true, []string{""}, func(h *harness) *recorder { return h.orderRec }},
		{"the gateway's own route", http.MethodGet, "/healthz", false, nil, nil},
		{"refused at the gateway", http.MethodGet, "/api/orders/open", false, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.withToken {
				req.Header.Set("Authorization", "Bearer "+h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))
			}
			for _, v := range tc.sent {
				req.Header.Add("X-Request-Id", v)
			}

			got := h.do(req)

			ids := got.Header().Values("X-Request-Id")
			if len(ids) != 1 || !uuidV4.MatchString(ids[0]) {
				t.Fatalf("response X-Request-Id = %q, want one version 4 UUID", ids)
			}
			if tc.callee != nil {
				if sent := tc.callee(h).header.Values("X-Request-Id"); !slices.Equal(sent, ids) {
					t.Errorf("the service received X-Request-Id %q, want %q", sent, ids)
				}
			}
			if line := h.logs.only(t, msgAccess); line["request_id"] != ids[0] {
				t.Errorf("access line request_id = %v, want %q", line["request_id"], ids[0])
			}
		})
	}
}

func TestEachRequestGetsItsOwnID(t *testing.T) {
	h := newHarness(t)

	first := h.do(httptest.NewRequest(http.MethodGet, "/healthz", nil)).Header().Get("X-Request-Id")
	second := h.do(httptest.NewRequest(http.MethodGet, "/healthz", nil)).Header().Get("X-Request-Id")

	if first == "" || first == second {
		t.Errorf("two requests got ids %q and %q, want two different ones", first, second)
	}
}

// A client's X-Request-Id reaches the service exactly as sent and comes back
// once, whatever the service puts on its own response: order-service keeps it
// as the errand-create duplicate key, so the gateway never checks or replaces
// it.
func TestClientRequestIDPassesThroughAndIsEchoedOnce(t *testing.T) {
	const formKey = "create-form-key-1"
	// Longer than order-service keeps; the gateway forwards it all the same.
	long := strings.Repeat("k", 300)
	cases := []struct {
		name, id string
		// echo is the X-Request-Id the service answers with; "" sets none.
		echo func(received string) string
	}{
		{"service echoes it", formKey, func(id string) string { return id }},
		{"service answers with an id of its own", formKey, func(string) string { return "minted-by-the-service" }},
		{"service sets none", formKey, func(string) string { return "" }},
		{"an id a service would not keep", long, func(string) string { return "minted-by-the-service" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan []string, 1)
			svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Values("X-Request-Id")
				if v := tc.echo(r.Header.Get("X-Request-Id")); v != "" {
					w.Header().Set("X-Request-Id", v)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			t.Cleanup(svc.Close)
			g := newLoggedGateway(t, gatewaySetup{upstream: svc.URL})

			req := httptest.NewRequest(http.MethodPost, "/api/orders", strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+g.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))
			req.Header.Set("X-Request-Id", tc.id)
			got := httptest.NewRecorder()
			g.router.ServeHTTP(got, req)

			want := []string{tc.id}
			select {
			case sent := <-received:
				if !slices.Equal(sent, want) {
					t.Errorf("the service received X-Request-Id %q, want %q unchanged", sent, want)
				}
			default:
				t.Fatalf("the request never reached the service (status %d)", got.Code)
			}
			if ids := got.Header().Values("X-Request-Id"); !slices.Equal(ids, want) {
				t.Errorf("response X-Request-Id = %q, want %q once", ids, want)
			}
			if line := g.logs.only(t, msgAccess); line["request_id"] != tc.id {
				t.Errorf("access line request_id = %v, want the client's id", line["request_id"])
			}
		})
	}
}

// When the client sends several X-Request-Id values, the first non-empty one
// is the request's id, and it alone reaches the service, so the service logs
// the same id the gateway logs and returns.
func TestFirstNonEmptyClientRequestIDIsForwardedAlone(t *testing.T) {
	cases := []struct {
		name string
		// sent is the X-Request-Id values the client sends, in order.
		sent []string
		want string
	}{
		{"an empty value ahead of the id", []string{"", "abc"}, "abc"},
		{"two ids", []string{"abc", "def"}, "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := httptest.NewRequest(http.MethodGet, "/api/orders/open", nil)
			req.Header.Set("Authorization", "Bearer "+h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))
			for _, v := range tc.sent {
				req.Header.Add("X-Request-Id", v)
			}

			got := h.do(req)

			if got.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", got.Code, got.Body.String())
			}
			want := []string{tc.want}
			if sent := h.orderRec.header.Values("X-Request-Id"); !slices.Equal(sent, want) {
				t.Errorf("order-service received X-Request-Id %q, want %q", sent, want)
			}
			if ids := got.Header().Values("X-Request-Id"); !slices.Equal(ids, want) {
				t.Errorf("response X-Request-Id = %q, want %q once", ids, want)
			}
			if line := h.logs.only(t, msgAccess); line["request_id"] != tc.want {
				t.Errorf("access line request_id = %v, want %q", line["request_id"], tc.want)
			}
		})
	}
}

// Every request gets one access line once it has finished: WARN when the
// client hung up, ERROR for a 5xx and INFO otherwise. A service's own 5xx is
// that service's to explain, so the gateway logs nothing more for it.
func TestAccessLineLevel(t *testing.T) {
	cases := []struct {
		name       string
		upstream   string
		path       string
		withToken  bool
		hungUp     bool
		wantStatus int
		wantLevel  string
		// wantUser is the line's user_id; "" means it has none.
		wantUser string
		// wantLines counts every line the request logs.
		wantLines int
	}{
		{"2xx from the gateway", "", "/healthz", false, false, http.StatusOK, "INFO", "", 1},
		{"2xx from a service", "", "/api/orders/requested?scope=active", true, false, http.StatusOK, "INFO", "uid-123", 1},
		{"4xx from the gateway", "", "/api/orders/open", false, false, http.StatusUnauthorized, "INFO", "", 1},
		{"5xx from a service", alwaysAnswers(t, http.StatusInternalServerError), "/api/orders/open", true, false, http.StatusInternalServerError, "ERROR", "uid-123", 1},
		{"502 for a service that cannot be reached", unreachable(t), "/api/orders/open", true, false, http.StatusBadGateway, "ERROR", "uid-123", 2},
		{"client hung up", "", "/api/orders/open", true, true, http.StatusBadGateway, "WARN", "uid-123", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newLoggedGateway(t, gatewaySetup{upstream: tc.upstream})
			req := httptest.NewRequestWithContext(requestContext(tc.hungUp), http.MethodGet, tc.path, nil)
			if tc.withToken {
				req.Header.Set("Authorization", "Bearer "+g.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))
			}
			got := httptest.NewRecorder()
			g.router.ServeHTTP(got, req)

			if got.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", got.Code, tc.wantStatus)
			}
			line := g.logs.only(t, msgAccess)
			wantPath, _, _ := strings.Cut(tc.path, "?")
			for key, want := range map[string]any{
				"level":      tc.wantLevel,
				"request_id": got.Header().Get("X-Request-Id"),
				"method":     http.MethodGet,
				"path":       wantPath,
				"status":     float64(tc.wantStatus),
			} {
				if line[key] != want {
					t.Errorf("access line %s = %#v, want %#v", key, line[key], want)
				}
			}
			if ms, ok := line["duration_ms"].(float64); !ok || ms < 0 {
				t.Errorf("access line duration_ms = %#v, want a duration", line["duration_ms"])
			}
			if user, ok := line["user_id"]; (tc.wantUser == "" && ok) || (tc.wantUser != "" && user != tc.wantUser) {
				t.Errorf("access line user_id = %#v, want %q", user, tc.wantUser)
			}
			if n := len(g.logs.lines(t)); n != tc.wantLines {
				t.Errorf("%d lines logged, want %d:\n%s", n, tc.wantLines, g.logs.String())
			}
		})
	}
}

// The 502 for a request the gateway could not proxy logs its cause once, with
// the request's id: ERROR, or WARN when the client hung up.
func TestProxyErrorCauseLine(t *testing.T) {
	cases := []struct {
		name      string
		upstream  string
		hungUp    bool
		wantLevel string
	}{
		{"service cannot be reached", unreachable(t), false, "ERROR"},
		{"client hung up", alwaysAnswers(t, http.StatusOK), true, "WARN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newLoggedGateway(t, gatewaySetup{upstream: tc.upstream})

			got := g.get(t, requestContext(tc.hungUp), "/api/orders/open")

			if got.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502", got.Code)
			}
			line := g.logs.only(t, msgProxyCause)
			if line["level"] != tc.wantLevel {
				t.Errorf("level = %v, want %s", line["level"], tc.wantLevel)
			}
			if id := got.Header().Get("X-Request-Id"); id == "" || line["request_id"] != id {
				t.Errorf("request_id = %v, want the response's X-Request-Id %q", line["request_id"], id)
			}
			if want := hostOf(t, tc.upstream); line["upstream"] != want {
				t.Errorf("upstream = %v, want %q", line["upstream"], want)
			}
			if cause, _ := line["err"].(string); cause == "" {
				t.Error("the line does not say what went wrong")
			}
		})
	}
}

// A 503 from the token checks logs its cause once, with the request's id:
// ERROR, or WARN when the client hung up. A lookup that runs out of its own
// time is the gateway's failure, so it stays an ERROR.
func TestTokenCheckCauseLines(t *testing.T) {
	jwksDown := alwaysAnswers(t, http.StatusInternalServerError)
	unavailable := func(err error) httpapi.RevocationChecker { return &fakeRevocations{err: err} }

	cases := []struct {
		name      string
		setup     gatewaySetup
		hungUp    bool
		wantMsg   string
		wantLevel string
		// wantUser is the line's user_id; "" means it has none.
		wantUser string
	}{
		{"signing keys unavailable", gatewaySetup{jwks: jwksDown}, false, msgKeysCause, "ERROR", ""},
		{"client hung up while the keys were unavailable", gatewaySetup{jwks: jwksDown}, true, msgKeysCause, "WARN", ""},
		{"Redis cannot be reached", gatewaySetup{revocations: unavailable(errors.New("read revocation keys: dial tcp: connection refused"))}, false, msgRevocationCause, "ERROR", "uid-123"},
		{"the lookup ran out of time", gatewaySetup{revocations: unavailable(fmt.Errorf("read revocation keys: %w", context.DeadlineExceeded))}, false, msgRevocationCause, "ERROR", "uid-123"},
		{"client hung up during the lookup", gatewaySetup{revocations: unavailable(fmt.Errorf("read revocation keys: %w", context.Canceled))}, true, msgRevocationCause, "WARN", "uid-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newLoggedGateway(t, tc.setup)

			got := g.get(t, requestContext(tc.hungUp), "/api/orders/open")

			if got.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", got.Code)
			}
			line := g.logs.only(t, tc.wantMsg)
			if line["level"] != tc.wantLevel {
				t.Errorf("level = %v, want %s", line["level"], tc.wantLevel)
			}
			if id := got.Header().Get("X-Request-Id"); id == "" || line["request_id"] != id {
				t.Errorf("request_id = %v, want the response's X-Request-Id %q", line["request_id"], id)
			}
			if user, ok := line["user_id"]; (tc.wantUser == "" && ok) || (tc.wantUser != "" && user != tc.wantUser) {
				t.Errorf("user_id = %#v, want %q", user, tc.wantUser)
			}
			if cause, _ := line["err"].(string); cause == "" {
				t.Error("the line does not say what went wrong")
			}
			wantAccess := "ERROR"
			if tc.hungUp {
				wantAccess = "WARN"
			}
			if access := g.logs.only(t, msgAccess); access["level"] != wantAccess {
				t.Errorf("access line level = %v, want %s", access["level"], wantAccess)
			}
		})
	}
}

// panickingRevocations stands in for a revocation checker with a bug in it.
type panickingRevocations struct{}

func (panickingRevocations) Revoked(context.Context, string, string, time.Time) (bool, error) {
	panic("revocation checker bug")
}

// A panic becomes a JSON 500 and one ERROR line with the request's id and the
// whole stack in a single field, and the access line records the 500.
func TestPanicIsRecoveredIntoOneErrorLine(t *testing.T) {
	g := newLoggedGateway(t, gatewaySetup{revocations: panickingRevocations{}})

	got := g.get(t, context.Background(), "/api/orders/open")

	if got.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", got.Code)
	}
	if body := strings.TrimSpace(got.Body.String()); body != `{"error":"internal error"}` {
		t.Errorf("body = %s, want the gateway's JSON error", body)
	}
	line := g.logs.only(t, msgPanic)
	if line["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", line["level"])
	}
	if id := got.Header().Get("X-Request-Id"); id == "" || line["request_id"] != id {
		t.Errorf("request_id = %v, want the response's X-Request-Id %q", line["request_id"], id)
	}
	if line["panic"] != "revocation checker bug" {
		t.Errorf("panic = %v, want the panic's value", line["panic"])
	}
	if stack, _ := line["stack"].(string); !strings.Contains(stack, "panickingRevocations") || !strings.Contains(stack, "\n") {
		t.Errorf("stack = %q, want the whole trace, down to the panicking call", stack)
	}
	access := g.logs.only(t, msgAccess)
	if access["level"] != "ERROR" || access["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("access line level %v, status %v; want ERROR and 500", access["level"], access["status"])
	}
}

// When a service's body breaks off mid-response, ReverseProxy aborts the
// response by panicking with http.ErrAbortHandler. That panic must reach
// net/http unrecovered, the request must still get its access line, and
// ReverseProxy's own message must come out as a JSON line too.
func TestAbortedResponseIsStillLogged(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("only part of the body"))
		w.(http.Flusher).Flush()
		// Drops the connection with the body unfinished.
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(svc.Close)
	g := newLoggedGateway(t, gatewaySetup{upstream: svc.URL})
	// ReverseProxy aborts this way only under an http.Server, which it
	// recognises by this context value.
	ctx := context.WithValue(context.Background(), http.ServerContextKey, &http.Server{})

	if p := recovered(func() { g.get(t, ctx, "/api/orders/open") }); p != http.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler passed on to net/http", p)
	}

	access := g.logs.only(t, msgAccess)
	if id, _ := access["request_id"].(string); !uuidV4.MatchString(id) {
		t.Errorf("access line request_id = %q, want the request's id", id)
	}
	// The status had gone out before the body broke off.
	if access["status"] != float64(http.StatusOK) {
		t.Errorf("access line status = %v, want 200", access["status"])
	}
	if lines := g.logs.withMsg(t, msgPanic); len(lines) != 0 {
		t.Errorf("an abort was logged as a panic: %v", lines)
	}
	var bridged bool
	for _, line := range g.logs.lines(t) {
		msg, _ := line["msg"].(string)
		bridged = bridged || (strings.Contains(msg, "ReverseProxy read error during body copy") && line["level"] == "ERROR")
	}
	if !bridged {
		t.Errorf("ReverseProxy's own message is not an ERROR line in the log:\n%s", g.logs.String())
	}
}

// A chat WebSocket upgrade on a real connection: ReverseProxy can switch
// protocols only if the access log's writer lets it hijack the connection.
// Bytes then flow both ways, and the access line, written once the connection
// closes, records the 101 that went out on the hijacked connection.
func TestChatWebSocketUpgradeSwitchesProtocolsAndIsLogged(t *testing.T) {
	const ticket = "test-ticket-xyz"
	forwardedID := make(chan string, 1)
	// Stands in for chat-service, which redeems the ticket itself.
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" || r.URL.Query().Get("ticket") != ticket || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "not a chat upgrade", http.StatusBadRequest)
			return
		}
		forwardedID <- r.Header.Get("X-Request-Id")
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()
		// Echoes a request id, as a service may; the gateway drops it.
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nX-Request-Id: chat-service-copy\r\n\r\n")
		_ = rw.Flush()
		if msg, err := rw.ReadString('\n'); err == nil {
			_, _ = rw.WriteString("chat-service got " + msg)
			_ = rw.Flush()
		}
		// Holds the connection open until the gateway closes it.
		_, _ = io.Copy(io.Discard, rw)
	}))
	t.Cleanup(chat.Close)
	g := newLoggedGateway(t, gatewaySetup{upstream: chat.URL})
	// Closed once the router returns, by which time the access line is
	// written; server.Close does not wait for a hijacked connection.
	served := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(served)
		g.router.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	reader := bufio.NewReader(conn)
	upgrade, err := http.NewRequest(http.MethodGet, server.URL+"/api/chat/ws?ticket="+ticket, nil)
	if err != nil {
		t.Fatal(err)
	}
	upgrade.Header.Set("Connection", "Upgrade")
	upgrade.Header.Set("Upgrade", "websocket")
	upgrade.Header.Set("Sec-WebSocket-Version", "13")
	upgrade.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if err := upgrade.Write(conn); err != nil {
		t.Fatalf("send the upgrade: %v", err)
	}
	res, err := http.ReadResponse(reader, upgrade)
	if err != nil {
		t.Fatalf("read the upgrade's response: %v", err)
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101; the log:\n%s", res.StatusCode, g.logs.String())
	}
	ids := res.Header.Values("X-Request-Id")
	if len(ids) != 1 || !uuidV4.MatchString(ids[0]) {
		t.Fatalf("X-Request-Id on the 101 = %q, want the one id the gateway minted", ids)
	}
	if got := <-forwardedID; got != ids[0] {
		t.Errorf("chat-service got X-Request-Id %q, want %q", got, ids[0])
	}

	if _, err := io.WriteString(conn, "hello from the browser\n"); err != nil {
		t.Fatalf("write on the switched connection: %v", err)
	}
	if got, err := reader.ReadString('\n'); err != nil || got != "chat-service got hello from the browser\n" {
		t.Fatalf("read on the switched connection = %q, %v; want chat-service's reply", got, err)
	}

	_ = conn.Close()
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatalf("the gateway still holds the connection after the client closed it; the log:\n%s", g.logs.String())
	}
	access := g.logs.only(t, msgAccess)
	if access["path"] != "/api/chat/ws" || access["status"] != float64(http.StatusSwitchingProtocols) ||
		access["level"] != "INFO" || access["request_id"] != ids[0] {
		t.Errorf("access line = %v, want path /api/chat/ws, status 101, level INFO and request_id %q", access, ids[0])
	}
	if strings.Contains(g.logs.String(), ticket) {
		t.Errorf("the log carries the ticket:\n%s", g.logs.String())
	}
}

// A streamed response, such as a chat image, reaches the client part by part:
// ReverseProxy flushes each part through the access log's writer.
func TestStreamedResponseIsFlushedPartByPart(t *testing.T) {
	release := make(chan struct{})
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "first part\n")
		w.(http.Flusher).Flush()
		// The rest waits until the client has read the first part.
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "second part\n")
	}))
	t.Cleanup(chat.Close)
	g := newLoggedGateway(t, gatewaySetup{upstream: chat.URL})
	server := httptest.NewServer(g.router)
	t.Cleanup(server.Close)

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(server.URL + "/api/chat/media/ee337867-10c6-4539-8256-bad7a2e95e8b")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	if got, err := reader.ReadString('\n'); err != nil || got != "first part\n" {
		t.Fatalf("first part = %q, %v; want it before the service has finished", got, err)
	}
	close(release)
	if rest, err := io.ReadAll(reader); err != nil || string(rest) != "second part\n" {
		t.Fatalf("rest = %q, %v; want the second part", rest, err)
	}
}

// No line carries the access token, the refresh cookie, a query string or a
// request body.
func TestLogLinesLeaveOutTokensCookiesQueriesAndBodies(t *testing.T) {
	const (
		cookie = "rt-cookie-value"
		body   = "body-text-that-is-not-json"
		query  = "scope=active&q=private-search"
	)
	cases := []struct {
		name, method, path, body string
		revocationErr            error
		// wantMsg is the line the request must log.
		wantMsg string
	}{
		{"a proxied request", http.MethodGet, "/api/orders/requested?" + query, "", nil, msgAccess},
		{"a 503 with its cause", http.MethodGet, "/api/orders/requested?" + query, "", errors.New("read revocation keys: dial tcp: connection refused"), msgRevocationCause},
		{"a refresh whose body is not JSON", http.MethodPost, "/auth/refresh?" + query, body, nil, msgCookieNotCopied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.revocations.err = tc.revocationErr
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.AddCookie(&http.Cookie{Name: "foc_refresh", Value: cookie})

			got := h.do(req)

			if line := h.logs.only(t, tc.wantMsg); line["request_id"] != got.Header().Get("X-Request-Id") {
				t.Errorf("request_id = %v, want the response's X-Request-Id", line["request_id"])
			}
			logged := h.logs.String()
			for _, secret := range []string{token, cookie, body, "scope=active", "private-search"} {
				if strings.Contains(logged, secret) {
					t.Errorf("the log carries %q:\n%s", secret, logged)
				}
			}
		})
	}
}

func TestNewRouterNeedsALogger(t *testing.T) {
	_, err := httpapi.NewRouter(config.Downstream{
		User: "http://user", Supplier: "http://supplier", Order: "http://order", Credit: "http://credit", Chat: "http://chat",
	}, auth.NewVerifier("http://jwks", nil), &fakeRevocations{}, testRefreshTTL, nil)
	if err == nil {
		t.Fatal("NewRouter with a nil logger: expected an error, got nil")
	}
}
