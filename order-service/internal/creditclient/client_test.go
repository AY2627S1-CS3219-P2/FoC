// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: httptest tests for the credit-service client: every status branch of
//   reserve, adjust and release, the same-errandId retry, timeouts and headers
//   (D-047, D-056, D-057, D-102).
// Author review: James Teo — reviewed and verified.

package creditclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/order"
)

// shortTimeout stands in for the 2s per-attempt timeout where a test waits it
// out; long enough that a loaded machine still delivers the request first.
const shortTimeout = 200 * time.Millisecond

// callerLimit is the caller's own deadline in the timeout tests. It is far
// above two short attempts, so if attempts were not bounded the caller's
// deadline would end the first one and the test would see one attempt, not hang.
const callerLimit = 5 * time.Second

// outcome is how the domain reads a credit call's error (ports.go).
type outcome int

const (
	applied      outcome = iota // nil
	insufficient                // order.ErrInsufficientCredits
	rejected                    // order.ErrCreditRejected: a definite refusal
	unknown                     // any other error: the outcome is not known
)

// testToken stands in for the requester's access token on the request context.
const testToken = "requester-access-token"

// tokenCtx is a context carrying testToken, as the HTTP middleware leaves it (D-056).
func tokenCtx() context.Context { return auth.ContextWithToken(context.Background(), testToken) }

func assertOutcome(t *testing.T, want outcome, err error) {
	t.Helper()
	switch want {
	case applied:
		assert.NoError(t, err)
	case insufficient:
		assert.ErrorIs(t, err, order.ErrInsufficientCredits)
		assert.NotErrorIs(t, err, order.ErrCreditRejected)
	case rejected:
		assert.ErrorIs(t, err, order.ErrCreditRejected)
		assert.NotErrorIs(t, err, order.ErrInsufficientCredits)
	case unknown:
		require.Error(t, err)
		assert.NotErrorIs(t, err, order.ErrInsufficientCredits)
		assert.NotErrorIs(t, err, order.ErrCreditRejected, "an unknown outcome must not read as a definite refusal")
	}
}

func newReservation() order.Reservation {
	return order.Reservation{
		ErrandID:      uuid.New(),
		RequesterID:   uuid.New(),
		Amount:        15,
		CorrelationID: "req-create-1",
	}
}

func TestReserveSendsTheRequestersReservation(t *testing.T) {
	r := newReservation()
	srv, s := serve(t, respond(http.StatusCreated, `{}`))

	// A trailing slash on the base URL must not double up in the path.
	err := New(srv.URL+"/", srv.Client(), nil).Reserve(tokenCtx(), r)

	require.NoError(t, err)
	reqs := s.requests()
	require.Len(t, reqs, 1)
	got := reqs[0]
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/api/v1/credits/reservations", got.path)
	assert.Equal(t, "application/json", got.header.Get("Content-Type"))
	assert.Equal(t, "Bearer "+testToken, got.header.Get("Authorization"), "the requester's own token is forwarded")
	assert.Empty(t, got.header.Get("X-User-Id"), "no gateway-style identity header")
	assert.Equal(t, r.CorrelationID, got.header.Get("X-Request-Id"))
	assert.JSONEq(t, fmt.Sprintf(`{"errandId":%q,"amount":15}`, r.ErrandID), string(got.body))
}

func TestReserveOutcomes(t *testing.T) {
	tests := []struct {
		name         string
		steps        func(t *testing.T) []step
		want         outcome
		wantAttempts int32
	}{
		{name: "201 reserves", steps: answers(http.StatusCreated), want: applied, wantAttempts: 1},
		// 200 is credit-service's answer to a request identical to a hold it
		// already made (D-073).
		{name: "200 reserves: the hold already matches this request", steps: answers(http.StatusOK), want: applied, wantAttempts: 1},
		{name: "422 is insufficient credits", steps: answers(http.StatusUnprocessableEntity), want: insufficient, wantAttempts: 1},
		{name: "400 is a refusal", steps: answers(http.StatusBadRequest), want: rejected, wantAttempts: 1},
		{name: "401 is a refusal", steps: answers(http.StatusUnauthorized), want: rejected, wantAttempts: 1},
		{name: "403 is a refusal", steps: answers(http.StatusForbidden), want: rejected, wantAttempts: 1},
		{name: "404 is a refusal", steps: answers(http.StatusNotFound), want: rejected, wantAttempts: 1},
		// A 409 is a hold that doesn't match this request, so it proves nothing
		// on either attempt (D-073).
		{name: "409 on the first attempt: whose hold it is, is unknown", steps: answers(http.StatusConflict), want: unknown, wantAttempts: 1},
		{name: "429 is not retried", steps: answers(http.StatusTooManyRequests), want: unknown, wantAttempts: 1},
		{name: "500, then 201", steps: answers(http.StatusInternalServerError, http.StatusCreated), want: applied, wantAttempts: 2},
		{name: "502, then 200: the first attempt had reserved", steps: answers(http.StatusBadGateway, http.StatusOK), want: applied, wantAttempts: 2},
		{name: "502, then 409: a hold that doesn't match is unknown", steps: answers(http.StatusBadGateway, http.StatusConflict), want: unknown, wantAttempts: 2},
		// AI-generated (edited by James Teo): the first attempt's own hold may be
		// what leaves too little for the retry, so its 422 proves nothing.
		{name: "503, then 422: the first attempt may have reserved", steps: answers(http.StatusServiceUnavailable, http.StatusUnprocessableEntity), want: unknown, wantAttempts: 2},
		{name: "500, then 401", steps: answers(http.StatusInternalServerError, http.StatusUnauthorized), want: rejected, wantAttempts: 2},
		{name: "500, then 429", steps: answers(http.StatusInternalServerError, http.StatusTooManyRequests), want: unknown, wantAttempts: 2},
		{name: "two 500s", steps: answers(http.StatusInternalServerError, http.StatusInternalServerError), want: unknown, wantAttempts: 2},
		{
			name:         "a dropped connection, then 201",
			steps:        func(t *testing.T) []step { return []step{drop(t), respond(http.StatusCreated, `{}`)} },
			want:         applied,
			wantAttempts: 2,
		},
		{
			name:         "a dropped connection, then 200: the first attempt had reserved",
			steps:        func(t *testing.T) []step { return []step{drop(t), respond(http.StatusOK, `{}`)} },
			want:         applied,
			wantAttempts: 2,
		},
		{
			name: "a dropped connection, then 409: a hold that doesn't match is unknown",
			steps: func(t *testing.T) []step {
				return []step{drop(t), respond(http.StatusConflict, `{"error":"reservation does not match"}`)}
			},
			want:         unknown,
			wantAttempts: 2,
		},
		// AI-generated (edited by James Teo).
		{
			name: "a dropped connection, then 422: the first attempt may have reserved",
			steps: func(t *testing.T) []step {
				return []step{drop(t), respond(http.StatusUnprocessableEntity, `{"error":"insufficient credits"}`)}
			},
			want:         unknown,
			wantAttempts: 2,
		},
		{
			name:         "two dropped connections",
			steps:        func(t *testing.T) []step { return []step{drop(t), drop(t)} },
			want:         unknown,
			wantAttempts: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serve(t, tc.steps(t)...)
			client, attempts := countingClient(srv)

			err := New(srv.URL, client, nil).Reserve(context.Background(), newReservation())

			assertOutcome(t, tc.want, err)
			assert.Equal(t, tc.wantAttempts, attempts.Load())
		})
	}
}

func TestReserveRetriesTheSameReservation(t *testing.T) {
	tests := []struct {
		name  string
		steps func(t *testing.T) []step
	}{
		{name: "after a 500", steps: answers(http.StatusInternalServerError, http.StatusCreated)},
		// The retry finds the lost first attempt's hold, which credit-service
		// answers with 200 (D-073).
		{name: "after a dropped connection", steps: func(t *testing.T) []step {
			return []step{drop(t), respond(http.StatusOK, `{}`)}
		}},
		{name: "after a timeout", steps: func(*testing.T) []step {
			return []step{hang(), respond(http.StatusOK, `{}`)}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReservation()
			srv, s := serve(t, tc.steps(t)...)
			c := New(srv.URL, srv.Client(), nil)
			c.attemptTimeout = shortTimeout
			ctx, cancel := context.WithTimeout(tokenCtx(), callerLimit)
			defer cancel()

			require.NoError(t, c.Reserve(ctx, r))

			reqs := s.requests()
			require.Len(t, reqs, 2)
			for _, req := range reqs {
				assert.JSONEq(t, fmt.Sprintf(`{"errandId":%q,"amount":15}`, r.ErrandID), string(req.body),
					"a retry must repeat the errandId, or credit would hold twice")
				assert.Equal(t, "Bearer "+testToken, req.header.Get("Authorization"))
				assert.Equal(t, r.CorrelationID, req.header.Get("X-Request-Id"))
				assert.Equal(t, "application/json", req.header.Get("Content-Type"))
			}
		})
	}
}

func TestReserveBoundsEachAttempt(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
		want  outcome
	}{
		{name: "a timeout, then 201", steps: []step{hang(), respond(http.StatusCreated, `{}`)}, want: applied},
		{name: "a timeout, then 200: the lost first attempt had reserved", steps: []step{hang(), respond(http.StatusOK, `{}`)}, want: applied},
		{name: "two timeouts", steps: []step{hang(), hang()}, want: unknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serve(t, tc.steps...)
			client, attempts := countingClient(srv)
			c := New(srv.URL, client, nil)
			c.attemptTimeout = shortTimeout
			ctx, cancel := context.WithTimeout(context.Background(), callerLimit)
			defer cancel()

			err := c.Reserve(ctx, newReservation())

			assertOutcome(t, tc.want, err)
			if tc.want == unknown {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			}
			assert.Equal(t, int32(2), attempts.Load())
		})
	}
}

func TestReserveNeverOutlivesTheCallersContext(t *testing.T) {
	srv, _ := serve(t, hang(), respond(http.StatusCreated, `{}`))
	client, attempts := countingClient(srv)
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()

	// The 2s per-attempt timeout is left as is: the caller's deadline is the
	// one that ends the wait.
	err := New(srv.URL, client, nil).Reserve(ctx, newReservation())

	assertOutcome(t, unknown, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), attempts.Load(), "no retry once the caller has given up")
}

func TestAdjustSendsTheNewRewardAndTheErrandsVersion(t *testing.T) {
	errandID, requesterID := uuid.New(), uuid.New()
	srv, s := serve(t, respond(http.StatusOK, `{}`))

	err := New(srv.URL+"/", srv.Client(), nil).Adjust(tokenCtx(), errandID, requesterID, 12, 3, "req-amend-1")

	require.NoError(t, err)
	reqs := s.requests()
	require.Len(t, reqs, 1)
	got := reqs[0]
	assert.Equal(t, http.MethodPatch, got.method)
	assert.Equal(t, "/api/v1/credits/reservations/"+errandID.String(), got.path)
	assert.Equal(t, "application/json", got.header.Get("Content-Type"))
	assert.Equal(t, "Bearer "+testToken, got.header.Get("Authorization"), "the requester's own token is forwarded")
	assert.Empty(t, got.header.Get("X-User-Id"), "no gateway-style identity header")
	opID, err := uuid.Parse(got.header.Get("X-Request-Id"))
	require.NoError(t, err, "X-Request-Id is the change's own UUID")
	assert.NotEqual(t, uuid.Nil, opID)
	assert.JSONEq(t, `{"newReward":12,"currVersion":3}`, string(got.body))
}

// credit-service keys each reward change on X-Request-Id, so every change,
// including one back to an earlier reward, gets its own UUID, and a caller's
// id is never sent, even one that is itself a UUID and is reused (D-081).
func TestAdjustSendsANewOperationIDForEachRewardChange(t *testing.T) {
	for _, corr := range []string{uuid.NewString(), "req-reused"} {
		t.Run(corr, func(t *testing.T) {
			srv, s := serve(t, respond(http.StatusOK, `{}`), respond(http.StatusOK, `{}`), respond(http.StatusOK, `{}`))
			c := New(srv.URL, srv.Client(), nil)
			errandID, requesterID := uuid.New(), uuid.New()

			for i, reward := range []int{20, 10, 20} {
				require.NoError(t, c.Adjust(tokenCtx(), errandID, requesterID, reward, i+1, corr))
			}

			reqs := s.requests()
			require.Len(t, reqs, 3)
			seen := map[uuid.UUID]bool{}
			for i, req := range reqs {
				sent := req.header.Get("X-Request-Id")
				assert.NotEqual(t, corr, sent, "change %d forwarded the caller's id", i+1)
				id, err := uuid.Parse(sent)
				require.NoError(t, err, "change %d sent a UUID", i+1)
				assert.NotEqual(t, uuid.Nil, id)
				assert.False(t, seen[id], "change %d reused an earlier change's id", i+1)
				seen[id] = true
			}
		})
	}
}

func TestAdjustLogsTheOperationIDWithTheCorrelationID(t *testing.T) {
	srv, s := serve(t, respond(http.StatusOK, `{}`))
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	require.NoError(t, New(srv.URL, srv.Client(), log).Adjust(tokenCtx(), uuid.New(), uuid.New(), 12, 3, "req-amend-1"))

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.Equal(t, "req-amend-1", line["request_id"])
	assert.Equal(t, s.requests()[0].header.Get("X-Request-Id"), line["operation_id"])
}

func TestAdjustOutcomes(t *testing.T) {
	tests := []struct {
		name  string
		steps func(t *testing.T) []step
		want  outcome
	}{
		{name: "200 applies", steps: answers(http.StatusOK), want: applied},
		{name: "422 is insufficient credits", steps: answers(http.StatusUnprocessableEntity), want: insufficient},
		{name: "400 is a refusal", steps: answers(http.StatusBadRequest), want: rejected},
		{name: "401 is a refusal", steps: answers(http.StatusUnauthorized), want: rejected},
		{name: "403 is a refusal", steps: answers(http.StatusForbidden), want: rejected},
		{name: "404 is a refusal", steps: answers(http.StatusNotFound), want: rejected},
		{name: "409 is a refusal", steps: answers(http.StatusConflict), want: rejected},
		{name: "429 is unknown", steps: answers(http.StatusTooManyRequests), want: unknown},
		{name: "500 is unknown, and not retried", steps: answers(http.StatusInternalServerError), want: unknown},
		{name: "503 is unknown, and not retried", steps: answers(http.StatusServiceUnavailable), want: unknown},
		{
			name:  "a dropped connection is unknown, and not retried",
			steps: func(t *testing.T) []step { return []step{drop(t)} },
			want:  unknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serve(t, tc.steps(t)...)
			client, attempts := countingClient(srv)

			err := New(srv.URL, client, nil).Adjust(context.Background(), uuid.New(), uuid.New(), 12, 3, "req-amend-1")

			assertOutcome(t, tc.want, err)
			assert.Equal(t, int32(1), attempts.Load(), "adjust is never retried here")
		})
	}
}

func TestAdjustBoundsTheAttempt(t *testing.T) {
	srv, _ := serve(t, hang(), respond(http.StatusOK, `{}`))
	client, attempts := countingClient(srv)
	c := New(srv.URL, client, nil)
	c.attemptTimeout = shortTimeout
	ctx, cancel := context.WithTimeout(context.Background(), callerLimit)
	defer cancel()

	err := c.Adjust(ctx, uuid.New(), uuid.New(), 12, 3, "req-amend-1")

	assertOutcome(t, unknown, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), attempts.Load(), "a timeout is not retried either")
}

func TestReserveLeavesOutAnEmptyRequestID(t *testing.T) {
	srv, s := serve(t, respond(http.StatusCreated, `{}`))
	r := newReservation()
	r.CorrelationID = ""

	require.NoError(t, New(srv.URL, srv.Client(), nil).Reserve(tokenCtx(), r))

	reqs := s.requests()
	require.Len(t, reqs, 1)
	assert.Empty(t, reqs[0].header.Values("X-Request-Id"))
	assert.Equal(t, "Bearer "+testToken, reqs[0].header.Get("Authorization"))
}

func TestReleaseIsKeyedOnTheEventID(t *testing.T) {
	// D-102: the release after a saved decrease carries the reward_set's
	// eventId as X-Request-Id, so credit-service treats the event as a repeat.
	errandID, eventID := uuid.New(), uuid.New()
	srv, s := serve(t, respond(http.StatusOK, `{}`))

	err := New(srv.URL, srv.Client(), nil).Release(tokenCtx(), errandID, uuid.New(), 3, 2, eventID, "req-amend-1")

	require.NoError(t, err)
	reqs := s.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, http.MethodPatch, reqs[0].method)
	assert.Equal(t, "/api/v1/credits/reservations/"+errandID.String(), reqs[0].path)
	assert.Equal(t, eventID.String(), reqs[0].header.Get("X-Request-Id"))
	assert.JSONEq(t, `{"newReward":3,"currVersion":2}`, string(reqs[0].body))
}

func TestReleaseOutcomesAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps func(t *testing.T) []step
		want  outcome
	}{
		{name: "200 applies", steps: answers(http.StatusOK), want: applied},
		{name: "409 is a refusal", steps: answers(http.StatusConflict), want: rejected},
		{name: "503 is unknown, and not retried", steps: answers(http.StatusServiceUnavailable), want: unknown},
		{name: "a dropped connection is unknown, and not retried", steps: func(t *testing.T) []step { return []step{drop(t)} }, want: unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serve(t, tc.steps(t)...)
			client, attempts := countingClient(srv)

			err := New(srv.URL, client, nil).Release(context.Background(), uuid.New(), uuid.New(), 3, 2, uuid.New(), "req-amend-1")

			assertOutcome(t, tc.want, err)
			assert.Equal(t, int32(1), attempts.Load())
		})
	}
}

func TestAdjustSendsAnOperationIDWithoutACorrelationID(t *testing.T) {
	srv, s := serve(t, respond(http.StatusOK, `{}`))

	require.NoError(t, New(srv.URL, srv.Client(), nil).Adjust(tokenCtx(), uuid.New(), uuid.New(), 20, 1, ""))

	_, err := uuid.Parse(s.requests()[0].header.Get("X-Request-Id"))
	assert.NoError(t, err)
}

// step answers one request. release is closed when the test ends, so a step
// that hangs can always return.
type step func(w http.ResponseWriter, r *http.Request, release <-chan struct{})

func respond(status int, body string) step {
	return func(w http.ResponseWriter, _ *http.Request, _ <-chan struct{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// answers is one plain answer per attempt, in order.
func answers(statuses ...int) func(*testing.T) []step {
	return func(*testing.T) []step {
		out := make([]step, 0, len(statuses))
		for _, status := range statuses {
			out = append(out, respond(status, fmt.Sprintf(`{"error":%q}`, http.StatusText(status))))
		}
		return out
	}
}

// hang never answers; it returns once the client has given up.
func hang() step {
	return func(_ http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
}

// drop closes the connection without answering: a transport error.
func drop(t *testing.T) step {
	return func(w http.ResponseWriter, _ *http.Request, _ <-chan struct{}) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}
}

type seenRequest struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// script answers the nth request with the nth step and records every request.
type script struct {
	t       *testing.T
	mu      sync.Mutex
	steps   []step
	seen    []seenRequest
	release chan struct{}
}

func (s *script) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("read request body: %v", err)
	}
	s.mu.Lock()
	n := len(s.seen)
	s.seen = append(s.seen, seenRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: body})
	s.mu.Unlock()
	if n >= len(s.steps) {
		http.Error(w, "no step scripted for this request", http.StatusTeapot)
		return
	}
	s.steps[n](w, r, s.release)
}

func (s *script) requests() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.seen...)
}

func serve(t *testing.T, steps ...step) (*httptest.Server, *script) {
	t.Helper()
	s := &script{t: t, steps: steps, release: make(chan struct{})}
	srv := httptest.NewServer(s)
	t.Cleanup(func() {
		close(s.release) // free any hanging step first, or Close waits on it
		srv.Close()
	})
	return srv, s
}

// countingClient counts the attempts the client makes, which a server cannot
// do for a request the client abandoned before it arrived.
func countingClient(srv *httptest.Server) (*http.Client, *atomic.Int32) {
	n := &atomic.Int32{}
	next := srv.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n.Add(1)
		return next.RoundTrip(r)
	})}, n
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
