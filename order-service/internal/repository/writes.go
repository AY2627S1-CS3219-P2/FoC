// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: order.Repository's writes: the queued refund and create (D-047),
//   conditional transitions with history and outbox rows (D-046,
//   D-048, D-051), accept with the per-courier lock (D-046) and the row-locked
//   amend and reward_set (D-057).
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"foc/order-service/internal/order"
)

// courierLockClass is the first key of Accept's per-courier advisory lock,
// pg_advisory_xact_lock(courierLockClass, hashtext(courier id)) (D-046). Any
// fixed value works: order-service owns its database, and no other code takes
// a two-key advisory lock in it. A hash collision between two couriers only
// makes their accepts wait for each other; it cannot break K.
const courierLockClass int32 = 8083

// AI-generated (edited by James Teo).
// requesterLockClass is the first key of Create's per-requester advisory lock,
// pg_advisory_xact_lock(requesterLockClass, hashtext(requester id)) (D-058).
// It differs from courierLockClass, so a student's creates never wait on their
// own accepts. A hash collision only makes two requesters' creates wait for
// each other; it cannot break the limit.
const requesterLockClass int32 = 8183

// Constraint and error names the writes map to domain errors.
const (
	// requestKeyIndex is the unique index on (requester_id, request_key) (D-047).
	requestKeyIndex = "orders_requester_request_key"
	// uniqueViolation is PostgreSQL's SQLSTATE for unique_violation.
	uniqueViolation = "23505"
)

const (
	insertOrderSQL = `INSERT INTO orders (` + orderColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			$19, $20, $21)`

	insertHistorySQL = `INSERT INTO order_history (order_id, action, from_state, to_state,
			actor_id, actor_kind, courier_id, changes, occurred_at, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	insertEventSQL = `INSERT INTO outbox (event_id, routing_key, payload, correlation_id, available_at)
		VALUES ($1, $2, $3, $4, $5)`

	// deleteGuardSQL removes D-047's queued refund, but only while the relay
	// has not published it.
	deleteGuardSQL = `DELETE FROM outbox WHERE event_id = $1 AND published_at IS NULL`

	// transitionSQL is D-046's conditional write. Each NULL expectation is
	// "not checked"; a NULL column never satisfies a non-NULL one.
	// AI-generated (edited by James Teo): it returns orderSelect (D-060).
	transitionSQL = `UPDATE orders
		SET state = $2, courier_id = $3, expires_at = $4, confirm_by = $5,
			state_changed_at = $6, updated_at = $6, version = version + 1
		WHERE id = $1
			AND state = $7
			AND ($8::uuid IS NULL OR requester_id = $8)
			AND ($9::uuid IS NULL OR courier_id = $9)
			AND ($10::timestamptz IS NULL OR expires_at > $10)
			AND ($11::timestamptz IS NULL OR expires_at <= $11)
			AND ($12::timestamptz IS NULL OR confirm_by <= $12)
			AND ($13::int IS NULL OR version = $13)
		RETURNING ` + orderSelect

	lockCourierSQL = `SELECT pg_advisory_xact_lock($1, hashtext($2::uuid::text))`

	// AI-generated (edited by James Teo).
	// lockRequesterSQL is lockCourierSQL's lock for one requester's creates.
	lockRequesterSQL = lockCourierSQL

	// countHeldSQL counts the courier's active assignments for K (F3.3.4):
	// DELIVERED no longer counts.
	countHeldSQL = `SELECT count(*)
		FROM orders
		WHERE courier_id = $1 AND state IN ('ACCEPTED', 'PICKED_UP')`

	// AI-generated (edited by James Teo).
	// acceptSQL is D-046's single conditional write. It also refuses a courier
	// whose withdrawal from the errand is in its history (D-060).
	acceptSQL = `UPDATE orders
		SET state = 'ACCEPTED', courier_id = $2, expires_at = NULL,
			state_changed_at = $3, updated_at = $3, version = version + 1
		WHERE id = $1
			AND state = 'OPEN'
			AND expires_at > $3
			AND requester_id <> $2
			AND version = $4
			AND NOT EXISTS (SELECT 1 FROM order_history h
				WHERE h.order_id = orders.id AND h.action = 'WITHDRAW' AND h.courier_id = $2)
		RETURNING ` + orderSelect

	lockOrderSQL = getOrderSQL + ` FOR UPDATE`

	// amendSQL writes the columns an amend may change (F3.7.1, D-052); the
	// state, people and deadlines are not among them (F3.7.6).
	// AI-generated (edited by James Teo): it returns orderSelect (D-060).
	amendSQL = `UPDATE orders
		SET supplier_id = $2, supplier_name = $3, supplier_location = $4,
			item_description = $5, delivery_location = $6, delivery_note = $7,
			reward = $8, version = $9, updated_at = $10,
			supplier_type = $11, supplier_lat = $12, supplier_lon = $13
		WHERE id = $1
		RETURNING ` + orderSelect
)

// QueueEvent implements order.Repository. A single INSERT is its own
// transaction.
func (p *Postgres) QueueEvent(ctx context.Context, e order.OutboxEvent) error {
	return insertEvent(ctx, p.pool, e)
}

// Create implements order.Repository. Deleting the guard first means a create
// whose refund was already published writes nothing (D-047); a duplicate
// request key rolls the guard's delete back, so that attempt's reservation is
// still refunded.
//
// AI-generated (edited by James Teo).
// The requester's advisory lock serialises their creates, so the count after
// the insert always includes every create that held the lock before (D-058).
// The insert comes before the count so that a twin with the same request key
// is still reported as ErrDuplicateRequestKey and replayed, not refused for
// the limit its own first attempt filled. Going over the limit rolls back the
// guard's delete too.
func (p *Postgres) Create(ctx context.Context, o order.Order, h order.HistoryEntry, guardEventID uuid.UUID) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, deleteGuardSQL, guardEventID)
		if err != nil {
			return fmt.Errorf("delete queued refund %s: %w", guardEventID, err)
		}
		if tag.RowsAffected() != 1 {
			return order.ErrGuardGone
		}
		if _, err := tx.Exec(ctx, lockRequesterSQL, requesterLockClass, o.RequesterID); err != nil {
			return fmt.Errorf("lock requester %s: %w", o.RequesterID, err)
		}
		if _, err := tx.Exec(ctx, insertOrderSQL, orderArgs(o)...); err != nil {
			if isUniqueViolation(err, requestKeyIndex) {
				return order.ErrDuplicateRequestKey
			}
			return fmt.Errorf("insert errand %s: %w", o.ID, err)
		}
		var open int
		if err := tx.QueryRow(ctx, countOpenByRequesterSQL, o.RequesterID, o.CreatedAt).Scan(&open); err != nil {
			return fmt.Errorf("count open errands of requester %s: %w", o.RequesterID, err)
		}
		if open > order.MaxOpenErrands {
			return order.ErrTooManyOpen
		}
		return insertHistory(ctx, tx, o.ID, h)
	})
}

// Transition implements order.Repository.
func (p *Postgres) Transition(ctx context.Context, t order.Transition) (order.Order, error) {
	var updated order.Order
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		o, err := scanOrder(tx.QueryRow(ctx, transitionSQL,
			t.OrderID, string(t.ToState), t.CourierID, t.ExpiresAt, t.ConfirmBy, t.Now,
			string(t.FromState), t.ExpectRequesterID, t.ExpectCourierID,
			t.OpenBefore, t.ExpiryDueBy, t.ConfirmDueBy, t.ExpectVersion,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return order.ErrNoMatch
		}
		if err != nil {
			return fmt.Errorf("move errand %s from %s to %s: %w", t.OrderID, t.FromState, t.ToState, err)
		}
		if err := insertHistory(ctx, tx, o.ID, t.History); err != nil {
			return err
		}
		if t.Event != nil {
			if err := insertEvent(ctx, tx, *t.Event); err != nil {
				return err
			}
		}
		// AI-generated (edited by James Teo).
		// RETURNING ran before this transaction's history row, so a
		// withdrawal's own courier is added here, as a read now would (D-060).
		if t.History.Action == order.ActionWithdraw && t.History.CourierID != nil {
			o.WithdrawnCouriers = append(o.WithdrawnCouriers, *t.History.CourierID)
		}
		updated = o
		return nil
	})
	if err != nil {
		return order.Order{}, err
	}
	return updated, nil
}

// Accept implements order.Repository. The advisory lock serialises one
// courier's accepts, so the count below always includes the accept that held
// the lock before; accepts by different couriers do not wait for each other.
func (p *Postgres) Accept(ctx context.Context, c order.AcceptCommand) (order.Order, error) {
	var accepted order.Order
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockCourierSQL, courierLockClass, c.CourierID); err != nil {
			return fmt.Errorf("lock courier %s: %w", c.CourierID, err)
		}
		var held int
		if err := tx.QueryRow(ctx, countHeldSQL, c.CourierID).Scan(&held); err != nil {
			return fmt.Errorf("count errands held by courier %s: %w", c.CourierID, err)
		}
		if held >= c.MaxActive {
			return order.ErrCourierAtCapacity
		}
		o, err := scanOrder(tx.QueryRow(ctx, acceptSQL, c.OrderID, c.CourierID, c.Now, c.ExpectedVersion))
		if errors.Is(err, pgx.ErrNoRows) {
			return order.ErrNoMatch
		}
		if err != nil {
			return fmt.Errorf("accept errand %s: %w", c.OrderID, err)
		}
		if err := insertHistory(ctx, tx, o.ID, c.History); err != nil {
			return err
		}
		accepted = o
		return nil
	})
	if err != nil {
		return order.Order{}, err
	}
	return accepted, nil
}

// Amend implements order.Repository. fn runs while this transaction holds the
// row lock, so a concurrent write to the errand waits for the commit (D-057).
// The errand returned is the row as written.
func (p *Postgres) Amend(ctx context.Context, id uuid.UUID, fn func(current order.Order) (*order.AmendResult, error)) (order.Order, error) {
	var out order.Order
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := lockOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		res, err := fn(cur)
		if err != nil {
			return err
		}
		if res == nil {
			out = cur
			return nil
		}
		next := res.Order
		o, err := scanOrder(tx.QueryRow(ctx, amendSQL, id,
			next.Supplier.ID, next.Supplier.Name, next.Supplier.Location,
			next.ItemDescription, next.DeliveryLocation, next.DeliveryNote,
			next.Reward, next.Version, next.UpdatedAt,
			next.Supplier.Type, supplierLat(next.Supplier), supplierLon(next.Supplier),
		))
		if errors.Is(err, pgx.ErrNoRows) {
			// Unreachable while the row lock is held; mapped so pgx's error never escapes.
			return order.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("amend errand %s: %w", id, err)
		}
		if err := insertHistory(ctx, tx, id, res.History); err != nil {
			return err
		}
		if res.Event != nil {
			if err := insertEvent(ctx, tx, *res.Event); err != nil {
				return err
			}
		}
		out = o
		return nil
	})
	if err != nil {
		return order.Order{}, err
	}
	return out, nil
}

// AI-generated (edited by James Teo).
// QueueLocked implements order.Repository. The row lock is held until the
// event is inserted, so a concurrent amend's reward_set is ordered after this
// one (D-057).
func (p *Postgres) QueueLocked(ctx context.Context, id uuid.UUID, fn func(current order.Order) (*order.OutboxEvent, error)) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := lockOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		e, err := fn(cur)
		if err != nil {
			return err
		}
		// AI-generated (edited by James Teo).
		if e == nil {
			return nil
		}
		return insertEvent(ctx, tx, *e)
	})
}

// lockOrder reads the errand and holds its row lock until tx ends.
func lockOrder(ctx context.Context, tx pgx.Tx, id uuid.UUID) (order.Order, error) {
	o, err := scanOrder(tx.QueryRow(ctx, lockOrderSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return order.Order{}, order.ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("lock errand %s: %w", id, err)
	}
	return o, nil
}

// insertHistory writes h as the history row of the errand this transaction
// changed, so the row is filed under orderID whatever h.OrderID holds.
func insertHistory(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, h order.HistoryEntry) error {
	changes, err := changesArg(h.Changes)
	if err != nil {
		return fmt.Errorf("history of errand %s: %w", orderID, err)
	}
	_, err = tx.Exec(ctx, insertHistorySQL,
		orderID, string(h.Action), nullableState(h.FromState), string(h.ToState),
		h.ActorID, string(h.ActorKind), h.CourierID, changes, h.OccurredAt, h.CorrelationID,
	)
	if err != nil {
		return fmt.Errorf("insert history of errand %s: %w", orderID, err)
	}
	return nil
}

// execer is what insertEvent needs from a pool or a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertEvent(ctx context.Context, db execer, e order.OutboxEvent) error {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("encode event %s: %w", e.EventID, err)
	}
	_, err = db.Exec(ctx, insertEventSQL, e.EventID, e.RoutingKey, string(payload), e.CorrelationID, e.AvailableAt)
	if err != nil {
		return fmt.Errorf("insert event %s (%s): %w", e.EventID, e.RoutingKey, err)
	}
	return nil
}

// orderArgs lists o's values in orderColumns order.
func orderArgs(o order.Order) []any {
	return []any{
		o.ID, o.RequesterID, o.CourierID, o.Supplier.ID, o.Supplier.Name,
		o.Supplier.Location, o.ItemDescription, o.DeliveryLocation, o.DeliveryNote, o.Reward,
		string(o.State), o.Version, nullableText(o.RequestKey), o.CreatedAt, o.UpdatedAt, o.StateChangedAt,
		o.ExpiresAt, o.ConfirmBy, o.Supplier.Type, supplierLat(o.Supplier), supplierLon(o.Supplier),
	}
}

// AI-generated (edited by PENDING): the supplier position's halves, NULL when
// there is none (D-089).
func supplierLat(s order.SupplierSnapshot) *float64 {
	if s.Point == nil {
		return nil
	}
	return &s.Point.Lat
}

func supplierLon(s order.SupplierSnapshot) *float64 {
	if s.Point == nil {
		return nil
	}
	return &s.Point.Lon
}

// isUniqueViolation reports whether err is PostgreSQL rejecting a duplicate
// in the named unique index or constraint.
func isUniqueViolation(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == name
}

// nullableText stores "" as NULL. For request_key this matters: the unique
// index skips NULLs, so errands created without an X-Request-Id never collide
// with each other (D-047).
func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableState stores the empty State as NULL: a CREATE row has no from_state.
func nullableState(s order.State) *string {
	return nullableText(string(s))
}

// changesArg encodes an entry's changed fields for the JSONB column, or NULL
// when there are none.
func changesArg(changes []order.FieldChange) (any, error) {
	if len(changes) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(changes)
	if err != nil {
		return nil, fmt.Errorf("encode changes: %w", err)
	}
	return string(b), nil
}
