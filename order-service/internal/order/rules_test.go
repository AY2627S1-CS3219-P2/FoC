// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: D-045's transition table with D-046's status codes and the admin
//   rules, run through the public service for every action, state and
//   relationship; AllowedActions, CanView and CanSeePrivate checked against it
//   (D-045, D-049, D-053, F3.6.2).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

// outcome is what one caller gets from one action on an errand in one state.
type outcome int

const (
	allowed   outcome = iota + 1
	forbidden         // 403: the caller's relationship to the errand rules it out
	conflict          // 409: the errand's state or deadline rules it out now
)

// matrixRow is one action on an errand in one state, and what each party gets,
// in parties() order: requester, courier, another student, admin.
type matrixRow struct {
	action order.APIAction
	state  order.State
	want   [4]outcome
}

// transitionMatrix is D-045's table as the API exposes it. D-046's codes: a
// caller the rule does not name gets 403 whatever the state, except that an
// assigned courier re-accepting their own errand gets 409; the caller the rule
// names gets 409 in any state the rule does not start from. D-045: ADMIN
// cancels any errand that is not final, completes PICKED_UP or DELIVERED, and
// cannot amend; final errands are frozen for everyone. In OPEN and EXPIRED
// nobody is assigned, so the courier column there is the student who held the
// errand and withdrew: just another student now.
func transitionMatrix() []matrixRow {
	return []matrixRow{
		// Amend: OPEN → OPEN, the requester.
		{order.APIAmend, order.StateOpen, [4]outcome{allowed, forbidden, forbidden, forbidden}},
		{order.APIAmend, order.StateAccepted, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIAmend, order.StatePickedUp, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIAmend, order.StateDelivered, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIAmend, order.StateCompleted, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIAmend, order.StateCancelled, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIAmend, order.StateExpired, [4]outcome{conflict, forbidden, forbidden, forbidden}},

		// Accept: OPEN → ACCEPTED, any STUDENT except the requester.
		{order.APIAccept, order.StateOpen, [4]outcome{forbidden, allowed, allowed, forbidden}},
		{order.APIAccept, order.StateAccepted, [4]outcome{forbidden, conflict, conflict, forbidden}},
		{order.APIAccept, order.StatePickedUp, [4]outcome{forbidden, conflict, conflict, forbidden}},
		{order.APIAccept, order.StateDelivered, [4]outcome{forbidden, conflict, conflict, forbidden}},
		{order.APIAccept, order.StateCompleted, [4]outcome{forbidden, conflict, conflict, forbidden}},
		{order.APIAccept, order.StateCancelled, [4]outcome{forbidden, conflict, conflict, forbidden}},
		{order.APIAccept, order.StateExpired, [4]outcome{forbidden, conflict, conflict, forbidden}},

		// Withdraw: ACCEPTED → OPEN, the assigned courier.
		{order.APIWithdraw, order.StateOpen, [4]outcome{forbidden, forbidden, forbidden, forbidden}},
		{order.APIWithdraw, order.StateAccepted, [4]outcome{forbidden, allowed, forbidden, forbidden}},
		{order.APIWithdraw, order.StatePickedUp, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIWithdraw, order.StateDelivered, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIWithdraw, order.StateCompleted, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIWithdraw, order.StateCancelled, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIWithdraw, order.StateExpired, [4]outcome{forbidden, forbidden, forbidden, forbidden}},

		// Pick up: ACCEPTED → PICKED_UP, the assigned courier.
		{order.APIPickUp, order.StateOpen, [4]outcome{forbidden, forbidden, forbidden, forbidden}},
		{order.APIPickUp, order.StateAccepted, [4]outcome{forbidden, allowed, forbidden, forbidden}},
		{order.APIPickUp, order.StatePickedUp, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIPickUp, order.StateDelivered, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIPickUp, order.StateCompleted, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIPickUp, order.StateCancelled, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIPickUp, order.StateExpired, [4]outcome{forbidden, forbidden, forbidden, forbidden}},

		// Deliver: PICKED_UP → DELIVERED, the assigned courier.
		{order.APIDeliver, order.StateOpen, [4]outcome{forbidden, forbidden, forbidden, forbidden}},
		{order.APIDeliver, order.StateAccepted, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIDeliver, order.StatePickedUp, [4]outcome{forbidden, allowed, forbidden, forbidden}},
		{order.APIDeliver, order.StateDelivered, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIDeliver, order.StateCompleted, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIDeliver, order.StateCancelled, [4]outcome{forbidden, conflict, forbidden, forbidden}},
		{order.APIDeliver, order.StateExpired, [4]outcome{forbidden, forbidden, forbidden, forbidden}},

		// Confirm: DELIVERED → COMPLETED, the requester.
		{order.APIConfirm, order.StateOpen, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIConfirm, order.StateAccepted, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIConfirm, order.StatePickedUp, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIConfirm, order.StateDelivered, [4]outcome{allowed, forbidden, forbidden, forbidden}},
		{order.APIConfirm, order.StateCompleted, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIConfirm, order.StateCancelled, [4]outcome{conflict, forbidden, forbidden, forbidden}},
		{order.APIConfirm, order.StateExpired, [4]outcome{conflict, forbidden, forbidden, forbidden}},

		// Cancel: OPEN → CANCELLED for the requester; any non-final state →
		// CANCELLED for ADMIN (admin cancel).
		{order.APICancel, order.StateOpen, [4]outcome{allowed, forbidden, forbidden, allowed}},
		{order.APICancel, order.StateAccepted, [4]outcome{conflict, forbidden, forbidden, allowed}},
		{order.APICancel, order.StatePickedUp, [4]outcome{conflict, forbidden, forbidden, allowed}},
		{order.APICancel, order.StateDelivered, [4]outcome{conflict, forbidden, forbidden, allowed}},
		{order.APICancel, order.StateCompleted, [4]outcome{conflict, forbidden, forbidden, conflict}},
		{order.APICancel, order.StateCancelled, [4]outcome{conflict, forbidden, forbidden, conflict}},
		{order.APICancel, order.StateExpired, [4]outcome{conflict, forbidden, forbidden, conflict}},

		// Complete: PICKED_UP or DELIVERED → COMPLETED, ADMIN (admin complete).
		{order.APIComplete, order.StateOpen, [4]outcome{forbidden, forbidden, forbidden, conflict}},
		{order.APIComplete, order.StateAccepted, [4]outcome{forbidden, forbidden, forbidden, conflict}},
		{order.APIComplete, order.StatePickedUp, [4]outcome{forbidden, forbidden, forbidden, allowed}},
		{order.APIComplete, order.StateDelivered, [4]outcome{forbidden, forbidden, forbidden, allowed}},
		{order.APIComplete, order.StateCompleted, [4]outcome{forbidden, forbidden, forbidden, conflict}},
		{order.APIComplete, order.StateCancelled, [4]outcome{forbidden, forbidden, forbidden, conflict}},
		{order.APIComplete, order.StateExpired, [4]outcome{forbidden, forbidden, forbidden, conflict}},
	}
}

// effect is what an allowed action leaves behind: the state (D-045), the
// history action and actor recorded (D-046), and the event, if it settles
// credits (D-048).
type effect struct {
	state  order.State
	action order.Action
	actor  order.ActorKind
	event  string
}

func effectOf(a order.APIAction, p party) effect {
	switch a {
	case order.APIAmend:
		return effect{order.StateOpen, order.ActionAmend, order.ActorRequester, ""}
	case order.APIAccept:
		return effect{order.StateAccepted, order.ActionAccept, order.ActorCourier, ""}
	case order.APIWithdraw:
		return effect{order.StateOpen, order.ActionWithdraw, order.ActorCourier, ""}
	case order.APIPickUp:
		return effect{order.StatePickedUp, order.ActionPickUp, order.ActorCourier, ""}
	case order.APIDeliver:
		return effect{order.StateDelivered, order.ActionDeliver, order.ActorCourier, ""}
	case order.APIConfirm:
		return effect{order.StateCompleted, order.ActionConfirm, order.ActorRequester, order.RoutingCompleted}
	case order.APICancel:
		if p == asAdmin {
			return effect{order.StateCancelled, order.ActionAdminCancel, order.ActorAdmin, order.RoutingCancelled}
		}
		return effect{order.StateCancelled, order.ActionCancel, order.ActorRequester, order.RoutingCancelled}
	default: // APIComplete
		return effect{order.StateCompleted, order.ActionAdminComplete, order.ActorAdmin, order.RoutingCompleted}
	}
}

// run performs the API action a on o as c, through the public service.
func (f *fixture) run(a order.APIAction, c order.Caller, o order.Order) (order.Order, error) {
	f.t.Helper()
	const corr = "corr-matrix"
	switch a {
	case order.APIAmend:
		return f.svc.Amend(f.ctx, c, o.ID, order.AmendInput{
			Version:         o.Version,
			ItemDescription: ptr("Three kaya toast sets"),
			CorrelationID:   corr,
		})
	case order.APIAccept:
		return f.svc.Accept(f.ctx, c, o.ID, o.Version, corr)
	case order.APIWithdraw:
		return f.svc.Withdraw(f.ctx, c, o.ID, corr)
	case order.APIPickUp:
		return f.svc.PickUp(f.ctx, c, o.ID, corr)
	case order.APIDeliver:
		return f.svc.Deliver(f.ctx, c, o.ID, corr)
	case order.APIConfirm:
		return f.svc.Confirm(f.ctx, c, o.ID, corr)
	case order.APICancel:
		return f.svc.Cancel(f.ctx, c, o.ID, corr)
	case order.APIComplete:
		return f.svc.Complete(f.ctx, c, o.ID, corr)
	}
	f.t.Fatalf("no API action %q", a)
	return order.Order{}, nil
}

func TestEveryActionInEveryStateForEveryParty(t *testing.T) {
	for _, row := range transitionMatrix() {
		for i, p := range parties() {
			want := row.want[i]
			t.Run(fmt.Sprintf("%s %s as %s", row.action, row.state, p), func(t *testing.T) {
				f := newFixture(t)
				o := f.seed(row.state)
				before := f.stored(o.ID)

				got, err := f.run(row.action, f.as(p), o)

				switch want {
				case allowed:
					require.NoError(t, err)
					e := effectOf(row.action, p)
					after := f.stored(o.ID)
					assert.Equal(t, after.order, got, "the service returns the row as written")
					assert.Equal(t, e.state, got.State)
					assert.Equal(t, o.Version+1, got.Version)

					require.Len(t, after.history, 1, "one history row per action")
					h := after.history[0]
					assert.Equal(t, e.action, h.Action)
					assert.Equal(t, row.state, h.FromState)
					assert.Equal(t, e.state, h.ToState)
					assert.Equal(t, e.actor, h.ActorKind)
					assert.Equal(t, ptr(f.as(p).UserID), h.ActorID)

					if e.event == "" {
						assert.Empty(t, after.events, "only a settlement emits an event")
					} else {
						require.Len(t, after.events, 1)
						assert.Equal(t, e.event, after.events[0].RoutingKey)
					}
				case forbidden:
					requireKind(t, err, order.KindForbidden)
					f.requireUnchanged(before)
					f.requireNoWriteSince(before)
				case conflict:
					requireKind(t, err, order.KindConflict)
					f.requireUnchanged(before)
					f.requireNoWriteSince(before)
				}
			})
		}
	}
}

func TestAllowedActionsListsExactlyTheAllowedCells(t *testing.T) {
	want := map[order.State]map[party][]order.APIAction{}
	for _, row := range transitionMatrix() {
		if want[row.state] == nil {
			want[row.state] = map[party][]order.APIAction{}
		}
		for i, p := range parties() {
			if row.want[i] == allowed {
				want[row.state][p] = append(want[row.state][p], row.action)
			}
		}
	}

	f := newFixture(t)
	for _, s := range allStates() {
		o := f.seed(s)
		for _, p := range parties() {
			t.Run(fmt.Sprintf("%s as %s", s, p), func(t *testing.T) {
				got := order.AllowedActions(o, f.as(p), f.clock.Now())
				assert.NotNil(t, got, "an empty list, not nil, so the JSON is []")
				assert.ElementsMatch(t, want[s][p], got)
			})
		}
	}
}

func TestFinalErrandsAreFrozenForEveryone(t *testing.T) {
	// D-045: no action, by anyone, leaves COMPLETED, CANCELLED or EXPIRED.
	f := newFixture(t)
	for _, s := range order.FinalStates() {
		assert.True(t, s.Final())
		o := f.seed(s)
		for _, p := range parties() {
			assert.Empty(t, order.AllowedActions(o, f.as(p), f.clock.Now()), "%s as %s", s, p)
		}
	}
}

func TestActionsOnAnOpenErrandPastItsDeadline(t *testing.T) {
	// D-049: user actions check the deadline themselves, so an errand the
	// sweeper has not expired yet is already closed (F3.10.2). The clock sits
	// exactly on the deadline, which already counts as passed.
	const expired = "this errand has expired"
	rows := []struct {
		action order.APIAction
		want   [4]outcome
		msg    string // the message every 409 in the row carries
	}{
		{order.APIAmend, [4]outcome{conflict, forbidden, forbidden, forbidden}, expired},
		{order.APIAccept, [4]outcome{forbidden, conflict, conflict, forbidden}, expired},
		{order.APIWithdraw, [4]outcome{forbidden, forbidden, forbidden, forbidden}, ""},
		{order.APIPickUp, [4]outcome{forbidden, forbidden, forbidden, forbidden}, ""},
		{order.APIDeliver, [4]outcome{forbidden, forbidden, forbidden, forbidden}, ""},
		{order.APIConfirm, [4]outcome{conflict, forbidden, forbidden, forbidden}, "this errand is now open"},
		{order.APICancel, [4]outcome{conflict, forbidden, forbidden, conflict}, expired},
		{order.APIComplete, [4]outcome{forbidden, forbidden, forbidden, conflict}, "this errand is now open"},
	}
	for _, row := range rows {
		for i, p := range parties() {
			want := row.want[i]
			t.Run(fmt.Sprintf("%s as %s", row.action, p), func(t *testing.T) {
				f := newFixture(t)
				o := f.seed(order.StateOpen)
				f.clock.Advance(time.Hour)
				require.Equal(t, *o.ExpiresAt, f.clock.Now())
				before := f.stored(o.ID)

				_, err := f.run(row.action, f.as(p), o)

				switch want {
				case forbidden:
					requireKind(t, err, order.KindForbidden)
				case conflict:
					requireKind(t, err, order.KindConflict)
					assert.Equal(t, row.msg, order.MessageOf(err))
				default:
					t.Fatalf("an errand past its deadline allows nothing")
				}
				f.requireUnchanged(before)
				f.requireNoWriteSince(before)
				assert.Empty(t, order.AllowedActions(o, f.as(p), f.clock.Now()))
			})
		}
	}
}

func TestCanView(t *testing.T) {
	// F3.6.2: the requester, the assigned courier and ADMIN always; anyone
	// else only while the errand is OPEN and before its deadline. D-045: a
	// cancelled errand is hidden from anyone who is not a party to it.
	f := newFixture(t)
	overdue := f.seed(order.StateOpen)
	overdue.ExpiresAt = ptr(f.clock.Now())
	cancelledWhileOpen := f.seed(order.StateCancelled)
	cancelledWhileOpen.CourierID = nil

	cases := []struct {
		name string
		o    order.Order
		want [4]bool // requester, courier, another student, admin
	}{
		{"OPEN before its deadline", f.seed(order.StateOpen), [4]bool{true, true, true, true}},
		{"OPEN at its deadline", overdue, [4]bool{true, false, false, true}},
		{"ACCEPTED", f.seed(order.StateAccepted), [4]bool{true, true, false, true}},
		{"PICKED_UP", f.seed(order.StatePickedUp), [4]bool{true, true, false, true}},
		{"DELIVERED", f.seed(order.StateDelivered), [4]bool{true, true, false, true}},
		{"COMPLETED", f.seed(order.StateCompleted), [4]bool{true, true, false, true}},
		{"CANCELLED after acceptance", f.seed(order.StateCancelled), [4]bool{true, true, false, true}},
		{"CANCELLED while OPEN", cancelledWhileOpen, [4]bool{true, false, false, true}},
		{"EXPIRED", f.seed(order.StateExpired), [4]bool{true, false, false, true}},
	}
	for _, tc := range cases {
		for i, p := range parties() {
			t.Run(fmt.Sprintf("%s as %s", tc.name, p), func(t *testing.T) {
				assert.Equal(t, tc.want[i], order.CanView(tc.o, f.as(p), f.clock.Now()))
			})
		}
	}
}

func TestCanSeePrivate(t *testing.T) {
	// The requester's id and the delivery note are for the errand's parties
	// and ADMIN only, even while anyone can see the errand itself.
	f := newFixture(t)
	for _, s := range allStates() {
		o := f.seed(s)
		courierAssigned := o.CourierID != nil
		want := [4]bool{true, courierAssigned, false, true}
		for i, p := range parties() {
			assert.Equal(t, want[i], order.CanSeePrivate(o, f.as(p)), "%s as %s", s, p)
		}
	}
}
