// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Test harness for the HTTP transport: the full router over a real
//   order.Service and the ordertest fakes, with T, K and C at D-049's values,
//   plus request, response and log helpers.
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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/gen"
	"foc/order-service/internal/httpapi"
	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

const (
	expiryPeriod  = 2 * time.Hour  // T (D-049)
	maxActive     = 2              // K (D-049)
	confirmWindow = 24 * time.Hour // C (D-049)

	supplierLocation = "COM3, level 1 — beside the lift"
	msgNoIdentity    = "missing, invalid or expired access token"
)

func startTime() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }

func student() order.Caller { return order.Caller{UserID: uuid.New(), Role: order.RoleStudent} }

func admin() order.Caller { return order.Caller{UserID: uuid.New(), Role: order.RoleAdmin} }

// harness is the full router over a real order.Service whose ports are the
// in-memory fakes, so each test drives HTTP and inspects the fakes.
type harness struct {
	repo      *ordertest.Repository
	credit    *ordertest.Credit
	suppliers *ordertest.Suppliers
	clock     *ordertest.Clock
	db        *fakePinger
	supplier  order.Supplier
	logs      *syncBuffer
	log       *slog.Logger
	tokens    *fakeTokens
	revoked   *fakeRevocations
	router    http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	sup := order.Supplier{
		ID:                  uuid.New(),
		Name:                "Spinelli Coffee",
		Building:            "COM3",
		Floor:               "1",
		LocationDescription: "beside the lift",
		Available:           true,
	}
	logs := &syncBuffer{}
	h := &harness{
		repo:      ordertest.NewRepository(),
		credit:    ordertest.NewCredit(),
		suppliers: ordertest.NewSuppliers(sup),
		clock:     ordertest.NewClock(startTime()),
		db:        &fakePinger{},
		supplier:  sup,
		logs:      logs,
		log:       slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		tokens:    &fakeTokens{},
		revoked:   &fakeRevocations{},
	}
	h.router = h.routerWith(ports{})
	return h
}

// ports replaces some of the harness's fakes in routerWith; a nil field keeps
// the harness's own.
type ports struct {
	repo   order.Repository
	dir    order.SupplierDirectory
	db     httpapi.Pinger
	credit order.CreditClient
	// AI-generated (edited by James Teo).
	revocations httpapi.RevocationChecker
}

// routerWith builds another router over the harness's fakes, for a test that
// wraps one of them.
func (h *harness) routerWith(p ports) http.Handler {
	if p.repo == nil {
		p.repo = h.repo
	}
	if p.dir == nil {
		p.dir = h.suppliers
	}
	if p.db == nil {
		p.db = h.db
	}
	if p.credit == nil {
		p.credit = h.credit
	}
	// AI-generated (edited by James Teo).
	if p.revocations == nil {
		p.revocations = h.revoked
	}
	svc := order.NewService(p.repo, p.credit, p.dir, h.clock, order.Config{
		ExpiryPeriod:  expiryPeriod,
		MaxActive:     maxActive,
		ConfirmWindow: confirmWindow,
	}, h.log)
	return httpapi.NewRouter(svc, p.db, h.tokens, p.revocations, h.log)
}

type reqOption func(*http.Request)

// as sends an access token for c, which fakeTokens accepts (D-056).
func as(c order.Caller) reqOption {
	return withToken(tokenFor(c.UserID.String(), string(c.Role)))
}

// withToken sends token as the bearer token.
func withToken(token string) reqOption {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// tokenFor builds a token fakeTokens reads back: "tok|<sub>|<role>|<jti>".
// The real verifier is tested in internal/auth.
func tokenFor(sub, role string) string { return "tok|" + sub + "|" + role + "|" + uuid.NewString() }

// fakeTokens accepts the tokens tokenFor builds, answers keysDown with
// auth.ErrKeysUnavailable, and turns anything else away as a bad signature.
type fakeTokens struct {
	keysDown atomic.Bool
}

func (f *fakeTokens) Verify(_ context.Context, token string) (auth.Claims, error) {
	if f.keysDown.Load() {
		return auth.Claims{}, fmt.Errorf("%w: user-service down", auth.ErrKeysUnavailable)
	}
	parts := strings.Split(token, "|")
	if len(parts) != 4 || parts[0] != "tok" {
		return auth.Claims{}, auth.ErrInvalidSignature
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return auth.Claims{}, auth.ErrMalformed
	}
	return auth.Claims{UserID: id, Role: parts[2], TokenID: parts[3], IssuedAt: startTime()}, nil
}

// AI-generated (edited by James Teo).
// fakeRevocations reports the token ids in revoked as revoked. While down is
// set it fails every check as Redis being unreachable would, and while
// refusing is set, as Redis answering with an error reply would.
type fakeRevocations struct {
	mu       sync.Mutex
	revoked  map[string]bool
	down     bool
	refusing bool
}

func (f *fakeRevocations) Revoked(_ context.Context, c auth.Claims) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false, fmt.Errorf("read revocation keys: %w: dial tcp: connection refused", auth.ErrRedisUnreachable)
	}
	if f.refusing {
		return false, errors.New("read revocation keys: NOAUTH Authentication required")
	}
	return f.revoked[c.TokenID], nil
}

func (f *fakeRevocations) revoke(tokenID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revoked == nil {
		f.revoked = map[string]bool{}
	}
	f.revoked[tokenID] = true
}

func (f *fakeRevocations) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// AI-generated (edited by James Teo).
func (f *fakeRevocations) setRefusing(refusing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusing = refusing
}

func withRequestID(id string) reqOption {
	return func(r *http.Request) { r.Header.Set("X-Request-Id", id) }
}

func withHeader(name, value string) reqOption {
	return func(r *http.Request) { r.Header.Add(name, value) }
}

// do sends one request through h.router. body is nil, a string sent as is, an
// io.Reader, or a value marshalled to JSON.
func (h *harness) do(t *testing.T, method, target string, body any, opts ...reqOption) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, h.router, method, target, body, opts...)
}

func serve(t *testing.T, handler http.Handler, method, target string, body any, opts ...reqOption) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case io.Reader:
		rd = b
	default:
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// decodeAs decodes a JSON response body, checking its Content-Type first.
func decodeAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"), rec.Body.String())
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

// requireError checks the status and the contract's {"error": msg} body.
func requireError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	require.Equal(t, msg, decodeAs[gen.Error](t, rec).Error)
}

// errandJSON returns the errand object of an ErrandDetail response as raw
// JSON, to check which keys are present.
func errandJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	detail := decodeAs[map[string]any](t, rec)
	errand, ok := detail["errand"].(map[string]any)
	require.True(t, ok, rec.Body.String())
	return errand
}

// createBody is a valid POST / body for h's supplier.
func (h *harness) createBody() map[string]any {
	return map[string]any{
		"supplierId":       h.supplier.ID.String(),
		"itemDescription":  "1 x iced latte, less ice",
		"deliveryLocation": "COM3 Lobby",
		"deliveryNote":     "by the vending machine",
		"reward":           5,
	}
}

// create posts an errand for requester through the API.
func (h *harness) create(t *testing.T, requester order.Caller, opts ...reqOption) gen.ErrandDetail {
	t.Helper()
	rec := h.do(t, http.MethodPost, "/", h.createBody(), append([]reqOption{as(requester)}, opts...)...)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decodeAs[gen.ErrandDetail](t, rec)
}

// newOrder builds an errand in state as the service would have left it, for a
// test to adjust and store with h.repo.Put.
func (h *harness) newOrder(state order.State, requester order.Caller, courier *order.Caller) order.Order {
	now := h.clock.Now()
	o := order.Order{
		ID:               uuid.New(),
		RequesterID:      requester.UserID,
		Supplier:         order.SupplierSnapshot{ID: h.supplier.ID, Name: h.supplier.Name, Location: supplierLocation},
		ItemDescription:  "1 x iced latte",
		DeliveryLocation: "COM3 Lobby",
		DeliveryNote:     "by the vending machine",
		Reward:           5,
		State:            state,
		Version:          3,
		CreatedAt:        now,
		UpdatedAt:        now,
		StateChangedAt:   now,
	}
	if courier != nil {
		id := courier.UserID
		o.CourierID = &id
	}
	switch state {
	case order.StateOpen:
		expires := now.Add(expiryPeriod)
		o.ExpiresAt = &expires
	case order.StateDelivered:
		confirmBy := now.Add(confirmWindow)
		o.ConfirmBy = &confirmBy
	}
	return o
}

// put stores an errand in state and returns it.
func (h *harness) put(state order.State, requester order.Caller, courier *order.Caller) order.Order {
	o := h.newOrder(state, requester, courier)
	h.repo.Put(o)
	return o
}

// logLines returns every log line written so far, decoded.
func (h *harness) logLines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(h.logs.String()))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &line), sc.Text())
		out = append(out, line)
	}
	require.NoError(t, sc.Err())
	return out
}

// findLog returns the first log line match accepts, failing the test if none does.
func (h *harness) findLog(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	for _, line := range h.logLines(t) {
		if match(line) {
			return line
		}
	}
	require.FailNow(t, "no matching log line", h.logs.String())
	return nil
}

// accessLine finds the access-log line of the request with this id.
func (h *harness) accessLine(t *testing.T, requestID string) map[string]any {
	t.Helper()
	return h.findLog(t, func(l map[string]any) bool {
		return l["msg"] == "http request" && l["request_id"] == requestID
	})
}

// syncBuffer is a bytes.Buffer safe for the logger to write while a test reads.
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

// fakePinger is an httpapi.Pinger that answers err and records how long its
// context had left.
type fakePinger struct {
	mu       sync.Mutex
	err      error
	timeLeft time.Duration // -1 when the context had no deadline
}

func (p *fakePinger) Ping(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeLeft = -1
	if deadline, ok := ctx.Deadline(); ok {
		p.timeLeft = time.Until(deadline)
	}
	return p.err
}

func (p *fakePinger) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *fakePinger) lastTimeLeft() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.timeLeft
}

// panicPinger panics, standing in for any handler bug.
type panicPinger struct{}

func (panicPinger) Ping(context.Context) error { panic("database driver exploded") }

// deadlineDirectory wraps the supplier fake and records how long each lookup's
// context had left: create and amend look the supplier up first.
type deadlineDirectory struct {
	*ordertest.Suppliers
	mu       sync.Mutex
	timeLeft []time.Duration // -1 for a context without a deadline
}

func (d *deadlineDirectory) Supplier(ctx context.Context, id uuid.UUID) (order.Supplier, error) {
	left := time.Duration(-1)
	if deadline, ok := ctx.Deadline(); ok {
		left = time.Until(deadline)
	}
	d.mu.Lock()
	d.timeLeft = append(d.timeLeft, left)
	d.mu.Unlock()
	return d.Suppliers.Supplier(ctx, id)
}

func (d *deadlineDirectory) recorded() []time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]time.Duration(nil), d.timeLeft...)
}

// historyFails wraps the repository fake so reading history fails. Only
// GET /{errandId} and the re-read after an action read history, so every action
// itself still succeeds.
type historyFails struct {
	*ordertest.Repository
}

func (historyFails) History(context.Context, uuid.UUID) ([]order.HistoryEntry, error) {
	return nil, ordertest.ErrInjected
}
