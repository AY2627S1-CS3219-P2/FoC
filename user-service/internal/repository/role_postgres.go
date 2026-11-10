// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Approved serialized role changes with atomic sessions and audit writes.
// Author review: ZI YANG - reviewed and verified

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// roleChangeLockKey is reserved for user-service administrator membership edits.
// Every promotion and demotion uses this transaction-scoped database lock.
const roleChangeLockKey int64 = 0x466f43526f6c65

// ChangeRoleAudited implements user.RoleChangeRepository.
func (r *PostgresRepository) ChangeRoleAudited(ctx context.Context, actorID, targetID uuid.UUID, role user.AccountRole, entry *user.AuditEntry, invalidate func() (time.Time, error)) error {
	if entry == nil || invalidate == nil {
		return errors.New("role audit and invalidation callback are required")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin role change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, roleChangeLockKey); err != nil {
		return fmt.Errorf("lock role changes: %w", err)
	}
	// One ordered query locks both identities before checking authorization or count.
	rows, err := tx.Query(ctx, `SELECT `+userListColumns+` FROM users WHERE uid = $1 OR uid = $2 ORDER BY uid FOR UPDATE`, actorID, targetID)
	if err != nil {
		return fmt.Errorf("lock role accounts: %w", err)
	}
	var actor, target *user.User
	for rows.Next() {
		account, scanErr := scanUser(rows)
		if scanErr != nil {
			rows.Close()
			return fmt.Errorf("scan role account: %w", scanErr)
		}
		if account.UID == actorID {
			actor = account
		}
		if account.UID == targetID {
			target = account
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read role accounts: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx, countActiveAdminsQuery).Scan(&count); err != nil {
		return fmt.Errorf("count admins for role change: %w", err)
	}
	if err := user.ValidateRoleChange(actor, target, role, count); err != nil {
		return err
	}
	entry.BeforeState = map[string]any{"account_role": string(target.AccountRole)}
	entry.AfterState = map[string]any{"account_role": string(role)}
	if target.AccountRole != role {
		cutoff, err := invalidate()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET account_role = $1::account_role, tokens_valid_after = GREATEST(tokens_valid_after, $2) WHERE uid = $3`, role, cutoff, targetID); err != nil {
			return fmt.Errorf("persist role change: %w", err)
		}
		if _, err := tx.Exec(ctx, revokeAllUserSessionsQuery, cutoff, targetID); err != nil {
			return fmt.Errorf("revoke role sessions: %w", err)
		}
	}
	if err := insertAudit(ctx, tx, entry); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role change: %w", err)
	}
	return nil
}
