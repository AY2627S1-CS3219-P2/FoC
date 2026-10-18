//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Concurrency integration tests of the PostgreSQL order.Repository:
//   one winner among racing accepts and K under contention (D-046, F3.3.2 to
//   F3.3.4), accept latency against NFR2.3, and the row lock Amend and
//   QueueLocked hold while their callback runs (D-057).
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

// acceptAll releases every command at the same moment, one goroutine each,
// and returns each call's error and duration in input order.
func (e *env) acceptAll(t *testing.T, cmds []order.AcceptCommand) ([]error, []time.Duration) {
	t.Helper()
	errs := make([]error, len(cmds))
	took := make([]time.Duration, len(cmds))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, c := range cmds {
		wg.Go(func() {
			<-start
			began := time.Now()
			_, errs[i] = e.repo.Accept(t.Context(), c)
			took[i] = time.Since(began)
		})
	}
	close(start)
	wg.Wait()
	return errs, took
}

// receive waits for a value on ch, failing the test after a deadline rather
// than hanging it.
func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// waitForLockWait polls until a session in this database is waiting for a
// lock: the racing write has reached the row and is blocked behind its lock.
func (e *env) waitForLockWait(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting int
		err := e.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting)
		return err == nil && waiting > 0
	}, 10*time.Second, 5*time.Millisecond, "no write started waiting for the row lock")
}

// holdInCallback starts run in a goroutine whose callback signals once it is
// running and then blocks until release is called. The test's cleanup
// releases it too, so a failing test never leaves a transaction open.
func holdInCallback(t *testing.T, run func(wait func()) error) (running <-chan struct{}, release func(), done <-chan error) {
	t.Helper()
	started := make(chan struct{})
	gate := make(chan struct{})
	finished := make(chan error, 1)
	release = sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	go func() {
		finished <- run(func() {
			close(started)
			<-gate
		})
	}()
	return started, release, finished
}

// testConcurrentAcceptsOneWinner is F3.3.2 and F3.3.3: of 20 couriers
// accepting one errand at once, exactly one gets it and the rest are told it
// changed (D-046).
func (e *env) testConcurrentAcceptsOneWinner(t *testing.T) {
	e.reset(t)
	o := openErrand(uuid.New(), at(0))
	e.insert(t, o)
	cmds := make([]order.AcceptCommand, 20)
	for i := range cmds {
		cmds[i] = acceptCmd(o, uuid.New(), at(time.Minute), 2)
	}

	errs, _ := e.acceptAll(t, cmds)

	winner := -1
	for i, err := range errs {
		switch {
		case err == nil:
			require.Equal(t, -1, winner, "couriers %d and %d both won", winner, i)
			winner = i
		case errors.Is(err, order.ErrNoMatch):
		default:
			t.Fatalf("courier %d: %v", i, err)
		}
	}
	require.NotEqual(t, -1, winner, "no courier won")
	got := e.get(t, o.ID)
	require.Equal(t, order.StateAccepted, got.State)
	require.Equal(t, cmds[winner].CourierID, *got.CourierID)
	require.Equal(t, 2, got.Version)
	require.Len(t, e.history(t, o.ID), 1, "one ACCEPT row")
}

// testConcurrentAcceptsRespectK is F3.3.4 under contention: one courier
// accepting six errands at once with K = 2 gets exactly two, because the
// advisory lock makes each count see the accept before it. It runs several
// rounds, since without the lock only some races would break K.
func (e *env) testConcurrentAcceptsRespectK(t *testing.T) {
	const rounds, errands, k = 5, 6, 2
	for round := range rounds {
		e.reset(t)
		courier := uuid.New()
		cmds := make([]order.AcceptCommand, errands)
		for i := range cmds {
			o := openErrand(uuid.New(), at(time.Duration(i)*time.Minute))
			e.insert(t, o)
			cmds[i] = acceptCmd(o, courier, at(time.Hour), k)
		}

		errs, _ := e.acceptAll(t, cmds)

		won, full := 0, 0
		for i, err := range errs {
			switch {
			case err == nil:
				won++
			case errors.Is(err, order.ErrCourierAtCapacity):
				full++
			default:
				t.Fatalf("round %d, errand %d: %v", round, i, err)
			}
		}
		require.Equal(t, k, won, "round %d: accepts that succeeded", round)
		require.Equal(t, errands-k, full, "round %d: accepts refused for K", round)
		require.Equal(t, k, e.count(t, `SELECT count(*) FROM orders WHERE courier_id = $1 AND state = 'ACCEPTED'`, courier),
			"round %d: errands the courier holds", round)
	}
}

// testAcceptLatency checks NFR2.3 (p95 under 1s, p99 under 2s) on 50 accepts
// racing over 10 errands. It times the repository's share of the write path
// only: the HTTP hop and the service's read before the write are not in it.
func (e *env) testAcceptLatency(t *testing.T) {
	e.reset(t)
	const errands, couriersPerErrand = 10, 5
	var cmds []order.AcceptCommand
	for i := range errands {
		o := openErrand(uuid.New(), at(time.Duration(i)*time.Minute))
		e.insert(t, o)
		for range couriersPerErrand {
			cmds = append(cmds, acceptCmd(o, uuid.New(), at(time.Hour), 2))
		}
	}
	e.warmPool(t, len(cmds))

	errs, took := e.acceptAll(t, cmds)

	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, order.ErrNoMatch):
		default:
			t.Fatalf("accept %d: %v", i, err)
		}
	}
	require.Equal(t, errands, won, "one courier wins each errand")

	slices.Sort(took)
	p50, p95, p99 := percentile(took, 50), percentile(took, 95), percentile(took, 99)
	t.Logf("Accept latency, %d concurrent calls over %d errands (repository only, warm pool): p50=%v p95=%v p99=%v",
		len(took), errands, p50, p95, p99)
	require.Less(t, p95, time.Second, "NFR2.3: p95 under 1.0s")
	require.Less(t, p99, 2*time.Second, "NFR2.3: p99 under 2.0s")
}

// percentile is the nearest-rank pth percentile of sorted.
func percentile(sorted []time.Duration, p int) time.Duration {
	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	return sorted[max(rank, 1)-1]
}

// warmPool opens n connections before the clock starts, so the latency test
// measures accepts rather than dialling PostgreSQL.
func (e *env) warmPool(t *testing.T, n int) {
	t.Helper()
	conns := make([]*pgxpool.Conn, 0, n)
	for range n {
		c, err := e.pool.Acquire(t.Context())
		require.NoError(t, err)
		conns = append(conns, c)
	}
	for _, c := range conns {
		c.Release()
	}
}

// testAmendHoldsRowLock is D-057's guarantee that fn runs under the row lock:
// an accept racing the amend waits until the amend commits, then fails its
// version check because the errand it was shown has changed (D-046).
func (e *env) testAmendHoldsRowLock(t *testing.T) {
	e.reset(t)
	o := openErrand(uuid.New(), at(0))
	e.insert(t, o)

	inFn, release, amended := holdInCallback(t, func(wait func()) error {
		_, err := e.repo.Amend(t.Context(), o.ID, func(cur order.Order) (*order.AmendResult, error) {
			wait()
			next := cur
			next.Reward = 9
			next.Version = cur.Version + 1
			next.UpdatedAt = at(5 * time.Minute)
			changes := []order.FieldChange{{Field: order.FieldReward, From: "5", To: "9"}}
			return &order.AmendResult{Order: next, History: amendEntry(cur, at(5*time.Minute), changes)}, nil
		})
		return err
	})
	receive(t, inFn, "the amend to lock the row")

	accepted := make(chan error, 1)
	go func() {
		_, err := e.repo.Accept(t.Context(), acceptCmd(o, uuid.New(), at(10*time.Minute), 2))
		accepted <- err
	}()
	e.waitForLockWait(t)
	select {
	case err := <-accepted:
		t.Fatalf("the accept finished while the amend held the row: %v", err)
	default:
	}

	release()
	require.NoError(t, receive(t, amended, "the amend to commit"))
	require.ErrorIs(t, receive(t, accepted, "the accept to finish"), order.ErrNoMatch,
		"the accept expected version 1; the amend committed version 2")
	got := e.get(t, o.ID)
	require.Equal(t, order.StateOpen, got.State)
	require.Nil(t, got.CourierID)
	require.Equal(t, 2, got.Version)
	require.Equal(t, 9, got.Reward)
}

// testQueueLockedHoldsRowLock is D-057's reward_set after a failed amend: fn
// sees the row as committed while holding its lock, so a racing write waits
// for it, and the event is in the outbox before that write goes through.
func (e *env) testQueueLockedHoldsRowLock(t *testing.T) {
	e.reset(t)
	o := openErrand(uuid.New(), at(0))
	e.insert(t, o)

	var ev order.OutboxEvent
	inFn, release, locked := holdInCallback(t, func(wait func()) error {
		return e.repo.QueueLocked(t.Context(), o.ID, func(cur order.Order) (order.OutboxEvent, error) {
			wait()
			ev = rewardSetEvent(cur, at(5*time.Minute), "corr-amend")
			return ev, nil
		})
	})
	receive(t, inFn, "QueueLocked to lock the row")

	cancelled := make(chan error, 1)
	go func() {
		_, err := e.repo.Transition(t.Context(), cancelOpen(o, at(10*time.Minute)))
		cancelled <- err
	}()
	e.waitForLockWait(t)
	select {
	case err := <-cancelled:
		t.Fatalf("the cancel finished while QueueLocked held the row: %v", err)
	default:
	}

	release()
	require.NoError(t, receive(t, locked, "QueueLocked to return"))
	require.NoError(t, receive(t, cancelled, "the cancel to finish"))
	got := e.get(t, o.ID)
	require.Equal(t, order.StateCancelled, got.State)
	require.Equal(t, 2, got.Version, "QueueLocked itself changed no errand column")
	require.Contains(t, e.outbox(t, ev.EventID).payload, `"version": 1`, "the event carries the version it saw")
}
