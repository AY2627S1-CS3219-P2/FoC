// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: The user.registered event that account activation queues for
//   credit-service: its routing key and JSON payload.
// Author review: James Teo — reviewed and verified.

package user

import (
	"time"

	"github.com/google/uuid"
)

// RoutingUserRegistered is the routing key of the event queued when an
// account is activated. RabbitMQ binds it to credit-service's
// credit.registration queue on the foc.events exchange.
const RoutingUserRegistered = "user.registered"

// UserRegisteredEvent is the JSON body of a user.registered event:
// credit-service's required fields (eventId, userId, occurredAt) plus
// eventType, which order-service's events also carry.
type UserRegisteredEvent struct {
	// EventID is also the outbox row's key and the message id, so a consumer
	// can drop a redelivery.
	EventID   uuid.UUID `json:"eventId"`
	EventType string    `json:"eventType"`
	// UserID is the activated account's uid.
	UserID uuid.UUID `json:"userId"`
	// OccurredAt is when the account was activated, in UTC. The relay may
	// publish the event later.
	OccurredAt time.Time `json:"occurredAt"`
}

// NewUserRegisteredEvent returns the user.registered event for the account
// userID, activated at activatedAt.
func NewUserRegisteredEvent(eventID, userID uuid.UUID, activatedAt time.Time) UserRegisteredEvent {
	return UserRegisteredEvent{
		EventID:    eventID,
		EventType:  RoutingUserRegistered,
		UserID:     userID,
		OccurredAt: activatedAt.UTC(),
	}
}
