//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Integration tests of credit-service's admin repository against a real
//   PostgreSQL (D-150 to D-153): Re-verify clears a flag only when the checks
//   pass and writes its audit row in the same transaction, the flagged list,
//   the stored wallet read, and the audit table's immutability and purge.
// Author review: Reviewed by Seth

package repository_test

import (
	"context"
	"testing"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// newWallet creates an account with its starting allocation, so its ledger and
// its stored balances agree.
func newWallet(t *testing.T, env *testEnv) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, env.repo.AllocateInitialCredits(context.Background(), id, 100, time.Now().UTC()))
	return id
}

// flagAs marks an account FLAGGED the way the reconciliation worker does,
// without changing any balance, as a false positive would leave it.
func flagAs(t *testing.T, env *testEnv, id uuid.UUID, at time.Time, code string) {
	t.Helper()
	_, err := env.pool.Exec(context.Background(), `
		UPDATE credit_accounts
		SET integrity_status = 'FLAGGED', integrity_flagged_at = $2, integrity_reason_code = $3,
		    integrity_detail = 'seeded by the test', integrity_expected = 100, integrity_observed = 105,
		    version = version + 1
		WHERE user_id = $1`, id, at, code)
	require.NoError(t, err)
}

func reverifyEntry(admin, target uuid.UUID) *credit.AuditEntry {
	return &credit.AuditEntry{
		OccurredAt:    time.Now().UTC(),
		ActorID:       admin.String(),
		ActorRole:     "ADMIN",
		Action:        credit.AuditReverify,
		TargetType:    credit.AuditTargetType,
		TargetID:      target.String(),
		Outcome:       credit.OutcomeSucceeded,
		CorrelationID: "req-1",
	}
}

func TestAdminReverify_Integration(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	admin := uuid.New()

	t.Run("clears a flag the checks no longer support, and logs it in the same transaction", func(t *testing.T) {
		id := newWallet(t, env)
		flagAs(t, env, id, time.Now().UTC(), "AVAILABLE_PROJECTION_MISMATCH")
		before, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)

		res, err := env.repo.ReverifyAccount(ctx, id, reverifyEntry(admin, id))
		require.NoError(t, err)
		require.Equal(t, credit.ReverifyCleared, res.Outcome)
		require.Nil(t, res.Finding)

		after, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, credit.IntegrityConsistent, after.IntegrityStatus)
		require.Nil(t, after.IntegrityFlaggedAt)
		require.Nil(t, after.IntegrityReasonCode)
		require.Nil(t, after.IntegrityDetail)
		require.Nil(t, after.IntegrityExpected)
		require.Nil(t, after.IntegrityObserved)
		require.Equal(t, before.Version+1, after.Version, "the transition bumps version")
		require.Equal(t, before.AvailableCredits, after.AvailableCredits, "no balance is touched")
		require.Equal(t, before.ReservedCredits, after.ReservedCredits)

		page, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: id.String(), Limit: 10})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		row := page.Items[0]
		require.Equal(t, credit.AuditReverify, row.Action)
		require.Equal(t, credit.OutcomeSucceeded, row.Outcome)
		require.Equal(t, admin.String(), row.ActorID)
		require.Equal(t, "req-1", row.CorrelationID)
		require.Equal(t, "FLAGGED", row.BeforeState["integrity_status"])
		require.Equal(t, "CONSISTENT", row.AfterState["integrity_status"])
		require.Equal(t, "AVAILABLE_PROJECTION_MISMATCH", row.AdditionalComments["reason_code"])
	})

	t.Run("leaves a real discrepancy flagged, writes nothing, and keeps the first reason", func(t *testing.T) {
		id := newWallet(t, env)
		// Corrupt the stored balance so it no longer matches the ledger, then
		// let reconciliation flag it as the worker would.
		_, err := env.pool.Exec(ctx, `UPDATE credit_accounts SET available_credits = available_credits + 5, version = version + 1 WHERE user_id = $1`, id)
		require.NoError(t, err)
		flagged, err := env.repo.ReconcileAccount(ctx, id)
		require.NoError(t, err)
		require.True(t, flagged)
		stored, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, credit.IntegrityFlagged, stored.IntegrityStatus)

		res, err := env.repo.ReverifyAccount(ctx, id, reverifyEntry(admin, id))
		require.NoError(t, err)
		require.Equal(t, credit.ReverifyStillFlagged, res.Outcome)
		require.NotNil(t, res.Finding)
		require.Equal(t, "AVAILABLE_PROJECTION_MISMATCH", res.Finding.ReasonCode)
		require.NotNil(t, res.Finding.Expected)
		require.NotNil(t, res.Finding.Observed)
		require.InDelta(t, 100, *res.Finding.Expected, 0)
		require.InDelta(t, 105, *res.Finding.Observed, 0)

		after, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, credit.IntegrityFlagged, after.IntegrityStatus)
		require.Equal(t, stored.Version, after.Version, "a failing Re-verify writes nothing to the account")
		require.Equal(t, *stored.IntegrityReasonCode, *after.IntegrityReasonCode, "the first finding stays (D-153)")

		page, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: id.String(), Limit: 10})
		require.NoError(t, err)
		require.Empty(t, page.Items, "the repository writes an audit row only for a clear; the service logs the refusal")
	})

	t.Run("changes nothing on an account that is not flagged", func(t *testing.T) {
		id := newWallet(t, env)
		before, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)

		res, err := env.repo.ReverifyAccount(ctx, id, reverifyEntry(admin, id))
		require.NoError(t, err)
		require.Equal(t, credit.ReverifyNotFlagged, res.Outcome)

		after, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, before.Version, after.Version)
		page, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: id.String(), Limit: 10})
		require.NoError(t, err)
		require.Empty(t, page.Items)
	})

	t.Run("reports a user with no account", func(t *testing.T) {
		_, err := env.repo.ReverifyAccount(ctx, uuid.New(), reverifyEntry(admin, uuid.New()))
		require.ErrorIs(t, err, credit.ErrWalletNotReady)
	})

	t.Run("reconciliation and scan idempotency on already flagged account", func(t *testing.T) {
		id := newWallet(t, env)

		// 1. Scan finds newly created consistent account
		scanned, _, err := env.repo.ScanAccountsForReconciliation(ctx, 100, nil)
		require.NoError(t, err)
		var found bool
		for _, s := range scanned {
			if s == id {
				found = true
				break
			}
		}
		require.True(t, found, "consistent account must appear in reconciliation scan")

		// 2. Corrupt account projection and reconcile
		_, err = env.pool.Exec(ctx, `UPDATE credit_accounts SET available_credits = 150 WHERE user_id = $1`, id)
		require.NoError(t, err)

		flagged, err := env.repo.ReconcileAccount(ctx, id)
		require.NoError(t, err)
		require.True(t, flagged)

		accBefore, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, credit.IntegrityFlagged, accBefore.IntegrityStatus)

		// 3. Repeat reconciliation must be idempotent: returns false, version not incremented, updated_at untouched
		flaggedRepeat, err := env.repo.ReconcileAccount(ctx, id)
		require.NoError(t, err)
		require.False(t, flaggedRepeat, "already flagged account must not report flagged on repeat")

		accRepeat, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, accBefore.Version, accRepeat.Version, "version must not inflate")
		require.Equal(t, accBefore.UpdatedAt, accRepeat.UpdatedAt, "updated_at must remain untouched")

		// 4. ScanAccountsForReconciliation must filter out the flagged account
		scannedAfter, _, err := env.repo.ScanAccountsForReconciliation(ctx, 100, nil)
		require.NoError(t, err)
		for _, s := range scannedAfter {
			require.NotEqual(t, id, s, "ScanAccountsForReconciliation must not return FLAGGED accounts")
		}
	})
}

func TestAdminReads_Integration(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	t.Run("reads the stored wallet of a flagged account, and reports a missing one", func(t *testing.T) {
		id := newWallet(t, env)
		flagAs(t, env, id, time.Now().UTC(), "OUT_OF_BOUNDS_BALANCE")

		got, err := env.repo.GetAdminAccount(ctx, id)
		require.NoError(t, err)
		require.Equal(t, int64(100), got.AvailableCredits, "an admin sees the real balance, unlike the user (D-150)")
		require.Equal(t, credit.IntegrityFlagged, got.IntegrityStatus)
		require.Equal(t, "OUT_OF_BOUNDS_BALANCE", *got.IntegrityReasonCode)

		_, err = env.repo.GetAdminAccount(ctx, uuid.New())
		require.ErrorIs(t, err, credit.ErrWalletNotReady)
	})
}

// TestAdminFlaggedList_Integration has its own database, so no other test's
// flagged account is in the count.
func TestAdminFlaggedList_Integration(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	t.Run("lists only flagged accounts, oldest flag first, and pages them", func(t *testing.T) {
		now := time.Now().UTC()
		oldest, middle, newest, clean := newWallet(t, env), newWallet(t, env), newWallet(t, env), newWallet(t, env)
		flagAs(t, env, newest, now.Add(-1*time.Minute), "RESERVED_PROJECTION_MISMATCH")
		flagAs(t, env, oldest, now.Add(-3*time.Hour), "OUT_OF_BOUNDS_BALANCE")
		flagAs(t, env, middle, now.Add(-2*time.Hour), "OPERATION_SHAPE_VIOLATION")

		page, err := env.repo.ListFlaggedAccounts(ctx, 2, 0)
		require.NoError(t, err)
		require.Equal(t, 3, page.Total)
		require.Len(t, page.Items, 2)
		require.Equal(t, oldest, page.Items[0].UserID)
		require.Equal(t, middle, page.Items[1].UserID)
		require.Equal(t, "OUT_OF_BOUNDS_BALANCE", page.Items[0].ReasonCode)
		require.NotNil(t, page.Items[0].Observed)

		next, err := env.repo.ListFlaggedAccounts(ctx, 2, 2)
		require.NoError(t, err)
		require.Len(t, next.Items, 1)
		require.Equal(t, newest, next.Items[0].UserID)

		for _, a := range append(page.Items, next.Items...) {
			require.NotEqual(t, clean, a.UserID, "a consistent account is not listed")
		}
	})
}

func TestAdminAudit_Integration(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	admin, target := uuid.New(), uuid.New()

	t.Run("filters, pages and orders the log newest first", func(t *testing.T) {
		now := time.Now().UTC()
		for i, outcome := range []string{credit.OutcomeSucceeded, credit.OutcomeRefused, credit.OutcomeRefused} {
			e := reverifyEntry(admin, target)
			e.OccurredAt = now.Add(time.Duration(i) * time.Minute)
			e.Outcome = outcome
			if outcome == credit.OutcomeRefused {
				e.RefusalReason = "a ledger check still fails"
			}
			require.NoError(t, env.repo.RecordAudit(ctx, e))
		}

		all, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: target.String(), Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 3, all.Total)
		require.Equal(t, credit.OutcomeRefused, all.Items[0].Outcome, "newest first")
		require.Equal(t, credit.OutcomeSucceeded, all.Items[2].Outcome)

		refused, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: target.String(), Outcome: credit.OutcomeRefused, Limit: 1, Offset: 1})
		require.NoError(t, err)
		require.Equal(t, 2, refused.Total)
		require.Len(t, refused.Items, 1)
		require.Equal(t, "a ledger check still fails", refused.Items[0].RefusalReason)
	})

	t.Run("refuses to change or delete a recent row, and purges only the old ones", func(t *testing.T) {
		recent := reverifyEntry(admin, uuid.New())
		require.NoError(t, env.repo.RecordAudit(ctx, recent))
		_, err := env.pool.Exec(ctx, `UPDATE admin_audit SET outcome = 'failed' WHERE target_id = $1`, recent.TargetID)
		require.Error(t, err, "an entry is immutable (D-114)")
		_, err = env.pool.Exec(ctx, `DELETE FROM admin_audit WHERE target_id = $1`, recent.TargetID)
		require.Error(t, err, "an entry under 6 months old cannot be deleted")

		old := reverifyEntry(admin, uuid.New())
		old.OccurredAt = time.Now().UTC().AddDate(0, -7, 0)
		require.NoError(t, env.repo.RecordAudit(ctx, old))

		n, err := env.repo.PurgeAudit(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, n, int64(1))

		gone, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: old.TargetID, Limit: 10})
		require.NoError(t, err)
		require.Empty(t, gone.Items)
		kept, err := env.repo.ListAudit(ctx, credit.AuditFilter{TargetID: recent.TargetID, Limit: 10})
		require.NoError(t, err)
		require.Len(t, kept.Items, 1)
	})
}
