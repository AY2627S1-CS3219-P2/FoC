//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Concurrency integration tests of the PostgreSQL order.Repository:
//   one winner among racing accepts and K under contention (D-046, F3.3.2 to
//   F3.3.4), accept latency against NFR2.3, and the row lock Amend holds
//   while its callback runs (D-057).
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
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

// AI-generated (edited by James Teo).
// testConcurrentCreatesRespectTheOpenLimit is D-058 under contention: ten
// creates by one requester at once, with none OPEN before, save exactly five,
// because the requester's advisory lock makes each count see the creates
// before it. The rest write nothing and keep their refunds queued. It runs
// several rounds, since without the lock only some races would pass the limit.
func (e *env) testConcurrentCreatesRespectTheOpenLimit(t *testing.T) {
	const rounds, creates = 3, 10
	for round := range rounds {
		e.reset(t)
		requester := uuid.New()
		errands := make([]order.Order, creates)
		guards := make([]uuid.UUID, creates)
		for i := range errands {
			errands[i] = openErrand(requester, at(time.Duration(i)*time.Second))
			guards[i] = e.queueGuard(t, errands[i]).EventID
		}

		errs := make([]error, creates)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, o := range errands {
			wg.Go(func() {
				<-start
				errs[i] = e.repo.Create(t.Context(), o, createdEntry(o), guards[i])
			})
		}
		close(start)
		wg.Wait()

		saved := 0
		for i, err := range errs {
			switch {
			case err == nil:
				saved++
			case errors.Is(err, order.ErrTooManyOpen):
				require.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox WHERE event_id = $1 AND published_at IS NULL`, guards[i]),
					"round %d, create %d: a refused create keeps its refund queued", round, i)
			default:
				t.Fatalf("round %d, create %d: %v", round, i, err)
			}
		}
		require.Equal(t, order.MaxOpenErrands, saved, "round %d: creates that saved", round)
		require.Equal(t, order.MaxOpenErrands, e.count(t, `SELECT count(*) FROM orders WHERE requester_id = $1`, requester),
			"round %d: errands the requester has", round)
	}
}

// AI-generated (edited by James Teo).
// serviceOn runs the domain service on repo, with ordertest's clock, credit
// and supplier fakes, T = 2h, K = 2 and C = 24h.
func serviceOn(repo order.Repository) (*order.Service, *ordertest.Clock, *ordertest.Credit, order.Supplier) {
	clock := ordertest.NewClock(at(0))
	credit := ordertest.NewCredit()
	supplier := order.Supplier{
		ID: uuid.New(), Name: "Spinelli Coffee", Building: "COM3", Floor: "1",
		LocationDescription: "next to the lift", Available: true,
	}
	svc := order.NewService(repo, credit, ordertest.NewSuppliers(supplier), clock,
		order.Config{ExpiryPeriod: 2 * time.Hour, MaxActive: 2, ConfirmWindow: 24 * time.Hour},
		slog.New(slog.DiscardHandler))
	return svc, clock, credit, supplier
}

// AI-generated (edited by James Teo).
// heldAdjust makes credit's next Adjust wait inside the call, as a slow
// credit-service would while the amend holds the errand's row lock (D-057),
// until release is called, then answer with apply and err. The test's
// cleanup releases it too, so a failing test never leaves a transaction open.
func heldAdjust(t *testing.T, credit *ordertest.Credit, apply bool, err error) (inAdjust <-chan struct{}, release func()) {
	t.Helper()
	entered := make(chan struct{})
	gate := make(chan struct{})
	release = sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) {
		close(entered)
		<-gate
		return apply, err
	}
	return entered, release
}

// AI-generated (edited by James Teo).
// testAcceptIsJudgedAtArrival pins down that a user action is judged as of
// when it arrived. An accept that arrives a second before the deadline T,
// then waits on the row lock an amend holds across its credit call (D-057)
// until T has passed, still goes through, recorded at its arrival time, and
// the sweeper then finds nothing to expire.
func (e *env) testAcceptIsJudgedAtArrival(t *testing.T) {
	e.reset(t)
	ctx := t.Context()
	svc, clock, credit, supplier := serviceOn(e.repo)
	requester := order.Caller{UserID: uuid.New(), Role: order.RoleStudent}
	courier := order.Caller{UserID: uuid.New(), Role: order.RoleStudent}
	o, _, err := svc.Create(ctx, requester, order.CreateInput{
		SupplierID: supplier.ID, ItemDescription: latte, DeliveryLocation: "COM3 Lobby", Reward: 5, CorrelationID: "corr-create",
	})
	require.NoError(t, err)
	clock.Advance(2*time.Hour - time.Second)
	arrival := clock.Now()

	inAdjust, release := heldAdjust(t, credit, false, order.ErrInsufficientCredits)
	amended := make(chan error, 1)
	go func() {
		_, err := svc.Amend(ctx, requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(9), CorrelationID: "corr-amend"})
		amended <- err
	}()
	receive(t, inAdjust, "the amend to hold the row across its credit call")
	accepted := make(chan error, 1)
	go func() {
		_, err := svc.Accept(ctx, courier, o.ID, o.Version, "corr-accept")
		accepted <- err
	}()
	e.waitForLockWait(t)
	clock.Advance(2 * time.Second)
	require.True(t, clock.Now().After(*o.ExpiresAt), "T has passed while the accept waits")

	release()
	require.Equal(t, order.KindInsufficientCredits, order.KindOf(receive(t, amended, "the amend to be refused")))
	require.NoError(t, receive(t, accepted, "the accept to go through"))
	got := e.get(t, o.ID)
	require.Equal(t, order.StateAccepted, got.State)
	require.Equal(t, arrival, got.StateChangedAt, "recorded at its arrival")
	history := e.history(t, o.ID)
	require.Equal(t, order.ActionAccept, history[len(history)-1].Action)
	require.Equal(t, arrival, history[len(history)-1].OccurredAt)
	expired, err := svc.Expire(ctx, o.ID, "corr-sweep")
	require.NoError(t, err)
	require.False(t, expired, "nothing left for the sweeper")
}

// AI-generated (edited by James Teo).
// testNoRewardSetAfterAFailedRaise is a raise whose answer is lost while the
// requester's cancel waits on the row lock the amend holds across its credit
// call. The amend saves and queues nothing (D-102), so the cancel's
// errand.cancelled is the only event, at the reward the errand kept, and
// credit-service returns the whole hold, the lost raise's extra included.
func (e *env) testNoRewardSetAfterAFailedRaise(t *testing.T) {
	e.reset(t)
	ctx := t.Context()
	svc, _, credit, supplier := serviceOn(e.repo)
	requester := order.Caller{UserID: uuid.New(), Role: order.RoleStudent}
	o, _, err := svc.Create(ctx, requester, order.CreateInput{
		SupplierID: supplier.ID, ItemDescription: latte, DeliveryLocation: "COM3 Lobby", Reward: 5, CorrelationID: "corr-create",
	})
	require.NoError(t, err)

	// credit applies the raise, but its answer is lost.
	inAdjust, release := heldAdjust(t, credit, true, ordertest.ErrInjected)
	amended := make(chan error, 1)
	go func() {
		_, err := svc.Amend(ctx, requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})
		amended <- err
	}()
	receive(t, inAdjust, "the amend to hold the row across its credit call")
	cancelled := make(chan error, 1)
	go func() {
		_, err := svc.Cancel(ctx, requester, o.ID, "corr-cancel")
		cancelled <- err
	}()
	e.waitForLockWait(t)

	release()
	require.Equal(t, order.KindUnavailable, order.KindOf(receive(t, amended, "the amend to fail")))
	require.NoError(t, receive(t, cancelled, "the cancel to commit"))
	require.Equal(t, order.StateCancelled, e.get(t, o.ID).State)
	rows, err := e.pool.Query(ctx, `SELECT routing_key, (payload->>'amount')::int FROM outbox
		WHERE payload->>'errandId' = $1 ORDER BY created_at`, o.ID.String())
	require.NoError(t, err)
	type queued struct {
		key    string
		amount int
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (queued, error) {
		var q queued
		return q, row.Scan(&q.key, &q.amount)
	})
	require.NoError(t, err)
	require.Equal(t, []queued{{order.RoutingCancelled, 5}}, events, "the settlement alone, at the reward the errand kept")
	held, _ := credit.Hold(o.ID)
	require.Equal(t, 8, held, "credit holds the lost raise; the refund returns all of it")
}
