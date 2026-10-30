// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: httptest tests for the supplier-service client: mapping, 404, retry
//   on 5xx, transport errors and timeouts, malformed answers, X-Request-Id
//   (D-047, D-052).
// Author review: James Teo — reviewed and verified.

package supplierclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/shuttle"
)

// shortTimeout stands in for the 2s per-attempt timeout where a test waits it
// out; long enough that a loaded machine still delivers the request first.
const shortTimeout = 200 * time.Millisecond

// callerLimit is the caller's own deadline in the timeout tests. It is far
// above two short attempts, so if attempts were not bounded the caller's
// deadline would end the first one and the test would see one attempt, not hang.
const callerLimit = 5 * time.Second

func TestSupplierMapsSupplierServicesAnswer(t *testing.T) {
	tests := []struct {
		name      string
		available bool
	}{
		{name: "available supplier", available: true},
		{name: "unavailable supplier", available: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			srv, s := serve(t, respond(http.StatusOK, supplierJSON(id, tc.available)))

			// A trailing slash on the base URL must not double up in the path.
			got, err := New(srv.URL+"/", srv.Client(), nil).Supplier(context.Background(), id)

			require.NoError(t, err)
			assert.Equal(t, order.Supplier{
				ID:                  id,
				Name:                "Hot Hideout",
				Building:            "Yusof Ishak House",
				Floor:               "1",
				LocationDescription: "Opp UHC bus stop",
				Available:           tc.available,
				// AI-generated (edited by PENDING): kept for ranking (D-089).
				Type:  "Food",
				Point: &shuttle.Point{Lat: 1.2988, Lon: 103.7756},
			}, got)
			reqs := s.requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, http.MethodGet, reqs[0].method)
			assert.Equal(t, "/suppliers/"+id.String(), reqs[0].path)
			assert.Equal(t, "application/json", reqs[0].header.Get("Accept"))
		})
	}
}

func TestSupplierOutcomes(t *testing.T) {
	type outcome int
	const (
		found outcome = iota
		notFound
		failed // supplier-service gave no usable answer
	)
	tests := []struct {
		name         string
		steps        func(t *testing.T, id uuid.UUID) []step
		want         outcome
		wantAttempts int32
	}{
		{
			name: "404 is not found, and not retried",
			steps: func(*testing.T, uuid.UUID) []step {
				return []step{respond(http.StatusNotFound, `{"error":"supplier not found"}`)}
			},
			want:         notFound,
			wantAttempts: 1,
		},
		{
			name: "500 then 200: the retry answers",
			steps: func(_ *testing.T, id uuid.UUID) []step {
				return []step{respond(http.StatusInternalServerError, `{"error":"internal server error"}`), respond(http.StatusOK, supplierJSON(id, true))}
			},
			want:         found,
			wantAttempts: 2,
		},
		{
			name: "503 then 404",
			steps: func(*testing.T, uuid.UUID) []step {
				return []step{respond(http.StatusServiceUnavailable, ""), respond(http.StatusNotFound, "")}
			},
			want:         notFound,
			wantAttempts: 2,
		},
		{
			name: "two 500s",
			steps: func(*testing.T, uuid.UUID) []step {
				return []step{respond(http.StatusInternalServerError, ""), respond(http.StatusInternalServerError, "")}
			},
			want:         failed,
			wantAttempts: 2,
		},
		{
			name: "a dropped connection, then 200",
			steps: func(t *testing.T, id uuid.UUID) []step {
				return []step{drop(t), respond(http.StatusOK, supplierJSON(id, true))}
			},
			want:         found,
			wantAttempts: 2,
		},
		{
			name:         "two dropped connections",
			steps:        func(t *testing.T, _ uuid.UUID) []step { return []step{drop(t), drop(t)} },
			want:         failed,
			wantAttempts: 2,
		},
		{
			name:         "malformed JSON is not retried",
			steps:        func(*testing.T, uuid.UUID) []step { return []step{respond(http.StatusOK, `{"id": "`)} },
			want:         failed,
			wantAttempts: 1,
		},
		{
			name: "an answer without is_available",
			steps: func(_ *testing.T, id uuid.UUID) []step {
				return []step{respond(http.StatusOK, fmt.Sprintf(`{"id":%q,"name":"Hot Hideout"}`, id))}
			},
			want:         failed,
			wantAttempts: 1,
		},
		{
			name: "an answer about another supplier",
			steps: func(*testing.T, uuid.UUID) []step {
				return []step{respond(http.StatusOK, supplierJSON(uuid.New(), true))}
			},
			want:         failed,
			wantAttempts: 1,
		},
		{
			name:         "an unexpected 400 is not retried",
			steps:        func(*testing.T, uuid.UUID) []step { return []step{respond(http.StatusBadRequest, "")} },
			want:         failed,
			wantAttempts: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			srv, _ := serve(t, tc.steps(t, id)...)
			client, attempts := countingClient(srv)

			got, err := New(srv.URL, client, nil).Supplier(context.Background(), id)

			switch tc.want {
			case found:
				require.NoError(t, err)
				assert.Equal(t, id, got.ID)
			case notFound:
				assert.ErrorIs(t, err, order.ErrSupplierNotFound)
			case failed:
				require.Error(t, err)
				assert.NotErrorIs(t, err, order.ErrSupplierNotFound, "only a 404 means the supplier does not exist")
			}
			assert.Equal(t, tc.wantAttempts, attempts.Load())
		})
	}
}

func TestSupplierBoundsEachAttempt(t *testing.T) {
	t.Run("a timeout, then 200", func(t *testing.T) {
		id := uuid.New()
		srv, _ := serve(t, hang(), respond(http.StatusOK, supplierJSON(id, true)))
		client, attempts := countingClient(srv)
		c := New(srv.URL, client, nil)
		c.attemptTimeout = shortTimeout
		ctx, cancel := context.WithTimeout(context.Background(), callerLimit)
		defer cancel()

		got, err := c.Supplier(ctx, id)

		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
		assert.Equal(t, int32(2), attempts.Load())
	})
	t.Run("two timeouts", func(t *testing.T) {
		srv, _ := serve(t, hang(), hang())
		client, attempts := countingClient(srv)
		c := New(srv.URL, client, nil)
		c.attemptTimeout = shortTimeout
		ctx, cancel := context.WithTimeout(context.Background(), callerLimit)
		defer cancel()

		_, err := c.Supplier(ctx, uuid.New())

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.NotErrorIs(t, err, order.ErrSupplierNotFound)
		assert.Equal(t, int32(2), attempts.Load(), "a timeout is retried once")
	})
	t.Run("never past the caller's context", func(t *testing.T) {
		srv, _ := serve(t, hang(), respond(http.StatusOK, ""))
		client, attempts := countingClient(srv)
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()

		// The 2s per-attempt timeout is left as is: the caller's deadline is
		// the one that ends the wait.
		_, err := New(srv.URL, client, nil).Supplier(ctx, uuid.New())

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, int32(1), attempts.Load(), "no retry once the caller has given up")
	})
}

func TestSupplierForwardsTheCorrelationID(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{name: "forwarded when the request has one", ctx: order.WithCorrelationID(context.Background(), "req-42"), want: []string{"req-42"}},
		{name: "left out when it has none", ctx: context.Background(), want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			// Two attempts, so the retry is checked too.
			srv, s := serve(t, respond(http.StatusBadGateway, ""), respond(http.StatusOK, supplierJSON(id, true)))

			_, err := New(srv.URL, srv.Client(), nil).Supplier(tc.ctx, id)

			require.NoError(t, err)
			reqs := s.requests()
			require.Len(t, reqs, 2)
			for _, r := range reqs {
				assert.Equal(t, tc.want, r.header.Values("X-Request-Id"))
			}
		})
	}
}

// supplierJSON is supplier-service's full supplierResponse, extra fields and all.
func supplierJSON(id uuid.UUID, available bool) string {
	return fmt.Sprintf(`{"id":%q,"name":"Hot Hideout","type":"Food","building":"Yusof Ishak House",`+
		`"floor":"1","location_description":"Opp UHC bus stop","latitude":1.2988,"longitude":103.7756,`+
		`"opening_time":"08:00","closing_time":"20:00","image_url":"","description":"",`+
		`"is_available":%t,"created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-01T00:00:00Z"}`, id, available)
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
}

// script answers the nth request with the nth step and records every request.
type script struct {
	mu      sync.Mutex
	steps   []step
	seen    []seenRequest
	release chan struct{}
}

func (s *script) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.seen)
	s.seen = append(s.seen, seenRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone()})
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
	s := &script{steps: steps, release: make(chan struct{})}
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

// AI-generated (edited by PENDING): D-089 keeps a position only when both
// halves of it came.
func TestSupplierWithoutAPositionHasNone(t *testing.T) {
	for _, drop := range []string{`"latitude":1.2988,`, `"longitude":103.7756,`} {
		id := uuid.New()
		body := strings.Replace(supplierJSON(id, true), drop, "", 1)
		srv, _ := serve(t, respond(http.StatusOK, body))

		got, err := New(srv.URL, srv.Client(), nil).Supplier(context.Background(), id)

		require.NoError(t, err)
		assert.Nil(t, got.Point, "without %s", drop)
		assert.Equal(t, "Food", got.Type)
	}
}
