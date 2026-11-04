// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Unit tests for the admin dashboard on the order domain (D-104 to D-117):
//   what an admin's cancel or complete logs, that a refused or failed attempt is
//   logged and a student's is not, the admin errand list's filters, sorting,
//   paging and checks, a user's errand counts, the audit log's checks, and the
//   purge loop.
// Author review: PENDING — <reviewer to complete>

package order_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func TestAnAdminsCancelAndCompleteAreLoggedWithTheTransition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		from   order.State
		to     order.State
		action string
		run    func(f *fixture, id uuid.UUID) error
	}{
		{"cancel", order.StateAccepted, order.StateCancelled, order.AuditCancel, func(f *fixture, id uuid.UUID) error {
			_, err := f.svc.Cancel(f.ctx, f.admin, id, "req-1")
			return err
		}},
		{"complete", order.StatePickedUp, order.StateCompleted, order.AuditComplete, func(f *fixture, id uuid.UUID) error {
			_, err := f.svc.Complete(f.ctx, f.admin, id, "req-1")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.from)

			require.NoError(t, tc.run(f, o.ID))

			rows := f.repo.Audits()
			require.Len(t, rows, 1)
			e := rows[0]
			assert.Equal(t, tc.action, e.Action)
			assert.Equal(t, order.AuditTargetType, e.TargetType)
			assert.Equal(t, o.ID.String(), e.TargetID)
			assert.Equal(t, order.OutcomeSucceeded, e.Outcome)
			assert.Equal(t, f.admin.UserID.String(), e.ActorID)
			assert.Equal(t, "ADMIN", e.ActorRole)
			assert.Equal(t, "req-1", e.CorrelationID)
			assert.Equal(t, map[string]any{"state": string(tc.from)}, e.BeforeState)
			assert.Equal(t, map[string]any{"state": string(tc.to)}, e.AfterState)
			assert.Equal(t, tc.to, f.stored(o.ID).order.State, "the errand moved with its row")
		})
	}
}

func TestARequestersOwnCancelWritesNoAuditRow(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateOpen)

	_, err := f.svc.Cancel(f.ctx, f.requester, o.ID, "req-1")

	require.NoError(t, err)
	assert.Empty(t, f.repo.Audits(), "only an admin's actions are audited")
}

func TestARefusedAdminAttemptIsLoggedAndChangesNothing(t *testing.T) {
	cases := []struct {
		name       string
		seed       order.State
		run        func(f *fixture, id uuid.UUID) error
		action     string
		wantReason string
	}{
		{"complete an errand that is not picked up", order.StateOpen, func(f *fixture, id uuid.UUID) error {
			_, err := f.svc.Complete(f.ctx, f.admin, id, "req-2")
			return err
		}, order.AuditComplete, ""},
		{"cancel a finished errand", order.StateCompleted, func(f *fixture, id uuid.UUID) error {
			_, err := f.svc.Cancel(f.ctx, f.admin, id, "req-2")
			return err
		}, order.AuditCancel, ""},
		{"cancel an errand that does not exist", order.StateOpen, func(f *fixture, _ uuid.UUID) error {
			_, err := f.svc.Cancel(f.ctx, f.admin, uuid.New(), "req-2")
			return err
		}, order.AuditCancel, "errand not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(tc.seed)
			before := f.stored(o.ID)

			err := tc.run(f, o.ID)

			require.Error(t, err)
			f.requireUnchanged(before)
			rows := f.repo.Audits()
			require.Len(t, rows, 1, "the refusal is logged (D-109)")
			assert.Equal(t, order.OutcomeRefused, rows[0].Outcome)
			assert.Equal(t, tc.action, rows[0].Action)
			assert.NotEmpty(t, rows[0].RefusalReason)
			if tc.wantReason != "" {
				assert.Equal(t, tc.wantReason, rows[0].RefusalReason)
			}
			assert.Equal(t, f.admin.UserID.String(), rows[0].ActorID)
		})
	}
}

func TestAStudentsRefusedAttemptIsNotAnAdminAttempt(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateAccepted)

	_, err := f.svc.Cancel(f.ctx, f.stranger, o.ID, "req-3")
	_, err2 := f.svc.Complete(f.ctx, f.requester, o.ID, "req-3")

	require.Error(t, err)
	require.Error(t, err2)
	assert.Empty(t, f.repo.Audits())
}

func TestAFailedAdminWriteSavesNothingAndIsLoggedAsFailed(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateAccepted)
	before := f.stored(o.ID)
	f.repo.FailNext("Transition", errors.New("connection reset"))

	_, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "req-4")

	require.Error(t, err)
	f.requireUnchanged(before)
	rows := f.repo.Audits()
	require.Len(t, rows, 1, "no succeeded row came with the failed write (D-108)")
	assert.Equal(t, order.OutcomeFailed, rows[0].Outcome)
	assert.Contains(t, rows[0].RefusalReason, "connection reset")
}

func TestAFailedAttemptRowNeverHidesTheRealError(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateOpen)
	f.repo.FailNext("RecordAudit", errors.New("audit table gone"))

	_, err := f.svc.Complete(f.ctx, f.admin, o.ID, "req-5")

	require.Error(t, err)
	assert.Equal(t, order.KindConflict, order.KindOf(err), "the caller still gets the conflict")
	assert.Empty(t, f.repo.Audits())
}

// finalErrand stores a final errand for supplier s that reached its state at ago.
func (f *fixture) finalErrand(state order.State, s order.Supplier, requester uuid.UUID, courier *uuid.UUID, ago time.Duration) order.Order {
	f.t.Helper()
	o := f.seed(state)
	o.Supplier = order.SupplierSnapshot{ID: s.ID, Name: s.Name, Location: seededLocation}
	o.RequesterID = requester
	o.CourierID = courier
	o.StateChangedAt = f.clock.Now().Add(-ago)
	f.repo.Put(o)
	return o
}

func idsOf(orders []order.Order) []uuid.UUID {
	out := make([]uuid.UUID, len(orders))
	for i, o := range orders {
		out[i] = o.ID
	}
	return out
}

func TestAdminListFiltersSortsAndPagesPastErrands(t *testing.T) {
	f := newFixture(t)
	other := uuid.New()
	a := f.finalErrand(order.StateCompleted, f.supplier, f.requester.UserID, ptr(f.courier.UserID), 1*time.Hour) // Kaya, newest
	b := f.finalErrand(order.StateCancelled, f.otherSupplier, other, nil, 3*time.Hour)                           // Campus Coffee
	c := f.finalErrand(order.StateExpired, f.supplier, other, nil, 5*time.Hour)                                  // Kaya, oldest
	d := f.finalErrand(order.StateCompleted, f.otherSupplier, f.requester.UserID, ptr(other), 2*time.Hour)       // Campus Coffee
	live := f.seed(order.StateOpen)
	hist := func(mod func(*order.AdminErrandFilter)) order.AdminErrandPage {
		flt := order.AdminErrandFilter{Scope: order.ScopeHistory}
		mod(&flt)
		page, err := f.svc.ListAdminErrands(f.ctx, f.admin, flt)
		require.NoError(t, err)
		return page
	}

	t.Run("history holds only final errands, most recent first by default", func(t *testing.T) {
		page := hist(func(*order.AdminErrandFilter) {})
		assert.Equal(t, []uuid.UUID{a.ID, d.ID, b.ID, c.ID}, idsOf(page.Items))
		assert.Equal(t, 4, page.Total)
		assert.NotContains(t, idsOf(page.Items), live.ID)
		assert.Equal(t, order.MaxAdminPage, page.Limit)
	})
	t.Run("by state", func(t *testing.T) {
		page := hist(func(f *order.AdminErrandFilter) { f.State = ptr(order.StateCompleted) })
		assert.Equal(t, []uuid.UUID{a.ID, d.ID}, idsOf(page.Items))
	})
	t.Run("by supplier", func(t *testing.T) {
		page := hist(func(f2 *order.AdminErrandFilter) { f2.SupplierID = &f.otherSupplier.ID })
		assert.ElementsMatch(t, []uuid.UUID{b.ID, d.ID}, idsOf(page.Items))
	})
	t.Run("by date range, from inclusive and to exclusive", func(t *testing.T) {
		from, to := f.clock.Now().Add(-3*time.Hour), f.clock.Now().Add(-2*time.Hour)
		page := hist(func(f *order.AdminErrandFilter) { f.From, f.To = &from, &to })
		assert.Equal(t, []uuid.UUID{b.ID}, idsOf(page.Items), "3 hours ago is in, 2 hours ago is out")
	})
	t.Run("by user, as requester or as courier", func(t *testing.T) {
		page := hist(func(f *order.AdminErrandFilter) { f.UserID = &other })
		assert.ElementsMatch(t, []uuid.UUID{b.ID, c.ID, d.ID}, idsOf(page.Items))
	})
	t.Run("sorted by supplier name A to Z, then newest", func(t *testing.T) {
		page := hist(func(f *order.AdminErrandFilter) { f.Sort = order.SortSupplierName })
		assert.Equal(t, []uuid.UUID{d.ID, b.ID, a.ID, c.ID}, idsOf(page.Items))
	})
	t.Run("pages by offset and reports the total", func(t *testing.T) {
		first := hist(func(f *order.AdminErrandFilter) { f.Limit = 3 })
		second := hist(func(f *order.AdminErrandFilter) { f.Limit = 3; f.Offset = 3 })
		assert.Len(t, first.Items, 3)
		assert.Equal(t, []uuid.UUID{c.ID}, idsOf(second.Items))
		assert.Equal(t, 4, second.Total)
		assert.Equal(t, 3, second.Offset)
		beyond := hist(func(f *order.AdminErrandFilter) { f.Offset = 100 })
		assert.NotNil(t, beyond.Items)
		assert.Empty(t, beyond.Items)
	})
	t.Run("a limit over 50 is 50", func(t *testing.T) {
		assert.Equal(t, order.MaxAdminPage, hist(func(f *order.AdminErrandFilter) { f.Limit = 500 }).Limit)
	})
	t.Run("active lists the errands that are not final, oldest first by default", func(t *testing.T) {
		old := f.seed(order.StateAccepted)
		old.StateChangedAt = f.clock.Now().Add(-10 * time.Hour)
		f.repo.Put(old)
		page, err := f.svc.ListAdminErrands(f.ctx, f.admin, order.AdminErrandFilter{Scope: order.ScopeActive})
		require.NoError(t, err)
		assert.Equal(t, old.ID, page.Items[0].ID)
		assert.NotContains(t, idsOf(page.Items), a.ID)
	})
}

func TestAdminListRefusesWhatItCannotMean(t *testing.T) {
	f := newFixture(t)
	now := f.clock.Now()
	earlier := now.Add(-time.Hour)
	for name, flt := range map[string]order.AdminErrandFilter{
		"negative offset":              {Scope: order.ScopeHistory, Offset: -1},
		"an unknown scope":             {Scope: "all"},
		"history of a state not final": {Scope: order.ScopeHistory, State: ptr(order.StateOpen)},
		"active of a final state":      {Scope: order.ScopeActive, State: ptr(order.StateCompleted)},
		"an unknown sort":              {Scope: order.ScopeHistory, Sort: "random"},
		"from not before to":           {Scope: order.ScopeHistory, From: &now, To: &earlier},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.ListAdminErrands(f.ctx, f.admin, flt)
			assert.Equal(t, order.KindValidation, order.KindOf(err))
		})
	}
	for _, c := range []order.Caller{f.requester, f.stranger} {
		_, err := f.svc.ListAdminErrands(f.ctx, c, order.AdminErrandFilter{Scope: order.ScopeHistory})
		assert.Equal(t, order.KindForbidden, order.KindOf(err), "a student cannot list every errand")
	}
}

func TestErrandCountsAreAdminOnlyAndCountByRole(t *testing.T) {
	f := newFixture(t)
	user := uuid.New()
	f.finalErrand(order.StateCompleted, f.supplier, user, ptr(f.courier.UserID), time.Hour)
	f.finalErrand(order.StateCancelled, f.supplier, user, nil, time.Hour)
	f.finalErrand(order.StateCancelled, f.supplier, user, nil, 2*time.Hour)
	f.finalErrand(order.StateCompleted, f.supplier, f.requester.UserID, ptr(user), time.Hour)

	counts, err := f.svc.ErrandCounts(f.ctx, f.admin, user)

	require.NoError(t, err)
	assert.Equal(t, map[order.State]int{order.StateCompleted: 1, order.StateCancelled: 2}, counts.Requested)
	assert.Equal(t, map[order.State]int{order.StateCompleted: 1}, counts.Assigned)
	_, err = f.svc.ErrandCounts(f.ctx, f.requester, user)
	assert.Equal(t, order.KindForbidden, order.KindOf(err))
}

func TestAuditLogIsAdminOnlyChecksItsFilterAndPagesAtFifty(t *testing.T) {
	f := newFixture(t)

	page, err := f.svc.AuditLog(f.ctx, f.admin, order.AuditFilter{Limit: 500})
	require.NoError(t, err)
	assert.Equal(t, order.MaxAdminPage, page.Limit)
	_, err = f.svc.AuditLog(f.ctx, f.admin, order.AuditFilter{Offset: -1})
	assert.Equal(t, order.KindValidation, order.KindOf(err))
	_, err = f.svc.AuditLog(f.ctx, f.admin, order.AuditFilter{Outcome: "maybe"})
	assert.Equal(t, order.KindValidation, order.KindOf(err))
	_, err = f.svc.AuditLog(f.ctx, f.stranger, order.AuditFilter{})
	assert.Equal(t, order.KindForbidden, order.KindOf(err))
}

func TestPurgeRemovesOnlyRowsOlderThanSixMonthsAndStopsWhenCancelled(t *testing.T) {
	f := newFixture(t)
	old := time.Now().UTC().AddDate(0, -7, 0)
	require.NoError(t, f.repo.RecordAudit(f.ctx, &order.AuditEntry{OccurredAt: old, ActorID: f.admin.UserID.String(), Action: order.AuditCancel}))
	require.NoError(t, f.repo.RecordAudit(f.ctx, &order.AuditEntry{OccurredAt: time.Now().UTC(), ActorID: f.admin.UserID.String(), Action: order.AuditCancel}))
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})

	go func() {
		f.svc.RunAuditPurge(ctx, 5*time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool { return len(f.repo.Audits()) == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.WithinDuration(t, time.Now(), f.repo.Audits()[0].OccurredAt, time.Minute, "the recent row is the one that stayed")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop when its context was cancelled")
	}
}
