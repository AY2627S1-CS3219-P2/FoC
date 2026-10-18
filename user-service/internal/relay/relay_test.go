// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Unit tests of the relay loop against an in-memory store and
//   publisher (draining, retries after failures, shutdown and defaults),
//   adapted from order-service/internal/relay to the standard testing package.
// Author review: James Teo — reviewed and verified.

package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// testDeadline bounds every wait in these tests; nothing here should take
// more than milliseconds.
const testDeadline = 5 * time.Second

// outcome is one record a fakeStore handed to the relay, with what the
// relay's publish callback returned for it.
type outcome struct {
	rec Record
	err error
}

// fakeStore is an in-memory Store: a queue of due records. It reports every
// claimed record on outcomes and every empty poll on idle.
type fakeStore struct {
	mu       sync.Mutex
	due      []Record
	failNext []error         // returned by the next calls, in order, before any claim
	delays   []time.Duration // the retryDelay of every call
	calls    []time.Time     // when every call began

	outcomes chan outcome
	idle     chan struct{}
}

func newFakeStore(due ...Record) *fakeStore {
	return &fakeStore{due: due, outcomes: make(chan outcome, 64), idle: make(chan struct{}, 1)}
}

func (s *fakeStore) ClaimNext(ctx context.Context, retryDelay time.Duration, publish func(ctx context.Context, r Record) error) (bool, error) {
	s.mu.Lock()
	s.calls = append(s.calls, time.Now())
	s.delays = append(s.delays, retryDelay)
	if len(s.failNext) > 0 {
		err := s.failNext[0]
		s.failNext = s.failNext[1:]
		s.mu.Unlock()
		return false, err
	}
	if len(s.due) == 0 {
		s.mu.Unlock()
		select {
		case s.idle <- struct{}{}:
		default:
		}
		return false, nil
	}
	rec := s.due[0]
	s.due = s.due[1:]
	s.mu.Unlock()

	err := publish(ctx, rec)
	s.outcomes <- outcome{rec: rec, err: err}
	return true, nil
}

func (s *fakeStore) retryDelays() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delays...)
}

// callTimes returns when each ClaimNext call began, in order.
func (s *fakeStore) callTimes() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.calls...)
}

// next returns the next claimed record's outcome.
func (s *fakeStore) next(t *testing.T) outcome {
	t.Helper()
	select {
	case o := <-s.outcomes:
		return o
	case <-time.After(testDeadline):
		t.Fatalf("no record was claimed within %s", testDeadline)
		return outcome{}
	}
}

// awaitIdle waits until the relay has polled and found nothing due.
func (s *fakeStore) awaitIdle(t *testing.T) {
	t.Helper()
	select {
	case <-s.idle:
	case <-time.After(testDeadline):
		t.Fatalf("the relay did not poll an empty store within %s", testDeadline)
	}
}

// publisherFunc adapts a function to Publisher.
type publisherFunc func(ctx context.Context, r Record) error

func (f publisherFunc) Publish(ctx context.Context, r Record) error { return f(ctx, r) }

func acceptAll(context.Context, Record) error { return nil }

// lockedBuffer is a bytes.Buffer safe to write from the relay's goroutine
// while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// entries parses the JSON log lines written so far.
func (b *lockedBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(b.buf.Bytes()))
	for sc.Scan() {
		var e map[string]any
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("log line %q is not JSON: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read log lines: %v", err)
	}
	return out
}

// withMessage returns the entries at level with message msg.
func withMessage(entries []map[string]any, level, msg string) []map[string]any {
	var out []map[string]any
	for _, e := range entries {
		if e["level"] == level && e["msg"] == msg {
			out = append(out, e)
		}
	}
	return out
}

func jsonLogger(w *lockedBuffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// runningRelay is a Relay whose Run is going in its own goroutine.
type runningRelay struct {
	t      *testing.T
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// runRelay starts r.Run in its own goroutine; the test's cleanup stops it.
func runRelay(t *testing.T, r *Relay) *runningRelay {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	rr := &runningRelay{t: t, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(rr.done)
		r.Run(ctx)
	}()
	t.Cleanup(rr.stop)
	return rr
}

// stop ends Run's context and waits for Run to return, failing the test if it
// outlives its context by testDeadline.
func (rr *runningRelay) stop() {
	rr.once.Do(func() {
		rr.cancel()
		select {
		case <-rr.done:
		case <-time.After(testDeadline):
			rr.t.Errorf("Run did not return within %s of its context ending", testDeadline)
		}
	})
}

// running reports whether Run has yet to return.
func (rr *runningRelay) running() bool {
	select {
	case <-rr.done:
		return false
	default:
		return true
	}
}

func newRecord(routingKey string) Record {
	return Record{
		EventID:       uuid.New(),
		RoutingKey:    routingKey,
		Payload:       []byte(`{"userId":"6f1c2a5e-0000-4000-8000-000000000002"}`),
		CorrelationID: "corr-" + routingKey,
	}
}

func TestRunDrainsDueRecordsBackToBack(t *testing.T) {
	var due []Record
	for range 5 {
		due = append(due, newRecord("user.registered"))
	}
	store := newFakeStore(due...)
	var mu sync.Mutex
	var published []uuid.UUID
	pub := publisherFunc(func(_ context.Context, r Record) error {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, r.EventID)
		return nil
	})
	// With an hour between polls, every record arrives inside the deadline only
	// if the relay claims the next one without waiting.
	rr := runRelay(t, New(store, pub, nil, Options{PollInterval: time.Hour}))

	for i, want := range due {
		got := store.next(t)
		if got.rec.EventID != want.EventID {
			t.Errorf("record %d claimed out of order: got %s, want %s", i, got.rec.EventID, want.EventID)
		}
		if got.err != nil {
			t.Errorf("record %d publish error = %v, want nil", i, got.err)
		}
	}
	store.awaitIdle(t)
	rr.stop()

	mu.Lock()
	defer mu.Unlock()
	want := make([]uuid.UUID, 0, len(due))
	for _, r := range due {
		want = append(want, r.EventID)
	}
	if !slices.Equal(published, want) {
		t.Fatalf("published = %v, want %v", published, want)
	}
}

func TestRunHandsPublishErrorsToTheStoreAndCarriesOn(t *testing.T) {
	errBroker := errors.New("broker unreachable")
	failing := newRecord("user.registered")
	failing.Attempts = 2
	healthy := newRecord("user.registered")
	store := newFakeStore(failing, healthy)
	pub := publisherFunc(func(_ context.Context, r Record) error {
		if r.EventID == failing.EventID {
			return errBroker
		}
		return nil
	})
	logs := &lockedBuffer{}
	rr := runRelay(t, New(store, pub, jsonLogger(logs), Options{PollInterval: time.Hour}))

	first := store.next(t)
	if first.rec.EventID != failing.EventID {
		t.Fatalf("first claim = %s, want %s", first.rec.EventID, failing.EventID)
	}
	if !errors.Is(first.err, errBroker) {
		t.Errorf("the store must receive the publisher's error: got %v", first.err)
	}
	second := store.next(t)
	if second.rec.EventID != healthy.EventID {
		t.Errorf("a failed publish must not stop the relay: second claim = %s, want %s", second.rec.EventID, healthy.EventID)
	}
	if second.err != nil {
		t.Errorf("second publish error = %v, want nil", second.err)
	}
	if !rr.running() {
		t.Error("the relay stopped after a failed publish")
	}
	rr.stop()

	failures := withMessage(logs.entries(t), "ERROR", "outbox publish failed; will retry")
	if len(failures) != 1 {
		t.Fatalf("failure log entries = %d, want 1: %v", len(failures), failures)
	}
	wantFields := map[string]any{
		"event_id":    failing.EventID.String(),
		"routing_key": failing.RoutingKey,
		"request_id":  failing.CorrelationID,
		"attempt":     float64(3), // this try, after two failures
		"error":       errBroker.Error(),
	}
	for field, want := range wantFields {
		if failures[0][field] != want {
			t.Errorf("failure log %q = %v, want %v", field, failures[0][field], want)
		}
	}

	published := withMessage(logs.entries(t), "INFO", "outbox event published")
	if len(published) != 1 {
		t.Fatalf("published log entries = %d, want 1: %v", len(published), published)
	}
	if published[0]["event_id"] != healthy.EventID.String() || published[0]["request_id"] != healthy.CorrelationID {
		t.Errorf("published log = %v, want event %s with request_id %s", published[0], healthy.EventID, healthy.CorrelationID)
	}
}

func TestRunKeepsPollingAfterStoreErrors(t *testing.T) {
	rec := newRecord("user.registered")
	store := newFakeStore(rec)
	store.failNext = []error{errors.New("connection reset"), errors.New("connection reset")}
	logs := &lockedBuffer{}
	rr := runRelay(t, New(store, publisherFunc(acceptAll), jsonLogger(logs), Options{PollInterval: time.Millisecond}))

	got := store.next(t)
	if got.rec.EventID != rec.EventID {
		t.Errorf("the relay must claim again after store errors: got %s, want %s", got.rec.EventID, rec.EventID)
	}
	if got.err != nil {
		t.Errorf("publish error = %v, want nil", got.err)
	}
	if !rr.running() {
		t.Error("the relay stopped after store errors")
	}
	rr.stop()

	failures := withMessage(logs.entries(t), "ERROR", "outbox claim failed; will retry")
	if len(failures) != 2 {
		t.Fatalf("claim failure log entries = %d, want 2", len(failures))
	}
	if failures[0]["error"] != "connection reset" {
		t.Errorf("claim failure log error = %v, want %q", failures[0]["error"], "connection reset")
	}
}

func TestRunBacksOffWhileTheStoreKeepsFailing(t *testing.T) {
	const (
		failures = 5
		// Small enough that the fifth pause, 16 times this, stays under
		// maxErrorPause, so every pause checked here is a full doubling.
		pollInterval = 10 * time.Millisecond
	)
	rec := newRecord("user.registered")
	store := newFakeStore(rec)
	for range failures {
		store.failNext = append(store.failNext, errors.New("connection refused"))
	}
	logs := &lockedBuffer{}
	rr := runRelay(t, New(store, publisherFunc(acceptAll), jsonLogger(logs), Options{PollInterval: pollInterval}))

	got := store.next(t)
	if got.rec.EventID != rec.EventID {
		t.Errorf("the relay must claim again once the store answers: got %s, want %s", got.rec.EventID, rec.EventID)
	}
	if got.err != nil {
		t.Errorf("publish error = %v, want nil", got.err)
	}
	rr.stop()

	calls := store.callTimes()
	if len(calls) < failures+1 {
		t.Fatalf("ClaimNext calls = %d, want five failed calls and the claim that worked", len(calls))
	}
	for i := 1; i <= failures; i++ {
		// Lower bounds only: a busy machine can stretch a pause but never
		// shorten it.
		want := pollInterval << (i - 1)
		if gap := calls[i].Sub(calls[i-1]); gap < want {
			t.Errorf("the pause after store error %d in a row = %s, want at least %s", i, gap, want)
		}
	}
	if got := len(withMessage(logs.entries(t), "ERROR", "outbox claim failed; will retry")); got != failures {
		t.Errorf("claim failure log entries = %d, want %d", got, failures)
	}
}

func TestNextErrorPause(t *testing.T) {
	tests := []struct {
		name         string
		prev         time.Duration
		pollInterval time.Duration
		want         time.Duration
	}{
		{name: "doubles the first pause", prev: DefaultPollInterval, pollInterval: DefaultPollInterval, want: 2 * DefaultPollInterval},
		{name: "doubles a longer pause", prev: 600 * time.Millisecond, pollInterval: DefaultPollInterval, want: 1200 * time.Millisecond},
		{name: "doubles up to the cap exactly", prev: maxErrorPause / 2, pollInterval: DefaultPollInterval, want: maxErrorPause},
		{name: "caps a doubling past maxErrorPause", prev: 1500 * time.Millisecond, pollInterval: DefaultPollInterval, want: maxErrorPause},
		{name: "stays at the cap", prev: maxErrorPause, pollInterval: DefaultPollInterval, want: maxErrorPause},
		{name: "never goes below the poll interval", prev: time.Millisecond, pollInterval: DefaultPollInterval, want: DefaultPollInterval},
		{name: "keeps a poll interval longer than the cap", prev: time.Hour, pollInterval: time.Hour, want: time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextErrorPause(tc.prev, tc.pollInterval); got != tc.want {
				t.Fatalf("nextErrorPause(%s, %s) = %s, want %s", tc.prev, tc.pollInterval, got, tc.want)
			}
		})
	}
}

func TestRunReturnsWhenTheContextEnds(t *testing.T) {
	t.Run("while waiting for the next poll", func(t *testing.T) {
		store := newFakeStore()
		rr := runRelay(t, New(store, publisherFunc(acceptAll), nil, Options{PollInterval: time.Hour}))
		store.awaitIdle(t)
		rr.stop()
		if rr.running() {
			t.Fatal("Run did not return after its context ended")
		}
	})

	t.Run("while a publish is in flight", func(t *testing.T) {
		store := newFakeStore(newRecord("user.registered"))
		started := make(chan struct{})
		pub := publisherFunc(func(ctx context.Context, _ Record) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		logs := &lockedBuffer{}
		rr := runRelay(t, New(store, pub, jsonLogger(logs), Options{PollInterval: time.Hour}))
		select {
		case <-started:
		case <-time.After(testDeadline):
			t.Fatalf("the relay did not start publishing within %s", testDeadline)
		}
		rr.stop()
		if rr.running() {
			t.Fatal("Run did not return after its context ended")
		}

		got := store.next(t)
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("publish error = %v, want context.Canceled", got.err)
		}
		if failures := withMessage(logs.entries(t), "ERROR", "outbox publish failed; will retry"); len(failures) != 0 {
			t.Errorf("a publish cut short by shutdown is not a broker failure, but it was logged: %v", failures)
		}
	})
}

func TestNewAppliesDefaultsAndPassesRetryDelayToTheStore(t *testing.T) {
	tests := []struct {
		name string
		in   Options
		want Options
	}{
		{
			name: "zero values take the defaults",
			in:   Options{},
			want: Options{PollInterval: DefaultPollInterval, RetryDelay: DefaultRetryDelay},
		},
		{
			name: "negative values take the defaults",
			in:   Options{PollInterval: -time.Second, RetryDelay: -time.Second},
			want: Options{PollInterval: DefaultPollInterval, RetryDelay: DefaultRetryDelay},
		},
		{
			name: "set values are kept",
			in:   Options{PollInterval: 10 * time.Millisecond, RetryDelay: 7 * time.Second},
			want: Options{PollInterval: 10 * time.Millisecond, RetryDelay: 7 * time.Second},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			r := New(store, publisherFunc(acceptAll), nil, tc.in)
			if r.opts != tc.want {
				t.Fatalf("options = %+v, want %+v", r.opts, tc.want)
			}

			rr := runRelay(t, r)
			store.awaitIdle(t)
			rr.stop()
			delays := store.retryDelays()
			if len(delays) == 0 {
				t.Fatal("the relay never called ClaimNext")
			}
			for _, d := range delays {
				if d != tc.want.RetryDelay {
					t.Errorf("retryDelay passed to the store = %s, want %s", d, tc.want.RetryDelay)
				}
			}
		})
	}
}
