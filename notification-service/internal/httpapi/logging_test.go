// AI Assistance Disclosure:
// Tool: Claude Code (model: Fable 5), date: 2026-11-10
// Scope: Tests of D-135's logging rules on the HTTP surface: the access line's
//   level and fields, one cause line for each 503, none for a client that hung
//   up, and panic recovery, read back from the JSON lines the router logs.
// Author review: nigeltzy - Checked that the unit test implementation seems correct and as expected according to requirements.

package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/notification-service/internal/auth"
	"foc/notification-service/internal/notification"
)

const sentRequestID = "req-from-the-gateway"

// logLine is one JSON line the router logged.
type logLine map[string]any

// logged serves req on h's router and returns the answer with every line the
// router logged for it.
func (h harness) logged(t *testing.T, req *http.Request) (*httptest.ResponseRecorder, []logLine) {
	t.Helper()
	var buf bytes.Buffer
	router := NewRouter(h.svc, h.db, h.tokens, h.rev, slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, parseLines(t, &buf)
}

func parseLines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var lines []logLine
	sc := bufio.NewScanner(buf)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var line logLine
		require.NoError(t, json.Unmarshal(sc.Bytes(), &line), "every log line is JSON: %s", sc.Text())
		lines = append(lines, line)
	}
	require.NoError(t, sc.Err())
	return lines
}

// request builds a request carrying the gateway's X-Request-Id, and a bearer
// token when bearer is set.
func request(method, path string, bearer bool) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Request-Id", sentRequestID)
	if bearer {
		req.Header.Set("Authorization", "Bearer a-token")
	}
	return req
}

// hungUp returns req as net/http leaves it once its client has gone away.
func hungUp(req *http.Request) *http.Request {
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	return req.WithContext(ctx)
}

// accessLine returns the one access line among lines.
func accessLine(t *testing.T, lines []logLine) logLine {
	t.Helper()
	var found []logLine
	for _, l := range lines {
		if l["msg"] == "http request" {
			found = append(found, l)
		}
	}
	require.Len(t, found, 1, "one access line per request")
	return found[0]
}

// causeLines returns every ERROR line among lines that is not the access line.
func causeLines(lines []logLine) []logLine {
	var found []logLine
	for _, l := range lines {
		if l["level"] == "ERROR" && l["msg"] != "http request" {
			found = append(found, l)
		}
	}
	return found
}

func TestAccessLineFields(t *testing.T) {
	h := harness{svc: &stubService{}}
	rec, lines := h.logged(t, request(http.MethodGet, "/notifications?limit=10&offset=20", true))
	require.Equal(t, http.StatusOK, rec.Code)

	line := accessLine(t, lines)
	assert.Equal(t, "INFO", line["level"])
	assert.Equal(t, sentRequestID, line["request_id"])
	assert.Equal(t, http.MethodGet, line["method"])
	assert.Equal(t, "/notifications", line["path"], "the query string is never logged")
	assert.EqualValues(t, http.StatusOK, line["status"])
	assert.Contains(t, line, "duration_ms")
	assert.Equal(t, caller.String(), line["user_id"])
	assert.Empty(t, causeLines(lines))
}

func TestAccessLineHasNoUserBeforeTheTokenIsVerified(t *testing.T) {
	h := harness{svc: &stubService{}}
	rec, lines := h.logged(t, request(http.MethodGet, "/notifications", false))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	line := accessLine(t, lines)
	assert.Equal(t, "INFO", line["level"])
	assert.NotContains(t, line, "user_id")
}

func TestARequestWithoutAnIDGetsOne(t *testing.T) {
	h := harness{svc: &stubService{}}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	_, lines := h.logged(t, req)
	assert.NotEmpty(t, accessLine(t, lines)["request_id"])
}

func TestNothingSecretIsLogged(t *testing.T) {
	h := harness{svc: &stubService{}}
	var buf bytes.Buffer
	router := NewRouter(h.svc, h.db, h.tokens, h.rev, slog.New(slog.NewJSONHandler(&buf, nil)))
	req := request(http.MethodGet, "/notifications?limit=10", false)
	req.Header.Set("Authorization", "Bearer the-secret-token")
	req.Header.Set("Cookie", "foc_refresh=the-secret-cookie")
	router.ServeHTTP(httptest.NewRecorder(), req)

	assert.NotContains(t, buf.String(), "the-secret-token")
	assert.NotContains(t, buf.String(), "the-secret-cookie")
	assert.NotContains(t, buf.String(), "limit=10")
}

// TestEach503LogsItsCauseOnce covers every way this service answers 503: the
// access line is at ERROR, and exactly one more ERROR line names the cause with
// the same request id.
func TestEach503LogsItsCauseOnce(t *testing.T) {
	read := "/notifications/" + uuid.NewString() + "/read"
	for _, tc := range []struct {
		name   string
		h      harness
		method string
		path   string
		bearer bool
	}{
		{"the list fails", harness{svc: &stubService{listErr: errors.New("database down")}}, http.MethodGet, "/notifications", true},
		{"marking read fails", harness{svc: &stubService{readErr: errors.New("database down")}}, http.MethodPost, read, true},
		{"the signing keys cannot be loaded", harness{svc: &stubService{}, tokens: stubTokens{err: auth.ErrKeysUnavailable}}, http.MethodGet, "/notifications", true},
		{"Redis answers with an error", harness{svc: &stubService{}, rev: stubRevocations{err: errors.New("NOAUTH")}}, http.MethodGet, "/notifications", true},
		{"the health check's database is down", harness{svc: &stubService{}, db: stubDB{err: errors.New("down")}}, http.MethodGet, "/health", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, lines := tc.h.logged(t, request(tc.method, tc.path, tc.bearer))
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)

			assert.Equal(t, "ERROR", accessLine(t, lines)["level"])
			causes := causeLines(lines)
			require.Len(t, causes, 1, "one cause line")
			assert.Equal(t, sentRequestID, causes[0]["request_id"])
			assert.NotEmpty(t, causes[0]["err"])
		})
	}
}

// TestAClientThatHungUpIsNeverAnError: the same failures, once the client has
// gone away, leave only the access line, at WARN.
func TestAClientThatHungUpIsNeverAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		h      harness
		path   string
		bearer bool
	}{
		{"the list fails", harness{svc: &stubService{listErr: context.Canceled}}, "/notifications", true},
		{"the signing keys cannot be loaded", harness{svc: &stubService{}, tokens: stubTokens{err: auth.ErrKeysUnavailable}}, "/notifications", true},
		{"the revocation check is cut short", harness{svc: &stubService{}, rev: stubRevocations{err: context.Canceled}}, "/notifications", true},
		{"the health check is cut short", harness{svc: &stubService{}, db: stubDB{err: context.Canceled}}, "/health", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, lines := tc.h.logged(t, hungUp(request(http.MethodGet, tc.path, tc.bearer)))
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)

			assert.Equal(t, "WARN", accessLine(t, lines)["level"])
			assert.Empty(t, causeLines(lines))
		})
	}
}

func TestRedisUnreachableIsLoggedWithTheRequestID(t *testing.T) {
	h := harness{svc: &stubService{}, rev: stubRevocations{err: auth.ErrRedisUnreachable}}
	rec, lines := h.logged(t, request(http.MethodGet, "/notifications", true))
	require.Equal(t, http.StatusOK, rec.Code, "the check is skipped, not failed (D-056)")

	causes := causeLines(lines)
	require.Len(t, causes, 1)
	assert.Equal(t, sentRequestID, causes[0]["request_id"])
	assert.Equal(t, caller.String(), causes[0]["user_id"])
}

// panickingService panics in place of answering.
type panickingService struct{}

func (panickingService) List(context.Context, uuid.UUID, int, int) (notification.Page, error) {
	panic("boom")
}

func (panickingService) MarkRead(context.Context, uuid.UUID, uuid.UUID) error {
	panic("boom")
}

func TestAPanicBecomesA500AndOneErrorLine(t *testing.T) {
	var buf bytes.Buffer
	router := NewRouter(panickingService{}, stubDB{}, stubTokens{}, stubRevocations{}, slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request(http.MethodGet, "/notifications", true))
	lines := parseLines(t, &buf)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, rec.Body.String())

	line := accessLine(t, lines)
	assert.Equal(t, "ERROR", line["level"])
	assert.EqualValues(t, http.StatusInternalServerError, line["status"])

	causes := causeLines(lines)
	require.Len(t, causes, 1)
	assert.Equal(t, "panic serving request", causes[0]["msg"])
	assert.Equal(t, sentRequestID, causes[0]["request_id"])
	assert.Equal(t, "boom", causes[0]["panic"])
	assert.NotEmpty(t, causes[0]["stack"], "the stack is one field of the one line")
}

func TestAPanicAfterTheAnswerStartedWritesNoSecondStatus(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := accessLog(log)(recoverPanics(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/notifications", nil))
	lines := parseLines(t, &buf)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "partial", rec.Body.String(), "no 500 body is appended to an answer already under way")
	assert.EqualValues(t, http.StatusAccepted, accessLine(t, lines)["status"])
	require.Len(t, causeLines(lines), 1)
}
