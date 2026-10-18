// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Settlement by events only (D-048) with D-051's payload and field
//   names, and the sweeper's expire and auto-confirm (D-049, F3.9.4, F3.10.1,
//   F3.14.1), including when they must do nothing.
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestSettlingTransitionsEmitOneEvent(t *testing.T) {
	// D-048: confirm, auto-confirm and admin complete pay the courier; cancel
	// and admin cancel refund the requester; expiry refunds the requester.
	cases := []struct {
		name    string
		state   order.State
		wait    time.Duration // how far the clock moves first, to make a sweep due
		run     func(f *fixture, o order.Order, corr string) error
		key     string
		paid    bool // the event names the courier
		action  order.Action
		actor   order.ActorKind
		byAdmin bool
	}{
		{
			name: "the requester confirms", state: order.StateDelivered,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Confirm(f.ctx, f.requester, o.ID, corr))
			},
			key: order.RoutingCompleted, paid: true, action: order.ActionConfirm, actor: order.ActorRequester,
		},
		{
			name: "auto-confirm once C has passed", state: order.StateDelivered, wait: 12 * time.Hour,
			run: func(f *fixture, o order.Order, corr string) error {
				return applied(f.svc.AutoConfirm(f.ctx, o.ID, corr))
			},
			key: order.RoutingCompleted, paid: true, action: order.ActionAutoConfirm, actor: order.ActorSystem,
		},
		{
			name: "an admin completes a PICKED_UP errand", state: order.StatePickedUp,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Complete(f.ctx, f.admin, o.ID, corr))
			},
			key: order.RoutingCompleted, paid: true, action: order.ActionAdminComplete, actor: order.ActorAdmin, byAdmin: true,
		},
		{
			name: "an admin completes a DELIVERED errand", state: order.StateDelivered,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Complete(f.ctx, f.admin, o.ID, corr))
			},
			key: order.RoutingCompleted, paid: true, action: order.ActionAdminComplete, actor: order.ActorAdmin, byAdmin: true,
		},
		{
			name: "the requester cancels", state: order.StateOpen,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Cancel(f.ctx, f.requester, o.ID, corr))
			},
			key: order.RoutingCancelled, action: order.ActionCancel, actor: order.ActorRequester,
		},
		{
			name: "an admin cancels an OPEN errand", state: order.StateOpen,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Cancel(f.ctx, f.admin, o.ID, corr))
			},
			key: order.RoutingCancelled, action: order.ActionAdminCancel, actor: order.ActorAdmin, byAdmin: true,
		},
		{
			name: "an admin cancels an ACCEPTED errand", state: order.StateAccepted,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Cancel(f.ctx, f.admin, o.ID, corr))
			},
			key: order.RoutingCancelled, action: order.ActionAdminCancel, actor: order.ActorAdmin, byAdmin: true,
		},
		{
			name: "an admin cancels a PICKED_UP errand", state: order.StatePickedUp,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Cancel(f.ctx, f.admin, o.ID, corr))
			},
			key: order.RoutingCancelled, action: order.ActionAdminCancel, actor: order.ActorAdmin, byAdmin: true,
		},
		{
			name: "an admin cancels a DELIVERED errand", state: order.StateDelivered,
			run: func(f *fixture, o order.Order, corr string) error {
				return just(f.svc.Cancel(f.ctx, f.admin, o.ID, corr))
			},
			key: order.RoutingCancelled, action: order.ActionAdminCancel, actor: order.ActorAdmin, byAdmin: true,
		},
		{
			name: "expiry once T has passed", state: order.StateOpen, wait: time.Hour,
			run: func(f *fixture, o order.Order, corr string) error { return applied(f.svc.Expire(f.ctx, o.ID, corr)) },
			key: order.RoutingExpired, action: order.ActionExpire, actor: order.ActorSystem,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.state)
			f.clock.Advance(tc.wait)
			now := f.clock.Now()

			require.NoError(t, tc.run(f, o, "corr-settle"))

			to := map[string]order.State{
				order.RoutingCompleted: order.StateCompleted,
				order.RoutingCancelled: order.StateCancelled,
				order.RoutingExpired:   order.StateExpired,
			}[tc.key]
			after := f.stored(o.ID)
			assert.Equal(t, to, after.order.State)
			assert.Equal(t, o.Version+1, after.order.Version)
			assert.Equal(t, now, after.order.StateChangedAt)
			require.Len(t, after.history, 1)
			assert.Equal(t, tc.action, after.history[0].Action)
			assert.Equal(t, tc.state, after.history[0].FromState)
			assert.Equal(t, to, after.history[0].ToState)
			assert.Equal(t, now, after.history[0].OccurredAt)
			assert.Equal(t, "corr-settle", after.history[0].CorrelationID, "the history row carries the correlation id")
			assert.Equal(t, tc.actor, after.history[0].ActorKind)
			assert.Equal(t, o.CourierID, after.history[0].CourierID)
			switch {
			case tc.actor == order.ActorSystem:
				assert.Nil(t, after.history[0].ActorID, "the sweeper is nobody")
			case tc.byAdmin:
				assert.Equal(t, ptr(f.admin.UserID), after.history[0].ActorID)
			default:
				assert.Equal(t, ptr(f.requester.UserID), after.history[0].ActorID)
			}
			assert.Nil(t, after.order.ExpiresAt, "deadlines only while OPEN")
			assert.Nil(t, after.order.ConfirmBy, "the window only while DELIVERED")
			assert.Equal(t, o.CourierID, after.order.CourierID, "a final errand keeps who carried it")

			require.Len(t, after.events, 1, "exactly one settlement")
			e := after.events[0]
			var courier *uuid.UUID
			if tc.paid {
				courier = o.CourierID
				require.NotNil(t, courier)
			}
			assertEvent(t, e, tc.key, o, courier, now, "corr-settle")
			assert.Equal(t, now, e.AvailableAt, "published straight away")
		})
	}
}

// just drops the order a user action returns.
func just(_ order.Order, err error) error { return err }

// applied turns a sweep that did nothing into an error, so the table can
// treat sweeps and user actions alike.
func applied(ok bool, err error) error {
	if err == nil && !ok {
		return errors.New("the sweep did nothing")
	}
	return err
}

func TestEventPayloadUsesCreditServiceFieldNames(t *testing.T) {
	// D-051: credit-service's documented fields plus eventType and
	// correlationId; courierId only on errand.completed.
	base := []string{"eventId", "eventType", "errandId", "requesterId", "amount", "occurredAt", "correlationId"}
	cases := []struct {
		name string
		emit func(f *fixture) order.OutboxEvent
		want []string
	}{
		{"errand.completed", func(f *fixture) order.OutboxEvent {
			o := f.seed(order.StateDelivered)
			require.NoError(f.t, just(f.svc.Confirm(f.ctx, f.requester, o.ID, "corr")))
			return f.repo.EventsFor(o.ID)[0]
		}, append(slices.Clone(base), "courierId")},
		{"errand.cancelled", func(f *fixture) order.OutboxEvent {
			o := f.seed(order.StateAccepted)
			require.NoError(f.t, just(f.svc.Cancel(f.ctx, f.admin, o.ID, "corr")))
			return f.repo.EventsFor(o.ID)[0]
		}, base},
		{"errand.expired", func(f *fixture) order.OutboxEvent {
			o := f.seed(order.StateOpen)
			f.clock.Advance(time.Hour)
			require.NoError(f.t, applied(f.svc.Expire(f.ctx, o.ID, "corr")))
			return f.repo.EventsFor(o.ID)[0]
		}, base},
		{"errand.failed", func(f *fixture) order.OutboxEvent {
			f.credit.ReserveHook = func(order.Reservation) (bool, error) { return true, ordertest.ErrInjected }
			_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())
			require.Error(f.t, err)
			return f.repo.Events()[0]
		}, base},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			e := tc.emit(f)
			require.Equal(t, tc.name, e.RoutingKey)

			raw, err := json.Marshal(e.Payload)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(raw, &fields))

			assert.ElementsMatch(t, tc.want, slices.Collect(maps.Keys(fields)))
			assert.Equal(t, tc.name, fields["eventType"])
			assert.Equal(t, e.EventID.String(), fields["eventId"])
			assert.InDelta(t, 5, fields["amount"], 0, "the reward, as a JSON number")
		})
	}
}

func TestOnlySettlementsEmitEvents(t *testing.T) {
	// D-048: the steps between posting and settling write no settlement event.
	// The reward amend writes only D-057's reward_set.
	f := newFixture(t)
	settlements := func() []order.OutboxEvent {
		return slices.DeleteFunc(f.repo.Events(), func(e order.OutboxEvent) bool { return e.RoutingKey == order.RoutingRewardSet })
	}
	o := f.post()
	version := o.Version
	steps := []struct {
		name string
		run  func() (order.Order, error)
	}{
		{"amend", func() (order.Order, error) {
			return f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: version, Reward: ptr(7)})
		}},
		{"accept", func() (order.Order, error) { return f.svc.Accept(f.ctx, f.courier, o.ID, version, "corr") }},
		{"withdraw", func() (order.Order, error) { return f.svc.Withdraw(f.ctx, f.courier, o.ID, "corr") }},
		{"accept again", func() (order.Order, error) { return f.svc.Accept(f.ctx, f.stranger, o.ID, version, "corr") }},
		{"pick up", func() (order.Order, error) { return f.svc.PickUp(f.ctx, f.stranger, o.ID, "corr") }},
		{"deliver", func() (order.Order, error) { return f.svc.Deliver(f.ctx, f.stranger, o.ID, "corr") }},
	}
	assert.Empty(t, f.repo.Events(), "after create")
	for _, step := range steps {
		got, err := step.run()
		require.NoError(t, err, step.name)
		version = got.Version
		assert.Empty(t, settlements(), "after %s", step.name)
	}
	assert.Len(t, f.repo.Events(), 1, "the amend's reward_set, and nothing else")

	done, err := f.svc.Confirm(f.ctx, f.requester, o.ID, "corr-confirm")
	require.NoError(t, err)
	events := settlements()
	require.Len(t, events, 1)
	assertEvent(t, events[0], order.RoutingCompleted, done, ptr(f.stranger.UserID), f.clock.Now(), "corr-confirm")
	assert.Equal(t, 7, events[0].Payload.Amount, "the amended reward is what the courier is paid")
}

func TestSweepsSkipWhatIsNotDue(t *testing.T) {
	// F3.14.1: each id is re-checked; one that is not due, already moved on,
	// or unknown is skipped without an error and without a write.
	type sweep func(f *fixture, id uuid.UUID) (bool, error)
	expire := func(f *fixture, id uuid.UUID) (bool, error) { return f.svc.Expire(f.ctx, id, "corr-sweep") }
	autoConfirm := func(f *fixture, id uuid.UUID) (bool, error) { return f.svc.AutoConfirm(f.ctx, id, "corr-sweep") }
	cases := []struct {
		name  string
		sweep sweep
		state order.State // "" for an id no errand has
		wait  time.Duration
	}{
		{"expire: an OPEN errand before its deadline", expire, order.StateOpen, 0},
		{"expire: one nanosecond before the deadline", expire, order.StateOpen, time.Hour - time.Nanosecond},
		{"expire: accepted in time", expire, order.StateAccepted, 2 * time.Hour},
		{"expire: already expired", expire, order.StateExpired, 2 * time.Hour},
		{"expire: cancelled", expire, order.StateCancelled, 2 * time.Hour},
		{"expire: an unknown errand", expire, "", 0},
		{"auto-confirm: a DELIVERED errand inside its window", autoConfirm, order.StateDelivered, 0},
		{"auto-confirm: one nanosecond before the window ends", autoConfirm, order.StateDelivered, 12*time.Hour - time.Nanosecond},
		{"auto-confirm: confirmed already", autoConfirm, order.StateCompleted, 48 * time.Hour},
		{"auto-confirm: cancelled by an admin", autoConfirm, order.StateCancelled, 48 * time.Hour},
		{"auto-confirm: never delivered", autoConfirm, order.StatePickedUp, 48 * time.Hour},
		{"auto-confirm: an unknown errand", autoConfirm, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			id := uuid.New()
			var before stored
			if tc.state != "" {
				id = f.seed(tc.state).ID
				before = f.stored(id)
			}
			f.clock.Advance(tc.wait)

			ok, err := tc.sweep(f, id)

			require.NoError(t, err)
			assert.False(t, ok)
			f.requireNoWriteSince(before)
			if tc.state != "" {
				f.requireUnchanged(before)
			}
		})
	}
}

func TestSweepsLoseRacesQuietly(t *testing.T) {
	t.Run("two sweepers expire the same errand", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateOpen)
		f.clock.Advance(time.Hour)
		f.repo.beforeNext("Transition", func() {
			require.NoError(t, applied(f.svc.Expire(f.ctx, o.ID, "corr-first")))
		})

		ok, err := f.svc.Expire(f.ctx, o.ID, "corr-second")

		require.NoError(t, err)
		assert.False(t, ok)
		events := f.repo.EventsFor(o.ID)
		require.Len(t, events, 1, "the requester is refunded once")
		assert.Equal(t, "corr-first", events[0].CorrelationID)
	})
	t.Run("the requester confirms just before auto-confirm writes", func(t *testing.T) {
		f := newFixture(t)
		o := f.seed(order.StateDelivered)
		f.clock.Advance(12 * time.Hour)
		f.repo.beforeNext("Transition", func() {
			require.NoError(t, just(f.svc.Confirm(f.ctx, f.requester, o.ID, "corr-confirm")))
		})

		ok, err := f.svc.AutoConfirm(f.ctx, o.ID, "corr-sweep")

		require.NoError(t, err)
		assert.False(t, ok)
		after := f.stored(o.ID)
		require.Len(t, after.events, 1, "the courier is paid once")
		require.Len(t, after.history, 1)
		assert.Equal(t, order.ActionConfirm, after.history[0].Action)
	})
}

func TestSweepsReportStorageFailures(t *testing.T) {
	cases := []struct {
		name   string
		method string
		state  order.State
		wait   time.Duration
		sweep  func(f *fixture, id uuid.UUID) (bool, error)
	}{
		{"expire: loading fails", "Get", order.StateOpen, time.Hour, func(f *fixture, id uuid.UUID) (bool, error) {
			return f.svc.Expire(f.ctx, id, "corr")
		}},
		{"expire: the write fails", "Transition", order.StateOpen, time.Hour, func(f *fixture, id uuid.UUID) (bool, error) {
			return f.svc.Expire(f.ctx, id, "corr")
		}},
		{"auto-confirm: loading fails", "Get", order.StateDelivered, 12 * time.Hour, func(f *fixture, id uuid.UUID) (bool, error) {
			return f.svc.AutoConfirm(f.ctx, id, "corr")
		}},
		{"auto-confirm: the write fails", "Transition", order.StateDelivered, 12 * time.Hour, func(f *fixture, id uuid.UUID) (bool, error) {
			return f.svc.AutoConfirm(f.ctx, id, "corr")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.state)
			f.clock.Advance(tc.wait)
			before := f.stored(o.ID)
			f.repo.FailNext(tc.method, ordertest.ErrInjected)

			ok, err := tc.sweep(f, o.ID)

			assert.False(t, ok)
			assert.ErrorIs(t, err, ordertest.ErrInjected)
			f.requireUnchanged(before)
		})
	}
}
