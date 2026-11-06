// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests for the access line's fields and levels, the level a 500's
//   cause is logged at when the client hung up, the panic recoverer, and an
//   oversized body still dropping its connection behind the access log.
// Author review: James Teo — reviewed and verified.

package handlers_test

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
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const validLogin = `{"identifier":"student","password":"ValidPass1"}`

func TestAccessLineLevels(t *testing.T) {
	healthy := handlers.SystemDependencies{HealthCheck: func(context.Context) error { return nil }}
	tests := map[string]struct {
		deps       routes.Dependencies
		method     string
		target     string
		body       string
		ctx        func(*testing.T) context.Context
		wantStatus int
		wantLevel  string
		wantPath   string
	}{
		"a success is info, and its path has no query string": {
			deps: routes.Dependencies{System: healthy}, method: http.MethodGet, target: "/api/v1/health?probe=compose",
			wantStatus: http.StatusOK, wantLevel: "INFO", wantPath: "/api/v1/health",
		},
		"a client error is info": {
			method: http.MethodPost, target: "/api/v1/users/login", body: `{`,
			wantStatus: http.StatusBadRequest, wantLevel: "INFO", wantPath: "/api/v1/users/login",
		},
		"an unknown route is info": {
			method: http.MethodGet, target: "/api/v1/nowhere",
			wantStatus: http.StatusNotFound, wantLevel: "INFO", wantPath: "/api/v1/nowhere",
		},
		"a server error is an error": {
			deps:   routes.Dependencies{Auth: handlers.AuthDependencies{LoginService: &fakeLoginService{err: errors.New("database unavailable")}}},
			method: http.MethodPost, target: "/api/v1/users/login", body: validLogin,
			wantStatus: http.StatusInternalServerError, wantLevel: "ERROR", wantPath: "/api/v1/users/login",
		},
		"an unhealthy dependency is an error": {
			deps:   routes.Dependencies{System: handlers.SystemDependencies{HealthCheck: func(context.Context) error { return errors.New("redis unavailable") }}},
			method: http.MethodGet, target: "/api/v1/health",
			wantStatus: http.StatusServiceUnavailable, wantLevel: "ERROR", wantPath: "/api/v1/health",
		},
		"a client that hung up is a warning instead of an error": {
			deps:   routes.Dependencies{System: handlers.SystemDependencies{HealthCheck: func(ctx context.Context) error { return ctx.Err() }}},
			method: http.MethodGet, target: "/api/v1/health", ctx: hungUp,
			wantStatus: http.StatusServiceUnavailable, wantLevel: "WARN", wantPath: "/api/v1/health",
		},
		"a client that hung up is a warning whatever the status": {
			deps: routes.Dependencies{System: healthy}, method: http.MethodGet, target: "/api/v1/health", ctx: hungUp,
			wantStatus: http.StatusOK, wantLevel: "WARN", wantPath: "/api/v1/health",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			tt.deps.Logger = jsonLogger(&logs)
			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			if tt.ctx != nil {
				req = req.WithContext(tt.ctx(t))
			}
			req.Header.Set("X-Request-Id", "req-access")
			res := httptest.NewRecorder()

			newTestRouter(tt.deps).ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tt.wantStatus)
			}
			line := onlyLine(t, logLines(t, &logs), "http request")
			want := map[string]any{
				"level": tt.wantLevel, "request_id": "req-access", "method": tt.method,
				"path": tt.wantPath, "status": float64(tt.wantStatus),
			}
			for key, value := range want {
				if line[key] != value {
					t.Errorf("%s = %v, want %v", key, line[key], value)
				}
			}
			if d, ok := line["duration_ms"].(float64); !ok || d < 0 {
				t.Errorf("duration_ms = %v, want a non-negative number", line["duration_ms"])
			}
			if got, named := line["user_id"]; named {
				t.Errorf("user_id = %v, want none for a caller nobody verified", got)
			}
		})
	}
}

func TestAccessLineNamesTheVerifiedCaller(t *testing.T) {
	caller := uuid.New()
	student := handlers.Principal{UserID: caller, Role: user.AccountRoleStudent}
	profile := &user.User{UID: caller, Username: "student", AccountRole: user.AccountRoleStudent, AccountStatus: user.AccountStatusActive}
	tests := map[string]struct {
		deps       routes.Dependencies
		method     string
		target     string
		body       string
		authorize  string
		wantStatus int
		wantUserID string
	}{
		"a verified caller is named": {
			deps:   routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: student}, Profile: handlers.ProfileDependencies{ProfileGetter: &fakeProfileGetter{account: profile}}},
			method: http.MethodGet, target: "/api/v1/users/" + caller.String(), authorize: "Bearer access-token",
			wantStatus: http.StatusOK, wantUserID: caller.String(),
		},
		"a request without a token names no one": {
			deps:   routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: student}},
			method: http.MethodGet, target: "/api/v1/users/" + caller.String(),
			wantStatus: http.StatusUnauthorized,
		},
		"a revoked token names no one": {
			deps:   routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: student}, RevocationChecker: &fakeRevocationChecker{revoked: true}},
			method: http.MethodGet, target: "/api/v1/users/" + caller.String(), authorize: "Bearer access-token",
			wantStatus: http.StatusUnauthorized,
		},
		"a logout names the owner of its access token": {
			deps: routes.Dependencies{Auth: handlers.AuthDependencies{
				AccessVerifier: &fakeAccessVerifier{claims: session.AccessTokenClaims{UserID: caller}},
				Logoutter:      &fakeLogoutter{},
			}},
			method: http.MethodPost, target: "/api/v1/users/logout", body: `{"refreshToken":"refresh-token"}`, authorize: "Bearer access-token",
			wantStatus: http.StatusNoContent, wantUserID: caller.String(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			tt.deps.Logger = jsonLogger(&logs)
			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			if tt.authorize != "" {
				req.Header.Set("Authorization", tt.authorize)
			}
			res := httptest.NewRecorder()

			newTestRouter(tt.deps).ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tt.wantStatus, res.Body.String())
			}
			line := onlyLine(t, logLines(t, &logs), "http request")
			got, named := line["user_id"]
			if tt.wantUserID == "" && named {
				t.Fatalf("user_id = %v, want none", got)
			}
			if tt.wantUserID != "" && got != tt.wantUserID {
				t.Fatalf("user_id = %v, want %s", got, tt.wantUserID)
			}
		})
	}
}

func TestA500CauseIsAWarningOnlyWhenTheClientHungUp(t *testing.T) {
	tests := map[string]struct {
		ctx       func(*testing.T) context.Context
		err       error
		wantLevel string
	}{
		"a failure in the service is an error": {
			ctx: live, err: errors.New("database unavailable"), wantLevel: "ERROR",
		},
		"a client that hung up is a warning": {
			ctx: hungUp, err: fmt.Errorf("create refresh session: %w", context.Canceled), wantLevel: "WARN",
		},
		"a call the service canceled itself is still an error": {
			ctx: live, err: fmt.Errorf("create refresh session: %w", context.Canceled), wantLevel: "ERROR",
		},
		"the service's own deadline is still an error": {
			ctx: pastDeadline, err: fmt.Errorf("create refresh session: %w", context.DeadlineExceeded), wantLevel: "ERROR",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := jsonLogger(&logs)
			router := newTestRouter(routes.Dependencies{
				Auth:   handlers.AuthDependencies{LoginService: &fakeLoginService{err: tt.err}, Logger: logger},
				Logger: logger,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/users/login", strings.NewReader(validLogin)).WithContext(tt.ctx(t))
			req.Header.Set("X-Request-Id", "req-cause")
			res := httptest.NewRecorder()

			router.ServeHTTP(res, req)

			if res.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", res.Code, http.StatusInternalServerError)
			}
			lines := logLines(t, &logs)
			cause := onlyLine(t, lines, "internal server error")
			want := map[string]any{"level": tt.wantLevel, "request_id": "req-cause", "error": tt.err.Error()}
			for key, value := range want {
				if cause[key] != value {
					t.Errorf("cause %s = %v, want %v", key, cause[key], value)
				}
			}
			if stack, _ := cause["stack_trace"].(string); !strings.Contains(stack, "goroutine") {
				t.Errorf("cause stack_trace = %q, want the stack", stack)
			}
			if access := onlyLine(t, lines, "http request"); access["level"] != tt.wantLevel {
				t.Errorf("access line level = %v, want %v", access["level"], tt.wantLevel)
			}
		})
	}
}

func TestAFailedHealthCheckLogsItsCause(t *testing.T) {
	tests := map[string]struct {
		ctx       func(*testing.T) context.Context
		check     handlers.HealthCheck
		wantCause map[string]any
	}{
		"a healthy service logs no cause": {
			ctx: live, check: func(context.Context) error { return nil },
		},
		"an unreachable dependency is an error": {
			ctx: live, check: func(context.Context) error { return errors.New("dial tcp redis:6379: connection refused") },
			wantCause: map[string]any{"level": "ERROR", "request_id": "req-health", "error": "dial tcp redis:6379: connection refused"},
		},
		"a probe that hung up is a warning": {
			ctx: hungUp, check: func(ctx context.Context) error { return ctx.Err() },
			wantCause: map[string]any{"level": "WARN", "request_id": "req-health", "error": context.Canceled.Error()},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			router := newTestRouter(routes.Dependencies{
				System: handlers.SystemDependencies{HealthCheck: tt.check, Logger: jsonLogger(&logs)},
			})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil).WithContext(tt.ctx(t))
			req.Header.Set("X-Request-Id", "req-health")

			router.ServeHTTP(httptest.NewRecorder(), req)

			lines := logLines(t, &logs)
			if tt.wantCause == nil {
				if len(lines) != 0 {
					t.Fatalf("logs = %v, want none", lines)
				}
				return
			}
			cause := onlyLine(t, lines, "health check failed")
			for key, value := range tt.wantCause {
				if cause[key] != value {
					t.Errorf("cause %s = %v, want %v", key, cause[key], value)
				}
			}
		})
	}
}

func TestPanicRecovery(t *testing.T) {
	tests := map[string]struct {
		handler    http.HandlerFunc
		wantStatus int
		wantBody   string
		wantPanic  string
	}{
		"a panic before the response is a 500": {
			handler:    func(http.ResponseWriter, *http.Request) { panic("assignment to entry in nil map") },
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"Internal Server Error"}`,
			wantPanic:  "assignment to entry in nil map",
		},
		"a panic after the response started keeps its status and body": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("partial"))
				panic(errors.New("index out of range"))
			},
			wantStatus: http.StatusCreated,
			wantBody:   "partial",
			wantPanic:  "index out of range",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := jsonLogger(&logs)
			chain := middleware.RequestID(handlers.AccessLog(logger)(handlers.RecoverPanics(logger)(tt.handler)))
			req := httptest.NewRequest(http.MethodGet, "/panics", nil)
			req.Header.Set("X-Request-Id", "req-panic")
			res := httptest.NewRecorder()

			chain.ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && strings.TrimSpace(res.Body.String()) != tt.wantBody {
				t.Fatalf("body = %q, want %q", res.Body.String(), tt.wantBody)
			}
			lines := logLines(t, &logs)
			report := onlyLine(t, lines, "panic serving request")
			want := map[string]any{"level": "ERROR", "request_id": "req-panic", "panic": tt.wantPanic}
			for key, value := range want {
				if report[key] != value {
					t.Errorf("panic line %s = %v, want %v", key, report[key], value)
				}
			}
			if stack, _ := report["stack_trace"].(string); !strings.Contains(stack, "goroutine") {
				t.Errorf("panic line stack_trace = %q, want the stack", stack)
			}
			if access := onlyLine(t, lines, "http request"); access["status"] != float64(tt.wantStatus) {
				t.Errorf("access line status = %v, want %d", access["status"], tt.wantStatus)
			}
		})
	}
}

func TestTheRouterTurnsAHandlerPanicIntoA500(t *testing.T) {
	var logs bytes.Buffer
	router := newTestRouter(routes.Dependencies{
		Auth:   handlers.AuthDependencies{LoginService: panickingLogin{}},
		Logger: jsonLogger(&logs),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/login", strings.NewReader(validLogin))
	req.Header.Set("X-Request-Id", "req-router-panic")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	lines := logLines(t, &logs)
	if report := onlyLine(t, lines, "panic serving request"); report["request_id"] != "req-router-panic" || report["panic"] != "login exploded" {
		t.Fatalf("panic line = %v, want the request id and the panic", report)
	}
	if access := onlyLine(t, lines, "http request"); access["status"] != float64(http.StatusInternalServerError) || access["level"] != "ERROR" {
		t.Fatalf("access line = %v, want a 500 at ERROR", access)
	}
}

func TestRecoverPanicsHandsAnAbortBackToNetHTTP(t *testing.T) {
	var logs bytes.Buffer
	handler := handlers.RecoverPanics(jsonLogger(&logs))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	rec := recovered(func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/aborts", nil))
	})

	if rec != http.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler passed on", rec)
	}
	if logs.Len() != 0 {
		t.Fatalf("logs = %q, want nothing for an aborted response", logs.String())
	}
}

func TestAnOversizedBodyDropsItsConnection(t *testing.T) {
	tests := map[string]struct {
		body       string
		wantStatus int
		wantClosed bool
	}{
		"a body over the limit is refused and its connection dropped": {
			body:       `{"identifier":"student","password":"` + strings.Repeat("a", 20*1024) + `"}`,
			wantStatus: http.StatusRequestEntityTooLarge, wantClosed: true,
		},
		"a body within the limit keeps its connection": {
			body:       validLogin,
			wantStatus: http.StatusOK, wantClosed: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			server := httptest.NewServer(newTestRouter(routes.Dependencies{
				Auth:   handlers.AuthDependencies{LoginService: loginServiceStub{}},
				Logger: jsonLogger(&logs),
			}))
			t.Cleanup(server.Close)
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			// Bounds every read below, so a server that neither answers nor
			// hangs up fails the test instead of stalling it.
			if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatalf("set deadline: %v", err)
			}
			reader := bufio.NewReader(conn)

			res, err := sendOn(conn, reader, http.MethodPost, "/api/v1/users/login", tt.body)
			if err != nil {
				t.Fatalf("first request: %v", err)
			}
			if res.StatusCode != tt.wantStatus || res.Close != tt.wantClosed {
				t.Fatalf("status = %d, Close = %t; want %d, %t", res.StatusCode, res.Close, tt.wantStatus, tt.wantClosed)
			}
			_, err = sendOn(conn, reader, http.MethodGet, "/api/v1/nowhere", "")
			if answered := err == nil; answered == tt.wantClosed {
				t.Fatalf("second request on the same connection answered = %t (err = %v), want %t", answered, err, !tt.wantClosed)
			}

			_ = conn.Close()
			server.Close() // returns once the server is done, so its access line is in logs
			lines := logLines(t, &logs)
			if len(lines) == 0 || lines[0]["msg"] != "http request" || lines[0]["status"] != float64(tt.wantStatus) {
				t.Fatalf("logs = %v, want an access line with status %d first", lines, tt.wantStatus)
			}
		})
	}
}

// sendOn writes one request on conn and reads its whole response, so each
// request a test sends travels on that one connection.
func sendOn(conn net.Conn, reader *bufio.Reader, method, target, body string) (*http.Response, error) {
	req, err := http.NewRequest(method, "http://"+conn.RemoteAddr().String()+target, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	if err := req.Write(conn); err != nil {
		return nil, err
	}
	res, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	_, err = io.Copy(io.Discard, res.Body)
	return res, err
}

type panickingLogin struct{}

func (panickingLogin) Login(context.Context, string, string) (session.TokenPair, error) {
	panic("login exploded")
}

// recovered runs f and returns what it panicked with, or nil.
func recovered(f func()) (rec any) {
	defer func() { rec = recover() }()
	f()
	return nil
}

func live(*testing.T) context.Context { return context.Background() }

// hungUp is the context of a request whose client went away: net/http
// cancels it.
func hungUp(*testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// pastDeadline is the context of a request the service's own deadline ended.
func pastDeadline(t *testing.T) context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return ctx
}

func jsonLogger(logs *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(logs, nil))
}

// logLines decodes every line written to logs, failing on any that is not JSON.
func logLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line %q is not JSON: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

// onlyLine returns the one line whose msg is msg, failing unless there is
// exactly one.
func onlyLine(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, line := range lines {
		if line["msg"] == msg {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d %q lines in %v, want exactly one", len(found), msg, lines)
	}
	return found[0]
}
