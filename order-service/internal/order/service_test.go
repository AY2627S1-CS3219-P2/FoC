// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The read side: one errand with its history (F3.6, D-045), the open,
//   requested, assigned and admin lists (F3.2, F3.4, F3.5, D-053) and the
//   supplier count (D-052).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestGetShowsAPartyTheErrandAndItsHistory(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	f.clock.Advance(time.Minute)
	accepted, err := f.svc.Accept(f.ctx, f.courier, o.ID, o.Version, "corr")
	require.NoError(t, err)

	for _, p := range []party{asRequester, asCourier, asAdmin} {
		got, history, err := f.svc.Get(f.ctx, f.as(p), o.ID)
		require.NoError(t, err, p.String())
		assert.Equal(t, accepted, got)
		require.Len(t, history, 2, "oldest first (F3.13.1)")
		assert.Equal(t, order.ActionCreate, history[0].Action)
		assert.Equal(t, order.ActionAccept, history[1].Action)
	}
}

func TestGetHidesWhatTheCallerMayNotSee(t *testing.T) {
	// F3.6.3: an errand the caller may not see answers exactly like one that
	// does not exist, so its existence does not leak.
	f := newFixture(t)
	_, _, unknown := f.svc.Get(f.ctx, f.stranger, uuid.New())
	requireKind(t, unknown, order.KindNotFound)

	for _, s := range []order.State{order.StateAccepted, order.StateCancelled, order.StateExpired} {
		o := f.seed(s)
		_, _, err := f.svc.Get(f.ctx, f.stranger, o.ID)
		requireKind(t, err, order.KindNotFound)
		assert.Equal(t, order.MessageOf(unknown), order.MessageOf(err), "%s", s)
	}

	open := f.seed(order.StateOpen)
	got, _, err := f.svc.Get(f.ctx, f.stranger, open.ID)
	require.NoError(t, err, "any signed-in user may see an OPEN errand (F3.6.2)")
	assert.Equal(t, open, got)
}

func TestGetWhenStorageFails(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateOpen)
	f.repo.FailNext("Get", ordertest.ErrInjected)

	_, _, err := f.svc.Get(f.ctx, f.requester, o.ID)

	assert.Equal(t, order.Kind(0), order.KindOf(err))
	assert.ErrorIs(t, err, ordertest.ErrInjected)
}

func TestListOpenOnlyHasErrandsStillOpen(t *testing.T) {
	// F3.2.1, D-045, D-049: OPEN and before the deadline; cancelled and
	// overdue errands are gone even before the sweeper runs.
	f := newFixture(t)
	open := f.seed(order.StateOpen)
	overdue := f.seed(order.StateOpen)
	overdue.ExpiresAt = ptr(f.clock.Now())
	f.repo.Put(overdue)
	for _, s := range []order.State{order.StateAccepted, order.StateCancelled, order.StateExpired, order.StateCompleted} {
		f.seed(s)
	}

	got, err := f.svc.ListOpen(f.ctx, f.stranger)

	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{open.ID}, ids(got))
}

func TestListRequestedSplitsActiveFromHistory(t *testing.T) {
	// F3.4.1 and F3.4.3: the caller's own errands, active or final.
	f := newFixture(t)
	var active, final []uuid.UUID
	for _, s := range order.ActiveStates() {
		active = append(active, f.seed(s).ID)
	}
	for _, s := range order.FinalStates() {
		final = append(final, f.seed(s).ID)
	}
	notMine := f.seed(order.StateOpen)
	notMine.RequesterID = f.stranger.UserID
	f.repo.Put(notMine)

	gotActive, err := f.svc.ListRequested(f.ctx, f.requester, order.ScopeActive)
	require.NoError(t, err)
	assert.ElementsMatch(t, active, ids(gotActive))

	gotHistory, err := f.svc.ListRequested(f.ctx, f.requester, order.ScopeHistory)
	require.NoError(t, err)
	assert.ElementsMatch(t, final, ids(gotHistory))
}

func TestListAssignedIsTheCouriersUnfinishedErrands(t *testing.T) {
	// F3.5.1.
	f := newFixture(t)
	var mine []uuid.UUID
	for _, s := range []order.State{order.StateAccepted, order.StatePickedUp, order.StateDelivered} {
		mine = append(mine, f.seed(s).ID)
	}
	f.seed(order.StateCompleted)
	f.seed(order.StateCancelled)
	someoneElses := f.seed(order.StateAccepted)
	someoneElses.CourierID = ptr(f.stranger.UserID)
	f.repo.Put(someoneElses)

	got, err := f.svc.ListAssigned(f.ctx, f.courier)

	require.NoError(t, err)
	assert.ElementsMatch(t, mine, ids(got))
}

func TestListAssignmentHistoryIncludesReleasedErrands(t *testing.T) {
	// F3.5.3: completed, cancelled, and released by withdrawal.
	f := newFixture(t)

	released := f.post()
	_, err := f.svc.Accept(f.ctx, f.courier, released.ID, released.Version, "corr")
	require.NoError(t, err)
	f.clock.Advance(time.Minute)
	withdrawnAt := f.clock.Now()
	_, err = f.svc.Withdraw(f.ctx, f.courier, released.ID, "corr")
	require.NoError(t, err)

	completed := f.post()
	accepted, err := f.svc.Accept(f.ctx, f.courier, completed.ID, completed.Version, "corr")
	require.NoError(t, err)
	for _, step := range []func() (order.Order, error){
		func() (order.Order, error) { return f.svc.PickUp(f.ctx, f.courier, accepted.ID, "corr") },
		func() (order.Order, error) { return f.svc.Deliver(f.ctx, f.courier, accepted.ID, "corr") },
		func() (order.Order, error) { return f.svc.Confirm(f.ctx, f.requester, accepted.ID, "corr") },
	} {
		f.clock.Advance(time.Minute)
		_, err := step()
		require.NoError(t, err)
	}
	// AI-generated (edited by PENDING): D-093, both sides rate the completed
	// errand before the requester posts and the courier accepts the next one.
	_, err = f.svc.Rate(f.ctx, f.courier, accepted.ID, 4)
	require.NoError(t, err)
	_, err = f.svc.Rate(f.ctx, f.requester, accepted.ID, 5)
	require.NoError(t, err)

	cancelled := f.post()
	_, err = f.svc.Accept(f.ctx, f.courier, cancelled.ID, cancelled.Version, "corr")
	require.NoError(t, err)
	f.clock.Advance(time.Minute)
	_, err = f.svc.Cancel(f.ctx, f.admin, cancelled.ID, "corr")
	require.NoError(t, err)

	got, err := f.svc.ListAssignmentHistory(f.ctx, f.courier)

	require.NoError(t, err)
	outcomes := map[uuid.UUID]order.AssignmentOutcome{}
	for _, a := range got {
		outcomes[a.OrderID] = a.Outcome
		if a.OrderID == released.ID {
			assert.Equal(t, withdrawnAt, a.EndedAt)
			assert.Equal(t, released.ItemDescription, a.ItemDescription)
		}
	}
	assert.Equal(t, map[uuid.UUID]order.AssignmentOutcome{
		released.ID:  order.OutcomeReleased,
		completed.ID: order.OutcomeCompleted,
		cancelled.ID: order.OutcomeCancelled,
	}, outcomes)
	assert.Len(t, got, 3)

	none, err := f.svc.ListAssignmentHistory(f.ctx, f.stranger)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestListActiveIsForAdminsOnly(t *testing.T) {
	// D-050, D-053: every errand that is not final, for an ADMIN to resolve stalls.
	f := newFixture(t)
	var active []uuid.UUID
	for _, s := range order.ActiveStates() {
		active = append(active, f.seed(s).ID)
	}
	for _, s := range order.FinalStates() {
		f.seed(s)
	}

	for _, p := range []party{asRequester, asCourier, asStranger} {
		_, err := f.svc.ListActive(f.ctx, f.as(p))
		requireKind(t, err, order.KindForbidden)
	}

	got, err := f.svc.ListActive(f.ctx, f.admin)
	require.NoError(t, err)
	assert.ElementsMatch(t, active, ids(got))
}

func TestLaterSupplierChangesDoNotChangeErrands(t *testing.T) {
	// D-052: an errand keeps the copy of its supplier taken at create, so no
	// later change in supplier-service, deactivation and deletion included,
	// alters what a read returns, and no read asks supplier-service again.
	cases := []struct {
		name   string
		change func(f *fixture)
	}{
		{"renamed, moved and no longer taking errands", func(f *fixture) {
			s := f.supplier
			s.Name, s.Building, s.LocationDescription = "Toast Box", "University Town", "Beside the bus stop"
			s.Available = false
			f.suppliers.Put(s)
		}},
		{"deleted", func(f *fixture) { f.suppliers.FailWith(order.ErrSupplierNotFound) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			open := f.post()
			held := f.post()
			_, err := f.svc.Accept(f.ctx, f.courier, held.ID, held.Version, "corr")
			require.NoError(t, err)
			done := f.post()
			accepted, err := f.svc.Accept(f.ctx, f.courier, done.ID, done.Version, "corr") // K=2 allows both
			require.NoError(t, err)
			for _, step := range []func() (order.Order, error){
				func() (order.Order, error) { return f.svc.PickUp(f.ctx, f.courier, accepted.ID, "corr") },
				func() (order.Order, error) { return f.svc.Deliver(f.ctx, f.courier, accepted.ID, "corr") },
				func() (order.Order, error) { return f.svc.Confirm(f.ctx, f.requester, accepted.ID, "corr") },
			} {
				_, err := step()
				require.NoError(t, err)
			}
			want := order.SupplierSnapshot{ID: f.supplier.ID, Name: f.supplier.Name, Location: seededLocation}

			tc.change(f)
			lookups := f.suppliers.calls.Load()

			for _, id := range []uuid.UUID{open.ID, done.ID} {
				got, _, err := f.svc.Get(f.ctx, f.requester, id)
				require.NoError(t, err)
				assert.Equal(t, want, got.Supplier, "Get")
			}
			lists := []struct {
				name string
				list func() ([]order.Order, error)
				want []uuid.UUID
			}{
				{"ListOpen", func() ([]order.Order, error) { return f.svc.ListOpen(f.ctx, f.stranger) }, []uuid.UUID{open.ID}},
				{"ListRequested active", func() ([]order.Order, error) {
					return f.svc.ListRequested(f.ctx, f.requester, order.ScopeActive)
				}, []uuid.UUID{open.ID, held.ID}},
				{"ListRequested history", func() ([]order.Order, error) {
					return f.svc.ListRequested(f.ctx, f.requester, order.ScopeHistory)
				}, []uuid.UUID{done.ID}},
				{"ListAssigned", func() ([]order.Order, error) { return f.svc.ListAssigned(f.ctx, f.courier) }, []uuid.UUID{held.ID}},
				{"ListActive", func() ([]order.Order, error) { return f.svc.ListActive(f.ctx, f.admin) }, []uuid.UUID{open.ID, held.ID}},
			}
			for _, l := range lists {
				got, err := l.list()
				require.NoError(t, err, l.name)
				assert.ElementsMatch(t, l.want, ids(got), l.name)
				for _, o := range got {
					assert.Equal(t, want, o.Supplier, l.name)
				}
			}
			ended, err := f.svc.ListAssignmentHistory(f.ctx, f.courier)
			require.NoError(t, err)
			require.Len(t, ended, 1)
			assert.Equal(t, done.ID, ended[0].OrderID)
			assert.Equal(t, want, ended[0].Supplier, "ListAssignmentHistory")

			assert.Equal(t, lookups, f.suppliers.calls.Load(), "no read asks supplier-service")
		})
	}
}

func TestListsAskForTheContractsLimits(t *testing.T) {
	// D-053: each capped list asks for at most what api/openapi.yaml promises.
	// The wants are the contract's numbers written out, not order's constants,
	// so a constant that is zeroed or changed fails here.
	cases := []struct {
		name   string
		method string
		list   func(f *fixture) error
		want   int
	}{
		{"the open list", "ListOpen", func(f *fixture) error {
			_, err := f.svc.ListOpen(f.ctx, f.stranger)
			return err
		}, 100},
		{"the admin list", "ListActive", func(f *fixture) error {
			_, err := f.svc.ListActive(f.ctx, f.admin)
			return err
		}, 100},
		{"the requester's history", "ListByRequester", func(f *fixture) error {
			_, err := f.svc.ListRequested(f.ctx, f.requester, order.ScopeHistory)
			return err
		}, 50},
		{"the courier's history", "ListAssignmentHistory", func(f *fixture) error {
			_, err := f.svc.ListAssignmentHistory(f.ctx, f.courier)
			return err
		}, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)

			require.NoError(t, tc.list(f))

			assert.Equal(t, map[string]int{tc.method: tc.want}, f.repo.sentLimits())
		})
	}
}

func TestCountSupplierActive(t *testing.T) {
	// D-052: unfinished errands naming the supplier, for supplier-service's
	// delete check (F2.2.3).
	f := newFixture(t)
	for _, s := range allStates() {
		f.seed(s)
	}
	elsewhere := f.seed(order.StateOpen)
	elsewhere.Supplier.ID = f.otherSupplier.ID
	f.repo.Put(elsewhere)

	cases := []struct {
		name     string
		supplier uuid.UUID
		want     int
	}{
		{"the seeded errands' supplier", f.supplier.ID, len(order.ActiveStates())},
		{"another supplier", f.otherSupplier.ID, 1},
		{"a supplier no errand names", uuid.New(), 0},
	}
	for _, tc := range cases {
		n, err := f.svc.CountSupplierActive(f.ctx, f.admin, tc.supplier)
		require.NoError(t, err)
		assert.Equal(t, tc.want, n, tc.name)
	}

	// D-052: only an ADMIN may ask, since the check runs with the admin's token.
	for _, c := range []order.Caller{f.requester, f.courier, f.stranger} {
		_, err := f.svc.CountSupplierActive(f.ctx, c, f.supplier.ID)
		requireKind(t, err, order.KindForbidden)
	}
}

func TestNowIsTheServiceClock(t *testing.T) {
	// D-053: responses carry the server time.
	f := newFixture(t)
	f.clock.Advance(3 * time.Minute)
	assert.Equal(t, startTime().Add(3*time.Minute), f.svc.Now())
}

func TestReadsWhenStorageFails(t *testing.T) {
	// A storage failure is not a domain error: the transport answers 500 and
	// logs the cause.
	cases := []struct {
		name string
		read func(f *fixture, o order.Order) error
	}{
		{"an errand's history", func(f *fixture, o order.Order) error {
			_, _, err := f.svc.Get(f.ctx, f.requester, o.ID)
			return err
		}},
		{"the open list", func(f *fixture, _ order.Order) error {
			_, err := f.svc.ListOpen(f.ctx, f.stranger)
			return err
		}},
		{"the requester's list", func(f *fixture, _ order.Order) error {
			_, err := f.svc.ListRequested(f.ctx, f.requester, order.ScopeHistory)
			return err
		}},
		{"the courier's list", func(f *fixture, _ order.Order) error {
			_, err := f.svc.ListAssigned(f.ctx, f.courier)
			return err
		}},
		{"the courier's history", func(f *fixture, _ order.Order) error {
			_, err := f.svc.ListAssignmentHistory(f.ctx, f.courier)
			return err
		}},
		{"the admin list", func(f *fixture, _ order.Order) error {
			_, err := f.svc.ListActive(f.ctx, f.admin)
			return err
		}},
		{"the supplier count", func(f *fixture, _ order.Order) error {
			_, err := f.svc.CountSupplierActive(f.ctx, f.admin, f.supplier.ID)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(order.StateOpen)
			f.repo.failReadsWith(ordertest.ErrInjected)

			err := tc.read(f, o)

			require.Error(t, err)
			assert.Equal(t, order.Kind(0), order.KindOf(err))
			assert.ErrorIs(t, err, ordertest.ErrInjected)
		})
	}
}
