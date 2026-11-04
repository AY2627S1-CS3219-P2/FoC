// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests the access line's fields and levels, the cause line on every
//   5xx path and the JSON panic recoverer through the full router (D-135).
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/gen"
	"foc/credit-service/internal/httpapi"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestAccessLineLevels(t *testing.T) {
	ctxErr := func(ctx context.Context) error { return ctx.Err() }
	down := func(context.Context) error { return errors.New("database unavailable") }

	cases := []struct {
		name       string
		health     httpapi.HealthCheck
		target     string
		hangUp     bool
		wantStatus int
		wantLevel  string
	}{
		{name: "a success is INFO", health: ctxErr, target: "/health/ready", wantStatus: http.StatusOK, wantLevel: "INFO"},
		{name: "a client error is INFO", health: ctxErr, target: "/api/v1/credits/accounts/me", wantStatus: http.StatusUnauthorized, wantLevel: "INFO"},
		{name: "a server error is ERROR", health: down, target: "/health/ready", wantStatus: http.StatusServiceUnavailable, wantLevel: "ERROR"},
		{name: "a failing health route is ERROR", health: down, target: "/api/v1/health", wantStatus: http.StatusServiceUnavailable, wantLevel: "ERROR"},
		{name: "a client that hung up is WARN even on a 5xx", health: ctxErr, target: "/health/ready", hangUp: true, wantStatus: http.StatusServiceUnavailable, wantLevel: "WARN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, logs := loggedRouter(t, tc.health, &mockReadRepo{}, "http://127.0.0.1/unused")
			req := httptest.NewRequest(http.MethodGet, tc.target+"?probe=query-secret", nil)
			req.Header.Set("X-Request-Id", "access-1")
			if tc.hangUp {
				req = req.WithContext(hungUp())
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			line := onlyLine(t, logs, "http request")
			want := map[string]any{
				"level":      tc.wantLevel,
				"request_id": "access-1",
				"method":     http.MethodGet,
				"path":       tc.target,
				"status":     float64(tc.wantStatus),
			}
			for key, value := range want {
				if line[key] != value {
					t.Errorf("%s = %v, want %v", key, line[key], value)
				}
			}
			if _, ok := line["duration_ms"].(float64); !ok {
				t.Errorf("duration_ms = %v, want a number", line["duration_ms"])
			}
			if _, ok := line["user_id"]; ok {
				t.Errorf("user_id = %v, want none for an unverified caller", line["user_id"])
			}
			if strings.Contains(logs.String(), "query-secret") {
				t.Errorf("the query string was logged: %s", logs.String())
			}
		})
	}
}

func TestAccessLineNamesTheVerifiedCaller(t *testing.T) {
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)
	userID := uuid.New()
	repo := &mockReadRepo{getAccountFn: func(_ context.Context, id uuid.UUID) (*credit.Account, error) {
		return &credit.Account{UserID: id, AvailableCredits: 100, IntegrityStatus: credit.IntegrityConsistent}, nil
	}}
	router, logs := loggedRouter(t, nil, repo, jwks.URL)
	token := iss.mint(t, tokenOpts{subject: userID.String(), role: "STUDENT"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	line := onlyLine(t, logs, "http request")
	if line["user_id"] != userID.String() {
		t.Errorf("user_id = %v, want %s", line["user_id"], userID)
	}
	if request, ok := line["request_id"].(string); !ok || request == "" {
		t.Errorf("request_id = %v, want one made up when the caller sent none", line["request_id"])
	}
	if strings.Contains(logs.String(), token) {
		t.Errorf("the access token was logged: %s", logs.String())
	}
}

func TestServerErrorLogsItsCauseOnce(t *testing.T) {
	cases := []struct {
		name        string
		repoErr     error
		keysDown    bool
		hangUp      bool
		wantStatus  int
		wantCause   string
		wantError   string
		wantAccess  string
		wantNoCause bool
	}{
		{
			name:       "a database failure, without the credentials it quotes",
			repoErr:    errors.New("dial postgres://credit:hunter2@credit-db:5432/credit: connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantCause:  "request failed",
			wantError:  "dial postgres://REDACTED@credit-db:5432/credit: connection refused",
			wantAccess: "ERROR",
		},
		{
			name:       "the service's own deadline",
			repoErr:    context.DeadlineExceeded,
			wantStatus: http.StatusServiceUnavailable,
			wantCause:  "request failed",
			wantError:  "context deadline exceeded",
			wantAccess: "ERROR",
		},
		{
			name:       "signing keys that cannot be loaded",
			keysDown:   true,
			wantStatus: http.StatusServiceUnavailable,
			wantCause:  "cannot load user-service's signing keys; refusing request",
			wantError:  "auth: signing keys unavailable",
			wantAccess: "ERROR",
		},
		{
			name:        "a client that hung up is no ERROR",
			repoErr:     context.Canceled,
			hangUp:      true,
			wantStatus:  http.StatusServiceUnavailable,
			wantAccess:  "WARN",
			wantNoCause: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iss := newIssuer(t)
			jwks := httptest.NewServer(iss.jwksHandler())
			t.Cleanup(jwks.Close)
			if tc.keysDown {
				jwks.Close()
			}
			repo := &mockReadRepo{getAccountFn: func(context.Context, uuid.UUID) (*credit.Account, error) {
				return nil, tc.repoErr
			}}
			router, logs := loggedRouter(t, nil, repo, jwks.URL)
			token := iss.mint(t, tokenOpts{subject: uuid.NewString(), role: "STUDENT"})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Request-Id", "cause-1")
			if tc.hangUp {
				req = req.WithContext(hungUp())
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if access := onlyLine(t, logs, "http request"); access["level"] != tc.wantAccess {
				t.Errorf("access line level = %v, want %s", access["level"], tc.wantAccess)
			}
			var errorLines []map[string]any
			for _, line := range logLines(t, logs) {
				if line["level"] == "ERROR" && line["msg"] != "http request" {
					errorLines = append(errorLines, line)
				}
			}
			if tc.wantNoCause {
				if len(errorLines) != 0 {
					t.Fatalf("logged %v, want no ERROR cause line", errorLines)
				}
				return
			}
			if len(errorLines) != 1 {
				t.Fatalf("logged %d ERROR cause lines, want exactly 1: %v", len(errorLines), errorLines)
			}
			cause := errorLines[0]
			if cause["msg"] != tc.wantCause || cause["request_id"] != "cause-1" {
				t.Errorf("cause line = %v, want msg %q with request_id cause-1", cause, tc.wantCause)
			}
			if got, _ := cause["error"].(string); !strings.Contains(got, tc.wantError) {
				t.Errorf("error = %q, want it to contain %q", got, tc.wantError)
			}
			if strings.Contains(logs.String(), "hunter2") || strings.Contains(logs.String(), token) {
				t.Errorf("a credential was logged: %s", logs.String())
			}
		})
	}
}

func TestEveryServerErrorPathLogsItsCause(t *testing.T) {
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)
	verifier := auth.NewVerifier(jwks.URL, nil)
	token := iss.mint(t, tokenOpts{subject: uuid.NewString(), role: "STUDENT"})
	// The reward route reads the request id as its operation id, a UUID.
	requestID := uuid.NewString()
	errandID := uuid.NewString()
	reserve := `{"errandId":"` + errandID + `","amount":25}`
	reward := `{"newReward":35,"currVersion":1}`
	leaky := errors.New("dial postgres://credit:hunter2@credit-db:5432/credit: connection refused")
	checkFails := func(context.Context) error { return leaky }
	endsWithRequest := redisReply(func(ctx context.Context) error { return ctx.Err() })
	refused := redisReply(func(context.Context) error {
		return errors.New("WRONGPASS invalid username-password pair or user is disabled.")
	})

	cases := []struct {
		name       string
		method     string
		target     string
		body       string
		repoErr    error
		rdb        redisReply
		noVerifier bool
		health     httpapi.HealthCheck
		ctx        context.Context
		wantStatus int
		wantCause  string
		wantError  string
		wantAccess string
	}{
		{name: "a reservation the ledger cannot take now", method: http.MethodPost, target: "/api/v1/credits/reservations", body: reserve,
			repoErr: credit.ErrServiceUnavailable, wantStatus: http.StatusServiceUnavailable,
			wantCause: "request failed", wantError: "service unavailable", wantAccess: "ERROR"},
		{name: "a reservation failing unexpectedly", method: http.MethodPost, target: "/api/v1/credits/reservations", body: reserve,
			repoErr: leaky, wantStatus: http.StatusInternalServerError,
			wantCause: "request failed", wantError: "dial postgres://REDACTED@credit-db:5432/credit", wantAccess: "ERROR"},
		{name: "a reward change the ledger cannot take now", method: http.MethodPatch, target: "/api/v1/credits/reservations/" + errandID, body: reward,
			repoErr: credit.ErrServiceUnavailable, wantStatus: http.StatusServiceUnavailable,
			wantCause: "request failed", wantError: "service unavailable", wantAccess: "ERROR"},
		{name: "a reward change failing unexpectedly", method: http.MethodPatch, target: "/api/v1/credits/reservations/" + errandID, body: reward,
			repoErr: leaky, wantStatus: http.StatusInternalServerError,
			wantCause: "request failed", wantError: "dial postgres://REDACTED@credit-db:5432/credit", wantAccess: "ERROR"},
		{name: "a transaction list failing", method: http.MethodGet, target: "/api/v1/credits/accounts/me/transactions",
			repoErr: leaky, wantStatus: http.StatusInternalServerError,
			wantCause: "request failed", wantError: "dial postgres://REDACTED@credit-db:5432/credit", wantAccess: "ERROR"},
		{name: "a revocation check ended by the service's own deadline", method: http.MethodGet, target: "/api/v1/credits/accounts/me",
			rdb: endsWithRequest, ctx: pastDeadline(), wantStatus: http.StatusServiceUnavailable,
			wantCause: "revocation check ended with the request; refusing it", wantError: "context deadline exceeded", wantAccess: "ERROR"},
		{name: "a revocation check ended by a client that hung up is no ERROR", method: http.MethodGet, target: "/api/v1/credits/accounts/me",
			rdb: endsWithRequest, ctx: hungUp(), wantStatus: http.StatusServiceUnavailable, wantAccess: "WARN"},
		{name: "revocation keys Redis refuses to read", method: http.MethodGet, target: "/api/v1/credits/accounts/me",
			rdb: refused, wantStatus: http.StatusServiceUnavailable,
			wantCause: "cannot read revocation keys in Redis; refusing request", wantError: "WRONGPASS", wantAccess: "ERROR"},
		{name: "no token verifier configured", method: http.MethodGet, target: "/api/v1/credits/accounts/me",
			noVerifier: true, wantStatus: http.StatusServiceUnavailable,
			wantCause: "no token verifier is configured; refusing request", wantAccess: "ERROR"},
		{name: "a readiness probe whose check fails", method: http.MethodGet, target: "/health/ready",
			health: checkFails, wantStatus: http.StatusServiceUnavailable,
			wantCause: "readiness check failed", wantError: "dial postgres://REDACTED@credit-db:5432/credit", wantAccess: "ERROR"},
		{name: "a health route whose check fails", method: http.MethodGet, target: "/api/v1/health",
			health: checkFails, wantStatus: http.StatusServiceUnavailable,
			wantCause: "health check failed", wantError: "dial postgres://REDACTED@credit-db:5432/credit", wantAccess: "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := verifier
			if tc.noVerifier {
				v = nil
			}
			var rev *auth.Revocations
			if tc.rdb != nil {
				rev = auth.NewRevocations(tc.rdb)
			}
			svc := credit.NewService(nil,
				credit.WithReadRepository(&mockReadRepo{
					listTransactionsFn: func(context.Context, uuid.UUID, int, *string) (*credit.PaginatedTransactions, error) {
						return nil, tc.repoErr
					},
				}),
				credit.WithReservationRepository(&mockReservationRepo{
					createReservationFn: func(context.Context, uuid.UUID, uuid.UUID, int64) (*credit.Reservation, bool, error) {
						return nil, false, tc.repoErr
					},
					adjustRewardFn: func(context.Context, uuid.UUID, uuid.UUID, int64, int64, uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
						return nil, nil, tc.repoErr
					},
				}))
			logs := &bytes.Buffer{}
			router := httpapi.NewRouter(tc.health, v, rev, svc, slog.New(slog.NewJSONHandler(logs, nil)))
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-Id", requestID)
			if tc.ctx != nil {
				req = req.WithContext(tc.ctx)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if access := onlyLine(t, logs, "http request"); access["level"] != tc.wantAccess {
				t.Errorf("access line level = %v, want %s", access["level"], tc.wantAccess)
			}
			var errorLines []map[string]any
			for _, line := range logLines(t, logs) {
				if line["level"] == "ERROR" && line["msg"] != "http request" {
					errorLines = append(errorLines, line)
				}
			}
			if tc.wantCause == "" {
				if len(errorLines) != 0 {
					t.Fatalf("logged %v, want no ERROR cause line", errorLines)
				}
				return
			}
			if len(errorLines) != 1 {
				t.Fatalf("logged %d ERROR cause lines, want exactly 1: %v", len(errorLines), errorLines)
			}
			cause := errorLines[0]
			if cause["msg"] != tc.wantCause || cause["request_id"] != requestID {
				t.Errorf("cause line = %v, want msg %q with request_id %s", cause, tc.wantCause, requestID)
			}
			got, hasError := cause["error"].(string)
			switch {
			case tc.wantError == "" && hasError:
				t.Errorf("error = %q, want none", got)
			case !strings.Contains(got, tc.wantError):
				t.Errorf("error = %q, want it to contain %q", got, tc.wantError)
			}
			if strings.Contains(logs.String(), "hunter2") || strings.Contains(logs.String(), token) {
				t.Errorf("a credential was logged: %s", logs.String())
			}
		})
	}
}

func TestAnOversizedBodyDropsItsConnection(t *testing.T) {
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)
	token := iss.mint(t, tokenOpts{subject: uuid.NewString(), role: "STUDENT"})
	reserve := `{"errandId":"` + uuid.NewString() + `","amount":25}`

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantClosed bool
	}{
		{name: "a body over the 16 KiB cap is refused and its connection dropped",
			body: reserve + strings.Repeat(" ", 20*1024), wantStatus: http.StatusBadRequest, wantClosed: true},
		{name: "a body within the cap keeps its connection",
			body: reserve, wantStatus: http.StatusCreated, wantClosed: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := credit.NewService(nil, credit.WithReservationRepository(&mockReservationRepo{
				createReservationFn: func(_ context.Context, errandID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error) {
					return &credit.Reservation{ID: uuid.New(), ErrandID: errandID, RequesterID: requesterID, Amount: amount, Status: credit.ReservationStatusReserved}, true, nil
				},
			}))
			logs := &bytes.Buffer{}
			server := httptest.NewServer(httpapi.NewRouter(nil, auth.NewVerifier(jwks.URL, nil), nil, svc, slog.New(slog.NewJSONHandler(logs, nil))))
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

			res, err := sendOn(conn, reader, http.MethodPost, "/api/v1/credits/reservations", tc.body, token)
			if err != nil {
				t.Fatalf("first request: %v", err)
			}
			if res.StatusCode != tc.wantStatus || res.Close != tc.wantClosed {
				t.Fatalf("status = %d, Close = %t; want %d, %t", res.StatusCode, res.Close, tc.wantStatus, tc.wantClosed)
			}
			_, err = sendOn(conn, reader, http.MethodGet, "/health/live", "", token)
			if answered := err == nil; answered == tc.wantClosed {
				t.Fatalf("second request on the same connection answered = %t (err = %v), want %t", answered, err, !tc.wantClosed)
			}

			_ = conn.Close()
			server.Close() // returns once the server is done, so its access lines are in logs
			var access []map[string]any
			for _, line := range logLines(t, logs) {
				if line["msg"] == "http request" && line["path"] == "/api/v1/credits/reservations" {
					access = append(access, line)
				}
			}
			if len(access) != 1 || access[0]["status"] != float64(tc.wantStatus) {
				t.Fatalf("access lines = %v, want one with status %d", access, tc.wantStatus)
			}
		})
	}
}

func TestPanicRecovery(t *testing.T) {
	cases := []struct {
		name      string
		value     any
		wantPanic string
	}{
		{name: "a string", value: "ledger exploded", wantPanic: "ledger exploded"},
		{name: "an error quoting a connection string", value: errors.New("dial amqp://foc:secret@rabbitmq:5672/foc"), wantPanic: "dial amqp://REDACTED@rabbitmq:5672/foc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, logs := loggedRouter(t, func(context.Context) error { panic(tc.value) }, &mockReadRepo{}, "http://127.0.0.1/unused")
			req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
			req.Header.Set("X-Request-Id", "panic-1")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			var body gen.ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Code != gen.INTERNALERROR {
				t.Errorf("body code = %v (decode error %v), want INTERNAL_ERROR", body.Code, err)
			}
			line := onlyLine(t, logs, "panic serving request")
			if line["level"] != "ERROR" || line["request_id"] != "panic-1" || line["panic"] != tc.wantPanic {
				t.Errorf("panic line = %v, want ERROR with request_id panic-1 and panic %q", line, tc.wantPanic)
			}
			if stack, _ := line["stack"].(string); !strings.Contains(stack, "goroutine ") {
				t.Errorf("stack = %q, want the goroutine's stack in one field", stack)
			}
			if access := onlyLine(t, logs, "http request"); access["status"] != float64(http.StatusInternalServerError) || access["level"] != "ERROR" {
				t.Errorf("access line = %v, want an ERROR for status 500", access)
			}
		})
	}

	t.Run("an aborted handler is passed on to net/http", func(t *testing.T) {
		router, logs := loggedRouter(t, func(context.Context) error { panic(http.ErrAbortHandler) }, &mockReadRepo{}, "http://127.0.0.1/unused")
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		defer func() {
			if rec := recover(); rec != http.ErrAbortHandler {
				t.Fatalf("recovered %v, want http.ErrAbortHandler re-panicked", rec)
			}
			for _, line := range logLines(t, logs) {
				if line["msg"] == "panic serving request" {
					t.Errorf("logged %v, want nothing for an aborted handler", line)
				}
			}
		}()
		router.ServeHTTP(httptest.NewRecorder(), req)
	})
}

// loggedRouter is the full router logging JSON to the returned buffer, its
// credit service reading accounts from repo, and tokens verified against the
// keys published at jwksURL.
func loggedRouter(t *testing.T, health httpapi.HealthCheck, repo credit.ReadRepository, jwksURL string) (http.Handler, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	svc := credit.NewService(nil, credit.WithReadRepository(repo))
	return httpapi.NewRouter(health, auth.NewVerifier(jwksURL, nil), nil, svc, logger), logs
}

// hungUp is a request context already ended the way net/http ends one when its
// client closes the connection.
func hungUp() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// pastDeadline is a request context ended by a deadline of the service's own.
func pastDeadline() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	return ctx
}

// redisReply answers every revocation lookup with the error it returns for the
// lookup's context.
type redisReply func(ctx context.Context) error

func (f redisReply) MGet(ctx context.Context, _ ...string) *redis.SliceCmd {
	return redis.NewSliceResult(nil, f(ctx))
}

// sendOn writes one JSON request bearing token on conn and reads its whole
// response, so each request a test sends travels on that one connection.
func sendOn(conn net.Conn, reader *bufio.Reader, method, target, body, token string) (*http.Response, error) {
	req, err := http.NewRequest(method, "http://"+conn.RemoteAddr().String()+target, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
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

func logLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("log line is not one JSON object: %v: %s", err, raw)
		}
		lines = append(lines, line)
	}
	return lines
}

// onlyLine returns the one line whose msg is msg, failing unless there is
// exactly one.
func onlyLine(t *testing.T, logs *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, line := range logLines(t, logs) {
		if line["msg"] == msg {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d %q lines, want exactly 1: %s", len(found), msg, logs.String())
	}
	return found[0]
}
