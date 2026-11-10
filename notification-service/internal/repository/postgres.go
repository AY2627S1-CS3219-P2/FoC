// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The PostgreSQL adapter for notification.Repository (F5.1, F5.5.2,
//   D-146).
// Author review: nigeltzy - Checked that the implementation is as expected and seems to be correct.

// Package repository is the PostgreSQL adapter for notification.Repository.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"foc/notification-service/internal/notification"
)

const (
	// An older event arriving late must not undo a newer one, so the courier
	// only changes for an event at least as new as the last one applied.
	upsertCourierSQL = `INSERT INTO errand_couriers (errand_id, courier_id, changed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (errand_id) DO UPDATE
		SET courier_id = EXCLUDED.courier_id, changed_at = EXCLUDED.changed_at
		WHERE errand_couriers.changed_at <= EXCLUDED.changed_at`

	insertNotificationSQL = `INSERT INTO notifications (id, recipient_id, errand_id, event_id, state_change, message)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT ON CONSTRAINT notifications_one_per_recipient_per_event DO NOTHING`

	currentCourierSQL = `SELECT courier_id FROM errand_couriers WHERE errand_id = $1`

	listSQL = `SELECT id, recipient_id, errand_id, event_id, state_change, message, is_read, created_at
		FROM notifications
		WHERE recipient_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`

	countSQL = `SELECT count(*) FROM notifications WHERE recipient_id = $1`

	markReadSQL = `UPDATE notifications SET is_read = true WHERE id = $1 AND recipient_id = $2`
)

// Postgres is notification.Repository on a pgx pool.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres returns a Postgres using pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// Apply stores one event's outcome in a single transaction.
func (p *Postgres) Apply(ctx context.Context, change *notification.CourierChange, notifications []notification.Notification) error {
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if change != nil {
			if _, err := tx.Exec(ctx, upsertCourierSQL, change.ErrandID, change.CourierID, change.ChangedAt); err != nil {
				return fmt.Errorf("record courier: %w", err)
			}
		}
		for _, n := range notifications {
			if _, err := tx.Exec(ctx, insertNotificationSQL,
				n.ID, n.RecipientID, n.ErrandID, n.EventID, n.StateChange, n.Message); err != nil {
				return fmt.Errorf("insert notification: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply event: %w", err)
	}
	return nil
}

// CurrentCourier returns the errand's courier, or nil when it has none or none
// was ever seen.
func (p *Postgres) CurrentCourier(ctx context.Context, errandID uuid.UUID) (*uuid.UUID, error) {
	var courier *uuid.UUID
	err := p.pool.QueryRow(ctx, currentCourierSQL, errandID).Scan(&courier)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read courier: %w", err)
	}
	return courier, nil
}

// List returns a page of recipientID's notifications, newest first.
func (p *Postgres) List(ctx context.Context, recipientID uuid.UUID, limit, offset int) (notification.Page, error) {
	rows, err := p.pool.Query(ctx, listSQL, recipientID, limit, offset)
	if err != nil {
		return notification.Page{}, fmt.Errorf("query notifications: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (notification.Notification, error) {
		var n notification.Notification
		err := row.Scan(&n.ID, &n.RecipientID, &n.ErrandID, &n.EventID, &n.StateChange, &n.Message, &n.Read, &n.CreatedAt)
		return n, err
	})
	if err != nil {
		return notification.Page{}, fmt.Errorf("read notifications: %w", err)
	}
	var total int
	if err := p.pool.QueryRow(ctx, countSQL, recipientID).Scan(&total); err != nil {
		return notification.Page{}, fmt.Errorf("count notifications: %w", err)
	}
	return notification.Page{Notifications: found, Total: total}, nil
}

// MarkRead marks one of recipientID's notifications as read.
func (p *Postgres) MarkRead(ctx context.Context, recipientID, id uuid.UUID) error {
	tag, err := p.pool.Exec(ctx, markReadSQL, id, recipientID)
	if err != nil {
		return fmt.Errorf("update notification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return notification.ErrNotFound
	}
	return nil
}
