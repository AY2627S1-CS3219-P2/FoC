//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Integration tests of the admin audit log over real PostgreSQL (D-106 to
//   D-115): an admin write and its audit row are saved together or not at all,
//   a refused attempt still leaves a row, the table refuses UPDATE and an early
//   DELETE, the purge removes only rows older than 6 months, and the read
//   filters and pages.
// Author review: PENDING — <reviewer to complete>

package repository_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/repository"
	"foc/supplier-service/internal/supplier"
)

var auditAdmin = supplier.AuditContext{ActorID: uuid.NewString(), ActorRole: "ADMIN", CorrelationID: "req-int"}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func TestAdminWritesAndTheirAuditRowsShareOneTransaction(t *testing.T) {
	t.Parallel()
	pool := openDatabase(t)
	repo := repository.NewPostgresRepository(pool)
	svc := supplier.NewService(repo, noErrands{}, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	t.Run("create, edit and delete each leave one succeeded row", func(t *testing.T) {
		created, err := svc.AdminCreate(ctx, auditAdmin, stall("Audited Stall"))
		require.NoError(t, err)
		edit := stall("Audited Stall")
		edit.OpeningTime = "08:00"
		_, err = svc.AdminUpdate(ctx, auditAdmin, created.ID, edit)
		require.NoError(t, err)
		require.NoError(t, svc.AdminDelete(ctx, auditAdmin, created.ID))

		page, err := svc.AuditLog(ctx, supplier.AuditFilter{TargetID: created.ID})
		require.NoError(t, err)
		require.Equal(t, 3, page.Total)
		// Newest first: delete, update, create.
		assert.Equal(t, []string{supplier.ActionDelete, supplier.ActionUpdate, supplier.ActionCreate},
			[]string{page.Items[0].Action, page.Items[1].Action, page.Items[2].Action})
		for _, e := range page.Items {
			assert.Equal(t, supplier.OutcomeSucceeded, e.Outcome)
			assert.Equal(t, auditAdmin.ActorID, e.ActorID)
			assert.Equal(t, "req-int", e.CorrelationID)
		}
		assert.Equal(t, map[string]any{"opening_time": "08:00"}, page.Items[1].AdditionalComments,
			"an edit logs the new value of the field it changed")
	})

	t.Run("a seed create writes no audit row", func(t *testing.T) {
		before := countRows(t, pool, "admin_audit")
		_, err := svc.Create(ctx, stall("Seeded Stall"))
		require.NoError(t, err)
		assert.Equal(t, before, countRows(t, pool, "admin_audit"))
	})

	t.Run("a refused create is logged and saves no supplier", func(t *testing.T) {
		_, err := svc.AdminCreate(ctx, auditAdmin, stall("Dup Stall"))
		require.NoError(t, err)
		suppliers := countRows(t, pool, "suppliers")

		_, err = svc.AdminCreate(ctx, auditAdmin, stall("Dup Stall"))

		require.ErrorIs(t, err, supplier.ErrDuplicate)
		assert.Equal(t, suppliers, countRows(t, pool, "suppliers"))
		page, err := svc.AuditLog(ctx, supplier.AuditFilter{Outcome: supplier.OutcomeRefused, Action: supplier.ActionCreate})
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)
		assert.Equal(t, supplier.ErrDuplicate.Error(), page.Items[0].RefusalReason)
		assert.Equal(t, "Dup Stall", page.Items[0].AdditionalComments["name"], "the attempted values are kept")
	})

	t.Run("if the audit row cannot be written the supplier is not saved either", func(t *testing.T) {
		_, err := pool.Exec(ctx, `ALTER TABLE admin_audit RENAME TO admin_audit_away`)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `ALTER TABLE IF EXISTS admin_audit_away RENAME TO admin_audit`) })
		suppliers := countRows(t, pool, "suppliers")

		_, err = svc.AdminCreate(ctx, auditAdmin, stall("Never Saved"))

		require.Error(t, err)
		assert.Equal(t, suppliers, countRows(t, pool, "suppliers"), "D-108: both writes or neither")
		_, err = pool.Exec(ctx, `ALTER TABLE admin_audit_away RENAME TO admin_audit`)
		require.NoError(t, err)
	})

	t.Run("a delete of a supplier that is already gone is refused and logged", func(t *testing.T) {
		missing := uuid.NewString()

		err := svc.AdminDelete(ctx, auditAdmin, missing)

		require.ErrorIs(t, err, supplier.ErrNotFound)
		page, err := svc.AuditLog(ctx, supplier.AuditFilter{TargetID: missing})
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)
		assert.Equal(t, supplier.OutcomeRefused, page.Items[0].Outcome)
	})
}

func TestAuditRowsAreImmutableAndKeptSixMonths(t *testing.T) {
	t.Parallel()
	pool := openDatabase(t)
	repo := repository.NewPostgresRepository(pool)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, -7, 0)
	recent := time.Now().UTC().Add(-24 * time.Hour)
	for _, at := range []time.Time{old, recent} {
		require.NoError(t, repo.RecordAudit(ctx, &supplier.AuditEntry{
			OccurredAt: at, ActorID: auditAdmin.ActorID, ActorRole: "ADMIN", Action: supplier.ActionCreate,
			TargetType: supplier.TargetType, TargetID: at.Format("20060102"), Outcome: supplier.OutcomeSucceeded,
		}))
	}

	t.Run("no row can be updated, old or new", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE admin_audit SET outcome = 'failed'`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "immutable")
	})
	t.Run("a row under 6 months old cannot be deleted", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM admin_audit WHERE occurred_at = $1`, recent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "6 months")
		assert.Equal(t, 2, countRows(t, pool, "admin_audit"))
	})
	t.Run("the purge removes the old row and keeps the recent one", func(t *testing.T) {
		n, err := repo.PurgeAudit(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 1, n)
		assert.Equal(t, 1, countRows(t, pool, "admin_audit"))
		again, err := repo.PurgeAudit(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 0, again, "a second run has nothing to remove")
	})
}

func TestAuditLogFiltersAndPages(t *testing.T) {
	t.Parallel()
	pool := openDatabase(t)
	repo := repository.NewPostgresRepository(pool)
	ctx := context.Background()
	other := uuid.NewString()
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 7; i++ {
		actor, outcome := auditAdmin.ActorID, supplier.OutcomeSucceeded
		if i%2 == 1 {
			actor, outcome = other, supplier.OutcomeRefused
		}
		require.NoError(t, repo.RecordAudit(ctx, &supplier.AuditEntry{
			OccurredAt: base.Add(time.Duration(i) * time.Minute), ActorID: actor, ActorRole: "ADMIN",
			Action: supplier.ActionUpdate, TargetType: supplier.TargetType, TargetID: fmt.Sprintf("t-%d", i),
			Outcome: outcome, AdditionalComments: map[string]any{"i": i},
		}))
	}

	t.Run("newest first, paged with a total", func(t *testing.T) {
		first, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 3})
		require.NoError(t, err)
		assert.Equal(t, 7, first.Total)
		require.Len(t, first.Items, 3)
		assert.Equal(t, "t-6", first.Items[0].TargetID)
		second, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 3, Offset: 3})
		require.NoError(t, err)
		assert.Equal(t, []string{"t-3", "t-2", "t-1"}, []string{second.Items[0].TargetID, second.Items[1].TargetID, second.Items[2].TargetID})
		last, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 3, Offset: 6})
		require.NoError(t, err)
		assert.Len(t, last.Items, 1)
		beyond, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 3, Offset: 70})
		require.NoError(t, err)
		assert.Empty(t, beyond.Items)
		assert.NotNil(t, beyond.Items, "an empty page is an empty list, not null")
	})
	t.Run("filters combine", func(t *testing.T) {
		byActor, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 50, ActorID: other})
		require.NoError(t, err)
		assert.Equal(t, 3, byActor.Total)
		refused, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 50, Outcome: supplier.OutcomeRefused, ActorID: auditAdmin.ActorID})
		require.NoError(t, err)
		assert.Equal(t, 0, refused.Total)
		target, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 50, TargetID: "T-2"})
		require.NoError(t, err)
		assert.Equal(t, 1, target.Total, "a target id matches whatever its case")
		from, to := base.Add(2*time.Minute), base.Add(5*time.Minute)
		window, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 50, From: &from, To: &to})
		require.NoError(t, err)
		assert.Equal(t, 3, window.Total, "from is inclusive and to is exclusive")
		bad, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 50, ActorID: "not-a-uuid"})
		require.NoError(t, err, "a malformed id finds nothing instead of failing")
		assert.Zero(t, bad.Total)
	})
	t.Run("the JSON details round-trip", func(t *testing.T) {
		page, err := repo.ListAudit(ctx, supplier.AuditFilter{Limit: 1})
		require.NoError(t, err)
		assert.EqualValues(t, 6, page.Items[0].AdditionalComments["i"])
		assert.Nil(t, page.Items[0].BeforeState)
	})
}
