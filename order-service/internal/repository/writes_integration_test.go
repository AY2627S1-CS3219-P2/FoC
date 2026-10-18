//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Integration tests of the PostgreSQL order.Repository's writes: queued
//   refund and create (D-047), conditional transitions with history and
//   outbox rows (D-046, D-048, D-051), accept and K (D-046), amend and the
//   locked re-sync (D-057), the schema's CHECKs, and the domain service's
//   whole lifecycle on this adapter.
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

// queueGuard queues o's refund the way the domain does before it reserves
// credit (D-047), and returns it.
func (e *env) queueGuard(t *testing.T, o order.Order) order.OutboxEvent {
	t.Helper()
	g := outboxEvent(order.RoutingFailed, o, nil, o.CreatedAt, "corr-create")
	g.AvailableAt = o.CreatedAt.Add(order.DefaultRefundDelay)
	require.NoError(t, e.repo.QueueEvent(t.Context(), g))
	return g
}

// move is a system transition of o to `to` at now that expects only o's
// state, keeps o's courier, clears both deadlines and writes one history row.
// Tests add the expectations and after-values they need.
func move(o order.Order, a order.Action, to order.State, now time.Time) order.Transition {
	return order.Transition{
		OrderID:   o.ID,
		FromState: o.State,
		ToState:   to,
		CourierID: o.CourierID,
		Now:       now,
		History: order.HistoryEntry{
			OrderID:       o.ID,
			Action:        a,
			FromState:     o.State,
			ToState:       to,
			ActorKind:     order.ActorSystem,
			CourierID:     o.CourierID,
			OccurredAt:    now,
			CorrelationID: "corr-move",
		},
	}
}

// cancelOpen is the requester cancelling an OPEN errand, as the domain builds
// it (D-045, D-046, D-048).
func cancelOpen(o order.Order, now time.Time) order.Transition {
	tr := move(o, order.ActionCancel, order.StateCancelled, now)
	tr.ExpectRequesterID = ptr(o.RequesterID)
	tr.OpenBefore = ptr(now)
	tr.History.ActorID = ptr(o.RequesterID)
	tr.History.ActorKind = order.ActorRequester
	ev := outboxEvent(order.RoutingCancelled, o, nil, now, "corr-cancel")
	tr.Event = &ev
	return tr
}

// applied is o as tr leaves it, per the Transition doc in ports.go.
func applied(o order.Order, tr order.Transition) order.Order {
	o.State = tr.ToState
	o.CourierID = tr.CourierID
	o.ExpiresAt = tr.ExpiresAt
	o.ConfirmBy = tr.ConfirmBy
	o.StateChangedAt = tr.Now
	o.UpdatedAt = tr.Now
	o.Version++
	return o
}

// acceptCmd is courier accepting o at the version o is at (D-046).
func acceptCmd(o order.Order, courier uuid.UUID, now time.Time, maxActive int) order.AcceptCommand {
	return order.AcceptCommand{
		OrderID:         o.ID,
		CourierID:       courier,
		ExpectedVersion: o.Version,
		Now:             now,
		MaxActive:       maxActive,
		History: order.HistoryEntry{
			OrderID:       o.ID,
			Action:        order.ActionAccept,
			FromState:     order.StateOpen,
			ToState:       order.StateAccepted,
			ActorID:       ptr(courier),
			ActorKind:     order.ActorCourier,
			CourierID:     ptr(courier),
			OccurredAt:    now,
			CorrelationID: "corr-accept",
		},
	}
}

// requireCheckViolation checks err is PostgreSQL's check_violation on constraint.
func requireCheckViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23514", pgErr.Code, "check_violation")
	require.Equal(t, constraint, pgErr.ConstraintName)
}

func (e *env) testQueueEvent(t *testing.T) {
	e.reset(t)
	o := openErrand(uuid.New(), at(0))
	ev := outboxEvent(order.RoutingFailed, o, nil, at(0), "corr-queue")
	ev.AvailableAt = at(order.DefaultRefundDelay)

	require.NoError(t, e.repo.QueueEvent(t.Context(), ev))

	row := e.outbox(t, ev.EventID)
	require.Equal(t, order.RoutingFailed, row.routingKey)
	require.JSONEq(t, wantPayload(ev.Payload), row.payload)
	require.Equal(t, "corr-queue", row.correlationID)
	require.True(t, row.availableAt.Equal(at(order.DefaultRefundDelay)), "available_at = %v", row.availableAt)
	require.False(t, row.published)
	require.Zero(t, row.attempts)

	t.Run("an event id already queued is an error", func(t *testing.T) {
		err := e.repo.QueueEvent(t.Context(), ev)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "23505", pgErr.Code, "unique_violation")
	})
}

func (e *env) testCreate(t *testing.T) {
	t.Run("saves the errand and its CREATE row and deletes the queued refund", func(t *testing.T) {
		e.reset(t)
		o := openErrand(uuid.New(), at(0))
		o.RequestKey = "key-create"
		guard := e.queueGuard(t, o)
		h := createdEntry(o)

		require.NoError(t, e.repo.Create(t.Context(), o, h, guard.EventID))

		require.Equal(t, o, e.get(t, o.ID))
		require.Zero(t, e.count(t, `SELECT count(*) FROM outbox WHERE event_id = $1`, guard.EventID), "queued refund deleted")
		entries := e.history(t, o.ID)
		require.Len(t, entries, 1)
		h.ID = entries[0].ID
		require.Equal(t, h, entries[0])
		require.Equal(t, 1, e.count(t, `
			SELECT count(*) FROM order_history
			WHERE order_id = $1 AND from_state IS NULL AND changes IS NULL`, o.ID),
			"a CREATE row has a NULL from_state and NULL changes")
	})

	t.Run("stores a missing request key as NULL, so keyless errands never collide", func(t *testing.T) {
		e.reset(t)
		requester := uuid.New()
		for i := range 2 {
			o := openErrand(requester, at(time.Duration(i)*time.Minute))
			require.NoError(t, e.repo.Create(t.Context(), o, createdEntry(o), e.queueGuard(t, o).EventID))
		}
		require.Equal(t, 2, e.count(t, `SELECT count(*) FROM orders WHERE requester_id = $1 AND request_key IS NULL`, requester))
	})

	t.Run("returns ErrGuardGone and writes nothing when the refund", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			guard func(t *testing.T, o order.Order) uuid.UUID
		}{
			{"was never queued", func(*testing.T, order.Order) uuid.UUID { return uuid.New() }},
			{"was already published", func(t *testing.T, o order.Order) uuid.UUID {
				g := e.queueGuard(t, o)
				_, err := e.pool.Exec(t.Context(), `UPDATE outbox SET published_at = now() WHERE event_id = $1`, g.EventID)
				require.NoError(t, err)
				return g.EventID
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e.reset(t)
				o := openErrand(uuid.New(), at(0))
				guardID := tc.guard(t, o)
				outboxBefore := e.count(t, `SELECT count(*) FROM outbox`)

				err := e.repo.Create(t.Context(), o, createdEntry(o), guardID)

				require.ErrorIs(t, err, order.ErrGuardGone)
				require.Zero(t, e.count(t, `SELECT count(*) FROM orders`))
				require.Zero(t, e.count(t, `SELECT count(*) FROM order_history`))
				require.Equal(t, outboxBefore, e.count(t, `SELECT count(*) FROM outbox`), "a published refund is left alone")
			})
		}
	})

	t.Run("returns ErrDuplicateRequestKey, writes nothing and keeps the refund queued", func(t *testing.T) {
		e.reset(t)
		requester := uuid.New()
		first := openErrand(requester, at(0))
		first.RequestKey = "key-twice"
		require.NoError(t, e.repo.Create(t.Context(), first, createdEntry(first), e.queueGuard(t, first).EventID))

		twin := openErrand(requester, at(time.Second))
		twin.RequestKey = "key-twice"
		guard := e.queueGuard(t, twin)
		err := e.repo.Create(t.Context(), twin, createdEntry(twin), guard.EventID)

		require.ErrorIs(t, err, order.ErrDuplicateRequestKey)
		_, err = e.repo.Get(t.Context(), twin.ID)
		require.ErrorIs(t, err, order.ErrNotFound)
		require.Empty(t, e.history(t, twin.ID))
		require.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox WHERE event_id = $1 AND published_at IS NULL`, guard.EventID),
			"the twin's refund stays queued, so its reservation is released")

		another := openErrand(uuid.New(), at(2*time.Second))
		another.RequestKey = "key-twice"
		require.NoError(t, e.repo.Create(t.Context(), another, createdEntry(another), e.queueGuard(t, another).EventID),
			"a key is unique per requester only")
	})
}

func (e *env) testTransition(t *testing.T) {
	requester, courier := uuid.New(), uuid.New()
	now := at(time.Hour)

	t.Run("writes the row, its history row and its event", func(t *testing.T) {
		delivered := heldBy(openErrand(requester, at(0)), courier, order.StateDelivered, at(time.Minute))
		delivered.ConfirmBy = ptr(at(30 * time.Minute))
		for _, tc := range []struct {
			name   string
			stored order.Order
			build  func(o order.Order) order.Transition
		}{
			{"cancel clears the deadline and queues errand.cancelled", openErrand(requester, at(0)), func(o order.Order) order.Transition {
				return cancelOpen(o, now)
			}},
			{"withdraw clears the courier and restarts expiry", heldBy(openErrand(requester, at(0)), courier, order.StateAccepted, at(time.Minute)), func(o order.Order) order.Transition {
				tr := move(o, order.ActionWithdraw, order.StateOpen, now)
				tr.ExpectCourierID = ptr(courier)
				tr.CourierID = nil
				tr.ExpiresAt = ptr(now.Add(2 * time.Hour))
				tr.History.ActorID, tr.History.ActorKind = ptr(courier), order.ActorCourier
				return tr
			}},
			{"deliver keeps the courier and opens the confirmation window", heldBy(openErrand(requester, at(0)), courier, order.StatePickedUp, at(time.Minute)), func(o order.Order) order.Transition {
				tr := move(o, order.ActionDeliver, order.StateDelivered, now)
				tr.ExpectCourierID = ptr(courier)
				tr.ConfirmBy = ptr(now.Add(24 * time.Hour))
				tr.History.ActorID, tr.History.ActorKind = ptr(courier), order.ActorCourier
				return tr
			}},
			{"auto-confirm closes the window and queues errand.completed with the courier", delivered, func(o order.Order) order.Transition {
				tr := move(o, order.ActionAutoConfirm, order.StateCompleted, now)
				tr.ConfirmDueBy = ptr(now)
				ev := outboxEvent(order.RoutingCompleted, o, o.CourierID, now, "corr-sweep")
				tr.Event = &ev
				return tr
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e.reset(t)
				e.insert(t, tc.stored)
				tr := tc.build(tc.stored)

				got, err := e.repo.Transition(t.Context(), tr)

				require.NoError(t, err)
				want := applied(tc.stored, tr)
				require.Equal(t, want, got)
				require.Equal(t, want, e.get(t, tc.stored.ID))
				entries := e.history(t, tc.stored.ID)
				require.Len(t, entries, 1)
				wantEntry := tr.History
				wantEntry.ID = entries[0].ID
				require.Equal(t, wantEntry, entries[0])
				if tr.Event == nil {
					require.Zero(t, e.count(t, `SELECT count(*) FROM outbox`), "no event")
					return
				}
				row := e.outbox(t, tr.Event.EventID)
				require.Equal(t, tr.Event.RoutingKey, row.routingKey)
				require.JSONEq(t, wantPayload(tr.Event.Payload), row.payload)
				require.Equal(t, tr.Event.CorrelationID, row.correlationID)
				require.True(t, row.availableAt.Equal(tr.Event.AvailableAt))
				require.False(t, row.published)
			})
		}
	})

	t.Run("matches only while every expectation holds", func(t *testing.T) {
		open := openErrand(requester, at(0))
		deadline := *open.ExpiresAt
		accepted := heldBy(openErrand(requester, at(0)), courier, order.StateAccepted, at(time.Minute))
		delivered := heldBy(openErrand(requester, at(0)), courier, order.StateDelivered, at(time.Minute))
		window := *delivered.ConfirmBy
		cancel := func(o order.Order) order.Transition { return move(o, order.ActionCancel, order.StateCancelled, now) }
		pickUp := func(o order.Order) order.Transition { return move(o, order.ActionPickUp, order.StatePickedUp, now) }
		expire := func(o order.Order) order.Transition { return move(o, order.ActionExpire, order.StateExpired, now) }
		autoConfirm := func(o order.Order) order.Transition {
			return move(o, order.ActionAutoConfirm, order.StateCompleted, now)
		}
		adminCancel := func(o order.Order) order.Transition {
			return move(o, order.ActionAdminCancel, order.StateCancelled, now)
		}
		with := func(build func(order.Order) order.Transition, set func(*order.Transition)) func(order.Order) order.Transition {
			return func(o order.Order) order.Transition {
				tr := build(o)
				set(&tr)
				return tr
			}
		}

		for _, tc := range []struct {
			name   string
			stored order.Order
			build  func(o order.Order) order.Transition
			match  bool
		}{
			{"the state matches", open, cancel, true},
			{"the state moved on", accepted, with(cancel, func(tr *order.Transition) { tr.FromState = order.StateOpen }), false},
			{"the requester matches", open, with(cancel, func(tr *order.Transition) { tr.ExpectRequesterID = ptr(requester) }), true},
			{"another requester", open, with(cancel, func(tr *order.Transition) { tr.ExpectRequesterID = ptr(uuid.New()) }), false},
			{"the courier matches", accepted, with(pickUp, func(tr *order.Transition) { tr.ExpectCourierID = ptr(courier) }), true},
			{"another courier", accepted, with(pickUp, func(tr *order.Transition) { tr.ExpectCourierID = ptr(uuid.New()) }), false},
			{"a courier expected where none is assigned", open, with(cancel, func(tr *order.Transition) { tr.ExpectCourierID = ptr(courier) }), false},
			{"just before the deadline", open, with(cancel, func(tr *order.Transition) { tr.OpenBefore = ptr(deadline.Add(-time.Microsecond)) }), true},
			{"at the deadline", open, with(cancel, func(tr *order.Transition) { tr.OpenBefore = ptr(deadline) }), false},
			{"expiry due at the deadline", open, with(expire, func(tr *order.Transition) { tr.ExpiryDueBy = ptr(deadline) }), true},
			{"expiry not yet due", open, with(expire, func(tr *order.Transition) { tr.ExpiryDueBy = ptr(deadline.Add(-time.Microsecond)) }), false},
			{"expiry checked on an errand with no deadline", accepted, with(adminCancel, func(tr *order.Transition) { tr.ExpiryDueBy = ptr(now) }), false},
			{"auto-confirm due as the window ends", delivered, with(autoConfirm, func(tr *order.Transition) { tr.ConfirmDueBy = ptr(window) }), true},
			{"auto-confirm not yet due", delivered, with(autoConfirm, func(tr *order.Transition) { tr.ConfirmDueBy = ptr(window.Add(-time.Microsecond)) }), false},
			{"auto-confirm checked on an errand with no window", accepted, with(adminCancel, func(tr *order.Transition) { tr.ConfirmDueBy = ptr(now) }), false},
			// AI-generated (edited by James Teo): the version re-check added at integration (D-046).
			{"the version matches", open, with(cancel, func(tr *order.Transition) { tr.ExpectVersion = ptr(open.Version) }), true},
			{"the version moved on", open, with(cancel, func(tr *order.Transition) { tr.ExpectVersion = ptr(open.Version - 1) }), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e.reset(t)
				e.insert(t, tc.stored)
				tr := tc.build(tc.stored)

				got, err := e.repo.Transition(t.Context(), tr)

				if tc.match {
					require.NoError(t, err)
					require.Equal(t, applied(tc.stored, tr), got)
					require.Len(t, e.history(t, tc.stored.ID), 1)
					return
				}
				require.ErrorIs(t, err, order.ErrNoMatch)
				e.requireUntouched(t, tc.stored)
			})
		}

		t.Run("an unknown errand", func(t *testing.T) {
			e.reset(t)
			_, err := e.repo.Transition(t.Context(), cancel(openErrand(requester, at(0))))
			require.ErrorIs(t, err, order.ErrNoMatch)
			require.Zero(t, e.count(t, `SELECT count(*) FROM order_history`))
		})
	})
}

func (e *env) testAccept(t *testing.T) {
	requester, courier := uuid.New(), uuid.New()
	now := at(time.Hour)

	t.Run("assigns the courier and writes the ACCEPT row", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		cmd := acceptCmd(o, courier, now, 2)

		got, err := e.repo.Accept(t.Context(), cmd)

		require.NoError(t, err)
		want := o
		want.State = order.StateAccepted
		want.CourierID = ptr(courier)
		want.ExpiresAt = nil
		want.StateChangedAt, want.UpdatedAt = now, now
		want.Version = 2
		require.Equal(t, want, got)
		require.Equal(t, want, e.get(t, o.ID))
		entries := e.history(t, o.ID)
		require.Len(t, entries, 1)
		wantEntry := cmd.History
		wantEntry.ID = entries[0].ID
		require.Equal(t, wantEntry, entries[0])
	})

	t.Run("returns ErrNoMatch and writes nothing when the errand", func(t *testing.T) {
		open := openErrand(requester, at(0))
		deadline := *open.ExpiresAt
		for _, tc := range []struct {
			name   string
			stored order.Order
			cmd    order.AcceptCommand
		}{
			{"is no longer OPEN", heldBy(open, uuid.New(), order.StateAccepted, at(time.Minute)), acceptCmd(open, courier, now, 2)},
			{"reaches its deadline now", open, acceptCmd(open, courier, deadline, 2)},
			{"is past its deadline", open, acceptCmd(open, courier, deadline.Add(time.Minute), 2)},
			{"belongs to the courier", open, acceptCmd(open, requester, now, 2)},
			{"moved past the version the courier saw", open, func() order.AcceptCommand {
				c := acceptCmd(open, courier, now, 2)
				c.ExpectedVersion = open.Version - 1
				return c
			}()},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e.reset(t)
				e.insert(t, tc.stored)
				_, err := e.repo.Accept(t.Context(), tc.cmd)
				require.ErrorIs(t, err, order.ErrNoMatch)
				e.requireUntouched(t, tc.stored)
			})
		}
		t.Run("does not exist", func(t *testing.T) {
			e.reset(t)
			_, err := e.repo.Accept(t.Context(), acceptCmd(open, courier, now, 2))
			require.ErrorIs(t, err, order.ErrNoMatch)
			require.Zero(t, e.count(t, `SELECT count(*) FROM order_history`))
		})
	})

	t.Run("counts only ACCEPTED and PICKED_UP towards K", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			held      []order.State
			targetGot bool // the target errand was already taken by someone else
			want      error
		}{
			{"below K", []order.State{order.StateAccepted}, false, nil},
			{"at K", []order.State{order.StateAccepted, order.StatePickedUp}, false, order.ErrCourierAtCapacity},
			{"DELIVERED and ended errands do not count", []order.State{order.StateAccepted, order.StateDelivered, order.StateCompleted, order.StateCancelled}, false, nil},
			{"K is checked before the errand, as in the fake", []order.State{order.StateAccepted, order.StatePickedUp}, true, order.ErrCourierAtCapacity},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e.reset(t)
				for i, s := range tc.held {
					// Accepted first, so an ended errand still names the courier.
					accepted := heldBy(openErrand(uuid.New(), at(0)), courier, order.StateAccepted, at(time.Duration(i)*time.Minute))
					e.insert(t, inState(accepted, s, courier, at(time.Duration(i)*time.Minute+time.Second)))
				}
				target := openErrand(requester, at(0))
				if tc.targetGot {
					target = heldBy(target, uuid.New(), order.StateAccepted, at(time.Minute))
				}
				e.insert(t, target)

				_, err := e.repo.Accept(t.Context(), acceptCmd(target, courier, now, 2))

				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					e.requireUntouched(t, target)
					return
				}
				require.NoError(t, err)
				require.Equal(t, order.StateAccepted, e.get(t, target.ID).State)
			})
		}
	})
}

func (e *env) testAmend(t *testing.T) {
	requester := uuid.New()

	t.Run("writes the amendable columns and the AMEND row", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		o.RequestKey = "key-amend"
		e.insert(t, o)
		next := o
		next.Supplier = order.SupplierSnapshot{ID: uuid.New(), Name: "Fong Seng", Location: "Kent Ridge, level 1"}
		next.ItemDescription = "Nasi lemak"
		next.DeliveryLocation = "Central Library entrance"
		next.DeliveryNote = "Red cap"
		next.Reward = 8
		next.Version = 2
		next.UpdatedAt = at(5 * time.Minute)
		h := amendEntry(o, at(5*time.Minute), []order.FieldChange{
			{Field: order.FieldReward, From: "5", To: "8"},
			{Field: order.FieldItemDescription, From: latte, To: "Nasi lemak"},
		})

		var seen order.Order
		got, err := e.repo.Amend(t.Context(), o.ID, func(cur order.Order) (*order.AmendResult, error) {
			seen = cur
			return &order.AmendResult{Order: next, History: h}, nil
		})

		require.NoError(t, err)
		require.Equal(t, o, seen, "fn sees the row as committed")
		require.Equal(t, next, got)
		require.Equal(t, next, e.get(t, o.ID))
		entries := e.history(t, o.ID)
		require.Len(t, entries, 1)
		h.ID = entries[0].ID
		require.Equal(t, h, entries[0])
		require.Equal(t, 1, e.count(t, `SELECT count(*) FROM order_history WHERE order_id = $1 AND changes = $2::jsonb`, o.ID,
			`[{"field": "reward", "from": "5", "to": "8"}, {"field": "itemDescription", "from": "`+latte+`", "to": "Nasi lemak"}]`),
			"changes are stored as a JSON array of field, from and to")
	})

	t.Run("writes fn's reward_set in the same transaction", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		next := o
		next.Reward = 8
		next.Version = 2
		next.UpdatedAt = at(5 * time.Minute)
		ev := rewardSetEvent(next, at(5*time.Minute), "corr-amend")

		_, err := e.repo.Amend(t.Context(), o.ID, func(order.Order) (*order.AmendResult, error) {
			return &order.AmendResult{Order: next, History: amendEntry(o, at(5*time.Minute), nil), Event: &ev}, nil
		})

		require.NoError(t, err)
		row := e.outbox(t, ev.EventID)
		require.Equal(t, order.RoutingRewardSet, row.routingKey)
		require.Contains(t, row.payload, `"version": 2`)
		require.Contains(t, row.payload, `"amount": 8`)
	})

	t.Run("writes only the amendable columns", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		next := o
		next.Reward = 9
		next.Version = 2
		next.UpdatedAt = at(5 * time.Minute)
		// None of these is an amend's to change (F3.7.6, D-045, D-049), so none is written.
		next.State = order.StateCancelled
		next.CourierID = ptr(uuid.New())
		next.ExpiresAt = ptr(at(10 * time.Hour))
		next.RequestKey = "not-written"
		next.StateChangedAt = at(5 * time.Minute)

		got, err := e.repo.Amend(t.Context(), o.ID, func(order.Order) (*order.AmendResult, error) {
			return &order.AmendResult{Order: next, History: amendEntry(o, at(5*time.Minute), nil)}, nil
		})

		require.NoError(t, err)
		want := o
		want.Reward = 9
		want.Version = 2
		want.UpdatedAt = at(5 * time.Minute)
		require.Equal(t, want, got, "the errand returned is the row as written")
		require.Equal(t, want, e.get(t, o.ID))
	})

	t.Run("a nil result writes nothing and returns the row as it is", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		got, err := e.repo.Amend(t.Context(), o.ID, func(order.Order) (*order.AmendResult, error) { return nil, nil })
		require.NoError(t, err)
		require.Equal(t, o, got)
		e.requireUntouched(t, o)
	})

	t.Run("fn's error rolls back, comes back unchanged and releases the row", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		refused := &order.Error{Kind: order.KindInsufficientCredits, Msg: "not enough available credits"}

		_, err := e.repo.Amend(t.Context(), o.ID, func(order.Order) (*order.AmendResult, error) { return nil, refused })

		require.Same(t, refused, err)
		e.requireUntouched(t, o)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		_, err = e.repo.Transition(ctx, cancelOpen(o, at(time.Minute)))
		require.NoError(t, err, "the row lock went with the rollback")
	})

	t.Run("an unknown errand is ErrNotFound and fn is not called", func(t *testing.T) {
		e.reset(t)
		called := false
		_, err := e.repo.Amend(t.Context(), uuid.New(), func(order.Order) (*order.AmendResult, error) {
			called = true
			return nil, nil
		})
		require.ErrorIs(t, err, order.ErrNotFound)
		require.False(t, called)
	})
}

func (e *env) testQueueLocked(t *testing.T) {
	t.Run("passes the row as committed and inserts fn's event", func(t *testing.T) {
		e.reset(t)
		o := openErrand(uuid.New(), at(0))
		e.insert(t, o)
		var seen order.Order
		var ev order.OutboxEvent
		require.NoError(t, e.repo.QueueLocked(t.Context(), o.ID, func(cur order.Order) (order.OutboxEvent, error) {
			seen = cur
			ev = rewardSetEvent(cur, at(5*time.Minute), "corr-amend")
			return ev, nil
		}))
		require.Equal(t, o, seen)
		require.Equal(t, o, e.get(t, o.ID), "the row is unchanged")
		require.Empty(t, e.history(t, o.ID), "no history row")
		row := e.outbox(t, ev.EventID)
		require.Equal(t, order.RoutingRewardSet, row.routingKey)
		require.Equal(t, "corr-amend", row.correlationID)
		require.False(t, row.published)
		require.Contains(t, row.payload, `"version": 1`, "the version credit compares")
		require.Contains(t, row.payload, `"amount": 5`)
	})

	t.Run("fn's error comes back unchanged and nothing is written", func(t *testing.T) {
		e.reset(t)
		o := openErrand(uuid.New(), at(0))
		e.insert(t, o)
		failed := &order.Error{Kind: order.KindUnavailable, Msg: "could not build the event"}
		err := e.repo.QueueLocked(t.Context(), o.ID, func(order.Order) (order.OutboxEvent, error) { return order.OutboxEvent{}, failed })
		require.Same(t, failed, err)
		e.requireUntouched(t, o)
	})

	t.Run("an unknown errand is ErrNotFound and fn is not called", func(t *testing.T) {
		e.reset(t)
		called := false
		err := e.repo.QueueLocked(t.Context(), uuid.New(), func(order.Order) (order.OutboxEvent, error) {
			called = true
			return order.OutboxEvent{}, nil
		})
		require.ErrorIs(t, err, order.ErrNotFound)
		require.False(t, called)
		require.Zero(t, e.count(t, `SELECT count(*) FROM outbox`), "no event")
	})
}

func (e *env) testCheckConstraints(t *testing.T) {
	t.Run("an errand the schema forbids rolls the whole create back", func(t *testing.T) {
		e.reset(t)
		o := openErrand(uuid.New(), at(0))
		o.Reward = 0
		guard := e.queueGuard(t, o)

		err := e.repo.Create(t.Context(), o, createdEntry(o), guard.EventID)

		requireCheckViolation(t, err, "orders_reward_positive")
		require.Zero(t, e.count(t, `SELECT count(*) FROM orders`))
		require.Zero(t, e.count(t, `SELECT count(*) FROM order_history`))
		require.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox WHERE event_id = $1`, guard.EventID), "the refund stays queued")
	})

	t.Run("a transition the schema forbids rolls back and is not ErrNoMatch", func(t *testing.T) {
		e.reset(t)
		o := heldBy(openErrand(uuid.New(), at(0)), uuid.New(), order.StateAccepted, at(time.Minute))
		e.insert(t, o)
		// Back to OPEN with no new deadline breaks orders_open_has_deadline (D-049).
		tr := move(o, order.ActionWithdraw, order.StateOpen, at(time.Hour))
		tr.CourierID = nil

		_, err := e.repo.Transition(t.Context(), tr)

		requireCheckViolation(t, err, "orders_open_has_deadline")
		require.NotErrorIs(t, err, order.ErrNoMatch)
		e.requireUntouched(t, o)
	})
}

// testServiceLifecycle runs the domain service (internal/order) on this
// adapter, with ordertest's fakes for credit, suppliers and the clock. The
// in-memory fake repository has no CHECK constraints, so this is where a
// transition the domain builds that the schema rejects would show up.
func (e *env) testServiceLifecycle(t *testing.T) {
	e.reset(t)
	ctx := t.Context()
	clock := ordertest.NewClock(at(0))
	supplier := order.Supplier{
		ID: uuid.New(), Name: "Spinelli Coffee", Building: "COM3", Floor: "1",
		LocationDescription: "next to the lift", Available: true,
	}
	credit := ordertest.NewCredit()
	svc := order.NewService(e.repo, credit, ordertest.NewSuppliers(supplier), clock,
		order.Config{ExpiryPeriod: 2 * time.Hour, MaxActive: 2, ConfirmWindow: 24 * time.Hour},
		slog.New(slog.DiscardHandler))
	requester := order.Caller{UserID: uuid.New(), Role: order.RoleStudent}
	courier := order.Caller{UserID: uuid.New(), Role: order.RoleStudent}
	admin := order.Caller{UserID: uuid.New(), Role: order.RoleAdmin}

	post := func(key string) (order.Order, bool) {
		t.Helper()
		o, replayed, err := svc.Create(ctx, requester, order.CreateInput{
			SupplierID: supplier.ID, ItemDescription: latte, DeliveryLocation: "COM3 Lobby",
			Reward: 5, RequestKey: key, CorrelationID: "corr-" + key,
		})
		require.NoError(t, err, "post %s", key)
		return o, replayed
	}
	step := func(name string, want order.State, run func() (order.Order, error)) order.Order {
		t.Helper()
		clock.Advance(time.Minute)
		o, err := run()
		require.NoError(t, err, name)
		require.Equal(t, want, o.State, name)
		return o
	}
	routingKeys := func(errandID uuid.UUID) []string {
		t.Helper()
		rows, err := e.pool.Query(ctx, `SELECT routing_key FROM outbox WHERE payload->>'errandId' = $1 ORDER BY created_at`, errandID.String())
		require.NoError(t, err)
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		return keys
	}

	// Post, replay, accept, withdraw, accept again, pick up, deliver, confirm.
	first, replayed := post("first")
	require.False(t, replayed)
	again, replayed := post("first")
	require.True(t, replayed, "the same X-Request-Id returns the errand already posted")
	require.Equal(t, first.ID, again.ID)
	require.Zero(t, e.count(t, `SELECT count(*) FROM outbox`), "saving the errand deleted its queued refund")

	step("accept", order.StateAccepted, func() (order.Order, error) { return svc.Accept(ctx, courier, first.ID, 1, "corr-accept") })
	withdrawn := step("withdraw", order.StateOpen, func() (order.Order, error) { return svc.Withdraw(ctx, courier, first.ID, "corr-withdraw") })
	require.Equal(t, clock.Now().Add(2*time.Hour), *withdrawn.ExpiresAt, "expiry restarts on withdrawal")
	step("accept again", order.StateAccepted, func() (order.Order, error) {
		return svc.Accept(ctx, courier, first.ID, withdrawn.Version, "corr-accept")
	})
	step("pick up", order.StatePickedUp, func() (order.Order, error) { return svc.PickUp(ctx, courier, first.ID, "corr-pickup") })
	delivered := step("deliver", order.StateDelivered, func() (order.Order, error) { return svc.Deliver(ctx, courier, first.ID, "corr-deliver") })
	require.Equal(t, clock.Now().Add(24*time.Hour), *delivered.ConfirmBy)
	step("confirm", order.StateCompleted, func() (order.Order, error) { return svc.Confirm(ctx, requester, first.ID, "corr-confirm") })
	require.Equal(t, []string{order.RoutingCompleted}, routingKeys(first.ID))
	_, history, err := svc.Get(ctx, requester, first.ID)
	require.NoError(t, err)
	var actions []order.Action
	for _, h := range history {
		actions = append(actions, h.Action)
	}
	require.Equal(t, []order.Action{
		order.ActionCreate, order.ActionAccept, order.ActionWithdraw, order.ActionAccept,
		order.ActionPickUp, order.ActionDeliver, order.ActionConfirm,
	}, actions)

	// Expiry once T has passed (D-049).
	second, _ := post("second")
	clock.Advance(2 * time.Hour)
	due, err := e.repo.DueForExpiry(ctx, clock.Now(), order.SweepBatchLimit)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{second.ID}, due)
	expired, err := svc.Expire(ctx, second.ID, "corr-sweep")
	require.NoError(t, err)
	require.True(t, expired)
	require.Equal(t, order.StateExpired, e.get(t, second.ID).State)
	require.Equal(t, []string{order.RoutingExpired}, routingKeys(second.ID))

	// Auto-confirm once C has passed (F3.9.4).
	third, _ := post("third")
	step("accept the third", order.StateAccepted, func() (order.Order, error) { return svc.Accept(ctx, courier, third.ID, 1, "corr-accept") })
	step("pick up the third", order.StatePickedUp, func() (order.Order, error) { return svc.PickUp(ctx, courier, third.ID, "corr-pickup") })
	step("deliver the third", order.StateDelivered, func() (order.Order, error) { return svc.Deliver(ctx, courier, third.ID, "corr-deliver") })
	clock.Advance(24 * time.Hour)
	confirmed, err := svc.AutoConfirm(ctx, third.ID, "corr-sweep")
	require.NoError(t, err)
	require.True(t, confirmed)
	require.Equal(t, order.StateCompleted, e.get(t, third.ID).State)

	// An amended reward with its reward_set (D-057), then an admin
	// cancel while the courier holds it (D-045).
	fourth, _ := post("fourth")
	amended := step("amend", order.StateOpen, func() (order.Order, error) {
		return svc.Amend(ctx, requester, fourth.ID, order.AmendInput{Version: 1, Reward: ptr(8), CorrelationID: "corr-amend"})
	})
	require.Equal(t, 8, amended.Reward)
	held, ok := credit.Hold(fourth.ID)
	require.True(t, ok)
	require.Equal(t, 8, held)
	step("accept the amended errand", order.StateAccepted, func() (order.Order, error) {
		return svc.Accept(ctx, courier, fourth.ID, amended.Version, "corr-accept")
	})
	step("admin cancel", order.StateCancelled, func() (order.Order, error) { return svc.Cancel(ctx, admin, fourth.ID, "corr-admin") })
	require.ElementsMatch(t, []string{order.RoutingRewardSet, order.RoutingCancelled}, routingKeys(fourth.ID))

	// The courier's assignment history shows each way an assignment ended (F3.5.3).
	assignments, err := svc.ListAssignmentHistory(ctx, courier)
	require.NoError(t, err)
	type ending struct {
		errand  uuid.UUID
		outcome order.AssignmentOutcome
	}
	var got []ending
	for _, a := range assignments {
		got = append(got, ending{a.OrderID, a.Outcome})
	}
	require.Equal(t, []ending{
		{fourth.ID, order.OutcomeCancelled},
		{third.ID, order.OutcomeCompleted},
		{first.ID, order.OutcomeCompleted},
		{first.ID, order.OutcomeReleased},
	}, got)
}
