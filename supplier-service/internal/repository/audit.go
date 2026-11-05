// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The Postgres side of the admin audit log (D-106 to D-115): the audited
//   supplier writes that share one transaction with their audit row, the
//   standalone row for an attempt that did not commit, the paged read, and the
//   purge of rows older than 6 months.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"foc/supplier-service/internal/supplier"
)

// execer is what both a pool and a transaction can do, so one insert serves the
// audited writes (a transaction) and RecordAudit (the pool).
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

const insertAuditSQL = `
	INSERT INTO admin_audit (occurred_at, actor_id, actor_role, action, target_type,
		target_id, outcome, refusal_reason, reason, before_state, after_state,
		additional_comments, correlation_id)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`

// jsonOrNull is a JSON column's value: NULL for an empty map, so "no details"
// is distinguishable from an empty object only where it matters (it does not).
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

func insertAudit(ctx context.Context, q execer, e *supplier.AuditEntry) error {
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
	_, err = q.Exec(ctx, insertAuditSQL,
		at, e.ActorID, e.ActorRole, e.Action, e.TargetType, e.TargetID,
		e.Outcome, e.RefusalReason, e.Reason, before, after, comments, e.CorrelationID)
	if err != nil {
		return fmt.Errorf("insert audit row: %w", err)
	}
	return nil
}

// inTx runs fn in a transaction: it commits when fn returns nil and rolls back
// otherwise, which is what makes an audited write all or nothing (D-108).
func (r *PostgresRepository) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// CreateAudited implements supplier.Repository.
func (r *PostgresRepository) CreateAudited(ctx context.Context, s *supplier.Supplier, e *supplier.AuditEntry) error {
	return r.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO suppliers (id, name, type, building, floor, location_description,
				latitude, longitude, opening_time, closing_time, image_url, description,
				is_available, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			s.ID, s.Name, s.Type, s.Building, s.Floor, s.LocationDescription,
			s.Latitude, s.Longitude, s.OpeningTime, s.ClosingTime, s.ImageURL, s.Description,
			s.IsAvailable, s.CreatedAt, s.UpdatedAt,
		)
		if err != nil {
			return mapWriteError(err)
		}
		return insertAudit(ctx, tx, e)
	})
}

// UpdateAudited implements supplier.Repository. Like Update, it answers
// ErrNotFound when no live row matched, and then saves nothing.
func (r *PostgresRepository) UpdateAudited(ctx context.Context, s *supplier.Supplier, e *supplier.AuditEntry) error {
	return r.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE suppliers SET
				name = $1, type = $2, building = $3, floor = $4, location_description = $5,
				latitude = $6, longitude = $7, opening_time = $8, closing_time = $9,
				image_url = $10, description = $11, is_available = $12, updated_at = $13
			WHERE id = $14 AND deleted_at IS NULL`,
			s.Name, s.Type, s.Building, s.Floor, s.LocationDescription,
			s.Latitude, s.Longitude, s.OpeningTime, s.ClosingTime,
			s.ImageURL, s.Description, s.IsAvailable, s.UpdatedAt, s.ID,
		)
		if err != nil {
			return mapWriteError(err)
		}
		if tag.RowsAffected() == 0 {
			return supplier.ErrNotFound
		}
		return insertAudit(ctx, tx, e)
	})
}

// SoftDeleteAudited implements supplier.Repository. Unlike SoftDelete it
// answers ErrNotFound when the supplier was already gone, so the audit log
// never records a delete that changed nothing.
func (r *PostgresRepository) SoftDeleteAudited(ctx context.Context, id string, e *supplier.AuditEntry) error {
	return r.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE suppliers SET deleted_at = now(), is_available = false, updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return supplier.ErrNotFound
		}
		return insertAudit(ctx, tx, e)
	})
}

// RecordAudit implements supplier.Repository.
func (r *PostgresRepository) RecordAudit(ctx context.Context, e *supplier.AuditEntry) error {
	return insertAudit(ctx, r.pool, e)
}

const auditColumns = `id, occurred_at, actor_id, actor_role, action, target_type, target_id,
	outcome, refusal_reason, reason, before_state, after_state, additional_comments, correlation_id`

// ListAudit implements supplier.Repository: newest first, filtered, paged.
func (r *PostgresRepository) ListAudit(ctx context.Context, f supplier.AuditFilter) (*supplier.AuditPage, error) {
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

	page := &supplier.AuditPage{Total: total, Limit: f.Limit, Offset: f.Offset, Items: []*supplier.AuditEntry{}}
	for rows.Next() {
		var e supplier.AuditEntry
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

// PurgeAudit implements supplier.Repository. The table's trigger refuses to
// delete a row under 6 months old, so this removes exactly the old ones.
func (r *PostgresRepository) PurgeAudit(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_audit WHERE occurred_at < now() - interval '6 months'`)
	if err != nil {
		return 0, fmt.Errorf("purge audit rows: %w", err)
	}
	return tag.RowsAffected(), nil
}
