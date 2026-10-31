// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Lifecycle event routing keys and payload (D-048, D-051), and the
//   reward_set event that brings credit's hold in line (D-057).
// Author review: James Teo — reviewed and verified.

package order

import (
	"time"

	"github.com/google/uuid"
)

// The only routing keys order-service publishes: the four lifecycle keys bound
// to credit-service's queue (D-051), and errand.reward_set (D-057).
const (
	RoutingCompleted = "errand.completed"
	RoutingCancelled = "errand.cancelled"
	RoutingExpired   = "errand.expired"
	RoutingFailed    = "errand.failed"
	RoutingRewardSet = "errand.reward_set"
)

// EventPayload is the JSON body of a lifecycle event: credit-service's
// documented fields plus eventType and correlationId (D-051).
type EventPayload struct {
	EventID     uuid.UUID `json:"eventId"`
	EventType   string    `json:"eventType"`
	ErrandID    uuid.UUID `json:"errandId"`
	RequesterID uuid.UUID `json:"requesterId"`
	// AI-generated (edited by James Teo).
	// Amount is the errand's reward: what a settlement event settles (D-059),
	// or what errand.reward_set lowers the hold to (D-102).
	Amount     int        `json:"amount"`
	OccurredAt time.Time  `json:"occurredAt"`
	CourierID  *uuid.UUID `json:"courierId,omitempty"`
	// CorrelationID ties the event to the request or sweep that caused it
	// (NFR7.1, D-047).
	CorrelationID string `json:"correlationId"`
	// Version is the errand's version, on errand.reward_set only: credit-service
	// ignores a reward_set older than the version it last recorded (D-102).
	Version *int `json:"version,omitempty"`
}

// newEvent builds an outbox event for o. occurredAt is the transition time;
// the relay may publish later.
//
// AI-generated (edited by James Teo).
// amount is o's reward. On errand.completed, errand.cancelled,
// errand.expired and errand.failed it is the reward at that transition and is
// what settles: credit-service pays or refunds amount, not the hold it has,
// and returns any hold left over to the requester (D-059).
func newEvent(eventID uuid.UUID, routingKey string, o Order, courierID *uuid.UUID, occurredAt time.Time, correlationID string) *OutboxEvent {
	return &OutboxEvent{
		EventID:    eventID,
		RoutingKey: routingKey,
		Payload: EventPayload{
			EventID:       eventID,
			EventType:     routingKey,
			ErrandID:      o.ID,
			RequesterID:   o.RequesterID,
			Amount:        o.Reward,
			OccurredAt:    occurredAt,
			CourierID:     courierID,
			CorrelationID: correlationID,
		},
		CorrelationID: correlationID,
		AvailableAt:   occurredAt,
	}
}

// rewardSetEvent builds the errand.reward_set for o as it stands: credit lowers
// its hold to o's reward if it holds more, unless it has recorded a later
// version (D-102).
func rewardSetEvent(o Order, occurredAt time.Time, correlationID string) *OutboxEvent {
	e := newEvent(uuid.New(), RoutingRewardSet, o, nil, occurredAt, correlationID)
	e.Payload.Version = &o.Version
	return e
}
