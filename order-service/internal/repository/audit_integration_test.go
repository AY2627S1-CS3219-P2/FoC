//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Integration tests over real PostgreSQL for the admin dashboard on
//   order-service (D-104 to D-117): an admin's transition and its audit row
//   share one transaction, the audit table refuses UPDATE and an early DELETE,
//   the purge removes only rows older than 6 months, the audit read filters and
//   pages, the admin errand list filters, sorts and pages, and a user's counts.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func adminCancel(o order.Order, admin uuid.UUID, now time.Time) order.Transition {
	tr := move(o, order.ActionAdminCancel, order.StateCancelled, now)
	tr.History.ActorID, tr.History.ActorKind = ptr(admin), order.ActorAdmin
	tr.Audit = &order.AuditEntry{
		OccurredAt: now, ActorID: admin.String(), ActorRole: "ADMIN", Action: order.AuditCancel,
		TargetType: order.AuditTargetType, TargetID: o.ID.String(), Outcome: order.OutcomeSucceeded,
		BeforeState: map[string]any{"state": string(o.State)}, AfterState: map[string]any{"state": string(order.StateCancelled)},
		CorrelationID: "corr-admin",
	}
	return tr
}

func (e *env) auditRows(t *testing.T) int { return e.count(t, `SELECT count(*) FROM admin_audit`) }

func (e *env) testAdminAudit(t *testing.T) {
	requester, admin := uuid.New(), uuid.New()
	now := at(time.Hour)

	t.Run("an admin's transition saves its audit row in the same transaction", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)

		got, err := e.repo.Transition(t.Context(), adminCancel(o, admin, now))

		require.NoError(t, err)
		assert.Equal(t, order.StateCancelled, got.State)
		page, err := e.repo.ListAudit(t.Context(), order.AuditFilter{TargetID: o.ID.String(), Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)
		row := page.Items[0]
		assert.Equal(t, order.AuditCancel, row.Action)
		assert.Equal(t, admin.String(), row.ActorID)
		assert.Equal(t, "OPEN", row.BeforeState["state"])
		assert.Equal(t, "CANCELLED", row.AfterState["state"])
		assert.Equal(t, "corr-admin", row.CorrelationID)
	})
	t.Run("a transition that matches no row leaves no audit row", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		tr := adminCancel(o, admin, now)
		tr.FromState = order.StateAccepted // the row is OPEN, so nothing matches
		tr.Audit.BeforeState = map[string]any{"state": "ACCEPTED"}

		_, err := e.repo.Transition(t.Context(), tr)

		require.ErrorIs(t, err, order.ErrNoMatch)
		assert.Equal(t, 0, e.auditRows(t))
		e.requireUntouched(t, o)
	})
	t.Run("if the audit row cannot be written the transition is rolled back", func(t *testing.T) {
		e.reset(t)
		o := openErrand(requester, at(0))
		e.insert(t, o)
		_, err := e.pool.Exec(t.Context(), `ALTER TABLE admin_audit RENAME TO admin_audit_away`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = e.pool.Exec(t.Context(), `ALTER TABLE IF EXISTS admin_audit_away RENAME TO admin_audit`)
		})

		_, err = e.repo.Transition(t.Context(), adminCancel(o, admin, now))

		require.Error(t, err)
		e.requireUntouched(t, o)
		assert.Equal(t, 0, e.count(t, `SELECT count(*) FROM order_history WHERE order_id = $1`, o.ID), "no history row either (D-108)")
		_, err = e.pool.Exec(t.Context(), `ALTER TABLE admin_audit_away RENAME TO admin_audit`)
		require.NoError(t, err)
	})
	t.Run("a refused attempt is recorded on its own", func(t *testing.T) {
		e.reset(t)
		require.NoError(t, e.repo.RecordAudit(t.Context(), &order.AuditEntry{
			OccurredAt: now, ActorID: admin.String(), ActorRole: "ADMIN", Action: order.AuditComplete,
			TargetType: order.AuditTargetType, TargetID: "x", Outcome: order.OutcomeRefused, RefusalReason: "this errand is not picked up",
		}))
		assert.Equal(t, 1, e.auditRows(t))
	})
}

func (e *env) testAdminAuditImmutableAndPurged(t *testing.T) {
	e.reset(t)
	old := time.Now().UTC().AddDate(0, -7, 0)
	recent := time.Now().UTC().Add(-24 * time.Hour)
	for _, when := range []time.Time{old, recent} {
		require.NoError(t, e.repo.RecordAudit(t.Context(), &order.AuditEntry{
			OccurredAt: when, ActorID: uuid.NewString(), ActorRole: "ADMIN", Action: order.AuditCancel,
			TargetType: order.AuditTargetType, TargetID: when.Format("20060102"), Outcome: order.OutcomeSucceeded,
		}))
	}

	_, err := e.pool.Exec(t.Context(), `UPDATE admin_audit SET outcome = 'failed'`)
	assert.Error(t, err, "no row may be updated")
	_, err = e.pool.Exec(t.Context(), `DELETE FROM admin_audit WHERE occurred_at = $1`, recent)
	assert.Error(t, err, "a row under 6 months old may not be deleted")

	n, err := e.repo.PurgeAudit(t.Context())
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "exactly the old row")
	assert.Equal(t, 1, e.auditRows(t))
	again, err := e.repo.PurgeAudit(t.Context())
	require.NoError(t, err)
	assert.EqualValues(t, 0, again)
}

func (e *env) testAuditLogFiltersAndPages(t *testing.T) {
	e.reset(t)
	mine, other := uuid.NewString(), uuid.NewString()
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 7; i++ {
		entry := &order.AuditEntry{
			OccurredAt: base.Add(time.Duration(i) * time.Minute), ActorID: mine, ActorRole: "ADMIN", Action: order.AuditCancel,
			TargetType: order.AuditTargetType, TargetID: fmt.Sprintf("t-%d", i), Outcome: order.OutcomeSucceeded,
			AdditionalComments: map[string]any{"i": i},
		}
		if i%2 == 1 {
			entry.ActorID, entry.Outcome = other, order.OutcomeRefused
		}
		require.NoError(t, e.repo.RecordAudit(t.Context(), entry))
	}
	list := func(f order.AuditFilter) *order.AuditPage {
		f.Limit = max(f.Limit, 50)
		page, err := e.repo.ListAudit(t.Context(), f)
		require.NoError(t, err)
		return page
	}

	first, err := e.repo.ListAudit(t.Context(), order.AuditFilter{Limit: 3})
	require.NoError(t, err)
	assert.Equal(t, 7, first.Total)
	assert.Equal(t, "t-6", first.Items[0].TargetID, "newest first")
	second, err := e.repo.ListAudit(t.Context(), order.AuditFilter{Limit: 3, Offset: 3})
	require.NoError(t, err)
	assert.Equal(t, "t-3", second.Items[0].TargetID)
	beyond, err := e.repo.ListAudit(t.Context(), order.AuditFilter{Limit: 3, Offset: 70})
	require.NoError(t, err)
	assert.NotNil(t, beyond.Items)
	assert.Empty(t, beyond.Items)
	assert.Equal(t, 3, list(order.AuditFilter{ActorID: other}).Total)
	assert.Equal(t, 0, list(order.AuditFilter{ActorID: mine, Outcome: order.OutcomeRefused}).Total)
	assert.Equal(t, 1, list(order.AuditFilter{TargetID: "T-2"}).Total, "a target id matches whatever its case")
	from, to := base.Add(2*time.Minute), base.Add(5*time.Minute)
	assert.Equal(t, 3, list(order.AuditFilter{From: &from, To: &to}).Total, "from is inclusive and to is exclusive")
	assert.Equal(t, 0, list(order.AuditFilter{ActorID: "not-a-uuid"}).Total, "a malformed id finds nothing instead of failing")
	assert.EqualValues(t, 6, list(order.AuditFilter{Limit: 1}).Items[0].AdditionalComments["i"], "the JSON details round-trip")
}

func (e *env) testListAdminErrands(t *testing.T) {
	e.reset(t)
	requester, courier, other := uuid.New(), uuid.New(), uuid.New()
	named := func(o order.Order, supplier uuid.UUID, name string) order.Order {
		o.Supplier.ID, o.Supplier.Name = supplier, name
		return o
	}
	kaya, coffee := uuid.New(), uuid.New()
	a := endedAs(named(openErrand(requester, at(0)), kaya, "Kaya Toast"), order.StateCompleted, at(5*time.Hour))
	a.CourierID = ptr(courier)
	b := endedAs(named(openErrand(other, at(0)), coffee, "campus coffee"), order.StateCancelled, at(3*time.Hour))
	c := endedAs(named(openErrand(other, at(0)), kaya, "Kaya Toast"), order.StateExpired, at(1*time.Hour))
	d := endedAs(named(openErrand(requester, at(0)), coffee, "campus coffee"), order.StateCompleted, at(4*time.Hour))
	d.CourierID = ptr(other)
	live := named(openErrand(requester, at(0)), kaya, "Kaya Toast")
	e.insert(t, a, b, c, d, live)

	ids := func(p order.AdminErrandPage) []uuid.UUID {
		out := make([]uuid.UUID, len(p.Items))
		for i, o := range p.Items {
			out[i] = o.ID
		}
		return out
	}
	list := func(f order.AdminErrandFilter) order.AdminErrandPage {
		if f.Limit == 0 {
			f.Limit = 50
		}
		if f.Sort == "" {
			f.Sort = order.SortRecent
		}
		p, err := e.repo.ListAdminErrands(t.Context(), f)
		require.NoError(t, err)
		return p
	}

	assert.Equal(t, []uuid.UUID{a.ID, d.ID, b.ID, c.ID}, ids(list(order.AdminErrandFilter{Scope: order.ScopeHistory})), "final errands only, most recent first")
	assert.Equal(t, []uuid.UUID{a.ID, d.ID}, ids(list(order.AdminErrandFilter{Scope: order.ScopeHistory, State: ptr(order.StateCompleted)})))
	assert.ElementsMatch(t, []uuid.UUID{b.ID, d.ID}, ids(list(order.AdminErrandFilter{Scope: order.ScopeHistory, SupplierID: &coffee})))
	assert.ElementsMatch(t, []uuid.UUID{b.ID, c.ID, d.ID}, ids(list(order.AdminErrandFilter{Scope: order.ScopeHistory, UserID: &other})), "requester or courier")
	from, to := at(3*time.Hour), at(5*time.Hour)
	assert.Equal(t, []uuid.UUID{d.ID, b.ID}, ids(list(order.AdminErrandFilter{Scope: order.ScopeHistory, From: &from, To: &to})), "from inclusive, to exclusive")
	byName := ids(list(order.AdminErrandFilter{Scope: order.ScopeHistory, Sort: order.SortSupplierName}))
	assert.Equal(t, []uuid.UUID{d.ID, b.ID, a.ID, c.ID}, byName, "A to Z ignoring case, then newest")
	assert.Equal(t, []uuid.UUID{live.ID}, ids(list(order.AdminErrandFilter{Scope: order.ScopeActive})))

	page := list(order.AdminErrandFilter{Scope: order.ScopeHistory, Limit: 3})
	assert.Equal(t, 4, page.Total)
	assert.Len(t, page.Items, 3)
	last := list(order.AdminErrandFilter{Scope: order.ScopeHistory, Limit: 3, Offset: 3})
	assert.Equal(t, []uuid.UUID{c.ID}, ids(last))
	beyond := list(order.AdminErrandFilter{Scope: order.ScopeHistory, Offset: 100})
	assert.NotNil(t, beyond.Items)
	assert.Empty(t, beyond.Items)
	assert.Equal(t, 4, beyond.Total, "the total ignores paging")
}

func (e *env) testCountByUser(t *testing.T) {
	e.reset(t)
	user, courier := uuid.New(), uuid.New()
	e.insert(t,
		endedAs(openErrand(user, at(0)), order.StateCancelled, at(time.Hour)),
		endedAs(openErrand(user, at(0)), order.StateCancelled, at(time.Hour)),
		func() order.Order {
			o := endedAs(openErrand(user, at(0)), order.StateCompleted, at(time.Hour))
			o.CourierID = ptr(courier)
			return o
		}(),
		func() order.Order {
			o := endedAs(openErrand(courier, at(0)), order.StateCompleted, at(time.Hour))
			o.CourierID = ptr(user)
			return o
		}(),
		openErrand(uuid.New(), at(0)),
	)

	counts, err := e.repo.CountByUser(t.Context(), user)

	require.NoError(t, err)
	assert.Equal(t, map[order.State]int{order.StateCancelled: 2, order.StateCompleted: 1}, counts.Requested)
	assert.Equal(t, map[order.State]int{order.StateCompleted: 1}, counts.Assigned)
	none, err := e.repo.CountByUser(t.Context(), uuid.New())
	require.NoError(t, err)
	assert.Empty(t, none.Requested)
	assert.Empty(t, none.Assigned)
}
