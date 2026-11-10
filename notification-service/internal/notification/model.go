// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The domain types: a stored notification (F5.1.1, D-146), the lifecycle
//   event it comes from (D-051, D-142) and the errors callers distinguish.
// Author review: nigeltzy - Checked that the implementation is as expected and seems to be correct.

// Package notification decides who hears about each order lifecycle event and
// serves a user's own notifications. It imports no HTTP, no SQL and no broker
// client (root AGENTS.md §6).
package notification

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// The routing keys this service is bound to (D-141, D-143).
const (
	EventAccepted  = "errand.accepted"
	EventPickedUp  = "errand.picked_up"
	EventDelivered = "errand.delivered"
	EventWithdrawn = "errand.withdrawn"
	EventCompleted = "errand.completed"
	EventCancelled = "errand.cancelled"
	EventExpired   = "errand.expired"
)

var (
	// ErrInvalidEvent marks an event that retrying can never fix, so it goes
	// straight to the dead-letter queue.
	ErrInvalidEvent = errors.New("invalid lifecycle event")
	// ErrNotFound means the caller has no such notification. Someone else's
	// notification answers the same way (F5.1.2).
	ErrNotFound = errors.New("notification not found")
	// ErrInvalidPage means a limit or offset is out of range.
	ErrInvalidPage = errors.New("invalid page")
)

// Event is the part of an order lifecycle event this service reads. A small
// copy of order-service's payload, kept here on purpose (root AGENTS.md §4
// rule 2).
type Event struct {
	ID          uuid.UUID
	Type        string
	ErrandID    uuid.UUID
	RequesterID uuid.UUID
	CourierID   *uuid.UUID
	Amount      int
	OccurredAt  time.Time
}

// Notification is one stored notification (F5.1.1).
type Notification struct {
	ID          uuid.UUID
	RecipientID uuid.UUID
	ErrandID    uuid.UUID
	EventID     uuid.UUID
	StateChange string
	Message     string
	Read        bool
	CreatedAt   time.Time
}

// CourierChange is an event's effect on who the errand's courier is. CourierID
// is nil once the courier has withdrawn.
type CourierChange struct {
	ErrandID  uuid.UUID
	CourierID *uuid.UUID
	ChangedAt time.Time
}

// Page is one page of a user's notifications, and how many they have in all.
type Page struct {
	Notifications []Notification
	Total         int
}
