// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Posting an errand: validation (F3.1.2-F3.1.4, D-053), the
//   supplier check (D-052), reserve-before-save with a queued refund
//   and duplicate-submission replay (D-047).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestCreatePostsAnOpenErrand(t *testing.T) {
	f := newFixture(t)
	now := f.clock.Now()
	in := f.createInput()
	in.RequestKey = "req-1"

	// D-047's order: when credit is asked, the refund is already queued and
	// the errand is not yet saved.
	var refundQueued, errandSaved bool
	f.credit.ReserveHook = func(r order.Reservation) (bool, error) {
		for _, e := range f.repo.Events() {
			if e.RoutingKey == order.RoutingFailed && e.Payload.ErrandID == r.ErrandID {
				refundQueued = true
			}
		}
		_, err := f.repo.Get(f.ctx, r.ErrandID)
		errandSaved = err == nil
		return true, nil
	}

	o, replayed, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.True(t, refundQueued, "the refund is queued before credit is reserved")
	assert.False(t, errandSaved, "the errand is saved only once credit is reserved")

	assert.NotEqual(t, uuid.Nil, o.ID)
	assert.Equal(t, order.StateOpen, o.State, "F3.1.7")
	assert.Equal(t, f.requester.UserID, o.RequesterID)
	assert.Nil(t, o.CourierID)
	assert.Equal(t, order.SupplierSnapshot{ID: f.supplier.ID, Name: f.supplier.Name, Location: seededLocation}, o.Supplier,
		"the errand keeps a copy of its supplier")
	assert.Equal(t, "Two kaya toast sets", o.ItemDescription)
	assert.Equal(t, "COM3 Lobby", o.DeliveryLocation)
	assert.Equal(t, "Blue shirt, by the lift", o.DeliveryNote)
	assert.Equal(t, 5, o.Reward)
	assert.Equal(t, 1, o.Version)
	assert.Equal(t, "req-1", o.RequestKey)
	assert.Equal(t, now, o.CreatedAt)
	assert.Equal(t, now, o.UpdatedAt)
	assert.Equal(t, now, o.StateChangedAt)
	assert.Equal(t, ptr(now.Add(expiryPeriod)), o.ExpiresAt, "the deadline is now + T")
	assert.Nil(t, o.ConfirmBy)

	saved, err := f.repo.Get(f.ctx, o.ID)
	require.NoError(t, err)
	assert.Equal(t, o, saved)

	// F3.1.5: one reservation, for this errand, in its requester's name (D-056).
	assert.Equal(t, []order.Reservation{{
		ErrandID: o.ID, RequesterID: f.requester.UserID, Amount: 5, CorrelationID: "corr-create",
	}}, f.credit.Reserves())
	held, ok := f.credit.Hold(o.ID)
	assert.True(t, ok)
	assert.Equal(t, 5, held)

	assert.Empty(t, f.repo.Events(), "saving the errand deleted its queued refund, and posting settles nothing")

	h := f.history(o.ID)
	require.Len(t, h, 1)
	assert.Equal(t, order.ActionCreate, h[0].Action)
	assert.Equal(t, order.State(""), h[0].FromState)
	assert.Equal(t, order.StateOpen, h[0].ToState)
	assert.Equal(t, ptr(f.requester.UserID), h[0].ActorID)
	assert.Equal(t, order.ActorRequester, h[0].ActorKind)
	assert.Nil(t, h[0].CourierID)
	assert.Empty(t, h[0].Changes)
	assert.Equal(t, now, h[0].OccurredAt)
	assert.Equal(t, "corr-create", h[0].CorrelationID)
}

// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — covers the recorded
// scheduled-create behavior, timestamp window, credit reservation and limit.
func TestCreatePostsScheduledErrandWithoutStartingExpiry(t *testing.T) {
	f := newFixture(t)
	for range order.MaxOpenErrands {
		f.seed(order.StateOpen)
	}
	scheduledAt := startTime().Add(29 * time.Hour)
	in := f.createInput()
	in.ScheduledAt = &scheduledAt

	o, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, order.StateScheduled, o.State)
	assert.Equal(t, &scheduledAt, o.ScheduledAt)
	assert.Nil(t, o.ExpiresAt, "expiry begins only when the errand becomes OPEN")
	assert.Equal(t, []order.Reservation{{ErrandID: o.ID, RequesterID: f.requester.UserID, Amount: 5, CorrelationID: "corr-create"}}, f.credit.Reserves())
	require.Len(t, f.history(o.ID), 1)
	assert.Equal(t, order.StateScheduled, f.history(o.ID)[0].ToState)
}

func TestCreateValidatesScheduledTimeAndAcceptsTheEarliestBoundary(t *testing.T) {
	cases := []struct {
		name       string
		clockDelta time.Duration
		schedule   time.Time
		valid      bool
	}{
		{name: "at current time", schedule: startTime(), valid: false},
		{name: "before current time", schedule: startTime().Add(-time.Second), valid: false},
		{name: "before previous-day midnight boundary", clockDelta: -9*time.Hour - time.Nanosecond, schedule: startTime().Add(29 * time.Hour), valid: false},
		{name: "exactly previous-day midnight boundary", clockDelta: -9 * time.Hour, schedule: startTime().Add(15 * time.Hour), valid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.clock.Advance(tc.clockDelta)
			in := f.createInput()
			in.ScheduledAt = &tc.schedule

			o, _, err := f.svc.Create(f.ctx, f.requester, in)

			if !tc.valid {
				requireKind(t, err, order.KindValidation)
				f.requireNoWrite()
				assert.Empty(t, f.credit.Reserves())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, &tc.schedule, o.ScheduledAt)
		})
	}
}

func TestCreateIsForStudentsOnly(t *testing.T) {
	// D-045: create is open to any STUDENT and to nobody else, refused before
	// supplier-service, credit or the outbox is touched.
	cases := []struct {
		name   string
		caller func(f *fixture) order.Caller
	}{
		{"an admin", func(f *fixture) order.Caller { return f.admin }},
		{"a caller with no role", func(*fixture) order.Caller { return order.Caller{UserID: uuid.New()} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			c := tc.caller(f)

			_, _, err := f.svc.Create(f.ctx, c, f.createInput())

			requireKind(t, err, order.KindForbidden)
			assert.Equal(t, "only students can post errands", order.MessageOf(err))
			f.requireNoWrite()
			assert.Zero(t, f.suppliers.calls.Load())
			assert.Empty(t, f.credit.Reserves())
			assert.Empty(t, f.repo.Events())
			assert.Empty(t, f.errandsOf(c))
		})
	}
}

func TestCreateValidatesTheErrand(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*order.CreateInput)
		valid bool
	}{
		{"no supplier", func(in *order.CreateInput) { in.SupplierID = uuid.Nil }, false},
		{"empty item", func(in *order.CreateInput) { in.ItemDescription = "" }, false},
		{"whitespace-only item", func(in *order.CreateInput) { in.ItemDescription = " \t\n " }, false},
		{"281-character item", func(in *order.CreateInput) { in.ItemDescription = strings.Repeat("a", 281) }, false},
		{"280-character item", func(in *order.CreateInput) { in.ItemDescription = strings.Repeat("a", 280) }, true},
		// Counted in characters, not bytes (D-053): "é" is two bytes.
		{"281 two-byte characters", func(in *order.CreateInput) { in.ItemDescription = strings.Repeat("é", 281) }, false},
		{"280 two-byte characters", func(in *order.CreateInput) { in.ItemDescription = strings.Repeat("é", 280) }, true},
		{"280 characters inside surrounding spaces", func(in *order.CreateInput) {
			in.ItemDescription = "  " + strings.Repeat("a", 280) + "  "
		}, true},
		{"location off the campus list", func(in *order.CreateInput) { in.DeliveryLocation = "My hall room" }, false},
		{"location in the wrong case", func(in *order.CreateInput) { in.DeliveryLocation = "com3 lobby" }, false},
		{"no location", func(in *order.CreateInput) { in.DeliveryLocation = "" }, false},
		{"141-character note", func(in *order.CreateInput) { in.DeliveryNote = strings.Repeat("n", 141) }, false},
		{"140-character note", func(in *order.CreateInput) { in.DeliveryNote = strings.Repeat("n", 140) }, true},
		// NUL is the one character a PostgreSQL text column cannot store.
		{"item with a NUL", func(in *order.CreateInput) { in.ItemDescription = "a\x00b" }, false},
		{"note with a NUL", func(in *order.CreateInput) { in.DeliveryNote = "a\x00b" }, false},
		{"no note", func(in *order.CreateInput) { in.DeliveryNote = "" }, true},
		// AI-generated (edited by James Teo): the text rule create and amend share.
		{"item of only a zero-width space", func(in *order.CreateInput) { in.ItemDescription = "\u200b" }, false},
		{"item of only format characters and spaces", func(in *order.CreateInput) { in.ItemDescription = "\u200b \u2060\ufeff" }, false},
		{"item with a line break", func(in *order.CreateInput) { in.ItemDescription = "Two kaya\ntoast sets" }, false},
		{"item with a tab", func(in *order.CreateInput) { in.ItemDescription = "Two\tkaya toast sets" }, false},
		{"item with a C1 control", func(in *order.CreateInput) { in.ItemDescription = "Two\u009bkaya toast sets" }, false},
		{"item with a BEL", func(in *order.CreateInput) { in.ItemDescription = "gnp.exe\u0007" }, false},
		{"item with a right-to-left override", func(in *order.CreateInput) { in.ItemDescription = "\u202egnp.exe" }, false},
		{"item with a direction embedding", func(in *order.CreateInput) { in.ItemDescription = "Two \u202akaya\u202c toast sets" }, false},
		{"item with a direction isolate", func(in *order.CreateInput) { in.ItemDescription = "Two \u2066kaya\u2069 toast sets" }, false},
		{"item with a zero-width joiner in an emoji", func(in *order.CreateInput) { in.ItemDescription = "Sticker \U0001F469\u200d\U0001F4BB" }, true},
		{"item with a right-to-left script", func(in *order.CreateInput) { in.ItemDescription = "\u0642\u0647\u0648\u0629" }, true},
		{"note with line breaks", func(in *order.CreateInput) { in.DeliveryNote = "Hall 3\nRoom 12" }, true},
		{"note with a carriage return", func(in *order.CreateInput) { in.DeliveryNote = "Hall 3\r\nRoom 12" }, false},
		{"note with a tab", func(in *order.CreateInput) { in.DeliveryNote = "Hall 3\tRoom 12" }, false},
		{"note with a BEL", func(in *order.CreateInput) { in.DeliveryNote = "Hall 3\u0007" }, false},
		{"note with a C1 control", func(in *order.CreateInput) { in.DeliveryNote = "Hall\u0085 3" }, false},
		{"note with a right-to-left override", func(in *order.CreateInput) { in.DeliveryNote = "Room \u202e21" }, false},
		{"note with a direction isolate", func(in *order.CreateInput) { in.DeliveryNote = "Room \u206721\u2069" }, false},
		// AI-generated (edited by James Teo): line and paragraph separators are
		// line breaks, and blank fillers are not visible.
		{"item with a line separator", func(in *order.CreateInput) { in.ItemDescription = "Two kaya\u2028toast sets" }, false},
		{"item with a paragraph separator", func(in *order.CreateInput) { in.ItemDescription = "Two kaya\u2029toast sets" }, false},
		{"note with a line separator", func(in *order.CreateInput) { in.DeliveryNote = "Hall 3\u2028Room 12" }, false},
		{"note with a paragraph separator", func(in *order.CreateInput) { in.DeliveryNote = "Hall 3\u2029Room 12" }, false},
		{"item of only Hangul fillers", func(in *order.CreateInput) { in.ItemDescription = "\u3164\u115f\u1160\uffa0" }, false},
		{"item of only blank Braille", func(in *order.CreateInput) { in.ItemDescription = "\u2800\u2800" }, false},
		{"item of only blank fillers, spaces and format characters", func(in *order.CreateInput) { in.ItemDescription = "\u3164 \u200b\u2800" }, false},
		{"item in Korean", func(in *order.CreateInput) { in.ItemDescription = "\uae40\ubc25 \ub450 \uc904" }, true},
		{"item in Braille", func(in *order.CreateInput) { in.ItemDescription = "\u2803\u2817\u2811\u2811\u2819" }, true},
		{"reward 0", func(in *order.CreateInput) { in.Reward = 0 }, false},
		{"negative reward", func(in *order.CreateInput) { in.Reward = -1 }, false},
		{"reward 1", func(in *order.CreateInput) { in.Reward = 1 }, true},
		{"the largest reward credit-service holds", func(in *order.CreateInput) { in.Reward = 1_000_000 }, true},
		{"a reward past credit-service's limit", func(in *order.CreateInput) { in.Reward = order.MaxReward + 1 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			in := f.createInput()
			tc.edit(&in)

			o, _, err := f.svc.Create(f.ctx, f.requester, in)

			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, order.StateOpen, o.State)
				return
			}
			requireKind(t, err, order.KindValidation)
			f.requireNoWrite()
			assert.Zero(t, f.suppliers.calls.Load(), "validation runs before the supplier check")
			assert.Empty(t, f.credit.Reserves())
			assert.Empty(t, f.repo.Events())
			assert.Empty(t, f.errandsOf(f.requester))
		})
	}
}

func TestCreateAcceptsEveryCampusLocation(t *testing.T) {
	for _, loc := range order.DeliveryLocations() {
		t.Run(loc.Name, func(t *testing.T) {
			f := newFixture(t)
			in := f.createInput()
			in.DeliveryLocation = loc.Name
			o, _, err := f.svc.Create(f.ctx, f.requester, in)
			require.NoError(t, err)
			assert.Equal(t, loc.Name, o.DeliveryLocation, "stored as the list's text")
		})
	}
}

func TestCreateTrimsTheItemAndNote(t *testing.T) {
	f := newFixture(t)
	in := f.createInput()
	in.ItemDescription = "  Two kaya toast sets \n"
	in.DeliveryNote = "\t Blue shirt  "
	o, _, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)
	assert.Equal(t, "Two kaya toast sets", o.ItemDescription)
	assert.Equal(t, "Blue shirt", o.DeliveryNote)
}

func TestCreateChecksTheSupplierBeforeTouchingCredit(t *testing.T) {
	// D-052: unknown or deleted → 400, unavailable → 400, supplier-service
	// down → 503, and in every case before any credit is reserved.
	cases := []struct {
		name  string
		setup func(f *fixture, in *order.CreateInput)
		want  order.Kind
	}{
		{"unknown supplier", func(_ *fixture, in *order.CreateInput) { in.SupplierID = uuid.New() }, order.KindValidation},
		{"supplier not taking errands", func(f *fixture, in *order.CreateInput) { in.SupplierID = f.closedSupplier.ID }, order.KindValidation},
		{"supplier-service not answering", func(f *fixture, _ *order.CreateInput) {
			f.suppliers.FailWith(ordertest.ErrInjected)
		}, order.KindUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			in := f.createInput()
			tc.setup(f, &in)

			_, _, err := f.svc.Create(f.ctx, f.requester, in)

			requireKind(t, err, tc.want)
			f.requireNoWrite()
			assert.Equal(t, int64(1), f.suppliers.calls.Load())
			assert.Empty(t, f.credit.Reserves(), "no credit is reserved")
			assert.Empty(t, f.repo.Events(), "no refund is queued")
			assert.Empty(t, f.errandsOf(f.requester))
		})
	}
}

func TestCreateCopiesTheSupplierLocation(t *testing.T) {
	// D-052: the errand keeps the supplier's place as one line of text.
	cases := []struct {
		name string
		sup  order.Supplier
		want string
	}{
		{"building, floor and description", order.Supplier{Building: "Yusof Ishak House", Floor: "1", LocationDescription: "Opp UHC bus stop"},
			"Yusof Ishak House, level 1 — Opp UHC bus stop"},
		{"building only", order.Supplier{Building: "COM3"}, "COM3"},
		{"building and floor", order.Supplier{Building: "COM3", Floor: "2"}, "COM3, level 2"},
		{"building and description", order.Supplier{Building: "COM3", LocationDescription: "Next to the lift"}, "COM3 — Next to the lift"},
		{"floor and description only", order.Supplier{Floor: "B1", LocationDescription: "Vending area"}, "level B1 — Vending area"},
		{"padded fields", order.Supplier{Building: " COM3 ", Floor: " 2 ", LocationDescription: " Lift "}, "COM3, level 2 — Lift"},
		{"nothing given", order.Supplier{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			sup := tc.sup
			sup.ID, sup.Name, sup.Available = uuid.New(), "Somewhere", true
			f.suppliers.Put(sup)
			in := f.createInput()
			in.SupplierID = sup.ID

			o, _, err := f.svc.Create(f.ctx, f.requester, in)

			require.NoError(t, err)
			assert.Equal(t, order.SupplierSnapshot{ID: sup.ID, Name: "Somewhere", Location: tc.want}, o.Supplier)
		})
	}
}

func TestCreateFailsSafelyWhenTheRefundCannotBeQueued(t *testing.T) {
	f := newFixture(t)
	f.repo.FailNext("QueueEvent", ordertest.ErrInjected)

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	requireKind(t, err, order.KindUnavailable)
	assert.ErrorIs(t, err, ordertest.ErrInjected, "the cause is kept for the logs")
	assert.Empty(t, f.credit.Reserves(), "no credit is reserved without a queued refund")
	assert.Empty(t, f.repo.Events())
	assert.Empty(t, f.errandsOf(f.requester))
}

func TestCreateWithInsufficientCredits(t *testing.T) {
	f := newFixture(t)
	now := f.clock.Now()
	f.credit.ReserveHook = func(order.Reservation) (bool, error) { return false, order.ErrInsufficientCredits }

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	requireKind(t, err, order.KindInsufficientCredits)
	assert.Empty(t, f.errandsOf(f.requester), "no errand without its credits (F3.1.6)")
	reserves := f.credit.Reserves()
	require.Len(t, reserves, 1)
	f.requireQueuedRefund(reserves[0], now, refundDelay)
}

func TestCreateWhenTheReserveOutcomeIsUnknown(t *testing.T) {
	// The queued refund stays behind and fires after the delay, releasing
	// whatever credit may hold (D-047, F3.1.6).
	cases := []struct {
		name  string
		apply bool
	}{
		{"credit holds the amount but the answer was lost", true},
		{"the call failed before credit acted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			now := f.clock.Now()
			f.credit.ReserveHook = func(order.Reservation) (bool, error) { return tc.apply, ordertest.ErrInjected }

			_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

			requireKind(t, err, order.KindUnavailable)
			assert.ErrorIs(t, err, ordertest.ErrInjected)
			assert.Empty(t, f.errandsOf(f.requester))
			reserves := f.credit.Reserves()
			require.Len(t, reserves, 1)
			f.requireQueuedRefund(reserves[0], now, refundDelay)
			_, held := f.credit.Hold(reserves[0].ErrandID)
			assert.Equal(t, tc.apply, held)
		})
	}
}

func TestCreateUsesTheDefaultRefundDelay(t *testing.T) {
	assert.Equal(t, 60*time.Second, order.DefaultRefundDelay)
	cfg := defaultConfig()
	cfg.RefundDelay = 0
	f := newFixtureWith(t, cfg)
	now := f.clock.Now()
	f.credit.ReserveHook = func(order.Reservation) (bool, error) { return true, ordertest.ErrInjected }

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	requireKind(t, err, order.KindUnavailable)
	reserves := f.credit.Reserves()
	require.Len(t, reserves, 1)
	f.requireQueuedRefund(reserves[0], now, order.DefaultRefundDelay)
}

func TestCreateIsUndoneWhenTheRefundWentOutFirst(t *testing.T) {
	// The create ran past the refund delay and the relay published the
	// queued refund: the errand must not be saved, and that refund is what
	// returns the reservation (D-047).
	cases := []struct {
		name  string
		setup func(f *fixture)
	}{
		{"the relay published it while credit was reserving", func(f *fixture) {
			f.credit.ReserveHook = func(order.Reservation) (bool, error) {
				for _, e := range f.repo.Events() {
					f.repo.MarkPublished(e.EventID)
				}
				return true, nil
			}
		}},
		{"the repository reports it gone", func(f *fixture) {
			f.repo.FailNext("Create", order.ErrGuardGone)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			now := f.clock.Now()
			tc.setup(f)

			_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

			requireKind(t, err, order.KindUnavailable)
			assert.ErrorIs(t, err, order.ErrGuardGone)
			assert.Equal(t, "posting took too long and was undone; your credits will be returned", order.MessageOf(err))
			assert.Empty(t, f.errandsOf(f.requester))
			reserves := f.credit.Reserves()
			require.Len(t, reserves, 1)
			f.requireQueuedRefund(reserves[0], now, refundDelay)
		})
	}
}

func TestCreateWhenSavingFails(t *testing.T) {
	f := newFixture(t)
	now := f.clock.Now()
	f.repo.FailNext("Create", ordertest.ErrInjected)

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	requireKind(t, err, order.KindUnavailable)
	assert.ErrorIs(t, err, ordertest.ErrInjected)
	assert.Empty(t, f.errandsOf(f.requester))
	reserves := f.credit.Reserves()
	require.Len(t, reserves, 1)
	f.requireQueuedRefund(reserves[0], now, refundDelay)
}

func TestCreateReplaysARepeatedRequestKey(t *testing.T) {
	f := newFixture(t)
	in := f.createInput()
	in.RequestKey = "req-1"
	first, replayed, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)
	require.False(t, replayed)
	lookups := f.suppliers.calls.Load()

	// The retry after a lost response comes later, with credit and
	// supplier-service both failing now: neither may be asked (D-047).
	f.clock.Advance(10 * time.Minute)
	f.suppliers.FailWith(ordertest.ErrInjected)
	f.credit.ReserveHook = func(order.Reservation) (bool, error) { return false, ordertest.ErrInjected }

	again, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first, again)
	assert.Len(t, f.credit.Reserves(), 1, "no second reservation")
	assert.Equal(t, lookups, f.suppliers.calls.Load(), "no second supplier lookup")
	assert.Len(t, f.errandsOf(f.requester), 1)
	assert.Len(t, f.history(first.ID), 1)
	assert.Empty(t, f.repo.Events())
}

// AI-generated (edited by James Teo).
func TestCreateReplayIgnoresADifferentBody(t *testing.T) {
	// D-047: the key alone names the submission. A create under a key the
	// requester already used returns the errand that key created, even if the
	// form was edited before the retry; nothing in the new body is saved,
	// looked up or reserved.
	f := newFixture(t)
	in := f.createInput()
	in.RequestKey = "form-1"
	first, _, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)
	lookups := f.suppliers.calls.Load()

	edited := in
	edited.SupplierID = f.otherSupplier.ID
	edited.ItemDescription = "Two sandwiches"
	edited.DeliveryLocation = "Central Library entrance"
	edited.DeliveryNote = ""
	edited.Reward = 50
	got, replayed, err := f.svc.Create(f.ctx, f.requester, edited)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first, got, "the stored errand, not the edited body")
	assert.Len(t, f.credit.Reserves(), 1, "the edited reward is not reserved")
	assert.Equal(t, lookups, f.suppliers.calls.Load(), "the edited supplier is not looked up")
	assert.Len(t, f.errandsOf(f.requester), 1)
}

// AI-generated (edited by James Teo).
func TestCreateStopsAtTheOpenErrandLimit(t *testing.T) {
	// D-058: a requester with five errands OPEN cannot post a sixth. It is
	// refused before supplier-service or credit is asked, so nothing is held.
	f := newFixture(t)
	for range order.MaxOpenErrands {
		f.post()
	}
	lookups := f.suppliers.calls.Load()

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "you already have 5 open errands; cancel one or wait for one to be accepted before posting another",
		order.MessageOf(err))
	assert.Equal(t, lookups, f.suppliers.calls.Load(), "no supplier lookup")
	assert.Len(t, f.credit.Reserves(), order.MaxOpenErrands, "no reservation")
	assert.Empty(t, f.repo.Events(), "no queued refund")
	assert.Len(t, f.errandsOf(f.requester), order.MaxOpenErrands)

	_, _, err = f.svc.Create(f.ctx, f.stranger, f.createInput())
	require.NoError(t, err, "the limit is per requester")
}

// AI-generated (edited by James Teo).
func TestCreateCountsOnlyErrandsStillOpen(t *testing.T) {
	// D-058 counts OPEN errands before their deadline: one past it is as good
	// as expired before the sweeper gets to it (D-049), and errands that left
	// OPEN do not count.
	f := newFixture(t)
	for _, s := range []order.State{
		order.StateAccepted, order.StatePickedUp, order.StateDelivered,
		order.StateCompleted, order.StateCancelled, order.StateExpired,
	} {
		f.seed(s)
	}
	for range order.MaxOpenErrands - 1 {
		f.seed(order.StateOpen)
	}
	due := f.seed(order.StateOpen)
	due.ExpiresAt = ptr(f.clock.Now())
	f.repo.Put(due)

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())
	require.NoError(t, err, "four open errands and one past its deadline leave one place")

	_, _, err = f.svc.Create(f.ctx, f.requester, f.createInput())
	requireKind(t, err, order.KindConflict)
}

// AI-generated (edited by James Teo).
func TestCreateReplaysAnErrandThatFilledTheLastPlace(t *testing.T) {
	// A retry of the create that filled the fifth place gets its errand back,
	// not the limit's 409 (D-047, D-058).
	f := newFixture(t)
	for range order.MaxOpenErrands - 1 {
		f.post()
	}
	in := f.createInput()
	in.RequestKey = "req-last"
	first, _, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)

	again, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first, again)
}

// AI-generated (edited by James Teo).
func TestCreateLosingTheLastPlaceToAConcurrentCreate(t *testing.T) {
	// Two creates pass the count together; the second to save is refused by
	// the count repo.Create makes under the requester's lock, and its
	// reservation is released by its queued refund (D-047, D-058).
	f := newFixture(t)
	now := f.clock.Now()
	for range order.MaxOpenErrands - 1 {
		f.post()
	}
	f.repo.beforeNext("Create", func() { f.seed(order.StateOpen) })

	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	requireKind(t, err, order.KindConflict)
	// AI-generated (edited by James Teo): this attempt held credits, so it says
	// they come back.
	assert.Equal(t, "you already have 5 open errands, so this errand was not posted; your credits will be returned",
		order.MessageOf(err))
	assert.Len(t, f.errandsOf(f.requester), order.MaxOpenErrands)
	reserves := f.credit.Reserves()
	require.Len(t, reserves, order.MaxOpenErrands, "four posts and this attempt")
	f.requireQueuedRefund(reserves[len(reserves)-1], now, refundDelay)
}

// AI-generated (edited by James Teo).
func TestCreateReplaysATwinThatFilledTheLastPlaceOnAnotherReplica(t *testing.T) {
	// The twin of this create, sent to another replica where the per-key lock
	// does not reach, saves the errand that fills the fifth place between this
	// request's key lookup and its count. This request replays the twin rather
	// than refusing the requester their own errand, and touches neither
	// supplier-service nor credit (D-047, D-058).
	f := newFixture(t)
	for range order.MaxOpenErrands - 1 {
		f.post()
	}
	lookups := f.suppliers.calls.Load()
	in := f.createInput()
	in.RequestKey = "req-last"
	twin := f.twinOnAnotherReplica(in)
	f.repo.beforeNext("CountOpenByRequester", func() { f.repo.Put(twin) })

	got, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, twin, got)
	assert.Equal(t, lookups, f.suppliers.calls.Load(), "no supplier lookup")
	assert.Len(t, f.credit.Reserves(), order.MaxOpenErrands-1, "no reservation")
	assert.Empty(t, f.repo.Events(), "no queued refund")
	assert.Len(t, f.errandsOf(f.requester), order.MaxOpenErrands)
}

// AI-generated (edited by James Teo).
func TestCreateAtTheLimitWithAKeyLooksItUpAgain(t *testing.T) {
	// A create with an X-Request-Id that finds the requester at the limit
	// looks the key up again before refusing (D-047, D-058).
	cases := []struct {
		name    string
		between func(f *fixture) // runs between the first lookup and the count
		kind    order.Kind
		msg     string
	}{
		{
			name:    "a key nothing was saved under is refused",
			between: func(*fixture) {},
			kind:    order.KindConflict,
			msg:     "you already have 5 open errands; cancel one or wait for one to be accepted before posting another",
		},
		{
			name:    "a second lookup that fails is a 503",
			between: func(f *fixture) { f.repo.failFindWith(ordertest.ErrInjected) },
			kind:    order.KindUnavailable,
			msg:     "could not post the errand; please try again",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			for range order.MaxOpenErrands {
				f.post()
			}
			in := f.createInput()
			in.RequestKey = "req-new"
			f.repo.beforeNext("CountOpenByRequester", func() { tc.between(f) })

			_, _, err := f.svc.Create(f.ctx, f.requester, in)

			requireKind(t, err, tc.kind)
			assert.Equal(t, tc.msg, order.MessageOf(err))
			assert.Len(t, f.credit.Reserves(), order.MaxOpenErrands, "no reservation")
			assert.Empty(t, f.repo.Events(), "no queued refund")
		})
	}
}

func TestCreateRequestKeysArePerRequester(t *testing.T) {
	f := newFixture(t)
	in := f.createInput()
	in.RequestKey = "req-1"
	mine, _, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)

	theirs, replayed, err := f.svc.Create(f.ctx, f.stranger, in)

	require.NoError(t, err)
	assert.False(t, replayed)
	assert.NotEqual(t, mine.ID, theirs.ID)
	assert.Equal(t, f.stranger.UserID, theirs.RequesterID)
	assert.Len(t, f.credit.Reserves(), 2)
}

func TestCreateWithoutARequestKeyNeverReplays(t *testing.T) {
	f := newFixture(t)
	// Not even asked: a create with no X-Request-Id never looks a key up.
	f.repo.failFindWith(ordertest.ErrInjected)

	a := f.post()
	b := f.post()

	assert.NotEqual(t, a.ID, b.ID)
	assert.Len(t, f.errandsOf(f.requester), 2)
	assert.Len(t, f.credit.Reserves(), 2)
}

func TestCreateWhenTheKeyLookupFails(t *testing.T) {
	f := newFixture(t)
	f.repo.failFindWith(ordertest.ErrInjected)
	in := f.createInput()
	in.RequestKey = "req-1"

	_, _, err := f.svc.Create(f.ctx, f.requester, in)

	requireKind(t, err, order.KindUnavailable)
	assert.ErrorIs(t, err, ordertest.ErrInjected)
	assert.Zero(t, f.suppliers.calls.Load())
	assert.Empty(t, f.credit.Reserves())
	assert.Empty(t, f.repo.Events())
}

func TestCreateMakesATwinWithTheSameKeyWaitAndReplay(t *testing.T) {
	// D-047: the same submission sent twice at once. The twin waits until the
	// first create has returned, then replays its errand, so credit is asked
	// once and no refund is left queued. The synctest bubble lets the first
	// create hold its save until the twin can go no further; without the
	// per-key lock, the twin would reserve and save an errand of its own.
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		in := f.createInput()
		in.RequestKey = "req-1"
		twin := make(chan createResult, 1)
		var once sync.Once
		f.credit.ReserveHook = func(order.Reservation) (bool, error) {
			once.Do(func() {
				go func() { twin <- createResultOf(f.svc.Create(f.ctx, f.requester, in)) }()
			})
			return true, nil
		}
		f.repo.beforeNext("Create", synctest.Wait)

		first, replayed, err := f.svc.Create(f.ctx, f.requester, in)

		require.NoError(t, err)
		require.False(t, replayed)
		got := receiveCreate(t, twin)
		require.NoError(t, got.err)
		assert.True(t, got.replayed)
		assert.Equal(t, first, got.o)
		assert.Len(t, f.credit.Reserves(), 1, "the twin reserved nothing")
		assert.Len(t, f.errandsOf(f.requester), 1, "one errand per X-Request-Id")
		assert.Empty(t, f.repo.Events(), "the twin queued no refund")
	})
}

func TestCreateTwinWhoseRequestEndsWhileItWaits(t *testing.T) {
	// A twin whose request is gone while the first create holds the key
	// answers 503, and touches neither supplier-service nor credit (D-047).
	f := newFixture(t)
	in := f.createInput()
	in.RequestKey = "req-1"
	gone, cancel := context.WithCancel(f.ctx)
	cancel()
	var got createResult
	var once sync.Once
	f.credit.ReserveHook = func(order.Reservation) (bool, error) {
		once.Do(func() {
			twin := make(chan createResult, 1)
			go func() { twin <- createResultOf(f.svc.Create(gone, f.requester, in)) }()
			got = receiveCreate(t, twin) // while this create still holds the key
		})
		return true, nil
	}

	first, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	require.False(t, replayed)
	requireKind(t, got.err, order.KindUnavailable)
	assert.Equal(t, "could not post the errand; please try again", order.MessageOf(got.err))
	assert.ErrorIs(t, got.err, context.Canceled)
	reserves := f.credit.Reserves()
	require.Len(t, reserves, 1, "the twin reserved nothing")
	assert.Equal(t, first.ID, reserves[0].ErrandID)
	assert.Equal(t, int64(1), f.suppliers.calls.Load(), "the twin looked up no supplier")
}

func TestCreateLosingToAConcurrentTwinReturnsTheTwin(t *testing.T) {
	// The same submission sent twice at once, the twin to another replica,
	// where this process's per-key lock does not reach: the twin saves its
	// errand between this request's key check and its own save (D-047).
	f := newFixture(t)
	now := f.clock.Now()
	in := f.createInput()
	in.RequestKey = "req-1"
	twin := f.twinOnAnotherReplica(in)
	f.repo.beforeNext("Create", func() { f.repo.Put(twin) })

	got, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, twin, got)
	assert.Len(t, f.errandsOf(f.requester), 1, "one errand per X-Request-Id")

	// This request reserved before it lost; that reservation is released by
	// its own queued refund (D-047).
	reserves := f.credit.Reserves()
	require.Len(t, reserves, 1)
	assert.NotEqual(t, twin.ID, reserves[0].ErrandID)
	f.requireQueuedRefund(reserves[0], now, refundDelay)
}

func TestCreateWhenTheTwinCannotBeReadBack(t *testing.T) {
	// The save lost to a twin saved on another replica, and the twin cannot
	// be read back: the request fails safely, and its own reservation is
	// released by its queued refund.
	f := newFixture(t)
	now := f.clock.Now()
	in := f.createInput()
	in.RequestKey = "req-1"
	twin := f.twinOnAnotherReplica(in)
	f.repo.beforeNext("Create", func() {
		f.repo.Put(twin)
		f.repo.failFindWith(ordertest.ErrInjected)
	})

	_, _, err := f.svc.Create(f.ctx, f.requester, in)

	requireKind(t, err, order.KindUnavailable)
	assert.ErrorIs(t, err, ordertest.ErrInjected)
	reserves := f.credit.Reserves()
	require.Len(t, reserves, 1)
	f.requireQueuedRefund(reserves[0], now, refundDelay)
}

// twinOnAnotherReplica is the errand a twin of in, sent with the same
// X-Request-Id, saved through another replica of order-service.
func (f *fixture) twinOnAnotherReplica(in order.CreateInput) order.Order {
	now := f.clock.Now()
	return order.Order{
		ID:               uuid.New(),
		RequesterID:      f.requester.UserID,
		Supplier:         order.SupplierSnapshot{ID: f.supplier.ID, Name: f.supplier.Name, Location: seededLocation},
		ItemDescription:  in.ItemDescription,
		DeliveryLocation: in.DeliveryLocation,
		DeliveryNote:     in.DeliveryNote,
		Reward:           in.Reward,
		State:            order.StateOpen,
		Version:          1,
		RequestKey:       in.RequestKey,
		CreatedAt:        now,
		UpdatedAt:        now,
		StateChangedAt:   now,
		ExpiresAt:        ptr(now.Add(expiryPeriod)),
	}
}

// createResult is what one Create call returned.
type createResult struct {
	o        order.Order
	replayed bool
	err      error
}

func createResultOf(o order.Order, replayed bool, err error) createResult {
	return createResult{o: o, replayed: replayed, err: err}
}

// receiveCreate waits for a create running in another goroutine, failing the
// test after a deadline instead of hanging.
func receiveCreate(t *testing.T, ch <-chan createResult) createResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the twin create did not return")
		return createResult{}
	}
}
