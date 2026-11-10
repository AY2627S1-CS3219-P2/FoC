// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Table-driven tests through NewRouter of the JSON access line (level,
//   status, path without the query string, user_id) and of every 5xx logging
//   its cause once with the request id; of the panic recoverer and the response
//   wrapper; and of a real WebSocket upgrade behind the access log. No token,
//   ticket, message text or image bytes may reach the log (D-135).
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"foc/chat-service/internal/auth"
	"foc/chat-service/internal/chat"
	"foc/chat-service/internal/hub"
	"foc/chat-service/internal/media"
	"foc/chat-service/internal/ticket"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// logRequestID is the X-Request-Id every logged request is sent with.
	logRequestID = "req-log-test"
	testKeyID    = "test-key"
	// secretText and mediaMarker are a message's text and part of an image's
	// bytes, which must never reach the log.
	secretText  = "meet me at the library 7f3a"
	mediaMarker = "media-bytes-c41b"
)

// syncBuffer collects the log lines a server's goroutines write while the
// test reads them.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// decodeLines decodes each line of log, failing on one that is not a JSON
// object.
func decodeLines(log string) ([]map[string]any, error) {
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(log), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return nil, fmt.Errorf("log line %q is not one JSON object: %w", raw, err)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func logLines(t *testing.T, log string) []map[string]any {
	t.Helper()
	lines, err := decodeLines(log)
	require.NoError(t, err)
	return lines
}

// withMsg returns the lines whose msg is msg.
func withMsg(lines []map[string]any, msg string) []map[string]any {
	var out []map[string]any
	for _, line := range lines {
		if line["msg"] == msg {
			out = append(out, line)
		}
	}
	return out
}

// requireLine waits up to 5 s for a line of logs that match accepts, and
// returns it: a WebSocket's lines are written by the server's goroutines.
func requireLine(t *testing.T, logs *syncBuffer, match func(map[string]any) bool) map[string]any {
	t.Helper()
	var found map[string]any
	ok := assert.Eventually(t, func() bool {
		lines, err := decodeLines(logs.String())
		if err != nil {
			return false
		}
		for _, line := range lines {
			if match(line) {
				found = line
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
	if !ok {
		t.Fatalf("no matching log line in:\n%s", logs.String())
	}
	return found
}

// fixture is the signing key every logged request's token is minted with, and
// the key set that serves it as user-service would.
type fixture struct {
	key     *rsa.PrivateKey
	jwksURL string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": testKeyID,
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return fixture{key: key, jwksURL: srv.URL}
}

// mintToken signs an access token for userID that auth.Verifier accepts.
func (fx fixture) mintToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":  "campusrun-user-service",
		"aud":  "campusrun-api",
		"sub":  userID.String(),
		"jti":  uuid.NewString(),
		"iat":  now.Unix(),
		"exp":  now.Add(time.Hour).Unix(),
		"typ":  "access",
		"role": "STUDENT",
	})
	token.Header["kid"] = testKeyID
	signed, err := token.SignedString(fx.key)
	require.NoError(t, err)
	return signed
}

// fakeRedis stands in for the chat Redis behind the ticket store and the
// shared Redis behind the revocation check. A set err is every command's
// answer; with none, a command fails as go-redis does once its context is done.
type fakeRedis struct {
	redis.UniversalClient // only the methods below are called
	err                   error
	mu                    sync.Mutex
	values                map[string]string
}

func newFakeRedis(err error) *fakeRedis {
	return &fakeRedis{err: err, values: map[string]string{}}
}

func (f *fakeRedis) fail(ctx context.Context) error {
	if f.err != nil {
		return f.err
	}
	return ctx.Err()
}

func (f *fakeRedis) Set(ctx context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	if err := f.fail(ctx); err != nil {
		return redis.NewStatusResult("", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = fmt.Sprint(value)
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeRedis) GetDel(ctx context.Context, key string) *redis.StringCmd {
	if err := f.fail(ctx); err != nil {
		return redis.NewStringResult("", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	delete(f.values, key)
	return redis.NewStringResult(v, nil)
}

// MGet finds no revocation or suspension key.
func (f *fakeRedis) MGet(ctx context.Context, keys ...string) *redis.SliceCmd {
	if err := f.fail(ctx); err != nil {
		return redis.NewSliceResult(nil, err)
	}
	return redis.NewSliceResult(make([]any, len(keys)), nil)
}

// failingRepo is a mockRepo whose conversation list and message saves can be
// made to fail, and its list to panic. A set listErr is returned even once the
// request's context is done; with none, the list fails as pgx does, with the
// context's error once it is done.
type failingRepo struct {
	*mockRepo
	listErr   error
	saveErr   error
	panicWith any
}

func (f *failingRepo) ListConversationsByUser(ctx context.Context, userID uuid.UUID, errandIDFilter *uuid.UUID) ([]*chat.Conversation, error) {
	if f.panicWith != nil {
		panic(f.panicWith)
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.mockRepo.ListConversationsByUser(ctx, userID, errandIDFilter)
}

func (f *failingRepo) SaveMessage(ctx context.Context, msg *chat.Message) (*chat.Message, bool, error) {
	if f.saveErr != nil {
		return nil, false, f.saveErr
	}
	return f.mockRepo.SaveMessage(ctx, msg)
}

// logged is one request through NewRouter and how the dependencies it reaches
// answer it. In target, "{conversation}" stands for the caller's conversation
// and "{ticket}" for a ticket issued to the caller.
type logged struct {
	method, target string
	token          bool  // send the caller's access token
	text           bool  // send a text message holding secretText
	image          bool  // send a PNG image holding mediaMarker
	upgrade        bool  // send a WebSocket handshake's headers
	hangUp         bool  // the client is gone before the request is served
	listErr        error // what the conversation list answers
	saveErr        error // what saving a message answers
	panicWith      any   // what the conversation list panics with
	authRedisErr   error // what the shared Redis answers the revocation check
	chatRedisErr   error // what the chat Redis answers the ticket store
}

// served is a request's answer, the log it left, and what that log must never
// hold.
type served struct {
	rec     *httptest.ResponseRecorder
	log     string
	target  string
	userID  uuid.UUID
	secrets []string
}

func serveLogged(t *testing.T, fx fixture, req logged) served {
	t.Helper()
	var logs syncBuffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))

	userID, convID := uuid.New(), uuid.New()
	token := fx.mintToken(t, userID)
	secrets := []string{token, secretText, mediaMarker}

	var authRedis redis.UniversalClient
	if req.authRedisErr != nil {
		authRedis = newFakeRedis(req.authRedisErr)
	}
	verifier := auth.NewVerifier(fx.jwksURL, authRedis)
	// Load the signing keys first: a hung-up request's context cannot fetch them.
	_, _ = verifier.Verify(context.Background(), token)

	tickets := ticket.NewStore(newFakeRedis(req.chatRedisErr))
	target := strings.ReplaceAll(req.target, "{conversation}", convID.String())
	if strings.Contains(target, "{ticket}") {
		tk, err := tickets.IssueTicket(context.Background(), userID)
		require.NoError(t, err)
		target = strings.ReplaceAll(target, "{ticket}", url.QueryEscape(tk))
		secrets = append(secrets, tk)
	}
	if _, query, ok := strings.Cut(target, "?"); ok {
		secrets = append(secrets, query)
	}

	repo := &failingRepo{mockRepo: newMockRepo(), listErr: req.listErr, saveErr: req.saveErr, panicWith: req.panicWith}
	repo.convs[convID] = &chat.Conversation{
		ID: convID, ErrandID: uuid.New(), RequesterID: userID, CourierID: uuid.New(), Status: chat.StatusActive,
	}
	mediaStore, err := media.NewStorage(t.TempDir())
	require.NoError(t, err)
	handler := NewHandler(chat.NewService(repo, nil, nil), tickets, mediaStore, log)
	router := NewRouter(handler, hub.NewHub(nil, nil, nil, log), verifier, log)

	body, contentType := requestBody(t, req)
	r := httptest.NewRequest(req.method, target, body)
	r.Header.Set("X-Request-Id", logRequestID)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if req.token {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if req.upgrade {
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	}
	if req.hangUp {
		ctx, cancel := context.WithCancel(r.Context())
		cancel()
		r = r.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	return served{rec: rec, log: logs.String(), target: target, userID: userID, secrets: secrets}
}

// requestBody is the text message or the multipart image req sends, if any.
func requestBody(t *testing.T, req logged) (io.Reader, string) {
	t.Helper()
	switch {
	case req.text:
		b, err := json.Marshal(map[string]string{"client_message_id": uuid.NewString(), "text": secretText})
		require.NoError(t, err)
		return bytes.NewReader(b), "application/json"
	case req.image:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		require.NoError(t, mw.WriteField("client_message_id", uuid.NewString()))
		part, err := mw.CreateFormFile("image", "photo.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("\x89PNG\r\n\x1a\n" + mediaMarker))
		require.NoError(t, err)
		require.NoError(t, mw.Close())
		return &buf, mw.FormDataContentType()
	default:
		return nil, ""
	}
}

func TestEveryRequestGetsOneAccessLine(t *testing.T) {
	fx := newFixture(t)
	redisDown := errors.New("dial tcp 10.0.0.9:6379: connect: connection refused")

	cases := []struct {
		name       string
		req        logged
		wantStatus int
		wantLevel  string
		wantUser   bool // the verified caller is named on the line
	}{
		{name: "a health probe", req: logged{method: http.MethodGet, target: "/health"},
			wantStatus: http.StatusOK, wantLevel: "INFO"},
		{name: "a list filtered by its query string", req: logged{method: http.MethodGet, target: "/conversations?errandId=" + uuid.NewString(), token: true},
			wantStatus: http.StatusOK, wantLevel: "INFO", wantUser: true},
		{name: "a list under the /api/chat prefix", req: logged{method: http.MethodGet, target: "/api/chat/conversations", token: true},
			wantStatus: http.StatusOK, wantLevel: "INFO", wantUser: true},
		{name: "a list without a token", req: logged{method: http.MethodGet, target: "/conversations"},
			wantStatus: http.StatusUnauthorized, wantLevel: "INFO"},
		{name: "a text message", req: logged{method: http.MethodPost, target: "/conversations/{conversation}/messages", token: true, text: true},
			wantStatus: http.StatusOK, wantLevel: "INFO", wantUser: true},
		{name: "an image", req: logged{method: http.MethodPost, target: "/conversations/{conversation}/messages", token: true, image: true},
			wantStatus: http.StatusOK, wantLevel: "INFO", wantUser: true},
		{name: "a ticket that cannot be redeemed", req: logged{method: http.MethodGet, target: "/ws?ticket=unknown-ticket-9e1d"},
			wantStatus: http.StatusUnauthorized, wantLevel: "INFO"},
		{name: "a redeemed ticket without a WebSocket handshake", req: logged{method: http.MethodGet, target: "/api/chat/ws?ticket={ticket}"},
			wantStatus: http.StatusBadRequest, wantLevel: "INFO", wantUser: true},
		{name: "a list the database fails", req: logged{method: http.MethodGet, target: "/conversations", token: true, listErr: errors.New("connection refused")},
			wantStatus: http.StatusInternalServerError, wantLevel: "ERROR", wantUser: true},
		{name: "a handler panic", req: logged{method: http.MethodGet, target: "/conversations", token: true, panicWith: "boom"},
			wantStatus: http.StatusInternalServerError, wantLevel: "ERROR", wantUser: true},
		{name: "a token check while Redis is down", req: logged{method: http.MethodGet, target: "/conversations", token: true, authRedisErr: redisDown},
			wantStatus: http.StatusServiceUnavailable, wantLevel: "ERROR"},
		{name: "a client that hung up on a list that then failed", req: logged{method: http.MethodGet, target: "/conversations", token: true, hangUp: true},
			wantStatus: http.StatusInternalServerError, wantLevel: "WARN", wantUser: true},
		{name: "a client that hung up on a health probe", req: logged{method: http.MethodGet, target: "/health", hangUp: true},
			wantStatus: http.StatusOK, wantLevel: "WARN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := serveLogged(t, fx, tc.req)

			require.Equal(t, tc.wantStatus, s.rec.Code, s.rec.Body.String())
			access := withMsg(logLines(t, s.log), "http request")
			require.Len(t, access, 1, s.log)
			line := access[0]
			assert.Equal(t, tc.wantLevel, line["level"])
			assert.Equal(t, logRequestID, line["request_id"])
			assert.Equal(t, tc.req.method, line["method"])
			path, _, _ := strings.Cut(s.target, "?")
			assert.Equal(t, path, line["path"])
			assert.EqualValues(t, tc.wantStatus, line["status"])
			assert.IsType(t, float64(0), line["duration_ms"])
			if tc.wantUser {
				assert.Equal(t, s.userID.String(), line["user_id"])
			} else {
				assert.NotContains(t, line, "user_id")
			}
			for _, secret := range s.secrets {
				assert.NotContains(t, s.log, secret)
			}
		})
	}
}

func TestEvery5xxLogsItsCauseOnce(t *testing.T) {
	fx := newFixture(t)
	redisDown := errors.New("dial tcp 10.0.0.9:6379: connect: connection refused")
	dbDown := errors.New("insert message: write tcp 10.0.0.5:5432: connection reset by peer")

	cases := []struct {
		name       string
		req        logged
		wantStatus int
		wantMsg    string // the cause line's msg; "" when no line but the access line may be written above INFO
		wantErr    string // part of the cause line's err; "" when not checked
	}{
		{
			name:       "a list the database fails",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, listErr: errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")},
			wantStatus: http.StatusInternalServerError, wantMsg: "request failed", wantErr: "connection refused",
		},
		{
			name:       "a list past the service's own deadline",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, listErr: fmt.Errorf("list conversations by user: %w", context.DeadlineExceeded)},
			wantStatus: http.StatusInternalServerError, wantMsg: "request failed", wantErr: "deadline exceeded",
		},
		{
			name:       "a list whose client hung up",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, hangUp: true},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "a list whose client hung up and whose cause is not the cancellation",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, hangUp: true, listErr: errors.New("conn closed")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "a text message the database fails to save",
			req:        logged{method: http.MethodPost, target: "/conversations/{conversation}/messages", token: true, text: true, saveErr: dbDown},
			wantStatus: http.StatusInternalServerError, wantMsg: "request failed", wantErr: "save text message",
		},
		{
			name:       "an image the database fails to save",
			req:        logged{method: http.MethodPost, target: "/conversations/{conversation}/messages", token: true, image: true, saveErr: dbDown},
			wantStatus: http.StatusInternalServerError, wantMsg: "request failed", wantErr: "save image message",
		},
		{
			name:       "a ticket the chat Redis cannot store",
			req:        logged{method: http.MethodPost, target: "/ws-ticket", token: true, chatRedisErr: redisDown},
			wantStatus: http.StatusInternalServerError, wantMsg: "request failed", wantErr: "issue ticket",
		},
		{
			name:       "a token check while the shared Redis is down",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, authRedisErr: redisDown},
			wantStatus: http.StatusServiceUnavailable,
			wantMsg:    "cannot check token revocation: Redis could not be reached", wantErr: "connection refused",
		},
		{
			name:       "a token check whose client hung up while the shared Redis is down",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, authRedisErr: redisDown, hangUp: true},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "an upgrade whose connection cannot be taken over",
			req:        logged{method: http.MethodGet, target: "/ws?ticket={ticket}", upgrade: true},
			wantStatus: http.StatusInternalServerError, wantMsg: "websocket upgrade refused", wantErr: "not supported",
		},
		{
			name:       "a handler panic",
			req:        logged{method: http.MethodGet, target: "/conversations", token: true, panicWith: "boom"},
			wantStatus: http.StatusInternalServerError, wantMsg: "panic serving request",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := serveLogged(t, fx, tc.req)

			require.Equal(t, tc.wantStatus, s.rec.Code, s.rec.Body.String())
			var access, causes []map[string]any
			for _, line := range logLines(t, s.log) {
				switch {
				case line["msg"] == "http request":
					access = append(access, line)
				case line["level"] == "ERROR":
					causes = append(causes, line)
				}
			}
			require.Len(t, access, 1, s.log)
			for _, secret := range s.secrets {
				assert.NotContains(t, s.log, secret)
			}
			if tc.wantMsg == "" {
				// A client that hung up is no fault of the service, whatever
				// the cause: the WARN access line is the only record of it.
				assert.Equal(t, "WARN", access[0]["level"], s.log)
				assert.Empty(t, causes, s.log)
				return
			}
			assert.Equal(t, "ERROR", access[0]["level"])
			require.Len(t, causes, 1, "the cause is logged exactly once: %s", s.log)
			assert.Equal(t, tc.wantMsg, causes[0]["msg"])
			assert.Equal(t, logRequestID, causes[0]["request_id"])
			if tc.wantErr != "" {
				assert.Contains(t, causes[0]["err"], tc.wantErr)
			}
		})
	}
}

func TestRecoverPanics(t *testing.T) {
	cases := []struct {
		name        string
		handler     http.HandlerFunc
		wantStatus  int
		wantBody    string // JSON; "" for an empty body
		wantPanic   string // the logged panic value; "" when no panic line may be written
		wantRepanic bool   // the panic must reach net/http
	}{
		{
			name:       "a panic before anything is sent is a JSON 500",
			handler:    func(http.ResponseWriter, *http.Request) { panic("boom") },
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}`,
			wantPanic:  "boom",
		},
		{
			name: "a panic after the status went out keeps that status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				panic(errors.New("late"))
			},
			wantStatus: http.StatusAccepted,
			wantPanic:  "late",
		},
		{
			name:        "an aborted response is panicked on to the server",
			handler:     func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) },
			wantRepanic: true,
		},
		{
			name:       "a handler that does not panic",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			wantStatus: http.StatusNoContent,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			// The router's order: the recoverer learns from the access log's
			// writer whether a status already went out.
			h := middleware.RequestID(accessLog(log)(recoverPanics(log)(tc.handler)))
			req := httptest.NewRequest(http.MethodGet, "/conversations", nil)
			req.Header.Set("X-Request-Id", "req-panic")
			rec := httptest.NewRecorder()

			if tc.wantRepanic {
				assert.PanicsWithValue(t, http.ErrAbortHandler, func() { h.ServeHTTP(rec, req) })
				assert.Empty(t, logs.String(), "net/http logs nothing for an aborted response")
				return
			}
			h.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantBody == "" {
				assert.Empty(t, rec.Body.String())
			} else {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
			}
			lines := logLines(t, logs.String())
			access := withMsg(lines, "http request")
			require.Len(t, access, 1)
			assert.EqualValues(t, tc.wantStatus, access[0]["status"])
			panics := withMsg(lines, "panic serving request")
			if tc.wantPanic == "" {
				assert.Empty(t, panics)
				return
			}
			require.Len(t, panics, 1)
			assert.Equal(t, "ERROR", panics[0]["level"])
			assert.Equal(t, "req-panic", panics[0]["request_id"])
			assert.Equal(t, tc.wantPanic, panics[0]["panic"])
			stack, _ := panics[0]["stack"].(string)
			assert.True(t, strings.HasPrefix(stack, "goroutine "), "the whole stack in one field, got %q", stack)
		})
	}
}

func TestStatusWriterPassesFlushAndHijackThrough(t *testing.T) {
	cases := []struct {
		name       string
		use        func(*statusWriter) error
		wantErr    error
		wantStatus int
		wantFlush  bool // the writer underneath was flushed
	}{
		{
			name:       "a flush reaches the writer underneath and sends 200",
			use:        func(w *statusWriter) error { w.Flush(); return nil },
			wantStatus: http.StatusOK,
			wantFlush:  true,
		},
		{
			name:       "a flush through http.ResponseController",
			use:        func(w *statusWriter) error { return http.NewResponseController(w).Flush() },
			wantStatus: http.StatusOK,
			wantFlush:  true,
		},
		{
			name: "a hijack where the connection cannot be taken over",
			use: func(w *statusWriter) error {
				_, _, err := w.Hijack()
				return err
			},
			wantErr: http.ErrNotSupported,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A ResponseRecorder can flush but cannot be hijacked.
			rec := httptest.NewRecorder()
			w := &statusWriter{ResponseWriter: rec}

			err := tc.use(w)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantStatus, w.status)
			assert.Equal(t, tc.wantFlush, rec.Flushed)
		})
	}
}

// A socket's close is logged only when its code is unexpected, and then by
// its code alone: the close reason is text the client chose.
func TestASocketCloseLogsOnlyAnUnexpectedCode(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte // the close frame the first client sends
	}{
		{name: "a normal close", payload: websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")},
		{name: "a close with no code", payload: []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs syncBuffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			wsHub := hub.NewHub(nil, nil, nil, log)
			go wsHub.Run(ctx)
			tickets := ticket.NewStore(newFakeRedis(nil))
			mediaStore, err := media.NewStorage(t.TempDir())
			require.NoError(t, err)
			handler := NewHandler(chat.NewService(newMockRepo(), wsHub, nil), tickets, mediaStore, log)
			srv := httptest.NewServer(NewRouter(handler, wsHub, auth.NewVerifier("http://127.0.0.1:1/jwks", nil), log))
			t.Cleanup(srv.Close)

			closeWith := func(payload []byte) {
				t.Helper()
				tk, err := tickets.IssueTicket(context.Background(), uuid.New())
				require.NoError(t, err)
				wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + url.QueryEscape(tk)
				conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
				require.NoError(t, err, "the upgrade failed; the log holds:\n%s", logs.String())
				defer conn.Close()
				require.NoError(t, conn.WriteControl(websocket.CloseMessage, payload, time.Now().Add(5*time.Second)))
				require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
				// The server answers a close once its read loop has seen it.
				_, _, err = conn.ReadMessage()
				var ce *websocket.CloseError
				require.ErrorAs(t, err, &ce, "the server never answered the close")
			}

			// The routine close first; the unexpected one's line then shows the
			// server has handled both.
			closeWith(tc.payload)
			const reason = "client-chosen reason 7f3a"
			closeWith(websocket.FormatCloseMessage(4000, reason))

			line := requireLine(t, &logs, func(line map[string]any) bool {
				return line["msg"] == "websocket read error"
			})
			assert.Equal(t, "WARN", line["level"])
			assert.Equal(t, float64(4000), line["close_code"])
			assert.Len(t, withMsg(logLines(t, logs.String()), "websocket read error"), 1, "a routine close was logged too:\n%s", logs.String())
			assert.NotContains(t, logs.String(), reason)
		})
	}
}

func TestAWebSocketUpgradeKeepsItsAccessLine(t *testing.T) {
	cases := []struct {
		name   string
		refuse bool // the hub's token check refuses the client's auth_refresh
	}{
		{name: "an upgraded socket carries messages both ways"},
		{name: "a refused auth_refresh closes the socket", refuse: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs syncBuffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			userID := uuid.New()
			const refreshToken = "refresh-token-5d2e"
			tokens := make(chan string, 1)
			verify := func(_ context.Context, token string) (uuid.UUID, error) {
				tokens <- token
				if tc.refuse {
					return uuid.Nil, auth.ErrExpired
				}
				return userID, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			wsHub := hub.NewHub(nil, nil, verify, log)
			go wsHub.Run(ctx)

			tickets := ticket.NewStore(newFakeRedis(nil))
			tk, err := tickets.IssueTicket(context.Background(), userID)
			require.NoError(t, err)
			mediaStore, err := media.NewStorage(t.TempDir())
			require.NoError(t, err)
			handler := NewHandler(chat.NewService(newMockRepo(), wsHub, nil), tickets, mediaStore, log)
			// No route here needs the verifier's key set.
			srv := httptest.NewServer(NewRouter(handler, wsHub, auth.NewVerifier("http://127.0.0.1:1/jwks", nil), log))
			t.Cleanup(srv.Close)

			wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket=" + url.QueryEscape(tk)
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Request-Id": {"req-ws"}})
			require.NoError(t, err, "the upgrade failed; the log holds:\n%s", logs.String())
			t.Cleanup(func() { _ = conn.Close() })
			assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

			// Client to server: the hub reads the client's auth_refresh.
			require.NoError(t, conn.WriteJSON(hub.ClientMessage{Type: "auth_refresh", Token: refreshToken}))
			select {
			case got := <-tokens:
				assert.Equal(t, refreshToken, got)
			case <-time.After(5 * time.Second):
				t.Fatal("the server never read the client's message")
			}

			if tc.refuse {
				_, _, err := conn.ReadMessage()
				require.Error(t, err)
				var netErr net.Error
				assert.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the server kept the socket open: %v", err)
				line := requireLine(t, &logs, func(line map[string]any) bool {
					return line["msg"] == "mid-session auth_refresh failed"
				})
				assert.Equal(t, "WARN", line["level"])
				assert.Equal(t, userID.String(), line["user_id"])
			} else {
				// Server to client. The hub registers the client only after
				// the upgrade, so the message goes out until it arrives.
				text := secretText
				msg := &chat.Message{ID: uuid.New(), SenderID: uuid.New(), Type: chat.MessageTypeText, TextContent: &text, CreatedAt: time.Now().UTC()}
				arrived := make(chan struct{})
				go func() {
					tick := time.NewTicker(10 * time.Millisecond)
					defer tick.Stop()
					for {
						_ = wsHub.BroadcastMessage(context.Background(), msg, userID)
						select {
						case <-arrived:
							return
						case <-tick.C:
						}
					}
				}()
				_, data, err := conn.ReadMessage()
				close(arrived)
				require.NoError(t, err)
				first, _, _ := bytes.Cut(data, []byte("\n"))
				var env struct {
					Type string `json:"type"`
					Data struct {
						Text string `json:"text_content"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(first, &env))
				assert.Equal(t, "message.created", env.Type)
				assert.Equal(t, secretText, env.Data.Text)
			}

			line := requireLine(t, &logs, func(line map[string]any) bool { return line["msg"] == "http request" })
			assert.Equal(t, "INFO", line["level"])
			assert.Equal(t, "req-ws", line["request_id"])
			assert.Equal(t, http.MethodGet, line["method"])
			assert.Equal(t, "/ws", line["path"])
			assert.EqualValues(t, http.StatusSwitchingProtocols, line["status"])
			assert.Equal(t, userID.String(), line["user_id"])
			all := logs.String()
			assert.Len(t, withMsg(logLines(t, all), "http request"), 1, all)
			for _, secret := range []string{tk, refreshToken, secretText} {
				assert.NotContains(t, all, secret)
			}
		})
	}
}
