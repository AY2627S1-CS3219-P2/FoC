// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The repository interface the domain depends on (root AGENTS.md §6).
// Author review: nigeltzy - Checked that the implementation is as expected and seems to be correct.

package notification

import (
	"context"

	"github.com/google/uuid"
)

// Repository is what the service needs from storage.
type Repository interface {
	// Apply stores one event's outcome in a single transaction: the courier
	// change, if any, and each notification. A notification whose event and
	// recipient are already stored is skipped (F5.5.2).
	Apply(ctx context.Context, change *CourierChange, notifications []Notification) error
	// CurrentCourier returns the errand's courier, or nil when it has none or
	// this service has not seen one.
	CurrentCourier(ctx context.Context, errandID uuid.UUID) (*uuid.UUID, error)
	// List returns a page of recipientID's notifications, newest first.
	List(ctx context.Context, recipientID uuid.UUID, limit, offset int) (Page, error)
	// MarkRead marks one of recipientID's notifications as read, returning
	// ErrNotFound when it has no such notification.
	MarkRead(ctx context.Context, recipientID, id uuid.UUID) error
}
