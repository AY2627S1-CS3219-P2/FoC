// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Accept (D-046, F3.3) with its version, deadline and K checks, races
//   and concurrent accepts; withdraw, pick up and deliver (D-049, D-050); the
//   conditions each conditional write carries and what a lost one reports (D-046).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestAcceptAssignsTheCourier(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	f.clock.Advance(15 * time.Minute)
	now := f.clock.Now()

	got, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-accept")

	require.NoError(t, err)
	assert.Equal(t, order.StateAccepted, got.State)
	assert.Equal(t, ptr(f.courier.UserID), got.CourierID)
	assert.Nil(t, got.ExpiresAt, "T runs only while OPEN")
	assert.Nil(t, got.ConfirmBy)
	assert.Equal(t, o.Version+1, got.Version)
	assert.Equal(t, now, got.StateChangedAt)
	assert.Equal(t, got, f.stored(o.ID).order)

	h := f.history(o.ID)
	require.Len(t, h, 2)
	a := h[1]
	assert.Equal(t, order.ActionAccept, a.Action)
	assert.Equal(t, order.StateOpen, a.FromState)
	assert.Equal(t, order.StateAccepted, a.ToState)
	assert.Equal(t, ptr(f.courier.UserID), a.ActorID)
	assert.Equal(t, order.ActorCourier, a.ActorKind)
	assert.Equal(t, ptr(f.courier.UserID), a.CourierID)
	assert.Equal(t, now, a.OccurredAt)
	assert.Equal(t, "corr-accept", a.CorrelationID)

	assert.Empty(t, f.repo.EventsFor(o.ID), "accepting settles nothing")
	assert.Empty(t, f.credit.Adjusts())
	assert.Len(t, f.credit.Reserves(), 1, "only the create's reservation")

	// D-046: one conditional write, carrying the version the courier saw and K.
	sent := f.repo.sentAccepts()
	require.Len(t, sent, 1)
	assert.Equal(t, o.ID, sent[0].OrderID)
	assert.Equal(t, f.courier.UserID, sent[0].CourierID)
	assert.Equal(t, o.Version, sent[0].ExpectedVersion)
	assert.Equal(t, now, sent[0].Now)
	assert.Equal(t, maxActive, sent[0].MaxActive)
}

func TestTransitionsCarryTheirConditions(t *testing.T) {
	// D-046: the conditional write re-checks the state, the version and the
	// people the Go check relied on, and D-049's deadlines; the repository
	// applies it only if the row still matches (ports.go). History and event are checked by
	// the other tests.
	type run func(f *fixture, o order.Order) error
	userAction := func(act func(f *fixture, o order.Order) (order.Order, error)) run {
		return func(f *fixture, o order.Order) error { return just(act(f, o)) }
	}
	cases := []struct {
		name  string
		state order.State
		wait  time.Duration
		run   run
		want  func(f *fixture, o order.Order, now time.Time) order.Transition
	}{
		{
			name: "withdraw", state: order.StateAccepted,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StateAccepted, ExpectCourierID: ptr(f.courier.UserID),
					ToState: order.StateOpen, ExpiresAt: ptr(now.Add(expiryPeriod)),
				}
			},
		},
		{
			name: "pick up", state: order.StateAccepted,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.PickUp(f.ctx, f.courier, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StateAccepted, ExpectCourierID: ptr(f.courier.UserID),
					ToState: order.StatePickedUp, CourierID: ptr(f.courier.UserID),
				}
			},
		},
		{
			name: "deliver", state: order.StatePickedUp,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Deliver(f.ctx, f.courier, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StatePickedUp, ExpectCourierID: ptr(f.courier.UserID),
					ToState: order.StateDelivered, CourierID: ptr(f.courier.UserID), ConfirmBy: ptr(now.Add(confirmWindow)),
				}
			},
		},
		{
			name: "confirm", state: order.StateDelivered,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Confirm(f.ctx, f.requester, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StateDelivered, ExpectRequesterID: ptr(f.requester.UserID),
					ToState: order.StateCompleted, CourierID: ptr(f.courier.UserID),
				}
			},
		},
		{
			name: "cancel by the requester", state: order.StateOpen,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Cancel(f.ctx, f.requester, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StateOpen, ExpectRequesterID: ptr(f.requester.UserID), OpenBefore: ptr(now),
					ToState: order.StateCancelled,
				}
			},
		},
		{
			name: "admin cancel while OPEN", state: order.StateOpen,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Cancel(f.ctx, f.admin, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{FromState: order.StateOpen, OpenBefore: ptr(now), ToState: order.StateCancelled}
			},
		},
		{
			name: "admin cancel while PICKED_UP", state: order.StatePickedUp,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Cancel(f.ctx, f.admin, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StatePickedUp, ToState: order.StateCancelled, CourierID: ptr(f.courier.UserID),
				}
			},
		},
		{
			name: "admin complete", state: order.StateDelivered,
			run: userAction(func(f *fixture, o order.Order) (order.Order, error) {
				return f.svc.Complete(f.ctx, f.admin, o.ID, "corr")
			}),
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StateDelivered, ToState: order.StateCompleted, CourierID: ptr(f.courier.UserID),
				}
			},
		},
		{
			name: "expire", state: order.StateOpen, wait: time.Hour,
			run: func(f *fixture, o order.Order) error { return applied(f.svc.Expire(f.ctx, o.ID, "corr")) },
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{FromState: order.StateOpen, ExpiryDueBy: ptr(now), ToState: order.StateExpired}
			},
		},
		{
			name: "auto-confirm", state: order.StateDelivered, wait: 12 * time.Hour,
			run: func(f *fixture, o order.Order) error { return applied(f.svc.AutoConfirm(f.ctx, o.ID, "corr")) },
			want: func(f *fixture, o order.Order, now time.Time) order.Transition {
				return order.Transition{
					FromState: order.StateDelivered, ConfirmDueBy: ptr(now),
					ToState: order.StateCompleted, CourierID: ptr(f.courier.UserID),
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.state)
			f.clock.Advance(tc.wait)
			now := f.clock.Now()

			require.NoError(t, tc.run(f, o))

			sent := f.repo.sentTransitions()
			require.Len(t, sent, 1)
			got := sent[0]
			got.History, got.Event = order.HistoryEntry{}, nil
			want := tc.want(f, o, now)
			want.OrderID, want.Now = o.ID, now
			want.ExpectVersion = ptr(o.Version) // every write re-checks the version it read
			assert.Equal(t, want, got)
		})
	}
}

func TestAcceptRejections(t *testing.T) {
	cases := []struct {
		name string
		// prepare may move the clock or the errand; it returns the caller and
		// the version they send.
		prepare func(f *fixture, o order.Order) (order.Caller, int)
		want    order.Kind
		msg     string
	}{
		{
			name:    "the requester's own errand (F3.3.1)",
			prepare: func(f *fixture, o order.Order) (order.Caller, int) { return f.requester, o.Version },
			want:    order.KindForbidden, msg: "you cannot accept your own errand",
		},
		{
			name:    "an admin",
			prepare: func(f *fixture, o order.Order) (order.Caller, int) { return f.admin, o.Version },
			want:    order.KindForbidden,
		},
		{
			name: "a role that is not STUDENT",
			prepare: func(_ *fixture, o order.Order) (order.Caller, int) {
				return order.Caller{UserID: uuid.New()}, o.Version
			},
			want: order.KindForbidden, msg: "only students can accept errands",
		},
		{
			name: "a version from before an amend",
			prepare: func(f *fixture, o order.Order) (order.Caller, int) {
				_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(9)})
				require.NoError(f.t, err)
				return f.courier, o.Version
			},
			want: order.KindConflict, msg: changedMsg,
		},
		{
			name: "exactly at its deadline",
			prepare: func(f *fixture, o order.Order) (order.Caller, int) {
				f.clock.Advance(expiryPeriod)
				return f.courier, o.Version
			},
			want: order.KindConflict, msg: "this errand has expired",
		},
		{
			name: "past its deadline",
			prepare: func(f *fixture, o order.Order) (order.Caller, int) {
				f.clock.Advance(expiryPeriod + time.Second)
				return f.courier, o.Version
			},
			want: order.KindConflict, msg: "this errand has expired",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			c, version := tc.prepare(f, o)
			before := f.stored(o.ID)

			_, err := f.svc.Accept(f.ctx, c, o.ID, version, "corr-accept")

			requireKind(t, err, tc.want)
			if tc.msg != "" {
				assert.Equal(t, tc.msg, order.MessageOf(err))
			}
			f.requireUnchanged(before)
			f.requireNoWriteSince(before)
		})
	}
}

func TestAcceptUnknownErrand(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Accept(f.ctx, f.courier, uuid.New(), 1, "corr")
	requireKind(t, err, order.KindNotFound)
}

func TestAcceptStopsAtKActiveAssignments(t *testing.T) {
	// D-045, F3.3.4: a courier holding K errands in ACCEPTED or PICKED_UP
	// cannot accept another; DELIVERED and final ones no longer count. The K
	// count is the repository's, so this holds the in-memory fake to the
	// Accept contract in ports.go. The real count (countHeldSQL) is checked
	// against PostgreSQL by testAccept's "counts only ACCEPTED and PICKED_UP
	// towards K" in repository/writes_integration_test.go.
	for _, k := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("K=%d", k), func(t *testing.T) {
			cfg := defaultConfig()
			cfg.MaxActive = k
			f := newFixtureWith(t, cfg)
			for i := range k {
				if i%2 == 0 {
					f.seed(order.StateAccepted)
				} else {
					f.seed(order.StatePickedUp)
				}
			}
			f.seed(order.StateDelivered)
			f.seed(order.StateCompleted)
			o := f.seed(order.StateOpen)
			before := f.stored(o.ID)

			_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr")

			requireKind(t, err, order.KindConflict)
			assert.Contains(t, order.MessageOf(err), fmt.Sprintf("you already have %d errand", k), "the message names K")
			f.requireUnchanged(before)

			// Another courier holding K-1 of their own, plus a DELIVERED, a
			// COMPLETED and a CANCELLED one that do not count, may still
			// accept it.
			other := student()
			states := []order.State{order.StateDelivered, order.StateCompleted, order.StateCancelled}
			for range k - 1 {
				states = append(states, order.StateAccepted)
			}
			for _, s := range states {
				held := f.seed(s)
				held.CourierID = ptr(other.UserID)
				f.repo.Put(held)
			}
			got, err := f.svc.Accept(f.ctx, other, o.ID, o.Version, "corr")
			require.NoError(t, err)
			assert.Equal(t, ptr(other.UserID), got.CourierID)
		})
	}
}

func TestAcceptLosesARace(t *testing.T) {
	// D-046: the conditional write matched no row, so the errand is re-read to
	// say why. Each race completes another request between this courier's
	// read and their write.
	cases := []struct {
		name  string
		race  func(f *fixture, o order.Order)
		msg   string
		state order.State
		// holder is who holds the errand after the race; nil means nobody.
		holder func(f *fixture) *uuid.UUID
	}{
		{
			name: "another courier accepted it first (F3.3.3)",
			race: func(f *fixture, o order.Order) {
				_, err := f.svc.Accept(f.ctx, f.stranger, o.ID, o.Version, "corr-race")
				require.NoError(f.t, err)
			},
			msg: "someone else accepted this errand first", state: order.StateAccepted,
			holder: func(f *fixture) *uuid.UUID { return ptr(f.stranger.UserID) },
		},
		{
			// Nobody holds it now, so nobody took it from this courier: it
			// is open again with a new version, and they must refresh.
			name: "another courier accepted it and withdrew",
			race: func(f *fixture, o order.Order) {
				held, err := f.svc.Accept(f.ctx, f.stranger, o.ID, o.Version, "corr-race")
				require.NoError(f.t, err)
				_, err = f.svc.Withdraw(f.ctx, f.stranger, held.ID, "corr-race")
				require.NoError(f.t, err)
			},
			msg: changedMsg, state: order.StateOpen,
		},
		{
			// A double tap, or the same courier's other tab: the one holding
			// the errand is this courier, not "someone else".
			name: "this courier's other tab accepted it first",
			race: func(f *fixture, o order.Order) {
				_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-other-tab")
				require.NoError(f.t, err)
			},
			msg: "this errand is now accepted", state: order.StateAccepted,
			holder: func(f *fixture) *uuid.UUID { return ptr(f.courier.UserID) },
		},
		{
			name: "the requester amended it",
			race: func(f *fixture, o order.Order) {
				_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(9)})
				require.NoError(f.t, err)
			},
			msg: changedMsg, state: order.StateOpen,
		},
		{
			// This courier has no part in the errand and may no longer see it,
			// so they are not told what became of it (D-045).
			name: "the requester cancelled it",
			race: func(f *fixture, o order.Order) {
				_, err := f.svc.Cancel(f.ctx, f.requester, o.ID, "corr-race")
				require.NoError(f.t, err)
			},
			msg: "this errand is no longer open", state: order.StateCancelled,
		},
		{
			name: "an admin cancelled it",
			race: func(f *fixture, o order.Order) {
				_, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "corr-race")
				require.NoError(f.t, err)
			},
			msg: "this errand is no longer open", state: order.StateCancelled,
		},
		{
			name: "the sweeper expired it",
			race: func(f *fixture, o order.Order) {
				f.clock.Advance(time.Hour)
				ok, err := f.svc.Expire(f.ctx, o.ID, "corr-sweep")
				require.NoError(f.t, err)
				require.True(f.t, ok)
			},
			msg: "this errand is no longer open", state: order.StateExpired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(order.StateOpen)
			f.repo.beforeNext("Accept", func() { tc.race(f, o) })

			_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-accept")

			requireKind(t, err, order.KindConflict)
			assert.Equal(t, tc.msg, order.MessageOf(err))
			after := f.stored(o.ID)
			assert.Equal(t, tc.state, after.order.State)
			var holder *uuid.UUID
			if tc.holder != nil {
				holder = tc.holder(f)
			}
			assert.Equal(t, holder, after.order.CourierID, "the race's outcome stands; the losing accept changed nobody's assignment")
			for _, h := range after.history {
				assert.NotEqual(t, "corr-accept", h.CorrelationID, "the losing accept wrote no history")
			}
		})
	}
}

func TestAcceptNamesNoStateToAStudentWithNoPartInTheErrand(t *testing.T) {
	// A student with no part in an errand that has left OPEN may no longer
	// see it (F3.6.2, D-045), so their 409 does not say what became of it.
	// The courier holding it may see it, and is told its state.
	const gone = "this errand is no longer open"
	cases := []struct {
		state order.State
		who   party
		msg   string
	}{
		{order.StateAccepted, asStranger, gone},
		{order.StatePickedUp, asStranger, gone},
		{order.StateDelivered, asStranger, gone},
		{order.StateCompleted, asStranger, gone},
		{order.StateCancelled, asStranger, gone},
		{order.StateExpired, asStranger, gone},
		{order.StateAccepted, asCourier, "this errand is now accepted"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s as %s", tc.state, tc.who), func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.state)
			c := f.as(tc.who)
			before := f.stored(o.ID)
			if tc.who == asStranger {
				require.False(t, order.CanView(o, c, f.clock.Now()), "the errand is hidden from them")
			}

			_, err := f.svc.Accept(f.ctx, c, o.ID, o.Version, "corr-accept")

			requireKind(t, err, order.KindConflict)
			assert.Equal(t, tc.msg, order.MessageOf(err))
			f.requireUnchanged(before)
			f.requireNoWriteSince(before)
		})
	}
}

func TestConcurrentAcceptsHaveExactlyOneWinner(t *testing.T) {
	// F3.3.2, F3.3.3: of many couriers accepting at once, exactly one is
	// assigned and every other is told the errand is gone.
	f := newFixture(t)
	o := f.seed(order.StateOpen)
	couriers := make([]order.Caller, 8)
	for i := range couriers {
		couriers[i] = student()
	}
	errs := make([]error, len(couriers))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, c := range couriers {
		wg.Go(func() {
			<-start
			_, errs[i] = f.svc.Accept(f.ctx, c, o.ID, o.Version, "corr")
		})
	}
	close(start)
	wg.Wait()

	var winner *uuid.UUID
	for i, err := range errs {
		if err == nil {
			require.Nil(t, winner, "a second courier was assigned")
			winner = ptr(couriers[i].UserID)
			continue
		}
		requireKind(t, err, order.KindConflict)
	}
	require.NotNil(t, winner, "nobody was assigned")
	got := f.stored(o.ID)
	assert.Equal(t, order.StateAccepted, got.order.State)
	assert.Equal(t, winner, got.order.CourierID)
	require.Len(t, got.history, 1, "one ACCEPT row")
	assert.Equal(t, winner, got.history[0].ActorID)
}

func TestConcurrentAcceptsByOneCourierStopAtK(t *testing.T) {
	f := newFixture(t)
	errands := make([]order.Order, 6)
	for i := range errands {
		errands[i] = f.seed(order.StateOpen)
	}
	errs := make([]error, len(errands))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, o := range errands {
		wg.Go(func() {
			<-start
			_, errs[i] = f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr")
		})
	}
	close(start)
	wg.Wait()

	accepted := 0
	for _, err := range errs {
		if err == nil {
			accepted++
			continue
		}
		requireKind(t, err, order.KindConflict)
	}
	assert.Equal(t, maxActive, accepted, "exactly K accepts succeed")
	held, err := f.repo.ListByCourier(f.ctx, f.courier.UserID, []order.State{order.StateAccepted}, 0)
	require.NoError(t, err)
	assert.Len(t, held, maxActive)
}

func TestWithdrawReopensTheErrandWithAFreshDeadline(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	f.clock.Advance(10 * time.Minute)
	accepted, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-accept")
	require.NoError(t, err)
	f.clock.Advance(40 * time.Minute)
	now := f.clock.Now()

	got, err := f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr-withdraw")

	require.NoError(t, err)
	assert.Equal(t, order.StateOpen, got.State)
	assert.Nil(t, got.CourierID, "F3.12.3: the courier is unassigned")
	assert.Equal(t, ptr(now.Add(expiryPeriod)), got.ExpiresAt, "expiry restarts at now + T")
	assert.Nil(t, got.ConfirmBy)
	assert.Equal(t, accepted.Version+1, got.Version)
	assert.Equal(t, now, got.StateChangedAt)
	assert.Equal(t, got, f.stored(o.ID).order)

	h := f.history(o.ID)
	require.Len(t, h, 3)
	w := h[2]
	assert.Equal(t, order.ActionWithdraw, w.Action)
	assert.Equal(t, order.StateAccepted, w.FromState)
	assert.Equal(t, order.StateOpen, w.ToState)
	assert.Equal(t, ptr(f.courier.UserID), w.ActorID)
	assert.Equal(t, order.ActorCourier, w.ActorKind)
	assert.Equal(t, ptr(f.courier.UserID), w.CourierID, "the row keeps who withdrew (F3.5.3)")
	assert.Equal(t, "corr-withdraw", w.CorrelationID)
	assert.Empty(t, f.repo.EventsFor(o.ID), "withdrawing settles nothing")

	// AI-generated (edited by James Teo).
	// Back in OPEN, any student but the requester may take it, except the
	// courier who withdrew (D-060).
	assert.Equal(t, []uuid.UUID{f.courier.UserID}, got.WithdrawnCouriers)
	assert.Empty(t, order.AllowedActions(got, f.courier, now))
	assert.ElementsMatch(t, []order.APIAction{order.APIAccept}, order.AllowedActions(got, f.stranger, now))
	again, err := f.svc.Accept(f.ctx, f.stranger, o.ID, got.Version, "corr-accept-2")
	require.NoError(t, err)
	assert.Equal(t, ptr(f.stranger.UserID), again.CourierID)
}

// AI-generated (edited by James Teo).
func TestACourierWhoWithdrewCannotAcceptAgain(t *testing.T) {
	// D-060: a withdrawal gives the errand up for good to that courier, so
	// accept and withdraw cannot be cycled to keep the requester's credits
	// held and the errand off the ADMIN's stalled list. Anyone else may still
	// take it.
	f := newFixture(t)
	o := f.post()
	_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-accept")
	require.NoError(t, err)
	reopened, err := f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr-withdraw")
	require.NoError(t, err)
	now := f.clock.Now()

	assert.Empty(t, order.AllowedActions(reopened, f.courier, now), "accept is not offered to them")
	open, err := f.svc.ListOpen(f.ctx, f.courier)
	require.NoError(t, err)
	require.Len(t, open, 1, "they still see it listed")
	assert.Empty(t, order.AllowedActions(open[0], f.courier, now), "without accept")
	before := f.stored(o.ID)

	_, err = f.svc.Accept(f.ctx, f.courier, o.ID, reopened.Version, "corr-accept-again")

	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "you withdrew from this errand, so you cannot accept it again", order.MessageOf(err))
	f.requireUnchanged(before)
	f.requireNoWriteSince(before)

	got, err := f.svc.Accept(f.ctx, f.stranger, o.ID, reopened.Version, "corr-accept-other")
	require.NoError(t, err)
	assert.Equal(t, ptr(f.stranger.UserID), got.CourierID)
	assert.Equal(t, []uuid.UUID{f.courier.UserID}, got.WithdrawnCouriers)
}

// AI-generated (edited by James Teo).
func TestTheAcceptWriteRefusesACourierWhoWithdrew(t *testing.T) {
	// The accept's conditional write checks D-060 itself, as acceptSQL does,
	// so the rule holds for a command the check in Go did not refuse.
	f := newFixture(t)
	o := f.post()
	_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-accept")
	require.NoError(t, err)
	reopened, err := f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr-withdraw")
	require.NoError(t, err)

	_, err = f.repo.Accept(f.ctx, order.AcceptCommand{
		OrderID: o.ID, CourierID: f.courier.UserID, ExpectedVersion: reopened.Version,
		Now: f.clock.Now(), MaxActive: maxActive,
	})

	require.ErrorIs(t, err, order.ErrNoMatch)
}

func TestTheSweepUsesTheDeadlineAWithdrawSets(t *testing.T) {
	// D-049: expiry restarts when a withdraw reopens the errand, so the
	// sweeper and the open list go by the new deadline, not the one
	// it was posted with.
	f := newFixture(t)
	o := f.post()
	f.clock.Advance(expiryPeriod - 10*time.Minute)
	_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-accept")
	require.NoError(t, err)
	f.clock.Advance(5 * time.Minute)
	withdrawnAt := f.clock.Now()
	_, err = f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr-withdraw")
	require.NoError(t, err)

	f.clock.Advance(10 * time.Minute)
	require.True(t, f.clock.Now().After(*o.ExpiresAt), "past the deadline it was posted with")
	ok, err := f.svc.Expire(f.ctx, o.ID, "corr-sweep-early")
	require.NoError(t, err)
	assert.False(t, ok, "not due: the withdraw restarted T")
	open, err := f.svc.ListOpen(f.ctx, f.stranger)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{o.ID}, ids(open), "still listed for couriers")

	f.clock.Advance(withdrawnAt.Add(expiryPeriod).Sub(f.clock.Now()))
	ok, err = f.svc.Expire(f.ctx, o.ID, "corr-sweep")
	require.NoError(t, err)
	assert.True(t, ok, "due exactly T after the withdraw")
	after := f.stored(o.ID)
	assert.Equal(t, order.StateExpired, after.order.State)
	require.Len(t, after.events, 1, "one refund")
	assertEvent(t, after.events[0], order.RoutingExpired, after.order, nil, f.clock.Now(), "corr-sweep")
}

func TestPickUpKeepsTheCourier(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateAccepted)
	f.clock.Advance(5 * time.Minute)
	now := f.clock.Now()

	got, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr-pickup")

	require.NoError(t, err)
	assert.Equal(t, order.StatePickedUp, got.State)
	assert.Equal(t, ptr(f.courier.UserID), got.CourierID)
	assert.Nil(t, got.ExpiresAt, "no timer runs in PICKED_UP")
	assert.Nil(t, got.ConfirmBy)
	assert.Equal(t, now, got.StateChangedAt)
	assert.Empty(t, f.repo.EventsFor(o.ID))
}

func TestDeliverStartsTheConfirmationWindow(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StatePickedUp)
	f.clock.Advance(5 * time.Minute)
	now := f.clock.Now()

	got, err := f.svc.Deliver(f.ctx, f.courier, o.ID, "corr-deliver")

	require.NoError(t, err)
	assert.Equal(t, order.StateDelivered, got.State)
	assert.Equal(t, ptr(now.Add(confirmWindow)), got.ConfirmBy, "C starts on delivery")
	assert.Nil(t, got.ExpiresAt)
	assert.Equal(t, ptr(f.courier.UserID), got.CourierID)
	assert.Empty(t, f.repo.EventsFor(o.ID), "the courier is paid on confirmation, not delivery")
	h := f.history(o.ID)
	require.Len(t, h, 1)
	assert.Equal(t, order.ActionDeliver, h[0].Action)
	assert.Equal(t, "corr-deliver", h[0].CorrelationID)
}

func TestUserActionsOnAnUnknownErrand(t *testing.T) {
	f := newFixture(t)
	actions := map[string]func(order.Caller, uuid.UUID) (order.Order, error){
		"withdraw": func(c order.Caller, id uuid.UUID) (order.Order, error) { return f.svc.Withdraw(f.ctx, c, id, "corr") },
		"pick up":  func(c order.Caller, id uuid.UUID) (order.Order, error) { return f.svc.PickUp(f.ctx, c, id, "corr") },
		"deliver":  func(c order.Caller, id uuid.UUID) (order.Order, error) { return f.svc.Deliver(f.ctx, c, id, "corr") },
		"confirm":  func(c order.Caller, id uuid.UUID) (order.Order, error) { return f.svc.Confirm(f.ctx, c, id, "corr") },
		"cancel":   func(c order.Caller, id uuid.UUID) (order.Order, error) { return f.svc.Cancel(f.ctx, c, id, "corr") },
		"complete": func(c order.Caller, id uuid.UUID) (order.Order, error) { return f.svc.Complete(f.ctx, c, id, "corr") },
	}
	for name, run := range actions {
		for _, p := range parties() {
			t.Run(name+" as "+p.String(), func(t *testing.T) {
				_, err := run(f.as(p), uuid.New())
				requireKind(t, err, order.KindNotFound)
				assert.Equal(t, "errand not found", order.MessageOf(err))
			})
		}
	}
}

func TestUserActionsWhenStorageFails(t *testing.T) {
	// A storage failure is not a domain error: the transport answers 500 and
	// logs the cause.
	cases := []struct {
		name   string
		method string
		state  order.State
		run    func(f *fixture, o order.Order) error
	}{
		{"loading the errand", "Get", order.StateAccepted, func(f *fixture, o order.Order) error {
			_, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr")
			return err
		}},
		{"writing a transition", "Transition", order.StateAccepted, func(f *fixture, o order.Order) error {
			_, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr")
			return err
		}},
		{"writing an accept", "Accept", order.StateOpen, func(f *fixture, o order.Order) error {
			_, err := f.svc.Accept(f.ctx, f.stranger, o.ID, o.Version, "corr")
			return err
		}},
		{"loading an errand to accept", "Get", order.StateOpen, func(f *fixture, o order.Order) error {
			_, err := f.svc.Accept(f.ctx, f.stranger, o.ID, o.Version, "corr")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.state)
			before := f.stored(o.ID)
			f.repo.FailNext(tc.method, ordertest.ErrInjected)

			err := tc.run(f, o)

			require.Error(t, err)
			assert.Equal(t, order.Kind(0), order.KindOf(err))
			assert.ErrorIs(t, err, ordertest.ErrInjected)
			f.requireUnchanged(before)
		})
	}
}

func TestUserActionLosesARace(t *testing.T) {
	// D-046: when the conditional write matches no row, the re-read explains
	// it with a 409 for the state the errand is now in.
	t.Run("an admin cancelled it before the pick-up was written", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateAccepted)
		f.repo.beforeNext("Transition", func() {
			_, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "corr-admin")
			require.NoError(t, err)
		})

		_, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr-pickup")

		requireKind(t, err, order.KindConflict)
		assert.Equal(t, "this errand is now cancelled", order.MessageOf(err))
		assert.Equal(t, order.StateCancelled, f.stored(o.ID).order.State)
	})
	t.Run("the courier withdrew and another accepted before the pick-up was written", func(t *testing.T) {
		// The write re-checks the courier, not only the state: the errand is
		// ACCEPTED again, but by someone else (D-046).
		f := newFixture(t)
		o := f.seed(order.StateAccepted)
		f.repo.beforeNext("Transition", func() {
			reopened, err := f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr-other-tab")
			require.NoError(t, err)
			_, err = f.svc.Accept(f.ctx, f.stranger, o.ID, reopened.Version, "corr-other-courier")
			require.NoError(t, err)
		})

		_, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr-pickup")

		requireKind(t, err, order.KindForbidden)
		assert.Equal(t, "only the errand's assigned courier can do this", order.MessageOf(err))
		now := f.stored(o.ID).order
		assert.Equal(t, order.StateAccepted, now.State, "not picked up")
		assert.Equal(t, ptr(f.stranger.UserID), now.CourierID)
	})
	t.Run("auto-confirm completed it before the confirm was written", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateDelivered)
		f.clock.Advance(12 * time.Hour)
		f.repo.beforeNext("Transition", func() {
			ok, err := f.svc.AutoConfirm(f.ctx, o.ID, "corr-sweep")
			require.NoError(t, err)
			require.True(t, ok)
		})

		_, err := f.svc.Confirm(f.ctx, f.requester, o.ID, "corr-confirm")

		requireKind(t, err, order.KindConflict)
		assert.Equal(t, "this errand is now completed", order.MessageOf(err))
		events := f.repo.EventsFor(o.ID)
		require.Len(t, events, 1, "the courier is paid once")
		assert.Equal(t, order.RoutingCompleted, events[0].RoutingKey)
	})
	t.Run("the requester cancelled it before an admin cancel was written", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateOpen)
		f.repo.beforeNext("Transition", func() {
			_, err := f.svc.Cancel(f.ctx, f.requester, o.ID, "corr-requester")
			require.NoError(t, err)
		})

		_, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "corr-admin")

		requireKind(t, err, order.KindConflict)
		assert.Equal(t, "this errand is now cancelled", order.MessageOf(err))
		require.Len(t, f.repo.EventsFor(o.ID), 1, "the requester is refunded once")
	})
}

func TestAMissedWriteTheReReadCannotExplain(t *testing.T) {
	// D-046's fallback: the conditional write matched no row, yet the re-read
	// errand still allows the action, so all the service can say is that it
	// changed meanwhile.
	const msg = "this errand changed while you were acting on it; refresh and try again"
	t.Run("a courier action", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateAccepted)
		f.repo.FailNext("Transition", order.ErrNoMatch)

		_, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr")

		requireKind(t, err, order.KindConflict)
		assert.Equal(t, msg, order.MessageOf(err))
	})
	t.Run("an accept", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateOpen)
		f.repo.FailNext("Accept", order.ErrNoMatch)

		_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr")

		requireKind(t, err, order.KindConflict)
		assert.Equal(t, msg, order.MessageOf(err))
	})
	t.Run("re-reading the errand fails", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateAccepted)
		f.repo.beforeNext("Transition", func() {
			f.repo.FailNext("Transition", order.ErrNoMatch)
			f.repo.FailNext("Get", ordertest.ErrInjected)
		})

		_, err := f.svc.PickUp(f.ctx, f.courier, o.ID, "corr")

		assert.Equal(t, order.Kind(0), order.KindOf(err))
		assert.ErrorIs(t, err, ordertest.ErrInjected)
	})
}

func TestAMissedWriteOnAnErrandThatIsGone(t *testing.T) {
	// D-046: the re-read after a write that matched no row answers 404 when
	// the errand is no longer there, the same as for an id never seen.
	cases := []struct {
		name   string
		method string // the conditional write that misses
		state  order.State
		run    func(f *fixture, o order.Order) error
	}{
		{"a courier action", "Transition", order.StateAccepted, func(f *fixture, o order.Order) error {
			return just(f.svc.PickUp(f.ctx, f.courier, o.ID, "corr"))
		}},
		{"an admin action", "Transition", order.StateDelivered, func(f *fixture, o order.Order) error {
			return just(f.svc.Complete(f.ctx, f.admin, o.ID, "corr"))
		}},
		{"an accept", "Accept", order.StateOpen, func(f *fixture, o order.Order) error {
			return just(f.svc.Accept(f.ctx, f.stranger, o.ID, o.Version, "corr"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.state)
			f.repo.beforeNext(tc.method, func() {
				f.repo.FailNext(tc.method, order.ErrNoMatch)
				f.repo.FailNext("Get", order.ErrNotFound)
			})

			err := tc.run(f, o)

			requireKind(t, err, order.KindNotFound)
			assert.Equal(t, "errand not found", order.MessageOf(err))
		})
	}
}
