// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Amending an OPEN errand (F3.7): recorded field changes, validation, the
//   supplier check on every amend (D-052), the credit adjust under the row
//   lock and the reward_set that brings credit's hold in line (D-057),
//   and the deadline that never moves (D-049).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestAmendRecordsEveryChangedField(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	f.clock.Advance(20 * time.Minute)
	now := f.clock.Now()
	var underLock []bool
	f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) {
		underLock = append(underLock, f.repo.locked.Load())
		return true, nil
	}

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
		Version:          o.Version,
		SupplierID:       ptr(f.otherSupplier.ID),
		ItemDescription:  ptr("  One iced kopi  "),
		DeliveryLocation: ptr("Central Library entrance"),
		DeliveryNote:     ptr("At the turnstiles"),
		Reward:           ptr(8),
		CorrelationID:    "corr-amend",
	})

	require.NoError(t, err)
	want := o
	want.Supplier = order.SupplierSnapshot{
		ID: f.otherSupplier.ID, Name: "Campus Coffee Cart", Location: "Central Library, level 1 — Beside the entrance",
	}
	want.ItemDescription = "One iced kopi"
	want.DeliveryLocation = "Central Library entrance"
	want.DeliveryNote = "At the turnstiles"
	want.Reward = 8
	want.Version = o.Version + 1
	want.UpdatedAt = now
	assert.Equal(t, want, got, "only the amended fields, the version and updatedAt change")
	assert.Equal(t, o.ExpiresAt, got.ExpiresAt, "F3.7.6: amend never moves the deadline")
	assert.Equal(t, o.StateChangedAt, got.StateChangedAt)
	assert.Equal(t, got, f.stored(o.ID).order)

	h := f.history(o.ID)
	require.Len(t, h, 2)
	a := h[1]
	assert.Equal(t, order.ActionAmend, a.Action)
	assert.Equal(t, order.StateOpen, a.FromState)
	assert.Equal(t, order.StateOpen, a.ToState)
	assert.Equal(t, ptr(f.requester.UserID), a.ActorID)
	assert.Equal(t, order.ActorRequester, a.ActorKind)
	assert.Equal(t, now, a.OccurredAt, "F3.7.7: when it was amended")
	assert.Equal(t, "corr-amend", a.CorrelationID)
	assert.Equal(t, []order.FieldChange{
		{Field: order.FieldSupplier, From: "Kaya Toast Stall", To: "Campus Coffee Cart"},
		{Field: order.FieldItemDescription, From: "Two kaya toast sets", To: "One iced kopi"},
		{Field: order.FieldDeliveryLocation, From: "COM3 Lobby", To: "Central Library entrance"},
		{Field: order.FieldDeliveryNote, From: "Blue shirt, by the lift", To: "At the turnstiles"},
		{Field: order.FieldReward, From: "5", To: "8"},
	}, a.Changes, "F3.7.7: what changed, from and to")

	// F3.7.5, D-057: credit's hold follows the reward, set while the row is
	// locked, in the requester's name (D-056).
	assert.Equal(t, []ordertest.AdjustCall{{
		ErrandID: o.ID, RequesterID: f.requester.UserID, NewReward: 8, CorrelationID: "corr-amend",
	}}, f.credit.Adjusts())
	assert.Equal(t, []bool{true}, underLock)
	held, _ := f.credit.Hold(o.ID)
	assert.Equal(t, 8, held)
	events := f.repo.EventsFor(o.ID)
	require.Len(t, events, 1, "an amend settles nothing; it only writes the reward_set")
	assert.Equal(t, order.RoutingRewardSet, events[0].RoutingKey)
}

func TestAmendWithoutARewardChangeLeavesCreditAlone(t *testing.T) {
	f := newFixture(t)
	o := f.post()

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
		Version: o.Version, DeliveryLocation: ptr("UTown Residence lobby"),
	})

	require.NoError(t, err)
	assert.Equal(t, "UTown Residence lobby", got.DeliveryLocation)
	assert.Empty(t, f.credit.Adjusts())
}

func TestAmendWithNothingNewWritesNothing(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
		Version:         o.Version,
		SupplierID:      ptr(o.Supplier.ID),
		ItemDescription: ptr("  " + o.ItemDescription + " "),
		Reward:          ptr(o.Reward),
	})

	require.NoError(t, err)
	assert.Equal(t, o, got)
	f.requireUnchanged(before)
	assert.Empty(t, f.credit.Adjusts())
}

func TestAmendClearsTheNoteWithAnEmptyString(t *testing.T) {
	f := newFixture(t)
	o := f.post()

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, DeliveryNote: ptr("")})

	require.NoError(t, err)
	assert.Empty(t, got.DeliveryNote)
	h := f.history(o.ID)
	require.Len(t, h, 2)
	assert.Equal(t, []order.FieldChange{{Field: order.FieldDeliveryNote, From: "Blue shirt, by the lift", To: ""}}, h[1].Changes)
}

func TestAmendValidatesLikeCreate(t *testing.T) {
	// F3.7.2: the same rules as posting.
	cases := []struct {
		name string
		in   order.AmendInput
	}{
		{"no field to change", order.AmendInput{CorrelationID: "corr"}},
		{"no supplier", order.AmendInput{SupplierID: ptr(uuid.Nil)}},
		{"empty item", order.AmendInput{ItemDescription: ptr("")}},
		{"whitespace-only item", order.AmendInput{ItemDescription: ptr("   ")}},
		{"281-character item", order.AmendInput{ItemDescription: ptr(strings.Repeat("a", 281))}},
		{"location off the campus list", order.AmendInput{DeliveryLocation: ptr("My hall room")}},
		{"141-character note", order.AmendInput{DeliveryNote: ptr(strings.Repeat("n", 141))}},
		{"item with a NUL", order.AmendInput{ItemDescription: ptr("a\x00b")}},
		{"note with a NUL", order.AmendInput{DeliveryNote: ptr("a\x00b")}},
		// AI-generated (edited by James Teo): create's text rule (F3.7.2).
		{"item of only a zero-width space", order.AmendInput{ItemDescription: ptr("\u200b")}},
		{"item with a line break", order.AmendInput{ItemDescription: ptr("Two kaya\ntoast sets")}},
		{"item with a right-to-left override", order.AmendInput{ItemDescription: ptr("\u202egnp.exe")}},
		{"note with a tab", order.AmendInput{DeliveryNote: ptr("Hall 3\tRoom 12")}},
		{"note with a direction isolate", order.AmendInput{DeliveryNote: ptr("Room \u206621\u2069")}},
		// AI-generated (edited by James Teo).
		{"item with a line separator", order.AmendInput{ItemDescription: ptr("Two kaya\u2028toast sets")}},
		{"note with a paragraph separator", order.AmendInput{DeliveryNote: ptr("Hall 3\u2029Room 12")}},
		{"item of only blank fillers", order.AmendInput{ItemDescription: ptr("\u3164\u2800")}},
		{"reward 0", order.AmendInput{Reward: ptr(0)}},
		{"negative reward", order.AmendInput{Reward: ptr(-3)}},
		{"a reward past credit-service's limit", order.AmendInput{Reward: ptr(order.MaxReward + 1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			before := f.stored(o.ID)
			in := tc.in
			in.Version = o.Version

			_, err := f.svc.Amend(f.ctx, f.requester, o.ID, in)

			requireKind(t, err, order.KindValidation)
			f.requireUnchanged(before)
			f.requireNoWriteSince(before)
			assert.Empty(t, f.credit.Adjusts())
		})
	}
}

func TestAmendRejections(t *testing.T) {
	cases := []struct {
		name string
		// prepare may move the clock or the errand; it returns who amends.
		prepare func(f *fixture, o *order.Order) order.Caller
		version func(o order.Order) int
		want    order.Kind
		msg     string
	}{
		{
			name:    "a version the requester did not see",
			prepare: func(f *fixture, _ *order.Order) order.Caller { return f.requester },
			version: func(o order.Order) int { return o.Version + 1 },
			want:    order.KindConflict, msg: changedMsg,
		},
		{
			name: "at its deadline (F3.10.2)",
			prepare: func(f *fixture, _ *order.Order) order.Caller {
				f.clock.Advance(expiryPeriod)
				return f.requester
			},
			want: order.KindConflict, msg: "this errand has expired",
		},
		{
			name: "past its deadline",
			prepare: func(f *fixture, _ *order.Order) order.Caller {
				f.clock.Advance(expiryPeriod + time.Minute)
				return f.requester
			},
			want: order.KindConflict, msg: "this errand has expired",
		},
		{
			name:    "another student (F3.7.3)",
			prepare: func(f *fixture, _ *order.Order) order.Caller { return f.stranger },
			want:    order.KindForbidden, msg: "only the errand's requester can do this",
		},
		{
			name:    "an admin",
			prepare: func(f *fixture, _ *order.Order) order.Caller { return f.admin },
			want:    order.KindForbidden, msg: "only the errand's requester can do this",
		},
		{
			name: "no longer OPEN (F3.7.4)",
			prepare: func(f *fixture, o *order.Order) order.Caller {
				o.State, o.CourierID, o.ExpiresAt = order.StateAccepted, ptr(f.courier.UserID), nil
				f.repo.Put(*o)
				return f.requester
			},
			want: order.KindConflict, msg: "this errand is now accepted",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			c := tc.prepare(f, &o)
			version := o.Version
			if tc.version != nil {
				version = tc.version(o)
			}
			before := f.stored(o.ID)

			_, err := f.svc.Amend(f.ctx, c, o.ID, order.AmendInput{Version: version, Reward: ptr(9)})

			requireKind(t, err, tc.want)
			assert.Equal(t, tc.msg, order.MessageOf(err))
			f.requireUnchanged(before)
			f.requireNoWriteSince(before)
			assert.Empty(t, f.credit.Adjusts())
		})
	}
}

func TestAmendRefusedWhenTheDeadlinePassesBeforeTheLock(t *testing.T) {
	// D-049, F3.10.2: the deadline is judged again under the row lock, so an
	// amend whose supplier lookup and lock wait ran past T is refused there,
	// before credit is touched.
	f := newFixture(t)
	o := f.seed(order.StateOpen) // expires in an hour
	before := f.stored(o.ID)
	f.repo.beforeNext("Amend", func() { f.clock.Advance(time.Hour) })

	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(9)})

	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "this errand has expired", order.MessageOf(err))
	assert.Empty(t, f.credit.Adjusts())
	f.requireUnchanged(before)
}

func TestAmendIsStampedWithTheTimeUnderTheLock(t *testing.T) {
	// The row and its AMEND entry carry the time the amend was checked under
	// the lock, not the time the request began (F3.7.7).
	f := newFixture(t)
	o := f.seed(order.StateOpen)
	f.repo.beforeNext("Amend", func() { f.clock.Advance(10 * time.Minute) })

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(9)})

	require.NoError(t, err)
	locked := f.clock.Now()
	assert.Equal(t, locked, got.UpdatedAt)
	h := f.history(o.ID)
	require.Len(t, h, 1)
	assert.Equal(t, locked, h[0].OccurredAt)
}

func TestAmendUnknownErrand(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Amend(f.ctx, f.requester, uuid.New(), order.AmendInput{Version: 1, Reward: ptr(9)})
	requireKind(t, err, order.KindNotFound)
}

func TestAmendChecksTheSupplierEveryTime(t *testing.T) {
	// D-052: on every amend, even one that keeps the supplier, and before
	// credit is touched.
	cases := []struct {
		name  string
		setup func(f *fixture, o *order.Order)
		in    func(f *fixture) order.AmendInput
		want  order.Kind
	}{
		{
			name:  "supplier-service not answering",
			setup: func(f *fixture, _ *order.Order) { f.suppliers.FailWith(ordertest.ErrInjected) },
			in:    func(*fixture) order.AmendInput { return order.AmendInput{Reward: ptr(9)} },
			want:  order.KindUnavailable,
		},
		{
			name: "the errand's supplier stopped taking errands",
			setup: func(f *fixture, _ *order.Order) {
				closed := f.supplier
				closed.Available = false
				f.suppliers.Put(closed)
			},
			in:   func(*fixture) order.AmendInput { return order.AmendInput{Reward: ptr(9)} },
			want: order.KindValidation,
		},
		{
			name: "the errand's supplier was deleted",
			setup: func(f *fixture, o *order.Order) {
				o.Supplier.ID = uuid.New()
				f.repo.Put(*o)
			},
			in:   func(*fixture) order.AmendInput { return order.AmendInput{Reward: ptr(9)} },
			want: order.KindValidation,
		},
		{
			name:  "switching to a supplier not taking errands",
			setup: func(*fixture, *order.Order) {},
			in: func(f *fixture) order.AmendInput {
				return order.AmendInput{SupplierID: ptr(f.closedSupplier.ID), Reward: ptr(9)}
			},
			want: order.KindValidation,
		},
		{
			name:  "switching to an unknown supplier",
			setup: func(*fixture, *order.Order) {},
			in:    func(*fixture) order.AmendInput { return order.AmendInput{SupplierID: ptr(uuid.New()), Reward: ptr(9)} },
			want:  order.KindValidation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			tc.setup(f, &o)
			before := f.stored(o.ID)
			in := tc.in(f)
			in.Version = o.Version

			_, err := f.svc.Amend(f.ctx, f.requester, o.ID, in)

			requireKind(t, err, tc.want)
			f.requireUnchanged(before)
			f.requireNoWriteSince(before)
			assert.Empty(t, f.credit.Adjusts(), "credit is not touched")
		})
	}
}

func TestAmendRefreshesTheSupplierCopy(t *testing.T) {
	// D-052: the copy is taken again at amend. A refreshed copy of the same
	// supplier is not a change the requester made, so it is not listed.
	f := newFixture(t)
	o := f.post()
	renamed := f.supplier
	renamed.Name, renamed.LocationDescription = "Kaya Toast Stall (Level 1)", "Beside the bus stop"
	f.suppliers.Put(renamed)

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, ItemDescription: ptr("One kaya toast set")})

	require.NoError(t, err)
	assert.Equal(t, order.SupplierSnapshot{
		ID: f.supplier.ID, Name: "Kaya Toast Stall (Level 1)", Location: "Yusof Ishak House, level 1 — Beside the bus stop",
	}, got.Supplier)
	h := f.history(o.ID)
	require.Len(t, h, 2)
	assert.Equal(t, []order.FieldChange{
		{Field: order.FieldItemDescription, From: "Two kaya toast sets", To: "One kaya toast set"},
	}, h[1].Changes)
}

func TestAmendWithInsufficientCredits(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)
	f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) { return false, order.ErrInsufficientCredits }

	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(50)})

	requireKind(t, err, order.KindInsufficientCredits)
	f.requireUnchanged(before)
	assert.Equal(t, []int{50}, rewards(f.credit.Adjusts()), "a definite refusal queues no reward_set")
	held, _ := f.credit.Hold(o.ID)
	assert.Equal(t, 5, held)
}

func TestAmendWhenCreditRefuses(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)
	f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) { return false, order.ErrCreditRejected }

	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8)})

	requireKind(t, err, order.KindUnavailable)
	f.requireUnchanged(before)
	assert.Equal(t, []int{8}, rewards(f.credit.Adjusts()), "a definite refusal queues no reward_set")
	held, _ := f.credit.Hold(o.ID)
	assert.Equal(t, 5, held)
}

func TestAmendQueuesRewardSetWhenTheAdjustOutcomeIsUnknown(t *testing.T) {
	// D-057: when the PATCH's outcome is unclear, the amend is refused and a
	// reward_set for the errand as saved is queued under the row lock, so
	// credit sets its hold back to the saved reward whether or not the PATCH
	// took effect. Nothing is retried over HTTP.
	cases := []struct {
		name  string
		apply bool
	}{
		{"credit applied it but the answer was lost", true},
		{"the call failed before credit acted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			before := f.stored(o.ID)
			f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) { return tc.apply, ordertest.ErrInjected }

			_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})

			requireKind(t, err, order.KindUnavailable)
			after := f.stored(o.ID)
			assert.Equal(t, before.order, after.order, "the errand keeps its saved reward")
			assert.Equal(t, before.history, after.history, "no AMEND history row")
			assert.Equal(t, []int{8}, rewards(f.credit.Adjusts()), "one PATCH, no retry over HTTP")
			assert.Contains(t, f.repo.attemptedWrites(), "QueueLocked", "built under the row lock")
			requireRewardSet(t, before, after, o, 5, o.Version, "corr-amend")
		})
	}
}

func TestAmendQueuesRewardSetWhenTheWriteFailsAfterTheAdjust(t *testing.T) {
	// The PATCH applied, then the commit failed: credit holds the new amount
	// for an errand still at the old reward, until it applies the queued
	// reward_set (D-057).
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)
	f.repo.AfterAmendFn = ordertest.ErrInjected

	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})

	requireKind(t, err, order.KindUnavailable)
	after := f.stored(o.ID)
	assert.Equal(t, before.order, after.order)
	assert.Equal(t, before.history, after.history)
	assert.Equal(t, []int{8}, rewards(f.credit.Adjusts()))
	requireRewardSet(t, before, after, o, 5, o.Version, "corr-amend")
}

func TestAmendQueuesRewardSetEvenWhenTheClientGivesUp(t *testing.T) {
	// The client disconnects while the PATCH is in flight and credit applied
	// it: the reward_set must still be queued, on a context the request's
	// cancellation does not reach (D-057).
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) {
		cancel()
		return true, context.Canceled
	}

	_, err := f.svc.Amend(ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})

	requireKind(t, err, order.KindUnavailable)
	require.Error(t, ctx.Err(), "the request was cancelled")
	assert.Equal(t, []bool{false}, f.repo.queueContexts(), "the queueing did not inherit the cancellation")
	requireRewardSet(t, before, f.stored(o.ID), o, 5, o.Version, "corr-amend")
}

func TestAmendLogsWhenTheRewardSetCannotBeQueued(t *testing.T) {
	// If even the reward_set cannot be written, the amend still answers 503
	// and the possible mismatch is logged at ERROR for someone to resolve.
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)
	logs := &logRecorder{}
	svc := order.NewService(f.repo, f.credit, f.suppliers, f.clock, f.cfg, slog.New(logs))
	f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) { return true, ordertest.ErrInjected }
	f.repo.FailNext("QueueLocked", ordertest.ErrInjected)

	_, err := svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})

	requireKind(t, err, order.KindUnavailable)
	f.requireUnchanged(before)
	errs := logs.at(slog.LevelError)
	require.Len(t, errs, 1)
	attrs := attrsOf(errs[0])
	assert.Equal(t, o.ID, attrs["errand_id"])
	assert.Equal(t, "corr-amend", attrs["request_id"])
	cause, ok := attrs["err"].(error)
	require.True(t, ok, "the failure is logged with its cause")
	assert.ErrorIs(t, cause, ordertest.ErrInjected)
}

// AI-generated (edited by James Teo).
func TestAmendQueuesNoRewardSetOnceTheErrandIsFinal(t *testing.T) {
	// The PATCH's outcome is unknown, and the errand settles before the
	// reward_set is queued. Its settlement event settles by its own amount
	// and credit returns any hold left over (D-059), so a reward_set behind
	// it could only put back a hold that nothing would release.
	cases := []struct {
		name   string
		settle func(f *fixture, o order.Order)
		key    string
	}{
		{"cancelled by the requester", func(f *fixture, o order.Order) {
			_, err := f.svc.Cancel(f.ctx, f.requester, o.ID, "corr-cancel")
			require.NoError(f.t, err)
		}, order.RoutingCancelled},
		{"expired by the sweeper", func(f *fixture, o order.Order) {
			f.clock.Advance(expiryPeriod)
			ok, err := f.svc.Expire(f.ctx, o.ID, "corr-sweep")
			require.NoError(f.t, err)
			require.True(f.t, ok)
		}, order.RoutingExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			f.credit.AdjustHook = func(ordertest.AdjustCall) (bool, error) { return true, ordertest.ErrInjected }
			f.repo.beforeNext("QueueLocked", func() { tc.settle(f, o) })

			_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})

			requireKind(t, err, order.KindUnavailable)
			assert.Contains(t, f.repo.attemptedWrites(), "QueueLocked", "the errand was read under its row lock")
			events := f.repo.EventsFor(o.ID)
			require.Len(t, events, 1, "the settlement event only, no reward_set")
			assert.Equal(t, tc.key, events[0].RoutingKey)
			assert.Equal(t, 5, events[0].Payload.Amount, "it settles the reward the errand kept")
		})
	}
}

func TestAmendWritesRewardSetWithTheRewardChange(t *testing.T) {
	// D-057: a saved reward change carries its reward_set in the same write,
	// at the errand's new version; a change that leaves the reward alone
	// carries none.
	f := newFixture(t)
	o := f.post()
	before := f.stored(o.ID)

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend"})
	require.NoError(t, err)

	requireRewardSet(t, before, f.stored(o.ID), o, 8, got.Version, "corr-amend")
	assert.Equal(t, o.Version+1, got.Version)
	assert.NotContains(t, f.repo.attemptedWrites(), "QueueLocked", "no separate write")

	mid := f.stored(o.ID)
	_, err = f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: got.Version, ItemDescription: ptr("One kaya toast set")})
	require.NoError(t, err)
	assert.ElementsMatch(t, mid.events, f.stored(o.ID).events, "no reward_set without a reward change")
}

// logRecorder is a slog.Handler that keeps every record, so a test can read
// what the service logged. It is safe for concurrent use. The service adds no
// attributes through Logger.With, so WithAttrs and WithGroup need not keep any.
type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *logRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *logRecorder) WithGroup(string) slog.Handler { return h }

// at returns the records logged at level so far, oldest first.
func (h *logRecorder) at(level slog.Level) []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Record
	for _, r := range h.records {
		if r.Level == level {
			out = append(out, r)
		}
	}
	return out
}

// attrsOf returns a record's attributes by key.
func attrsOf(r slog.Record) map[string]any {
	out := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.Resolve().Any()
		return true
	})
	return out
}

// requireRewardSet fails unless exactly one outbox event was added between
// before and after, and it is an errand.reward_set for o carrying amount and
// version (D-057).
func requireRewardSet(t *testing.T, before, after stored, o order.Order, amount, version int, correlationID string) {
	t.Helper()
	seen := map[uuid.UUID]bool{}
	for _, e := range before.events {
		seen[e.EventID] = true
	}
	var added []order.OutboxEvent
	for _, e := range after.events {
		if !seen[e.EventID] {
			added = append(added, e)
		}
	}
	require.Len(t, added, 1, "exactly one new outbox event")
	e := added[0]
	assert.Equal(t, order.RoutingRewardSet, e.RoutingKey)
	assert.Equal(t, order.RoutingRewardSet, e.Payload.EventType)
	assert.Equal(t, o.ID, e.Payload.ErrandID)
	assert.Equal(t, o.RequesterID, e.Payload.RequesterID)
	assert.Equal(t, amount, e.Payload.Amount, "the reward credit should hold")
	require.NotNil(t, e.Payload.Version)
	assert.Equal(t, version, *e.Payload.Version)
	assert.Nil(t, e.Payload.CourierID)
	assert.Equal(t, correlationID, e.CorrelationID)
	assert.Equal(t, correlationID, e.Payload.CorrelationID)
}

func TestAmendLosesARaceToAccept(t *testing.T) {
	// A courier accepts between the requester's read and the row lock: the
	// check under the lock refuses, before credit is touched (D-046, D-057).
	f := newFixture(t)
	o := f.post()
	f.repo.beforeNext("Amend", func() {
		_, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr-race")
		require.NoError(t, err)
	})

	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8)})

	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "this errand is now accepted", order.MessageOf(err))
	assert.Empty(t, f.credit.Adjusts())
	now := f.stored(o.ID).order
	assert.Equal(t, order.StateAccepted, now.State)
	assert.Equal(t, 5, now.Reward)
}

func TestAmendLosesARaceToAnotherAmend(t *testing.T) {
	// The requester's other tab amends first: this amend's version is stale
	// under the lock (D-046).
	f := newFixture(t)
	o := f.post()
	f.repo.beforeNext("Amend", func() {
		_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, ItemDescription: ptr("From the other tab")})
		require.NoError(t, err)
	})

	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{Version: o.Version, Reward: ptr(8)})

	requireKind(t, err, order.KindConflict)
	assert.Equal(t, changedMsg, order.MessageOf(err))
	assert.Empty(t, f.credit.Adjusts())
	now := f.stored(o.ID).order
	assert.Equal(t, "From the other tab", now.ItemDescription)
	assert.Equal(t, 5, now.Reward)
	assert.Equal(t, o.Version+1, now.Version)
}
