//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: PostgreSQL role atomicity, authorization, cross-demotion, suspension
// and credential-issuance races using actual transaction lock waits.
// Author review: ZI YANG - reviewed and verified

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

func roleAudit(actor, target uuid.UUID) *user.AuditEntry {
	return &user.AuditEntry{OccurredAt: time.Now().UTC(), ActorID: actor.String(), ActorRole: "ADMIN", Action: user.ActionChangeRole, TargetType: user.AuditTargetType, TargetID: target.String(), Outcome: user.OutcomeSucceeded}
}
func roleCutoff() (time.Time, error) { return time.Now().UTC().Truncate(time.Second), nil }
func roleOf(t *testing.T, ctx context.Context, repo *PostgresRepository, id uuid.UUID) user.AccountRole {
	t.Helper()
	account, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return account.AccountRole
}
func waitForRoleSignal(t *testing.T, ctx context.Context, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func roleResult(t *testing.T, ctx context.Context, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return ctx.Err()
	}
}

// Observe the database's lock wait instead of assuming goroutine scheduling.
func waitForRoleLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fragment string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, "%"+fragment+"%").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no lock wait for %q: %v", fragment, ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestRoleChangeAtomicity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	sessions := NewPostgresSessionRepository(pool)
	actor := createIntegrationAccount(t, ctx, repo, "role-owner", user.AccountRoleAdmin)
	target := createIntegrationAccount(t, ctx, repo, "role-target", user.AccountRoleStudent)
	oldSession := integrationSession(target.UID, "role-before", time.Now().Add(-time.Minute))
	if err := sessions.CreateSession(ctx, oldSession); err != nil {
		t.Fatal(err)
	}
	e := roleAudit(actor.UID, target.UID)
	if err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, e, roleCutoff); err != nil {
		t.Fatal(err)
	}
	if roleOf(t, ctx, repo, target.UID) != user.AccountRoleAdmin {
		t.Fatal("promotion missing")
	}
	stored, err := sessions.GetSessionByHash(ctx, oldSession.TokenHash)
	if err != nil || stored.RevokedAt == nil {
		t.Fatalf("old session = %+v, %v", stored, err)
	}
	if e.BeforeState["account_role"] != "STUDENT" || e.AfterState["account_role"] != "ADMIN" {
		t.Fatalf("role audit=%+v", e)
	}
	called := false
	if err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, roleAudit(actor.UID, target.UID), func() (time.Time, error) { called = true; return roleCutoff() }); err != nil || called {
		t.Fatalf("no-op invalidated credentials: %t, %v", called, err)
	}
	if err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleStudent, roleAudit(actor.UID, target.UID), roleCutoff); err != nil {
		t.Fatal(err)
	}
	if roleOf(t, ctx, repo, target.UID) != user.AccountRoleStudent {
		t.Fatal("demotion missing")
	}
	for _, failure := range []string{"invalidation", "audit"} {
		t.Run(failure, func(t *testing.T) {
			e := roleAudit(actor.UID, target.UID)
			invalidate := roleCutoff
			sentinel := errors.New("Redis unavailable")
			if failure == "audit" {
				e.ActorID = "invalid-uuid"
			} else {
				invalidate = func() (time.Time, error) { return time.Time{}, sentinel }
			}
			before := auditCount(t, ctx, pool)
			freshSession := integrationSession(target.UID, "rollback-"+failure, time.Now())
			if err := sessions.CreateSession(ctx, freshSession); err != nil {
				t.Fatal(err)
			}
			err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, e, invalidate)
			if err == nil {
				t.Fatal("failure did not abort change")
			}
			if failure == "invalidation" && !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			if roleOf(t, ctx, repo, target.UID) != user.AccountRoleStudent || auditCount(t, ctx, pool) != before {
				t.Fatal("failed change committed role or audit")
			}
			stored, err := sessions.GetSessionByHash(ctx, freshSession.TokenHash)
			if err != nil || stored.RevokedAt != nil {
				t.Fatalf("rollback did not preserve session: %+v %v", stored, err)
			}
		})
	}
	// Self-demotion remains forbidden even with a second active administrator.
	if err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, roleAudit(actor.UID, target.UID), roleCutoff); err != nil {
		t.Fatal(err)
	}
	if err := repo.ChangeRoleAudited(ctx, actor.UID, actor.UID, user.AccountRoleStudent, roleAudit(actor.UID, actor.UID), roleCutoff); !errors.Is(err, user.ErrCannotDemoteSelf) {
		t.Fatalf("self demotion = %v", err)
	}
}

func TestConcurrentAdminCrossDemotions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	for iteration := 0; iteration < 5; iteration++ {
		resetIntegrationDatabase(t, ctx, pool)
		first := createIntegrationAccount(t, ctx, repo, fmt.Sprintf("cross-a-%d", iteration), user.AccountRoleAdmin)
		second := createIntegrationAccount(t, ctx, repo, fmt.Sprintf("cross-b-%d", iteration), user.AccountRoleAdmin)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, ids := range [][2]uuid.UUID{{first.UID, second.UID}, {second.UID, first.UID}} {
			go func(actor, target uuid.UUID) {
				<-start
				results <- repo.ChangeRoleAudited(ctx, actor, target, user.AccountRoleStudent, roleAudit(actor, target), roleCutoff)
			}(ids[0], ids[1])
		}
		close(start)
		successes, refusals := 0, 0
		for range 2 {
			err := roleResult(t, ctx, results)
			if err == nil {
				successes++
			} else if errors.Is(err, user.ErrAdminRequired) {
				refusals++
			} else {
				t.Fatal(err)
			}
		}
		count, err := repo.CountActiveAdmins(ctx)
		if err != nil || count != 1 || successes != 1 || refusals != 1 {
			t.Fatalf("admins=%d successes=%d refusals=%d err=%v", count, successes, refusals, err)
		}
	}
}

func TestPromotionAndSuspensionSerialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	actor := createIntegrationAccount(t, ctx, repo, "promotion-owner", user.AccountRoleAdmin)
	t.Run("promotion commits before suspension", func(t *testing.T) {
		target := createIntegrationAccount(t, ctx, repo, "promotion-wins", user.AccountRoleStudent)
		locked, release := make(chan struct{}), make(chan struct{})
		promotion := make(chan error, 1)
		suspension := make(chan error, 1)
		go func() {
			promotion <- repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, roleAudit(actor.UID, target.UID), func() (time.Time, error) {
				close(locked)
				select {
				case <-release:
					return roleCutoff()
				case <-ctx.Done():
					return time.Time{}, ctx.Err()
				}
			})
		}()
		waitForRoleSignal(t, ctx, locked)
		sideEffects := false
		go func() {
			suspension <- repo.UpdateAccountStatusAudited(ctx, target.UID, user.AccountStatusSuspended, time.Now(), roleAudit(actor.UID, target.UID), func() error { sideEffects = true; return nil })
		}()
		waitForRoleLock(t, ctx, pool, "SELECT account_status, account_role")
		close(release)
		if err := roleResult(t, ctx, promotion); err != nil {
			t.Fatal(err)
		}
		if err := roleResult(t, ctx, suspension); !errors.Is(err, user.ErrCannotSuspendAdmin) {
			t.Fatalf("suspension = %v", err)
		}
		if sideEffects || statusOf(t, ctx, repo, target.UID) != user.AccountStatusActive {
			t.Fatal("suspension changed promoted admin")
		}
	})
	t.Run("suspension commits before promotion", func(t *testing.T) {
		target := createIntegrationAccount(t, ctx, repo, "suspension-wins", user.AccountRoleStudent)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `UPDATE users SET account_status='SUSPENDED' WHERE uid=$1`, target.UID); err != nil {
			t.Fatal(err)
		}
		promotion := make(chan error, 1)
		go func() {
			promotion <- repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, roleAudit(actor.UID, target.UID), roleCutoff)
		}()
		waitForRoleLock(t, ctx, pool, "ORDER BY uid FOR UPDATE")
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := roleResult(t, ctx, promotion); !errors.Is(err, user.ErrInactivePromotion) {
			t.Fatalf("promoted suspended student: %v", err)
		}
		if roleOf(t, ctx, repo, target.UID) != user.AccountRoleStudent {
			t.Fatal("suspended student promoted")
		}
	})
}

func TestCredentialIssuanceAndRoleChangeSerialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	sessions := NewPostgresSessionRepository(pool)
	actor := createIntegrationAccount(t, ctx, repo, "credential-owner", user.AccountRoleAdmin)
	t.Run("issuance first is revoked by demotion", func(t *testing.T) {
		target := createIntegrationAccount(t, ctx, repo, "issuance-first", user.AccountRoleAdmin)
		locked, release := make(chan struct{}), make(chan struct{})
		issued, changed := make(chan error, 1), make(chan error, 1)
		pending := integrationSession(target.UID, "issued-before-demotion", time.Now())
		go func() {
			issued <- sessions.WithAccountCredentials(ctx, target.UID, func(account *user.User, scoped user.SessionRepository) error {
				if account.AccountRole != user.AccountRoleAdmin {
					return errors.New("unexpected role before demotion")
				}
				close(locked)
				select {
				case <-release:
					return scoped.CreateSession(ctx, pending)
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		waitForRoleSignal(t, ctx, locked)
		go func() {
			changed <- repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleStudent, roleAudit(actor.UID, target.UID), roleCutoff)
		}()
		waitForRoleLock(t, ctx, pool, "ORDER BY uid FOR UPDATE")
		close(release)
		if err := roleResult(t, ctx, issued); err != nil {
			t.Fatal(err)
		}
		if err := roleResult(t, ctx, changed); err != nil {
			t.Fatal(err)
		}
		stored, err := sessions.GetSessionByHash(ctx, pending.TokenHash)
		if err != nil || stored.RevokedAt == nil {
			t.Fatalf("overlapping credential escaped revocation: %+v %v", stored, err)
		}
	})
	t.Run("demotion first yields fresh role", func(t *testing.T) {
		target := createIntegrationAccount(t, ctx, repo, "role-first", user.AccountRoleAdmin)
		locked, release := make(chan struct{}), make(chan struct{})
		issued, changed := make(chan error, 1), make(chan error, 1)
		go func() {
			changed <- repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleStudent, roleAudit(actor.UID, target.UID), func() (time.Time, error) {
				close(locked)
				select {
				case <-release:
					return roleCutoff()
				case <-ctx.Done():
					return time.Time{}, ctx.Err()
				}
			})
		}()
		waitForRoleSignal(t, ctx, locked)
		go func() {
			issued <- sessions.WithAccountCredentials(ctx, target.UID, func(account *user.User, _ user.SessionRepository) error {
				if account.AccountRole != user.AccountRoleStudent {
					return fmt.Errorf("stale role %s", account.AccountRole)
				}
				return nil
			})
		}()
		waitForRoleLock(t, ctx, pool, "FOR SHARE")
		close(release)
		if err := roleResult(t, ctx, changed); err != nil {
			t.Fatal(err)
		}
		if err := roleResult(t, ctx, issued); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("credential callback failure rolls back session", func(t *testing.T) {
		target := createIntegrationAccount(t, ctx, repo, "credential-rollback", user.AccountRoleStudent)
		pending := integrationSession(target.UID, "credential-rollback", time.Now())
		failure := errors.New("issuer persistence failure")
		err := sessions.WithAccountCredentials(ctx, target.UID, func(_ *user.User, scoped user.SessionRepository) error {
			if err := scoped.CreateSession(ctx, pending); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if _, err := sessions.GetSessionByHash(ctx, pending.TokenHash); !errors.Is(err, user.ErrSessionNotFound) {
			t.Fatalf("rolled back session persisted: %v", err)
		}
	})
}

func TestPostgresCutoffsNeverMoveBackwards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx)
	repo := NewPostgresRepository(pool)
	actor := createIntegrationAccount(t, ctx, repo, "cutoff-owner", user.AccountRoleAdmin)
	target := createIntegrationAccount(t, ctx, repo, "cutoff-target", user.AccountRoleStudent)
	stale := *target
	cutoff := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	invalidate := func() (time.Time, error) { return cutoff, nil }
	if err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleAdmin, roleAudit(actor.UID, target.UID), invalidate); err != nil {
		t.Fatal(err)
	}
	// A profile read before promotion must not restore the earlier token cutoff.
	if err := repo.Update(ctx, &stale); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, target.UID)
	if err != nil || !got.TokensValidAfter.Equal(cutoff) || got.AccountRole != user.AccountRoleAdmin {
		t.Fatalf("stale profile undid role/cutoff: %+v %v", got, err)
	}
	if err := repo.ChangeRoleAudited(ctx, actor.UID, target.UID, user.AccountRoleStudent, roleAudit(actor.UID, target.UID), invalidate); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateAccountStatusByID(ctx, target.UID, user.AccountStatusSuspended, cutoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetByID(ctx, target.UID)
	if err != nil || !got.TokensValidAfter.Equal(cutoff) {
		t.Fatalf("older suspension lowered cutoff: %+v %v", got, err)
	}
}
