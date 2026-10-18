// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: PostgreSQL adapter for order.Repository: the type, row scanning, the
//   transaction helper and every read and list (D-046, D-049, D-052, D-053).
// Author review: James Teo — reviewed and verified.

// Package repository is order-service's PostgreSQL adapter. It implements
// order.Repository (internal/order/ports.go) over the orders, order_history
// and outbox tables (D-053) with pgx, and applies the service's migrations.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"foc/order-service/internal/order"
)

// Postgres implements order.Repository. It is safe for concurrent use.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres returns a ready-to-use Postgres over pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

var _ order.Repository = (*Postgres)(nil)

// The state lists below are written out rather than passed as parameters so
// the planner can match them to the partial indexes in
// migrations/000001_create_orders.up.sql. They spell the order.State values.
const (
	// orderColumns is every orders column, in the order scanOrder reads them.
	orderColumns = `id, requester_id, courier_id, supplier_id, supplier_name,
		supplier_location, item_description, delivery_location, delivery_note, reward,
		state, version, request_key, created_at, updated_at, state_changed_at,
		expires_at, confirm_by`

	// AI-generated (edited by James Teo).
	// withdrawnCouriersColumn lists the couriers who withdrew from the errand,
	// oldest first, from its WITHDRAW history rows (D-060). It names the row
	// being read as "orders", so it goes only in a query on orders that does
	// not alias the table.
	withdrawnCouriersColumn = `ARRAY(SELECT h.courier_id FROM order_history h
			WHERE h.order_id = orders.id AND h.action = 'WITHDRAW' AND h.courier_id IS NOT NULL
			ORDER BY h.id)`

	// orderSelect is what every read of an errand selects: orderColumns, then
	// withdrawnCouriersColumn, in the order scanOrder reads them.
	orderSelect = orderColumns + `, ` + withdrawnCouriersColumn

	// historyColumns is every order_history column, in the order scanHistory reads them.
	historyColumns = `id, order_id, action, from_state, to_state, actor_id,
		actor_kind, courier_id, changes, occurred_at, correlation_id`

	// AI-generated (edited by James Teo): this read and every list below select
	// orderSelect, so each errand comes with its WithdrawnCouriers (D-060).
	getOrderSQL = `SELECT ` + orderSelect + ` FROM orders WHERE id = $1`

	findByRequestKeySQL = `SELECT ` + orderSelect + `
		FROM orders
		WHERE requester_id = $1 AND request_key = $2`

	historySQL = `SELECT ` + historyColumns + `
		FROM order_history
		WHERE order_id = $1
		ORDER BY id`

	listOpenSQL = `SELECT ` + orderSelect + `
		FROM orders
		WHERE state = 'OPEN' AND expires_at > $1
		ORDER BY created_at DESC
		LIMIT $2`

	listByRequesterSQL = `SELECT ` + orderSelect + `
		FROM orders
		WHERE requester_id = $1 AND state = ANY($2)
		ORDER BY state_changed_at DESC
		LIMIT $3`

	listByCourierSQL = `SELECT ` + orderSelect + `
		FROM orders
		WHERE courier_id = $1 AND state = ANY($2)
		ORDER BY state_changed_at DESC
		LIMIT $3`

	listActiveSQL = `SELECT ` + orderSelect + `
		FROM orders
		WHERE state IN ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED')
		ORDER BY state_changed_at
		LIMIT $1`

	countActiveBySupplierSQL = `SELECT count(*)
		FROM orders
		WHERE supplier_id = $1 AND state IN ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED')`

	// AI-generated (edited by James Teo).
	// countOpenByRequesterSQL counts the requester's errands that are OPEN
	// with a deadline after $2 (D-058); Create runs it too, under the
	// requester's lock.
	countOpenByRequesterSQL = `SELECT count(*)
		FROM orders
		WHERE requester_id = $1 AND state = 'OPEN' AND expires_at > $2`

	dueForExpirySQL = `SELECT id
		FROM orders
		WHERE state = 'OPEN' AND expires_at <= $1
		ORDER BY expires_at
		LIMIT $2`

	dueForAutoConfirmSQL = `SELECT id
		FROM orders
		WHERE state = 'DELIVERED' AND confirm_by <= $1
		ORDER BY confirm_by
		LIMIT $2`

	// assignmentHistorySQL reads a courier's ended assignments from the history
	// rows that name them (F3.5.3): a withdrawal is RELEASED whatever it led
	// to, otherwise the errand reached COMPLETED or CANCELLED while they held it.
	// The outcome strings are the order.AssignmentOutcome values.
	assignmentHistorySQL = `SELECT h.id, h.order_id,
			CASE
				WHEN h.action = 'WITHDRAW' THEN 'RELEASED'
				WHEN h.to_state = 'COMPLETED' THEN 'COMPLETED'
				ELSE 'CANCELLED'
			END,
			h.occurred_at, o.supplier_id, o.supplier_name, o.supplier_location,
			o.item_description, o.delivery_location, o.reward
		FROM order_history h
		JOIN orders o ON o.id = h.order_id
		WHERE h.courier_id = $1
			AND (h.action = 'WITHDRAW' OR h.to_state IN ('COMPLETED', 'CANCELLED'))
		ORDER BY h.occurred_at DESC, h.id DESC
		LIMIT $2`
)

// Get implements order.Repository.
func (p *Postgres) Get(ctx context.Context, id uuid.UUID) (order.Order, error) {
	o, err := scanOrder(p.pool.QueryRow(ctx, getOrderSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return order.Order{}, order.ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("get errand %s: %w", id, err)
	}
	return o, nil
}

// History implements order.Repository. An unknown id has no history, which is
// not an error.
func (p *Postgres) History(ctx context.Context, id uuid.UUID) ([]order.HistoryEntry, error) {
	rows, err := p.pool.Query(ctx, historySQL, id)
	if err != nil {
		return nil, fmt.Errorf("history of errand %s: %w", id, err)
	}
	entries, err := pgx.CollectRows(rows, scanHistory)
	if err != nil {
		return nil, fmt.Errorf("history of errand %s: %w", id, err)
	}
	return entries, nil
}

// FindByRequestKey implements order.Repository.
func (p *Postgres) FindByRequestKey(ctx context.Context, requesterID uuid.UUID, key string) (order.Order, error) {
	o, err := scanOrder(p.pool.QueryRow(ctx, findByRequestKeySQL, requesterID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return order.Order{}, order.ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("find errand by request key for requester %s: %w", requesterID, err)
	}
	return o, nil
}

// ListOpen implements order.Repository.
func (p *Postgres) ListOpen(ctx context.Context, now time.Time, limit int) ([]order.Order, error) {
	return p.listOrders(ctx, "list open errands", listOpenSQL, now, limitArg(limit))
}

// ListByRequester implements order.Repository.
func (p *Postgres) ListByRequester(ctx context.Context, requesterID uuid.UUID, states []order.State, limit int) ([]order.Order, error) {
	return p.listOrders(ctx, "list errands by requester", listByRequesterSQL, requesterID, stateNames(states), limitArg(limit))
}

// ListByCourier implements order.Repository.
func (p *Postgres) ListByCourier(ctx context.Context, courierID uuid.UUID, states []order.State, limit int) ([]order.Order, error) {
	return p.listOrders(ctx, "list errands by courier", listByCourierSQL, courierID, stateNames(states), limitArg(limit))
}

// ListActive implements order.Repository.
func (p *Postgres) ListActive(ctx context.Context, limit int) ([]order.Order, error) {
	return p.listOrders(ctx, "list active errands", listActiveSQL, limitArg(limit))
}

// ListAssignmentHistory implements order.Repository.
func (p *Postgres) ListAssignmentHistory(ctx context.Context, courierID uuid.UUID, limit int) ([]order.Assignment, error) {
	rows, err := p.pool.Query(ctx, assignmentHistorySQL, courierID, limitArg(limit))
	if err != nil {
		return nil, fmt.Errorf("list assignment history of courier %s: %w", courierID, err)
	}
	out, err := pgx.CollectRows(rows, scanAssignment)
	if err != nil {
		return nil, fmt.Errorf("list assignment history of courier %s: %w", courierID, err)
	}
	return out, nil
}

// CountActiveBySupplier implements order.Repository.
func (p *Postgres) CountActiveBySupplier(ctx context.Context, supplierID uuid.UUID) (int, error) {
	var n int
	if err := p.pool.QueryRow(ctx, countActiveBySupplierSQL, supplierID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active errands for supplier %s: %w", supplierID, err)
	}
	return n, nil
}

// AI-generated (edited by James Teo).
// CountOpenByRequester implements order.Repository.
func (p *Postgres) CountOpenByRequester(ctx context.Context, requesterID uuid.UUID, now time.Time) (int, error) {
	var n int
	if err := p.pool.QueryRow(ctx, countOpenByRequesterSQL, requesterID, now).Scan(&n); err != nil {
		return 0, fmt.Errorf("count open errands of requester %s: %w", requesterID, err)
	}
	return n, nil
}

// DueForExpiry implements order.Repository.
func (p *Postgres) DueForExpiry(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	return p.listIDs(ctx, "list errands due to expire", dueForExpirySQL, now, limitArg(limit))
}

// DueForAutoConfirm implements order.Repository.
func (p *Postgres) DueForAutoConfirm(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	return p.listIDs(ctx, "list errands due for auto-confirm", dueForAutoConfirmSQL, now, limitArg(limit))
}

func (p *Postgres) listOrders(ctx context.Context, what, query string, args ...any) ([]order.Order, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, scanOrderRow)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

func (p *Postgres) listIDs(ctx context.Context, what, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return ids, nil
}

// inTx runs fn in one READ COMMITTED transaction and commits if fn returns
// nil. READ COMMITTED is what the conditional writes rely on (D-046):
// a write that waited on a row lock re-checks its WHERE against the committed
// row and matches nothing, instead of failing with a serialization error as
// it would under REPEATABLE READ; and each statement after Accept's advisory
// lock sees what the previous holder committed. fn's error is returned
// unchanged, so sentinels and Amend's callback errors reach the caller as is.
func (p *Postgres) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// After a successful Commit this is a no-op returning pgx.ErrTxClosed.
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// AI-generated (edited by James Teo).
// scanOrder reads one row of orderSelect. Times come back in UTC, and an
// errand nobody withdrew from has nil WithdrawnCouriers.
func scanOrder(row pgx.Row) (order.Order, error) {
	var (
		o          order.Order
		state      string
		requestKey *string
	)
	err := row.Scan(
		&o.ID, &o.RequesterID, &o.CourierID, &o.Supplier.ID, &o.Supplier.Name,
		&o.Supplier.Location, &o.ItemDescription, &o.DeliveryLocation, &o.DeliveryNote, &o.Reward,
		&state, &o.Version, &requestKey, &o.CreatedAt, &o.UpdatedAt, &o.StateChangedAt,
		&o.ExpiresAt, &o.ConfirmBy, &o.WithdrawnCouriers,
	)
	if err != nil {
		return order.Order{}, err
	}
	if len(o.WithdrawnCouriers) == 0 {
		o.WithdrawnCouriers = nil
	}
	o.State = order.State(state)
	if requestKey != nil {
		o.RequestKey = *requestKey
	}
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	o.StateChangedAt = o.StateChangedAt.UTC()
	o.ExpiresAt = utcPtr(o.ExpiresAt)
	o.ConfirmBy = utcPtr(o.ConfirmBy)
	return o, nil
}

// scanOrderRow adapts scanOrder to pgx.CollectRows.
func scanOrderRow(row pgx.CollectableRow) (order.Order, error) {
	return scanOrder(row)
}

// scanHistory reads one row of historyColumns. A NULL from_state (CREATE) is
// the empty State, a NULL actor_id (the system) a nil ActorID, and NULL
// changes no changes.
func scanHistory(row pgx.CollectableRow) (order.HistoryEntry, error) {
	var (
		h                          order.HistoryEntry
		action, toState, actorKind string
		fromState                  *string
		changes                    []byte
	)
	err := row.Scan(
		&h.ID, &h.OrderID, &action, &fromState, &toState, &h.ActorID,
		&actorKind, &h.CourierID, &changes, &h.OccurredAt, &h.CorrelationID,
	)
	if err != nil {
		return order.HistoryEntry{}, err
	}
	h.Action = order.Action(action)
	if fromState != nil {
		h.FromState = order.State(*fromState)
	}
	h.ToState = order.State(toState)
	h.ActorKind = order.ActorKind(actorKind)
	h.OccurredAt = h.OccurredAt.UTC()
	if changes != nil {
		if err := json.Unmarshal(changes, &h.Changes); err != nil {
			return order.HistoryEntry{}, fmt.Errorf("decode changes of history row %d: %w", h.ID, err)
		}
	}
	return h, nil
}

// scanAssignment reads one row of assignmentHistorySQL.
func scanAssignment(row pgx.CollectableRow) (order.Assignment, error) {
	var (
		a       order.Assignment
		outcome string
	)
	err := row.Scan(
		&a.HistoryID, &a.OrderID, &outcome, &a.EndedAt, &a.Supplier.ID, &a.Supplier.Name,
		&a.Supplier.Location, &a.ItemDescription, &a.DeliveryLocation, &a.Reward,
	)
	if err != nil {
		return order.Assignment{}, err
	}
	a.Outcome = order.AssignmentOutcome(outcome)
	a.EndedAt = a.EndedAt.UTC()
	return a, nil
}

// limitArg turns a list limit into a LIMIT parameter. As in the in-memory
// fake (ordertest), zero or less means no limit, which LIMIT NULL gives.
func limitArg(limit int) any {
	if limit <= 0 {
		return nil
	}
	return limit
}

// stateNames converts states to the text[] that state = ANY($n) compares with.
func stateNames(states []order.State) []string {
	out := make([]string, len(states))
	for i, s := range states {
		out[i] = string(s)
	}
	return out
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
