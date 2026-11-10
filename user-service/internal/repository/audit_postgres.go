// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The Postgres side of the admin audit log and the account search
//   (D-106 to D-123): audited writes that share one transaction with their audit
//   row, the standalone row for an attempt that did not commit, the paged read,
//   the purge of rows older than 6 months, and the contains-search on accounts.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

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

// insertAudit writes one audit row on executor, which is a transaction for an
// audited write and the pool for a standalone attempt row.
func insertAudit(ctx context.Context, executor userInserter, e *user.AuditEntry) error {
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
	if _, err := executor.Exec(ctx, insertAuditSQL,
		at, e.ActorID, e.ActorRole, e.Action, e.TargetType, e.TargetID,
		e.Outcome, e.RefusalReason, e.Reason, before, after, comments, e.CorrelationID); err != nil {
		return fmt.Errorf("insert audit row: %w", err)
	}
	return nil
}

// UpdateAccountStatusAudited implements user.UserRepository.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
func (r *PostgresRepository) UpdateAccountStatusAudited(ctx context.Context, uid uuid.UUID, status user.AccountStatus, tokensValidAfter time.Time, e *user.AuditEntry, invalidate func() error) error {
	return r.updateAccountStatus(ctx, uid, status, tokensValidAfter, e, invalidate)
}

// UpdateAsAdminAudited implements user.UserRepository: the profile update and
// its audit rows commit together or not at all (D-108).
func (r *PostgresRepository) UpdateAsAdminAudited(ctx context.Context, u *user.User, entries []*user.AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin admin update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, updateUserQuery, u.Username, u.PasswordHash, u.PhoneNum, u.TokensValidAfter, u.UID)
	if err != nil {
		return fmt.Errorf("update user: %w", mapDatabaseError(err))
	}
	if result.RowsAffected() == 0 {
		return user.ErrNotFound
	}
	for _, e := range entries {
		if err := insertAudit(ctx, tx, e); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin update transaction: %w", err)
	}
	return nil
}

// RecordAudit implements user.UserRepository.
func (r *PostgresRepository) RecordAudit(ctx context.Context, e *user.AuditEntry) error {
	return insertAudit(ctx, r.pool, e)
}

const auditColumns = `id, occurred_at, actor_id, actor_role, action, target_type, target_id,
	outcome, refusal_reason, reason, before_state, after_state, additional_comments, correlation_id`

// ListAudit implements user.UserRepository: newest first, filtered, paged.
func (r *PostgresRepository) ListAudit(ctx context.Context, f user.AuditFilter) (*user.AuditPage, error) {
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

	page := &user.AuditPage{Total: total, Limit: f.Limit, Offset: f.Offset, Items: []*user.AuditEntry{}}
	for rows.Next() {
		var e user.AuditEntry
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

// PurgeAudit implements user.UserRepository. The table's trigger refuses to
// delete a row under 6 months old, so this removes exactly the old ones.
func (r *PostgresRepository) PurgeAudit(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_audit WHERE occurred_at < now() - interval '6 months'`)
	if err != nil {
		return 0, fmt.Errorf("purge audit rows: %w", err)
	}
	return tag.RowsAffected(), nil
}

// likeEscaper makes a search term literal inside a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

const userListColumns = `uid, username, email, password, phone_num, date_created,
	last_login_date, account_role, account_status, tokens_valid_after`

// SearchUsers implements user.UserRepository: a contains-match on the username
// or email ignoring case (D-123), optionally one status (D-121), sorted by
// username and paged. The term is matched literally: % and _ are not wildcards.
func (r *PostgresRepository) SearchUsers(ctx context.Context, f user.UserFilter) (*user.UserPage, error) {
	var where []string
	var args []any
	if f.Query != "" {
		args = append(args, "%"+likeEscaper.Replace(strings.ToLower(f.Query))+"%")
		where = append(where, fmt.Sprintf(`(LOWER(username) LIKE $%d ESCAPE '\' OR LOWER(email) LIKE $%d ESCAPE '\')`, len(args), len(args)))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		where = append(where, fmt.Sprintf("account_status = $%d::account_status", len(args)))
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM users"+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count accounts: %w", err)
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(
		"SELECT %s FROM users%s ORDER BY LOWER(username), uid LIMIT $%d OFFSET $%d",
		userListColumns, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	page := &user.UserPage{Total: total, Limit: f.Limit, Offset: f.Offset, Items: []*user.User{}}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		page.Items = append(page.Items, u)
	}
	return page, rows.Err()
}
