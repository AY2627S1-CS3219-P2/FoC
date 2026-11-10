// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The Postgres side of credit-service's admin routes (D-150 to D-153):
//   the flagged-accounts list, an account read that never invents a wallet,
//   Re-verify under the row lock and observed-version rule, and the admin
//   audit log's writes, paged read and purge (copied from order-service's).
// Author review: Reviewed by Seth

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// execer is the write side of a pool or a transaction, so an audit row can be
// written in either.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ListFlaggedAccounts implements credit.AdminRepository: the oldest flag first,
// which is the order idx_accounts_flagged holds.
func (r *PostgresRepository) ListFlaggedAccounts(ctx context.Context, limit, offset int) (*credit.FlaggedPage, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM credit_accounts WHERE integrity_status = 'FLAGGED'`).Scan(&total); err != nil {
		return nil, fmt.Errorf("count flagged accounts: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT user_id, integrity_reason_code, integrity_detail, integrity_expected,
		       integrity_observed, integrity_flagged_at
		FROM credit_accounts
		WHERE integrity_status = 'FLAGGED'
		ORDER BY integrity_flagged_at, user_id
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list flagged accounts: %w", err)
	}
	defer rows.Close()

	page := &credit.FlaggedPage{Items: []credit.FlaggedAccount{}, Total: total, Limit: limit, Offset: offset}
	for rows.Next() {
		var a credit.FlaggedAccount
		if err := rows.Scan(&a.UserID, &a.ReasonCode, &a.Detail, &a.Expected, &a.Observed, &a.FlaggedAt); err != nil {
			return nil, fmt.Errorf("scan flagged account: %w", err)
		}
		page.Items = append(page.Items, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flagged accounts: %w", err)
	}
	return page, nil
}

// GetAdminAccount implements credit.AdminRepository. Unlike GetAccount, which
// answers a user who has no row with a virtual starting wallet, it reports no
// wallet, because an admin looks at what is stored.
func (r *PostgresRepository) GetAdminAccount(ctx context.Context, userID uuid.UUID) (*credit.Account, error) {
	var a credit.Account
	err := r.pool.QueryRow(ctx, `
		SELECT user_id, available_credits, reserved_credits, version, integrity_status,
		       integrity_flagged_at, integrity_reason_code, integrity_detail,
		       integrity_expected, integrity_observed, created_at, updated_at
		FROM credit_accounts
		WHERE user_id = $1`, userID,
	).Scan(
		&a.UserID, &a.AvailableCredits, &a.ReservedCredits, &a.Version, &a.IntegrityStatus,
		&a.IntegrityFlaggedAt, &a.IntegrityReasonCode, &a.IntegrityDetail,
		&a.IntegrityExpected, &a.IntegrityObserved, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, credit.ErrWalletNotReady
		}
		return nil, fmt.Errorf("get account for admin: %w", err)
	}
	return &a, nil
}

// ReverifyAccount implements credit.AdminRepository (D-150, D-153).
//
// The checks run first, in a read-only snapshot that also records the account's
// version. Only a clean result goes on to write: the account row is locked, and
// the flag is cleared only if it is still flagged at the version the checks saw
// (the rule sourceOfTruth.md sets for any process that clears a flag). A
// failing result writes nothing, and the stored reason stays the first finding.
func (r *PostgresRepository) ReverifyAccount(ctx context.Context, userID uuid.UUID, entry *credit.AuditEntry) (*credit.ReverifyResult, error) {
	finding, observedVersion, err := r.auditAccountSnapshot(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Versions start at 1 (chk_accounts_version_positive), so 0 means no row.
	if observedVersion == 0 {
		return nil, credit.ErrWalletNotReady
	}

	if finding != nil {
		var flagged bool
		if err := r.pool.QueryRow(ctx,
			`SELECT integrity_status = 'FLAGGED' FROM credit_accounts WHERE user_id = $1`, userID).Scan(&flagged); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, credit.ErrWalletNotReady
			}
			return nil, fmt.Errorf("read integrity status: %w", err)
		}
		if !flagged {
			// The background worker flags it on its next pass; an admin does not.
			return &credit.ReverifyResult{Outcome: credit.ReverifyNotFlagged}, nil
		}
		return &credit.ReverifyResult{
			Outcome: credit.ReverifyStillFlagged,
			Finding: &credit.Finding{
				ReasonCode: finding.reasonCode,
				Detail:     finding.detail,
				Expected:   finding.expected,
				Observed:   finding.observed,
			},
		}, nil
	}

	var outcome credit.ReverifyOutcome
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var (
			version    int64
			status     credit.AccountIntegrityStatus
			storedCode *string
		)
		err := tx.QueryRow(ctx, `
			SELECT version, integrity_status, integrity_reason_code
			FROM credit_accounts
			WHERE user_id = $1
			FOR UPDATE`, userID).Scan(&version, &status, &storedCode)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrWalletNotReady
			}
			return fmt.Errorf("lock account: %w", err)
		}
		if status != credit.IntegrityFlagged {
			outcome = credit.ReverifyNotFlagged
			return nil
		}
		if version != observedVersion {
			outcome = credit.ReverifyStale
			return nil
		}
		// The five integrity fields must all be NULL when CONSISTENT
		// (chk_accounts_integrity_fields), and the transition bumps version.
		if _, err := tx.Exec(ctx, `
			UPDATE credit_accounts
			SET integrity_status = 'CONSISTENT',
			    integrity_flagged_at = NULL,
			    integrity_reason_code = NULL,
			    integrity_detail = NULL,
			    integrity_expected = NULL,
			    integrity_observed = NULL,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("clear integrity flag: %w", err)
		}
		e := *entry
		e.BeforeState = map[string]any{"integrity_status": string(credit.IntegrityFlagged)}
		e.AfterState = map[string]any{"integrity_status": string(credit.IntegrityConsistent)}
		if storedCode != nil {
			e.AdditionalComments = map[string]any{"reason_code": *storedCode}
		}
		if err := insertAudit(ctx, tx, &e); err != nil {
			return err
		}
		outcome = credit.ReverifyCleared
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &credit.ReverifyResult{Outcome: outcome}, nil
}

const insertAuditSQL = `
	INSERT INTO admin_audit (occurred_at, actor_id, actor_role, action, target_type,
		target_id, outcome, refusal_reason, reason, before_state, after_state,
		additional_comments, correlation_id)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`

// jsonOrNull is a JSON column's value: NULL for a nil map.
func jsonOrNull(m map[string]any) (any, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// insertAudit writes one audit row on q, which is a transaction for the clear
// and the pool for a standalone attempt row.
func insertAudit(ctx context.Context, q execer, e *credit.AuditEntry) error {
	before, err := jsonOrNull(e.BeforeState)
	if err != nil {
		return fmt.Errorf("encode audit before_state: %w", err)
	}
	after, err := jsonOrNull(e.AfterState)
	if err != nil {
		return fmt.Errorf("encode audit after_state: %w", err)
	}
	comments, err := jsonOrNull(e.AdditionalComments)
	if err != nil {
		return fmt.Errorf("encode audit additional_comments: %w", err)
	}
	at := e.OccurredAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if _, err := q.Exec(ctx, insertAuditSQL,
		at, e.ActorID, e.ActorRole, e.Action, e.TargetType, e.TargetID,
		e.Outcome, e.RefusalReason, e.Reason, before, after, comments, e.CorrelationID); err != nil {
		return fmt.Errorf("insert audit row: %w", err)
	}
	return nil
}

// RecordAudit implements credit.AdminRepository.
func (r *PostgresRepository) RecordAudit(ctx context.Context, e *credit.AuditEntry) error {
	return insertAudit(ctx, r.pool, e)
}

const auditColumns = `id, occurred_at, actor_id, actor_role, action, target_type, target_id,
	outcome, refusal_reason, reason, before_state, after_state, additional_comments, correlation_id`

// ListAudit implements credit.AdminRepository: newest first, filtered, paged.
func (r *PostgresRepository) ListAudit(ctx context.Context, f credit.AuditFilter) (*credit.AuditPage, error) {
	var where []string
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.Outcome != "" {
		add("outcome = $%d", f.Outcome)
	}
	if f.ActorID != "" {
		// Compared as text, so a malformed id finds nothing instead of failing.
		add("actor_id::text = $%d", strings.ToLower(f.ActorID))
	}
	if f.TargetID != "" {
		add("target_id = $%d", strings.ToLower(f.TargetID))
	}
	if f.From != nil {
		add("occurred_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("occurred_at < $%d", *f.To)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM admin_audit"+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count audit rows: %w", err)
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(
		"SELECT %s FROM admin_audit%s ORDER BY occurred_at DESC, id LIMIT $%d OFFSET $%d",
		auditColumns, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list audit rows: %w", err)
	}
	defer rows.Close()

	page := &credit.AuditPage{Total: total, Limit: f.Limit, Offset: f.Offset, Items: []*credit.AuditEntry{}}
	for rows.Next() {
		var e credit.AuditEntry
		var before, after, comments []byte
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorRole, &e.Action, &e.TargetType,
			&e.TargetID, &e.Outcome, &e.RefusalReason, &e.Reason, &before, &after, &comments, &e.CorrelationID); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		for _, d := range []struct {
			raw []byte
			dst *map[string]any
		}{{before, &e.BeforeState}, {after, &e.AfterState}, {comments, &e.AdditionalComments}} {
			if d.raw == nil {
				continue
			}
			if err := json.Unmarshal(d.raw, d.dst); err != nil {
				return nil, fmt.Errorf("decode audit row %s: %w", e.ID, err)
			}
		}
		page.Items = append(page.Items, &e)
	}
	return page, rows.Err()
}

// PurgeAudit implements credit.AdminRepository. The table's trigger refuses to
// delete a row under 6 months old, so this removes exactly the old ones.
func (r *PostgresRepository) PurgeAudit(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_audit WHERE occurred_at < now() - interval '6 months'`)
	if err != nil {
		return 0, fmt.Errorf("purge audit rows: %w", err)
	}
	return tag.RowsAffected(), nil
}
