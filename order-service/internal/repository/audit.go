// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The Postgres side of the admin dashboard (D-104 to D-117): the admin
//   audit log's writes, paged read and purge, the admin's filtered and sorted
//   errand list, and a user's errand counts by state.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
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

// insertAudit writes one audit row on q, which is a transaction for an audited
// transition and the pool for a standalone attempt row.
func insertAudit(ctx context.Context, q execer, e *order.AuditEntry) error {
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

// RecordAudit implements order.Repository.
func (p *Postgres) RecordAudit(ctx context.Context, e *order.AuditEntry) error {
	return insertAudit(ctx, p.pool, e)
}

const auditColumns = `id, occurred_at, actor_id, actor_role, action, target_type, target_id,
	outcome, refusal_reason, reason, before_state, after_state, additional_comments, correlation_id`

// ListAudit implements order.Repository: newest first, filtered, paged.
func (p *Postgres) ListAudit(ctx context.Context, f order.AuditFilter) (*order.AuditPage, error) {
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
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM admin_audit"+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count audit rows: %w", err)
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(
		"SELECT %s FROM admin_audit%s ORDER BY occurred_at DESC, id LIMIT $%d OFFSET $%d",
		auditColumns, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list audit rows: %w", err)
	}
	defer rows.Close()

	page := &order.AuditPage{Total: total, Limit: f.Limit, Offset: f.Offset, Items: []*order.AuditEntry{}}
	for rows.Next() {
		var e order.AuditEntry
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

// PurgeAudit implements order.Repository. The table's trigger refuses to delete
// a row under 6 months old, so this removes exactly the old ones.
func (p *Postgres) PurgeAudit(ctx context.Context) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM admin_audit WHERE occurred_at < now() - interval '6 months'`)
	if err != nil {
		return 0, fmt.Errorf("purge audit rows: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListAdminErrands implements order.Repository: the admin's errand list,
// filtered, sorted and paged (D-105, D-117).
func (p *Postgres) ListAdminErrands(ctx context.Context, f order.AdminErrandFilter) (order.AdminErrandPage, error) {
	var where []string
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.State != nil {
		add("state = $%d", string(*f.State))
	} else if f.Scope == order.ScopeHistory {
		add("state = ANY($%d)", stateNames(order.FinalStates()))
	} else {
		add("state = ANY($%d)", stateNames(order.ActiveStates()))
	}
	if f.From != nil {
		add("state_changed_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("state_changed_at < $%d", *f.To)
	}
	if f.SupplierID != nil {
		add("supplier_id = $%d", *f.SupplierID)
	}
	if f.UserID != nil {
		args = append(args, *f.UserID)
		where = append(where, fmt.Sprintf("(requester_id = $%d OR courier_id = $%d)", len(args), len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM orders"+clause, args...).Scan(&total); err != nil {
		return order.AdminErrandPage{}, fmt.Errorf("count errands for admin: %w", err)
	}
	var orderBy string
	switch f.Sort {
	case order.SortSupplierName:
		orderBy = "lower(supplier_name), state_changed_at DESC, id"
	case order.SortOldest:
		orderBy = "state_changed_at, id"
	default:
		orderBy = "state_changed_at DESC, id"
	}
	args = append(args, f.Limit, f.Offset)
	items, err := p.listOrders(ctx, "list errands for admin",
		fmt.Sprintf("SELECT %s FROM orders%s ORDER BY %s LIMIT $%d OFFSET $%d", orderSelect, clause, orderBy, len(args)-1, len(args)),
		args...)
	if err != nil {
		return order.AdminErrandPage{}, err
	}
	if items == nil {
		items = []order.Order{}
	}
	return order.AdminErrandPage{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// CountByUser implements order.Repository.
func (p *Postgres) CountByUser(ctx context.Context, userID uuid.UUID) (order.ErrandCounts, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT 'requested', state, count(*) FROM orders WHERE requester_id = $1 GROUP BY state
		UNION ALL
		SELECT 'assigned', state, count(*) FROM orders WHERE courier_id = $1 GROUP BY state`, userID)
	if err != nil {
		return order.ErrandCounts{}, fmt.Errorf("count errands of user %s: %w", userID, err)
	}
	defer rows.Close()
	counts := order.ErrandCounts{Requested: map[order.State]int{}, Assigned: map[order.State]int{}}
	for rows.Next() {
		var as, state string
		var n int
		if err := rows.Scan(&as, &state, &n); err != nil {
			return order.ErrandCounts{}, fmt.Errorf("scan errand counts: %w", err)
		}
		if as == "requested" {
			counts.Requested[order.State(state)] = n
		} else {
			counts.Assigned[order.State(state)] = n
		}
	}
	return counts, rows.Err()
}
