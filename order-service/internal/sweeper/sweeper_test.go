// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Unit tests for the sweeper against a real order.Service over the
//   in-memory fakes: expiry, auto-confirm, races with the requester and with a
//   second sweeper, and per-errand error handling (D-048, D-049, D-050, F3.14.1).
// Author review: James Teo — reviewed and verified.

package sweeper_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
	"foc/order-service/internal/sweeper"
)

// T, K and C as D-049 sets them.
const (
	expiryPeriod  = 2 * time.Hour
	maxActive     = 2
	confirmWindow = 24 * time.Hour
)

// waitLimit bounds every wait on another goroutine, so a broken sweeper fails
// the test instead of hanging it.
const waitLimit = 5 * time.Second

func startTime() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }

func TestProductionTypesSatisfyThePorts(t *testing.T) {
	// Compile-time checks: main.go hands the sweeper the repository and the service.
	var _ sweeper.Source = order.Repository(nil)
	var _ sweeper.Actions = (*order.Service)(nil)
}

func TestSweepOnceExpiresDueOpenErrands(t *testing.T) {
	e := newEnv(t)
	first, second := student(), student()
	a := e.post(t, first, 5)
	e.clock.Advance(time.Minute)
	b := e.post(t, second, 7)
	// a's deadline passed a minute ago; b's is exactly now, which is due too.
	e.clock.Advance(expiryPeriod)

	expired, confirmed, err := e.newSweeper().SweepOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, expired)
	assert.Zero(t, confirmed)
	seen := map[string]bool{}
	for _, tc := range []struct {
		errand    order.Order
		requester order.Caller
	}{{a, first}, {b, second}} {
		got := e.get(t, tc.errand.ID)
		assert.Equal(t, order.StateExpired, got.State)
		assert.Nil(t, got.ExpiresAt)

		events := e.repo.EventsFor(tc.errand.ID)
		require.Len(t, events, 1)
		ev := events[0]
		assert.Equal(t, order.RoutingExpired, ev.RoutingKey)
		assert.Equal(t, order.RoutingExpired, ev.Payload.EventType)
		assert.Equal(t, tc.errand.ID, ev.Payload.ErrandID)
		assert.Equal(t, tc.requester.UserID, ev.Payload.RequesterID)
		assert.Equal(t, tc.errand.Reward, ev.Payload.Amount)
		assert.Nil(t, ev.Payload.CourierID)
		assert.Equal(t, e.clock.Now(), ev.Payload.OccurredAt)
		assertSweepCorrelationID(t, ev.CorrelationID)
		assert.Equal(t, ev.CorrelationID, ev.Payload.CorrelationID)
		assert.False(t, seen[ev.CorrelationID], "each errand gets its own correlation id")
		seen[ev.CorrelationID] = true

		last := e.lastHistory(t, tc.errand.ID)
		assert.Equal(t, order.ActionExpire, last.Action)
		assert.Equal(t, order.ActorSystem, last.ActorKind)
		assert.Nil(t, last.ActorID)
		assert.Equal(t, ev.CorrelationID, last.CorrelationID)
	}
}

// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — verifies due schedules
// become OPEN with a fresh expiry window, then expire only after that window.
func TestSweepOnceOpensScheduledErrandAndStartsExpiryAtOpening(t *testing.T) {
	e := newEnv(t)
	requester := student()
	scheduledAt := e.clock.Now().Add(time.Hour)
	o := e.postScheduled(t, requester, 8, scheduledAt)
	e.clock.Advance(time.Hour)

	expired, confirmed, err := e.newSweeper().SweepOnce(context.Background())

	require.NoError(t, err)
	assert.Zero(t, expired)
	assert.Zero(t, confirmed)
	opened := e.get(t, o.ID)
	assert.Equal(t, order.StateOpen, opened.State)
	assert.Nil(t, opened.ScheduledAt)
	assert.Equal(t, e.clock.Now().Add(expiryPeriod), *opened.ExpiresAt)
	assert.Equal(t, order.ActionOpen, e.lastHistory(t, o.ID).Action)
	assert.Equal(t, order.ActorSystem, e.lastHistory(t, o.ID).ActorKind)
	assert.Empty(t, e.repo.EventsFor(o.ID))

	e.clock.Advance(expiryPeriod)
	expired, _, err = e.newSweeper().SweepOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, expired)
	assert.Equal(t, order.StateExpired, e.get(t, o.ID).State)
}

func TestSweepOnceCancelsDueScheduledErrandAtOpenLimit(t *testing.T) {
	e := newEnv(t)
	requester := student()
	for i := 0; i < order.MaxOpenErrands; i++ {
		e.post(t, requester, i+1)
	}
	scheduledAt := e.clock.Now().Add(time.Hour)
	o := e.postScheduled(t, requester, 12, scheduledAt)
	e.clock.Advance(time.Hour)

	_, _, err := e.newSweeper().SweepOnce(context.Background())
	require.NoError(t, err)
	got := e.get(t, o.ID)
	assert.Equal(t, order.StateCancelled, got.State)
	assert.Nil(t, got.ExpiresAt)
	events := e.repo.EventsFor(o.ID)
	require.Len(t, events, 1)
	assert.Equal(t, order.RoutingCancelled, events[0].RoutingKey)
	assert.Equal(t, 12, events[0].Payload.Amount)
	assert.Equal(t, order.ActionCancel, e.lastHistory(t, o.ID).Action)
}

func TestSweepOnceRetriesScheduledErrandAfterStorageFailure(t *testing.T) {
	e := newEnv(t)
	requester := student()
	o := e.postScheduled(t, requester, 8, e.clock.Now().Add(time.Hour))
	e.clock.Advance(time.Hour)
	e.repo.AfterAmendFn = errors.New("temporary database failure")

	_, _, err := e.newSweeper().SweepOnce(context.Background())
	require.NoError(t, err, "a per-errand failure is logged and left due")
	assert.Equal(t, order.StateScheduled, e.get(t, o.ID).State)
	assert.Empty(t, e.repo.EventsFor(o.ID))

	_, _, err = e.newSweeper().SweepOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, order.StateOpen, e.get(t, o.ID).State)
	assert.Equal(t, order.ActionOpen, e.lastHistory(t, o.ID).Action)
}

func TestSweepOnceAutoConfirmsDueDeliveredErrands(t *testing.T) {
	e := newEnv(t)
	requester, courier := student(), student()
	o := e.deliver(t, requester, courier, 9)
	e.clock.Advance(confirmWindow)

	expired, confirmed, err := e.newSweeper().SweepOnce(context.Background())

	require.NoError(t, err)
	assert.Zero(t, expired)
	assert.Equal(t, 1, confirmed)

	got := e.get(t, o.ID)
	assert.Equal(t, order.StateCompleted, got.State)
	assert.Nil(t, got.ConfirmBy)
	require.NotNil(t, got.CourierID)
	assert.Equal(t, courier.UserID, *got.CourierID)

	events := e.repo.EventsFor(o.ID)
	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, order.RoutingCompleted, ev.RoutingKey)
	assert.Equal(t, order.RoutingCompleted, ev.Payload.EventType)
	require.NotNil(t, ev.Payload.CourierID, "errand.completed names the courier to pay")
	assert.Equal(t, courier.UserID, *ev.Payload.CourierID)
	assert.Equal(t, requester.UserID, ev.Payload.RequesterID)
	assert.Equal(t, 9, ev.Payload.Amount)
	assertSweepCorrelationID(t, ev.CorrelationID)

	last := e.lastHistory(t, o.ID)
	assert.Equal(t, order.ActionAutoConfirm, last.Action)
	assert.Equal(t, order.ActorSystem, last.ActorKind)
	assert.Equal(t, ev.CorrelationID, last.CorrelationID)
}

func TestSweepOnceLeavesErrandsThatAreNotDue(t *testing.T) {
	const month = 30 * 24 * time.Hour
	tests := []struct {
		name    string
		setup   func(t *testing.T, e *env) order.Order
		advance time.Duration
	}{
		{
			name:    "OPEN, one second before its deadline",
			setup:   func(t *testing.T, e *env) order.Order { return e.post(t, student(), 5) },
			advance: expiryPeriod - time.Second,
		},
		{
			name:    "DELIVERED, one second before its window closes",
			setup:   func(t *testing.T, e *env) order.Order { return e.deliver(t, student(), student(), 5) },
			advance: confirmWindow - time.Second,
		},
		{
			name: "ACCEPTED, which runs no timer",
			setup: func(t *testing.T, e *env) order.Order {
				return e.accept(t, e.post(t, student(), 5), student())
			},
			advance: month,
		},
		{
			name: "PICKED_UP, which runs no timer",
			setup: func(t *testing.T, e *env) order.Order {
				courier := student()
				o := e.accept(t, e.post(t, student(), 5), courier)
				o, err := e.svc.PickUp(context.Background(), courier, o.ID, "req-pickup")
				require.NoError(t, err)
				return o
			},
			advance: month,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			before := tc.setup(t, e)
			e.clock.Advance(tc.advance)

			expired, confirmed, err := e.newSweeper().SweepOnce(context.Background())

			require.NoError(t, err)
			assert.Zero(t, expired)
			assert.Zero(t, confirmed)
			assert.Equal(t, before, e.get(t, before.ID), "the errand must be untouched")
			assert.Empty(t, e.repo.EventsFor(before.ID))
		})
	}
}

func TestSweepOnceDoesNotCompleteAnErrandTheRequesterConfirmedFirst(t *testing.T) {
	// Each case lands the requester's confirm at a different point of the
	// sweep and returns the Source the sweeper lists from.
	tests := []struct {
		name    string
		arrange func(t *testing.T, e *env, id uuid.UUID, confirm func()) sweeper.Source
	}{
		{
			name: "confirmed after the deadline, before the sweep lists it",
			arrange: func(_ *testing.T, e *env, _ uuid.UUID, confirm func()) sweeper.Source {
				confirm()
				return e.repo
			},
		},
		{
			name: "confirmed after the sweep listed it as due",
			arrange: func(t *testing.T, e *env, id uuid.UUID, confirm func()) sweeper.Source {
				return &hookedSource{Source: e.repo, afterConfirmList: func(listed []uuid.UUID) {
					assert.Contains(t, listed, id, "the sweep must have seen the errand as due")
					confirm()
				}}
			},
		},
		{
			name: "confirmed between the sweeper's read and its conditional write",
			arrange: func(_ *testing.T, e *env, _ uuid.UUID, confirm func()) sweeper.Source {
				e.repo.runBeforeNextTransition(confirm)
				return e.repo
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			requester, courier := student(), student()
			o := e.deliver(t, requester, courier, 9)
			e.clock.Advance(confirmWindow + time.Minute)
			const requestID = "req-confirm"
			confirmRan := false
			confirm := func() {
				_, err := e.svc.Confirm(context.Background(), requester, o.ID, requestID)
				assert.NoError(t, err)
				confirmRan = true
			}
			src := tc.arrange(t, e, o.ID, confirm)

			_, confirmed, err := sweeper.New(src, e.svc, e.clock, nil, time.Hour).SweepOnce(context.Background())

			require.NoError(t, err)
			require.True(t, confirmRan, "the requester's confirm never ran")
			assert.Zero(t, confirmed, "the sweeper must not count an errand it did not move")
			assert.Equal(t, order.StateCompleted, e.get(t, o.ID).State)
			events := e.repo.EventsFor(o.ID)
			require.Len(t, events, 1, "the courier is paid exactly once")
			assert.Equal(t, order.RoutingCompleted, events[0].RoutingKey)
			assert.Equal(t, requestID, events[0].CorrelationID, "the requester's confirm emitted it")
			assert.Equal(t,
				[]order.Action{order.ActionCreate, order.ActionAccept, order.ActionPickUp, order.ActionDeliver, order.ActionConfirm},
				e.actionsOf(t, o.ID))
		})
	}
}

func TestConcurrentSweepersMoveEachErrandOnce(t *testing.T) {
	e := newEnv(t)
	var openIDs, deliveredIDs []uuid.UUID
	for range 20 {
		openIDs = append(openIDs, e.post(t, student(), 3).ID)
	}
	for range 10 {
		deliveredIDs = append(deliveredIDs, e.deliver(t, student(), student(), 4).ID)
	}
	e.clock.Advance(confirmWindow) // past T for the OPEN ones, and C for the DELIVERED ones

	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	defer cancel()
	// Neither sweeper moves anything until both have listed, so both try
	// every due errand.
	expiryListed, confirmListed := newBarrier(2), newBarrier(2)
	type result struct {
		expired, confirmed int
		err                error
	}
	results := make([]result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		src := &barrierSource{Source: e.repo, expiry: expiryListed, confirm: confirmListed}
		sw := sweeper.New(src, e.svc, e.clock, nil, time.Hour)
		wg.Go(func() {
			<-start
			r := &results[i]
			r.expired, r.confirmed, r.err = sw.SweepOnce(ctx)
		})
	}
	close(start)
	wg.Wait()

	for _, r := range results {
		require.NoError(t, r.err)
	}
	assert.Equal(t, len(openIDs), results[0].expired+results[1].expired, "one transition per errand")
	assert.Equal(t, len(deliveredIDs), results[0].confirmed+results[1].confirmed, "one transition per errand")
	for _, id := range openIDs {
		assert.Equal(t, order.StateExpired, e.get(t, id).State)
		assert.Equal(t, 1, count(e.actionsOf(t, id), order.ActionExpire))
		events := e.repo.EventsFor(id)
		if assert.Len(t, events, 1) {
			assert.Equal(t, order.RoutingExpired, events[0].RoutingKey)
		}
	}
	for _, id := range deliveredIDs {
		assert.Equal(t, order.StateCompleted, e.get(t, id).State)
		assert.Equal(t, 1, count(e.actionsOf(t, id), order.ActionAutoConfirm))
		events := e.repo.EventsFor(id)
		if assert.Len(t, events, 1) {
			assert.Equal(t, order.RoutingCompleted, events[0].RoutingKey)
		}
	}
	assert.Len(t, e.repo.Events(), len(openIDs)+len(deliveredIDs), "no event beyond one per errand")
}

func TestSweepOnceCarriesOnPastAFailedErrand(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	errBoom := errors.New("database went away")
	src := &stubSource{expiry: []uuid.UUID{a, b, c}, confirm: []uuid.UUID{d}}
	acts := &stubActions{errs: map[uuid.UUID]error{b: errBoom}}
	log, logs := captureLogs()

	expired, confirmed, err := sweeper.New(src, acts, ordertest.NewClock(startTime()), log, time.Hour).
		SweepOnce(context.Background())

	require.NoError(t, err, "one errand's failure is not the sweep's")
	assert.Equal(t, 2, expired)
	assert.Equal(t, 1, confirmed)
	assert.Equal(t, []uuid.UUID{a, b, c, d}, acts.ids(), "every due errand is still tried")

	failures := logs.where(t, func(r logRecord) bool { return r["level"] == "ERROR" })
	require.Len(t, failures, 1)
	assert.Equal(t, b.String(), failures[0]["errand_id"])
	assert.Equal(t, acts.callFor(t, b).correlationID, failures[0]["request_id"])
	assert.Contains(t, failures[0]["err"], errBoom.Error())
}

func TestSweepOnceReportsADueListThatCannotBeRead(t *testing.T) {
	errExpiryList := errors.New("expiry list failed")
	errConfirmList := errors.New("auto-confirm list failed")
	due := uuid.New()
	tests := []struct {
		name          string
		src           *stubSource
		wantErrs      []error
		wantExpired   int
		wantConfirmed int
	}{
		{
			name:          "expiry list fails; auto-confirm still runs",
			src:           &stubSource{expiryErr: errExpiryList, confirm: []uuid.UUID{due}},
			wantErrs:      []error{errExpiryList},
			wantConfirmed: 1,
		},
		{
			name:        "auto-confirm list fails; expiry already ran",
			src:         &stubSource{expiry: []uuid.UUID{due}, confirmErr: errConfirmList},
			wantErrs:    []error{errConfirmList},
			wantExpired: 1,
		},
		{
			name:     "both lists fail",
			src:      &stubSource{expiryErr: errExpiryList, confirmErr: errConfirmList},
			wantErrs: []error{errExpiryList, errConfirmList},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expired, confirmed, err := sweeper.New(tc.src, &stubActions{}, ordertest.NewClock(startTime()), nil, time.Hour).
				SweepOnce(context.Background())

			require.Error(t, err)
			for _, want := range tc.wantErrs {
				assert.ErrorIs(t, err, want)
			}
			assert.Equal(t, tc.wantExpired, expired)
			assert.Equal(t, tc.wantConfirmed, confirmed)
		})
	}
}

func TestSweepOnceGivesEachErrandItsOwnCorrelationID(t *testing.T) {
	expiry := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	confirm := []uuid.UUID{uuid.New(), uuid.New()}
	src := &stubSource{expiry: expiry, confirm: confirm}
	// The last one was already moved by someone else: still logged, not counted.
	acts := &stubActions{notApplied: map[uuid.UUID]bool{confirm[1]: true}}
	clock := ordertest.NewClock(startTime())
	log, logs := captureLogs()

	expired, confirmed, err := sweeper.New(src, acts, clock, log, time.Hour).SweepOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 3, expired)
	assert.Equal(t, 1, confirmed)
	assert.Equal(t, []listCall{
		{list: "expiry", now: clock.Now(), limit: order.SweepBatchLimit},
		{list: "auto-confirm", now: clock.Now(), limit: order.SweepBatchLimit},
		{list: "scheduled", now: clock.Now(), limit: order.SweepBatchLimit},
	}, src.callsSoFar(), "all lists are read as of the sweep's clock, one batch each")

	calls := acts.callsSoFar()
	assert.Equal(t, slices.Concat(expiry, confirm), acts.ids())
	assert.Equal(t, []string{"expire", "expire", "expire", "auto-confirm", "auto-confirm"}, actionNames(calls))
	seen := map[string]bool{}
	for _, c := range calls {
		assertSweepCorrelationID(t, c.correlationID)
		assert.Equal(t, c.correlationID, c.ctxCorrelationID, "the context carries the same id, for adapters that log it")
		assert.False(t, seen[c.correlationID], "correlation id %s reused", c.correlationID)
		seen[c.correlationID] = true

		logged := logs.where(t, func(r logRecord) bool { return r["request_id"] == c.correlationID })
		if assert.Len(t, logged, 1, "each errand is logged once under its own id") {
			assert.Equal(t, c.id.String(), logged[0]["errand_id"])
			assert.Equal(t, "INFO", logged[0]["level"])
		}
	}
}

func TestSweepOnceStopsWhenTheContextEnds(t *testing.T) {
	t.Run("already ended: lists and moves nothing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		src := &stubSource{expiry: []uuid.UUID{uuid.New()}, confirm: []uuid.UUID{uuid.New()}}
		acts := &stubActions{}

		expired, confirmed, err := sweeper.New(src, acts, ordertest.NewClock(startTime()), nil, time.Hour).SweepOnce(ctx)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, expired)
		assert.Zero(t, confirmed)
		assert.Empty(t, src.callsSoFar())
		assert.Empty(t, acts.callsSoFar())
	})
	t.Run("ends mid-sweep: stops before the next errand", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		expiry := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		src := &stubSource{expiry: expiry, confirm: []uuid.UUID{uuid.New()}}
		acts := &stubActions{onCall: cancel}

		expired, confirmed, err := sweeper.New(src, acts, ordertest.NewClock(startTime()), nil, time.Hour).SweepOnce(ctx)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, expired, "the errand moved before the context ended still counts")
		assert.Zero(t, confirmed)
		assert.Equal(t, expiry[:1], acts.ids())
		assert.Len(t, src.callsSoFar(), 1, "the auto-confirm list is not read")
	})
}

func TestRunSweepsStraightAway(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
	}{
		// An hour-long interval cannot tick within the test, so the sweep
		// seen must be the one Run makes before waiting.
		{name: "before the first interval", interval: time.Hour},
		{name: "with no interval given (the default)", interval: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			swept := make(chan struct{}, 16)
			src := &stubSource{swept: swept}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				sweeper.New(src, &stubActions{}, ordertest.NewClock(startTime()), nil, tc.interval).Run(ctx)
			}()

			waitFor(t, swept, "the first sweep")
			cancel()
			waitFor(t, done, "Run to return")
		})
	}
}

func TestRunSweepsEveryIntervalUntilCancelled(t *testing.T) {
	tests := []struct {
		name string
		src  *stubSource
		// wantFailureLogs is the least number of "sweep failed" records.
		wantFailureLogs int
	}{
		{name: "sweeps that succeed", src: &stubSource{}},
		{
			name: "a failing sweep is logged and the next still runs",
			src: &stubSource{
				expiryErr:  errors.New("database went away"),
				confirmErr: errors.New("database went away"),
			},
			// Sweeps 1 and 2 finish before sweep 3 starts, and the context
			// is cancelled only after sweep 3 starts.
			wantFailureLogs: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			swept := make(chan struct{}, 64)
			tc.src.swept = swept
			log, logs := captureLogs()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				sweeper.New(tc.src, &stubActions{}, ordertest.NewClock(startTime()), log, time.Millisecond).Run(ctx)
			}()

			for i := range 3 {
				waitFor(t, swept, fmt.Sprintf("sweep %d", i+1))
			}
			cancel()
			waitFor(t, done, "Run to return")

			failures := logs.where(t, func(r logRecord) bool { return r["msg"] == "sweep failed; the next sweep retries" })
			assert.GreaterOrEqual(t, len(failures), tc.wantFailureLogs)
			if tc.wantFailureLogs == 0 {
				assert.Empty(t, failures)
			}
		})
	}
}

// --- a real service over the fakes ---

// env is a real order.Service over the in-memory fakes, with a settable clock.
type env struct {
	repo     *hookedRepo
	clock    *ordertest.Clock
	svc      *order.Service
	supplier uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	sup := order.Supplier{
		ID:                  uuid.New(),
		Name:                "Hot Hideout",
		Building:            "Yusof Ishak House",
		Floor:               "1",
		LocationDescription: "Opp UHC bus stop",
		Available:           true,
	}
	repo := &hookedRepo{Repository: ordertest.NewRepository()}
	clock := ordertest.NewClock(startTime())
	cfg := order.Config{ExpiryPeriod: expiryPeriod, MaxActive: maxActive, ConfirmWindow: confirmWindow}
	svc := order.NewService(repo, ordertest.NewCredit(), ordertest.NewSuppliers(sup), clock, cfg, slog.New(slog.DiscardHandler))
	return &env{repo: repo, clock: clock, svc: svc, supplier: sup.ID}
}

func (e *env) newSweeper() *sweeper.Sweeper {
	return sweeper.New(e.repo, e.svc, e.clock, nil, time.Hour)
}

func student() order.Caller { return order.Caller{UserID: uuid.New(), Role: order.RoleStudent} }

// post has requester post an errand: OPEN, with T running.
func (e *env) post(t *testing.T, requester order.Caller, reward int) order.Order {
	t.Helper()
	o, _, err := e.svc.Create(context.Background(), requester, order.CreateInput{
		SupplierID:       e.supplier,
		ItemDescription:  "Iced milo, less sugar",
		DeliveryLocation: "COM3 Lobby",
		Reward:           reward,
		CorrelationID:    "req-create-" + uuid.NewString(),
	})
	require.NoError(t, err)
	require.Equal(t, order.StateOpen, o.State)
	return o
}

func (e *env) postScheduled(t *testing.T, requester order.Caller, reward int, scheduledAt time.Time) order.Order {
	t.Helper()
	o, _, err := e.svc.Create(context.Background(), requester, order.CreateInput{
		SupplierID: e.supplier, ItemDescription: "Iced milo, less sugar",
		DeliveryLocation: "COM3 Lobby", Reward: reward, ScheduledAt: &scheduledAt,
		CorrelationID: "req-scheduled-" + uuid.NewString(),
	})
	require.NoError(t, err)
	require.Equal(t, order.StateScheduled, o.State)
	return o
}

// accept has courier accept o.
func (e *env) accept(t *testing.T, o order.Order, courier order.Caller) order.Order {
	t.Helper()
	o, err := e.svc.Accept(context.Background(), courier, o.ID, o.Version, "req-accept")
	require.NoError(t, err)
	return o
}

// deliver has requester post an errand and courier carry it to DELIVERED,
// which starts C.
func (e *env) deliver(t *testing.T, requester, courier order.Caller, reward int) order.Order {
	t.Helper()
	ctx := context.Background()
	o := e.accept(t, e.post(t, requester, reward), courier)
	o, err := e.svc.PickUp(ctx, courier, o.ID, "req-pickup")
	require.NoError(t, err)
	o, err = e.svc.Deliver(ctx, courier, o.ID, "req-deliver")
	require.NoError(t, err)
	require.Equal(t, order.StateDelivered, o.State)
	return o
}

func (e *env) get(t *testing.T, id uuid.UUID) order.Order {
	t.Helper()
	o, err := e.repo.Get(context.Background(), id)
	require.NoError(t, err)
	return o
}

func (e *env) history(t *testing.T, id uuid.UUID) []order.HistoryEntry {
	t.Helper()
	h, err := e.repo.History(context.Background(), id)
	require.NoError(t, err)
	return h
}

func (e *env) lastHistory(t *testing.T, id uuid.UUID) order.HistoryEntry {
	t.Helper()
	h := e.history(t, id)
	require.NotEmpty(t, h)
	return h[len(h)-1]
}

// actionsOf lists the actions recorded for an errand, oldest first.
func (e *env) actionsOf(t *testing.T, id uuid.UUID) []order.Action {
	t.Helper()
	var out []order.Action
	for _, h := range e.history(t, id) {
		out = append(out, h.Action)
	}
	return out
}

func count(actions []order.Action, want order.Action) int {
	n := 0
	for _, a := range actions {
		if a == want {
			n++
		}
	}
	return n
}

func assertSweepCorrelationID(t *testing.T, id string) {
	t.Helper()
	rest, ok := strings.CutPrefix(id, "sweep-")
	if assert.True(t, ok, "correlation id %q lacks the sweep- prefix", id) {
		_, err := uuid.Parse(rest)
		assert.NoError(t, err, "correlation id %q is not sweep-<uuid>", id)
	}
}

// hookedRepo is the in-memory repository with a one-shot hook run just before
// the next Transition, to land a user's action between the sweeper's read of
// an errand and its conditional write.
type hookedRepo struct {
	*ordertest.Repository
	mu               sync.Mutex
	beforeTransition func()
}

func (r *hookedRepo) runBeforeNextTransition(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beforeTransition = fn
}

func (r *hookedRepo) Transition(ctx context.Context, t order.Transition) (order.Order, error) {
	r.mu.Lock()
	hook := r.beforeTransition
	r.beforeTransition = nil
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return r.Repository.Transition(ctx, t)
}

// hookedSource runs a hook once the auto-confirm list has been read, to land a
// user's action between the sweep's list and its write.
type hookedSource struct {
	sweeper.Source
	afterConfirmList func(listed []uuid.UUID)
}

func (s *hookedSource) DueForAutoConfirm(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	ids, err := s.Source.DueForAutoConfirm(ctx, now, limit)
	if err == nil {
		s.afterConfirmList(ids)
	}
	return ids, err
}

// barrier releases its waiters once n of them have arrived.
type barrier struct {
	mu      sync.Mutex
	waiting int
	all     chan struct{}
}

func newBarrier(n int) *barrier { return &barrier{waiting: n, all: make(chan struct{})} }

func (b *barrier) wait(ctx context.Context) error {
	b.mu.Lock()
	b.waiting--
	if b.waiting == 0 {
		close(b.all)
	}
	b.mu.Unlock()
	select {
	case <-b.all:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("barrier: %w", ctx.Err())
	}
}

// barrierSource holds each list until every sweeper sharing the barriers has
// read the same one.
type barrierSource struct {
	sweeper.Source
	expiry, confirm *barrier
}

func (s *barrierSource) DueForExpiry(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	ids, err := s.Source.DueForExpiry(ctx, now, limit)
	if werr := s.expiry.wait(ctx); werr != nil {
		return nil, werr
	}
	return ids, err
}

func (s *barrierSource) DueForAutoConfirm(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	ids, err := s.Source.DueForAutoConfirm(ctx, now, limit)
	if werr := s.confirm.wait(ctx); werr != nil {
		return nil, werr
	}
	return ids, err
}

// --- stubs for the error and timing paths ---

type listCall struct {
	list  string
	now   time.Time
	limit int
}

// stubSource answers with fixed ids or errors and records each call. swept,
// when set, gets a value at the start of every sweep.
type stubSource struct {
	mu                                  sync.Mutex
	expiry, confirm, scheduled          []uuid.UUID
	expiryErr, confirmErr, scheduledErr error
	calls                               []listCall
	swept                               chan struct{}
}

func (s *stubSource) DueForScheduled(_ context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, listCall{list: "scheduled", now: now, limit: limit})
	return slices.Clone(s.scheduled), s.scheduledErr
}

func (s *stubSource) DueForExpiry(_ context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, listCall{list: "expiry", now: now, limit: limit})
	if s.swept != nil {
		select {
		case s.swept <- struct{}{}:
		default: // the test has seen enough sweeps; never block Run
		}
	}
	return slices.Clone(s.expiry), s.expiryErr
}

func (s *stubSource) DueForAutoConfirm(_ context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, listCall{list: "auto-confirm", now: now, limit: limit})
	return slices.Clone(s.confirm), s.confirmErr
}

func (s *stubSource) callsSoFar() []listCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

type actCall struct {
	action           string
	id               uuid.UUID
	correlationID    string
	ctxCorrelationID string
}

// stubActions applies every call unless told otherwise, and records each.
type stubActions struct {
	mu         sync.Mutex
	calls      []actCall
	errs       map[uuid.UUID]error
	notApplied map[uuid.UUID]bool
	onCall     func()
}

func (a *stubActions) Expire(ctx context.Context, id uuid.UUID, correlationID string) (bool, error) {
	return a.record(ctx, "expire", id, correlationID)
}

func (a *stubActions) OpenScheduled(ctx context.Context, id uuid.UUID, correlationID string) (bool, error) {
	return a.record(ctx, "open-scheduled", id, correlationID)
}

func (a *stubActions) AutoConfirm(ctx context.Context, id uuid.UUID, correlationID string) (bool, error) {
	return a.record(ctx, "auto-confirm", id, correlationID)
}

func (a *stubActions) record(ctx context.Context, action string, id uuid.UUID, correlationID string) (bool, error) {
	a.mu.Lock()
	a.calls = append(a.calls, actCall{
		action:           action,
		id:               id,
		correlationID:    correlationID,
		ctxCorrelationID: order.CorrelationID(ctx),
	})
	err, skip, hook := a.errs[id], a.notApplied[id], a.onCall
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return false, err
	}
	return !skip, nil
}

func (a *stubActions) callsSoFar() []actCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.calls)
}

func (a *stubActions) ids() []uuid.UUID {
	var out []uuid.UUID
	for _, c := range a.callsSoFar() {
		out = append(out, c.id)
	}
	return out
}

func (a *stubActions) callFor(t *testing.T, id uuid.UUID) actCall {
	t.Helper()
	for _, c := range a.callsSoFar() {
		if c.id == id {
			return c
		}
	}
	t.Fatalf("no call for errand %s", id)
	return actCall{}
}

func actionNames(calls []actCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.action)
	}
	return out
}

// --- logs and waiting ---

type logRecord map[string]any

// logSink collects JSON log lines. The mutex keeps a read safe while a
// sweeper goroutine may still be writing.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) where(t *testing.T, keep func(logRecord) bool) []logRecord {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []logRecord
	for line := range bytes.Lines(s.buf.Bytes()) {
		var r logRecord
		require.NoError(t, json.Unmarshal(line, &r))
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func captureLogs() (*slog.Logger, *logSink) {
	sink := &logSink{}
	return slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})), sink
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitLimit):
		t.Fatalf("waited %s for %s", waitLimit, what)
	}
}
