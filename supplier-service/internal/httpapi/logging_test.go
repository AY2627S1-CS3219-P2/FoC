// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Table-driven tests through NewRouter of the JSON access line (level,
//   status, path without the query string, user_id) and of every 5xx logging
//   its cause exactly once with the request id, GET /suppliers included (D-135).
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/auth"
	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/supplier"
)

// logRequestID is the X-Request-Id every logged request is sent with.
const logRequestID = "req-log-test"

// discardLog is the logger for tests that do not read the log.
func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// failingList is a memRepository whose List can be made to fail or panic. A set
// err is returned even after the request's context is done; with none, it
// fails like pgx, with the context's error once that context is done.
type failingList struct {
	*memRepository
	err       error
	panicWith any
}

func (f *failingList) List(ctx context.Context, filter supplier.ListFilter) ([]*supplier.Supplier, error) {
	if f.panicWith != nil {
		panic(f.panicWith)
	}
	if f.err != nil {
		return nil, f.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.memRepository.List(ctx, filter)
}

// logged is one request and how the dependencies it reaches answer it.
type logged struct {
	method, target, body string
	token                string // sent as a Bearer token; "" for none
	hangUp               bool   // the client is gone before the request is served
	listErr              error
	panicWith            any
	keysStatus           int // what user-service's key set answers; 0 for 200
	redisErr             error
	orderStatus          int // what order-service answers the errand check; 0 for 200
}

// serveLogged serves req through a router that logs into a buffer, and returns
// the answer and the log.
func serveLogged(t *testing.T, req logged) (*httptest.ResponseRecorder, string) {
	t.Helper()
	_, orders := startOrderService(t, cmp.Or(req.orderStatus, http.StatusOK), activeErrands(0))
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	repo := &failingList{memRepository: newMemRepository(), err: req.listErr, panicWith: req.panicWith}
	router := httpapi.NewRouter(supplier.NewService(repo, orderclient.New(orders.URL), log),
		startKeySet(t, cmp.Or(req.keysStatus, http.StatusOK)), &fakeRevocations{err: req.redisErr}, log)

	r := httptest.NewRequest(req.method, req.target, strings.NewReader(req.body))
	r.Header.Set("X-Request-Id", logRequestID)
	if req.token != "" {
		r.Header.Set("Authorization", "Bearer "+req.token)
	}
	if req.hangUp {
		ctx, cancel := context.WithCancel(r.Context())
		cancel()
		r = r.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	return rec, logs.String()
}

// logLines decodes each line of log, failing on one that is not a JSON object.
func logLines(t *testing.T, log string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(log))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &line), sc.Text())
		out = append(out, line)
	}
	require.NoError(t, sc.Err())
	return out
}

func TestEveryRequestGetsOneAccessLine(t *testing.T) {
	adminID, studentID := uuid.New(), uuid.New()
	admin := mintToken(t, tokenSpec{sub: adminID})
	student := mintToken(t, tokenSpec{sub: studentID, role: "STUDENT"})
	redisDown := fmt.Errorf("read revocation keys: %w: dial tcp: connection refused", auth.ErrRedisUnreachable)

	cases := []struct {
		name       string
		req        logged
		wantStatus int
		wantLevel  string
		wantUserID string // "" when no caller was verified
	}{
		{name: "a read", req: logged{method: http.MethodGet, target: "/suppliers?q=cool"},
			wantStatus: http.StatusOK, wantLevel: "INFO"},
		{name: "a health probe", req: logged{method: http.MethodGet, target: "/health"},
			wantStatus: http.StatusOK, wantLevel: "INFO"},
		{name: "an unknown supplier", req: logged{method: http.MethodGet, target: "/suppliers/" + unknownID},
			wantStatus: http.StatusNotFound, wantLevel: "INFO"},
		{name: "a write without a token", req: logged{method: http.MethodPost, target: "/suppliers", body: supplierBody},
			wantStatus: http.StatusUnauthorized, wantLevel: "INFO"},
		{name: "an admin's write", req: logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: admin},
			wantStatus: http.StatusCreated, wantLevel: "INFO", wantUserID: adminID.String()},
		{name: "a student's write", req: logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: student},
			wantStatus: http.StatusForbidden, wantLevel: "INFO", wantUserID: studentID.String()},
		{name: "a read that fails", req: logged{method: http.MethodGet, target: "/suppliers", listErr: errors.New("connection refused")},
			wantStatus: http.StatusInternalServerError, wantLevel: "ERROR"},
		{name: "a write while Redis is down", req: logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: admin, redisErr: redisDown},
			wantStatus: http.StatusServiceUnavailable, wantLevel: "ERROR"},
		{name: "a handler panic", req: logged{method: http.MethodGet, target: "/suppliers", panicWith: "boom"},
			wantStatus: http.StatusInternalServerError, wantLevel: "ERROR"},
		{name: "a client that hung up on a read that then failed", req: logged{method: http.MethodGet, target: "/suppliers", hangUp: true},
			wantStatus: http.StatusInternalServerError, wantLevel: "WARN"},
		{name: "a client that hung up on a health probe", req: logged{method: http.MethodGet, target: "/health", hangUp: true},
			wantStatus: http.StatusOK, wantLevel: "WARN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, log := serveLogged(t, tc.req)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			var access []map[string]any
			for _, line := range logLines(t, log) {
				if line["msg"] == "http request" {
					access = append(access, line)
				}
			}
			require.Len(t, access, 1, log)
			line := access[0]
			assert.Equal(t, tc.wantLevel, line["level"])
			assert.Equal(t, logRequestID, line["request_id"])
			assert.Equal(t, tc.req.method, line["method"])
			path, _, _ := strings.Cut(tc.req.target, "?")
			assert.Equal(t, path, line["path"])
			assert.EqualValues(t, tc.wantStatus, line["status"])
			assert.IsType(t, float64(0), line["duration_ms"])
			if tc.wantUserID == "" {
				assert.NotContains(t, line, "user_id")
			} else {
				assert.Equal(t, tc.wantUserID, line["user_id"])
			}
			// No line holds a query string, a body or a token.
			assert.NotContains(t, log, "q=cool")
			assert.NotContains(t, log, "Test Stall")
			if tc.req.token != "" {
				assert.NotContains(t, log, tc.req.token)
			}
		})
	}
}

func TestEvery5xxLogsItsCauseOnce(t *testing.T) {
	admin := mintToken(t, tokenSpec{})

	cases := []struct {
		name       string
		req        logged
		wantStatus int
		wantBody   string // JSON; "" when not checked
		wantMsg    string // the cause line's msg; "" when there must be none
		wantErr    string // part of the cause line's err; "" when not checked
	}{
		{
			name:       "a list the database fails",
			req:        logged{method: http.MethodGet, target: "/suppliers", listErr: errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")},
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"failed to list suppliers"}`,
			wantMsg: "request failed", wantErr: "connection refused",
		},
		{
			name:       "a list past the service's own deadline",
			req:        logged{method: http.MethodGet, target: "/suppliers", listErr: fmt.Errorf("list suppliers: %w", context.DeadlineExceeded)},
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"failed to list suppliers"}`,
			wantMsg: "request failed", wantErr: "deadline exceeded",
		},
		{
			name:       "a list whose client hung up",
			req:        logged{method: http.MethodGet, target: "/suppliers", hangUp: true},
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"failed to list suppliers"}`,
		},
		{
			name:       "a list whose client hung up and whose cause is not the cancellation",
			req:        logged{method: http.MethodGet, target: "/suppliers", hangUp: true, listErr: errors.New("list suppliers: conn closed")},
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"failed to list suppliers"}`,
		},
		{
			name:       "a delete order-service cannot answer",
			req:        logged{method: http.MethodDelete, target: "/suppliers/" + storedID, token: admin, orderStatus: http.StatusServiceUnavailable},
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal server error"}`,
			wantMsg: "request failed", wantErr: "order-service answered status 503",
		},
		{
			name:       "a write when user-service's keys cannot be loaded",
			req:        logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: admin, keysStatus: http.StatusBadGateway},
			wantStatus: http.StatusServiceUnavailable,
			wantMsg:    "cannot load user-service's signing keys; refusing the request", wantErr: "signing keys unavailable",
		},
		{
			name:       "a write whose client hung up while user-service's keys cannot be loaded",
			req:        logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: admin, keysStatus: http.StatusBadGateway, hangUp: true},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "a write when Redis cannot be reached",
			req: logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: admin,
				redisErr: fmt.Errorf("read revocation keys: %w: dial tcp: connection refused", auth.ErrRedisUnreachable)},
			wantStatus: http.StatusServiceUnavailable,
			wantMsg:    "Redis could not be reached; refusing the request", wantErr: "connection refused",
		},
		{
			name: "a write when Redis answers with an error",
			req: logged{method: http.MethodPost, target: "/suppliers", body: supplierBody, token: admin,
				redisErr: errors.New("read revocation keys: NOAUTH Authentication required.")},
			wantStatus: http.StatusServiceUnavailable,
			wantMsg:    "cannot read the revocation keys in Redis; refusing the request", wantErr: "NOAUTH",
		},
		{
			name:       "a handler panic",
			req:        logged{method: http.MethodGet, target: "/suppliers", panicWith: "boom"},
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal server error"}`,
			wantMsg: "panic serving request",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, log := serveLogged(t, tc.req)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantBody != "" {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
			}
			var access, causes []map[string]any
			for _, line := range logLines(t, log) {
				switch {
				case line["msg"] == "http request":
					access = append(access, line)
				case line["level"] == "ERROR":
					causes = append(causes, line)
				}
			}
			require.Len(t, access, 1, log)
			if tc.wantMsg == "" {
				// A client hanging up is no fault of the service, whatever the
				// cause: the WARN access line is the only record, and no line is
				// an ERROR.
				assert.Equal(t, "WARN", access[0]["level"], log)
				assert.Empty(t, causes, log)
				return
			}
			require.Len(t, causes, 1, "the cause is logged exactly once: %s", log)
			assert.Equal(t, tc.wantMsg, causes[0]["msg"])
			assert.Equal(t, logRequestID, causes[0]["request_id"])
			if tc.wantErr != "" {
				assert.Contains(t, causes[0]["err"], tc.wantErr)
			}
		})
	}
}
