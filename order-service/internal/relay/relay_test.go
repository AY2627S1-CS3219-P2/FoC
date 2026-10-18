// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Unit tests of the relay loop (D-051) against an in-memory store and
//   publisher: draining, retries after failures, shutdown and defaults.
// Author review: James Teo — reviewed and verified.

package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e), "log line %q", sc.Text())
		out = append(out, e)
	}
	require.NoError(t, sc.Err())
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
		Payload:       []byte(`{"amount":5}`),
		CorrelationID: "corr-" + routingKey,
	}
}

func TestRunDrainsDueRecordsBackToBack(t *testing.T) {
	due := []Record{
		newRecord("errand.completed"), newRecord("errand.cancelled"), newRecord("errand.expired"),
		newRecord("errand.failed"), newRecord("errand.completed"),
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
		assert.Equal(t, want.EventID, got.rec.EventID, "record %d claimed out of order", i)
		assert.NoError(t, got.err)
	}
	store.awaitIdle(t)
	rr.stop()

	mu.Lock()
	defer mu.Unlock()
	want := make([]uuid.UUID, 0, len(due))
	for _, r := range due {
		want = append(want, r.EventID)
	}
	assert.Equal(t, want, published)
}

func TestRunHandsPublishErrorsToTheStoreAndCarriesOn(t *testing.T) {
	errBroker := errors.New("broker unreachable")
	failing := newRecord("errand.completed")
	failing.Attempts = 2
	healthy := newRecord("errand.cancelled")
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
	require.Equal(t, failing.EventID, first.rec.EventID)
	assert.ErrorIs(t, first.err, errBroker, "the store must receive the publisher's error")
	second := store.next(t)
	assert.Equal(t, healthy.EventID, second.rec.EventID, "a failed publish must not stop the relay")
	assert.NoError(t, second.err)
	assert.True(t, rr.running())
	rr.stop()

	failures := withMessage(logs.entries(t), "ERROR", "outbox publish failed; will retry")
	require.Len(t, failures, 1)
	assert.Equal(t, failing.EventID.String(), failures[0]["event_id"])
	assert.Equal(t, failing.RoutingKey, failures[0]["routing_key"])
	assert.Equal(t, failing.CorrelationID, failures[0]["request_id"])
	assert.EqualValues(t, 3, failures[0]["attempt"], "attempt counts this try after two failures")
	assert.Equal(t, errBroker.Error(), failures[0]["err"])

	published := withMessage(logs.entries(t), "INFO", "outbox event published")
	require.Len(t, published, 1)
	assert.Equal(t, healthy.EventID.String(), published[0]["event_id"])
	assert.Equal(t, healthy.CorrelationID, published[0]["request_id"])
}

func TestRunKeepsPollingAfterStoreErrors(t *testing.T) {
	rec := newRecord("errand.expired")
	store := newFakeStore(rec)
	store.failNext = []error{errors.New("connection reset"), errors.New("connection reset")}
	logs := &lockedBuffer{}
	rr := runRelay(t, New(store, publisherFunc(acceptAll), jsonLogger(logs), Options{PollInterval: time.Millisecond}))

	got := store.next(t)
	assert.Equal(t, rec.EventID, got.rec.EventID, "the relay must claim again after store errors")
	assert.NoError(t, got.err)
	assert.True(t, rr.running())
	rr.stop()

	failures := withMessage(logs.entries(t), "ERROR", "outbox claim failed; will retry")
	require.Len(t, failures, 2)
	assert.Equal(t, "connection reset", failures[0]["err"])
}

func TestRunBacksOffWhileTheStoreKeepsFailing(t *testing.T) {
	const (
		failures = 5
		// Small enough that the fifth pause, 16 times this, stays under
		// maxErrorPause, so every pause checked here is a full doubling.
		pollInterval = 10 * time.Millisecond
	)
	rec := newRecord("errand.failed")
	store := newFakeStore(rec)
	for range failures {
		store.failNext = append(store.failNext, errors.New("connection refused"))
	}
	logs := &lockedBuffer{}
	rr := runRelay(t, New(store, publisherFunc(acceptAll), jsonLogger(logs), Options{PollInterval: pollInterval}))

	got := store.next(t)
	assert.Equal(t, rec.EventID, got.rec.EventID, "the relay must claim again once the store answers")
	assert.NoError(t, got.err)
	rr.stop()

	calls := store.callTimes()
	require.GreaterOrEqual(t, len(calls), failures+1, "five failed calls and the claim that worked")
	for i := 1; i <= failures; i++ {
		// Lower bounds only: a busy machine can stretch a pause but never
		// shorten it.
		want := pollInterval << (i - 1)
		assert.GreaterOrEqual(t, calls[i].Sub(calls[i-1]), want, "the pause after store error %d in a row", i)
	}
	assert.Len(t, withMessage(logs.entries(t), "ERROR", "outbox claim failed; will retry"), failures)
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
			assert.Equal(t, tc.want, nextErrorPause(tc.prev, tc.pollInterval))
		})
	}
}

func TestRunReturnsWhenTheContextEnds(t *testing.T) {
	t.Run("while waiting for the next poll", func(t *testing.T) {
		store := newFakeStore()
		rr := runRelay(t, New(store, publisherFunc(acceptAll), nil, Options{PollInterval: time.Hour}))
		store.awaitIdle(t)
		rr.stop()
		assert.False(t, rr.running())
	})

	t.Run("while a publish is in flight", func(t *testing.T) {
		store := newFakeStore(newRecord("errand.completed"))
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
		assert.False(t, rr.running())

		got := store.next(t)
		assert.ErrorIs(t, got.err, context.Canceled)
		assert.Empty(t, withMessage(logs.entries(t), "ERROR", "outbox publish failed; will retry"),
			"a publish cut short by shutdown is not a broker failure")
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
			assert.Equal(t, tc.want, r.opts)

			rr := runRelay(t, r)
			store.awaitIdle(t)
			rr.stop()
			delays := store.retryDelays()
			require.NotEmpty(t, delays)
			for _, d := range delays {
				assert.Equal(t, tc.want.RetryDelay, d)
			}
		})
	}
}
