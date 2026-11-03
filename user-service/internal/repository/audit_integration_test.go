//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Integration tests over real PostgreSQL for the admin dashboard
//   (D-106 to D-123): an admin write and its audit row share one transaction, a
//   refused attempt still leaves a row, the table refuses UPDATE and an early
//   DELETE, the purge removes only rows older than 6 months, the audit read
//   filters and pages, and the account search matches anywhere, literally.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var integrationAdmin = user.AuditContext{ActorID: uuid.NewString(), ActorRole: "ADMIN", CorrelationID: "req-int"}

func auditCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM admin_audit").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func statusOf(t *testing.T, ctx context.Context, repo *PostgresRepository, uid uuid.UUID) user.AccountStatus {
	t.Helper()
	got, err := repo.GetByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return got.AccountStatus
}

func TestAdminChangesAndTheirAuditRowsShareOneTransaction(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)

	t.Run("a suspension is saved with a row that records the status before and after", func(t *testing.T) {
		account := createIntegrationAccount(t, ctx, repo, "suspend", user.AccountRoleStudent)
		e := integrationAdmin.EntryForTest(user.ActionSuspend, account.UID.String())

		if err := repo.UpdateAccountStatusAudited(ctx, account.UID, user.AccountStatusSuspended, time.Now().UTC().Truncate(time.Second), e); err != nil {
			t.Fatal(err)
		}

		if got := statusOf(t, ctx, repo, account.UID); got != user.AccountStatusSuspended {
			t.Fatalf("status = %s", got)
		}
		page, err := repo.ListAudit(ctx, user.AuditFilter{TargetID: account.UID.String(), Limit: 10})
		if err != nil || page.Total != 1 {
			t.Fatalf("rows = %+v, %v", page, err)
		}
		row := page.Items[0]
		if row.BeforeState["account_status"] != "ACTIVE" || row.AfterState["account_status"] != "SUSPENDED" || row.Outcome != user.OutcomeSucceeded {
			t.Errorf("row = %+v", row)
		}
		if row.ActorID != integrationAdmin.ActorID || row.CorrelationID != "req-int" {
			t.Errorf("attribution = %+v", row)
		}
	})

	t.Run("if the audit row cannot be written the status is not saved either", func(t *testing.T) {
		account := createIntegrationAccount(t, ctx, repo, "atomic", user.AccountRoleStudent)
		if _, err := pool.Exec(ctx, `ALTER TABLE admin_audit RENAME TO admin_audit_away`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `ALTER TABLE IF EXISTS admin_audit_away RENAME TO admin_audit`) })

		err := repo.UpdateAccountStatusAudited(ctx, account.UID, user.AccountStatusSuspended, time.Now().UTC(), integrationAdmin.EntryForTest(user.ActionSuspend, account.UID.String()))

		if err == nil {
			t.Fatal("expected an error")
		}
		if got := statusOf(t, ctx, repo, account.UID); got != user.AccountStatusActive {
			t.Fatalf("status = %s: D-108 means both writes or neither", got)
		}
		if _, err := pool.Exec(ctx, `ALTER TABLE admin_audit_away RENAME TO admin_audit`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("an admin edit saves the account and every row together, or none", func(t *testing.T) {
		a := createIntegrationAccount(t, ctx, repo, "edit-a", user.AccountRoleStudent)
		b := createIntegrationAccount(t, ctx, repo, "edit-b", user.AccountRoleStudent)
		before := auditCount(t, ctx, pool)

		a.Username = "renamed"
		reset := integrationAdmin.EntryForTest(user.ActionPasswordReset, a.UID.String())
		update := integrationAdmin.EntryForTest(user.ActionUpdate, a.UID.String())
		update.AdditionalComments = map[string]any{"username": "renamed"}
		if err := repo.UpdateAsAdminAudited(ctx, a, []*user.AuditEntry{reset, update}); err != nil {
			t.Fatal(err)
		}
		if got := auditCount(t, ctx, pool); got != before+2 {
			t.Fatalf("rows = %d, want %d", got, before+2)
		}

		// A taken username is refused, and the audit rows roll back with it.
		b.Username = "renamed"
		err := repo.UpdateAsAdminAudited(ctx, b, []*user.AuditEntry{integrationAdmin.EntryForTest(user.ActionUpdate, b.UID.String())})
		if !errors.Is(err, user.ErrDuplicateUsername) {
			t.Fatalf("error = %v", err)
		}
		if got := auditCount(t, ctx, pool); got != before+2 {
			t.Fatalf("rows = %d: the refused edit must leave no row of its own", got)
		}
	})

	t.Run("a refused attempt is recorded on its own", func(t *testing.T) {
		svc := user.NewAccountService(repo, &recordingSuspensionWriter{}, &recordingSessionRepository{}, time.Minute)
		admin := createIntegrationAccount(t, ctx, repo, "other-admin", user.AccountRoleAdmin)
		before := auditCount(t, ctx, pool)

		err := svc.AdminUpdateAccountStatus(ctx, integrationAdmin, admin.UID, user.AccountStatusSuspended)

		if !errors.Is(err, user.ErrCannotSuspendAdmin) {
			t.Fatalf("error = %v", err)
		}
		if got := statusOf(t, ctx, repo, admin.UID); got != user.AccountStatusActive {
			t.Fatalf("an admin was suspended: %s", got)
		}
		page, err := repo.ListAudit(ctx, user.AuditFilter{TargetID: admin.UID.String(), Limit: 10})
		if err != nil || page.Total != 1 || auditCount(t, ctx, pool) != before+1 {
			t.Fatalf("rows = %+v, %v", page, err)
		}
		if page.Items[0].Outcome != user.OutcomeRefused || page.Items[0].RefusalReason != user.ErrCannotSuspendAdmin.Error() {
			t.Errorf("row = %+v", page.Items[0])
		}
	})
}

func TestAuditRowsAreImmutableAndKeptSixMonths(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	old := time.Now().UTC().AddDate(0, -7, 0)
	recent := time.Now().UTC().Add(-24 * time.Hour)
	for _, at := range []time.Time{old, recent} {
		e := integrationAdmin.EntryForTest(user.ActionSuspend, at.Format("20060102"))
		e.OccurredAt = at
		if err := repo.RecordAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE admin_audit SET outcome = 'failed'`); err == nil {
		t.Error("no row may be updated")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM admin_audit WHERE occurred_at = $1`, recent); err == nil {
		t.Error("a row under 6 months old may not be deleted")
	}
	n, err := repo.PurgeAudit(ctx)
	if err != nil || n != 1 {
		t.Fatalf("purged %d, %v: want exactly the old row", n, err)
	}
	if got := auditCount(t, ctx, pool); got != 1 {
		t.Fatalf("rows left = %d", got)
	}
	if again, _ := repo.PurgeAudit(ctx); again != 0 {
		t.Errorf("a second run removed %d", again)
	}
}

func TestAuditLogFiltersAndPages(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	other := uuid.NewString()
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 7; i++ {
		e := integrationAdmin.EntryForTest(user.ActionSuspend, fmt.Sprintf("t-%d", i))
		e.OccurredAt = base.Add(time.Duration(i) * time.Minute)
		if i%2 == 1 {
			e.ActorID, e.Outcome = other, user.OutcomeRefused
		}
		if err := repo.RecordAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	first, err := repo.ListAudit(ctx, user.AuditFilter{Limit: 3})
	if err != nil || first.Total != 7 || len(first.Items) != 3 || first.Items[0].TargetID != "t-6" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, _ := repo.ListAudit(ctx, user.AuditFilter{Limit: 3, Offset: 3})
	if second.Items[0].TargetID != "t-3" {
		t.Errorf("second page starts at %s", second.Items[0].TargetID)
	}
	beyond, _ := repo.ListAudit(ctx, user.AuditFilter{Limit: 3, Offset: 70})
	if beyond.Items == nil || len(beyond.Items) != 0 {
		t.Errorf("an empty page is an empty list, got %#v", beyond.Items)
	}
	if byActor, _ := repo.ListAudit(ctx, user.AuditFilter{Limit: 50, ActorID: other}); byActor.Total != 3 {
		t.Errorf("by actor = %d", byActor.Total)
	}
	if mixed, _ := repo.ListAudit(ctx, user.AuditFilter{Limit: 50, ActorID: integrationAdmin.ActorID, Outcome: user.OutcomeRefused}); mixed.Total != 0 {
		t.Errorf("combined filters = %d", mixed.Total)
	}
	from, to := base.Add(2*time.Minute), base.Add(5*time.Minute)
	if window, _ := repo.ListAudit(ctx, user.AuditFilter{Limit: 50, From: &from, To: &to}); window.Total != 3 {
		t.Errorf("window = %d, want from inclusive and to exclusive", window.Total)
	}
	if bad, err := repo.ListAudit(ctx, user.AuditFilter{Limit: 50, ActorID: "not-a-uuid"}); err != nil || bad.Total != 0 {
		t.Errorf("a malformed id finds nothing instead of failing: %v %v", bad, err)
	}
}

func TestSearchUsersMatchesAnywhereLiterallyAndPages(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	mk := func(username, email string, status user.AccountStatus) *user.User {
		u := &user.User{Username: username, Email: email, PasswordHash: "h", PhoneNum: "1", AccountRole: user.AccountRoleStudent, AccountStatus: status}
		if err := repo.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	mk("alice", "alice@u.nus.edu", user.AccountStatusActive)
	mk("Natalie", "n@u.nus.edu", user.AccountStatusSuspended)
	mk("bob", "bob@u.nus.edu", user.AccountStatusActive)
	mk("carol", "carol_x@u.nus.edu", user.AccountStatusUnverified)
	mk("dave", "dave@alibaba.example", user.AccountStatusActive)

	names := func(f user.UserFilter) []string {
		f.Limit = 50
		page, err := repo.SearchUsers(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, u := range page.Items {
			out = append(out, u.Username)
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	eq(names(user.UserFilter{Query: "ALI"}), "alice", "dave", "Natalie") // username or email, any case, sorted
	eq(names(user.UserFilter{Query: "ali", Status: user.AccountStatusSuspended}), "Natalie")
	eq(names(user.UserFilter{Status: user.AccountStatusUnverified}), "carol")
	eq(names(user.UserFilter{Query: "_"}), "carol")  // an underscore is not a wildcard
	eq(names(user.UserFilter{Query: "%"}))           // nor is a percent sign
	eq(names(user.UserFilter{Query: "nobody-here"})) // no match is an empty list
	if got := names(user.UserFilter{}); len(got) != 5 || got[0] != "alice" {
		t.Errorf("everyone, by username: %v", got)
	}

	page, err := repo.SearchUsers(ctx, user.UserFilter{Limit: 2, Offset: 2})
	if err != nil || page.Total != 5 || len(page.Items) != 2 || page.Items[0].Username != "carol" {
		t.Fatalf("page 2 of 3 = %+v, %v", page, err)
	}
	empty, _ := repo.SearchUsers(ctx, user.UserFilter{Query: "zzz", Limit: 50})
	if empty.Items == nil {
		t.Error("an empty page is an empty list, not null")
	}
}
