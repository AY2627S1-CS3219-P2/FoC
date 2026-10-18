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
	EventID     uuid.UUID  `json:"eventId"`
	EventType   string     `json:"eventType"`
	ErrandID    uuid.UUID  `json:"errandId"`
	RequesterID uuid.UUID  `json:"requesterId"`
	Amount      int        `json:"amount"`
	OccurredAt  time.Time  `json:"occurredAt"`
	CourierID   *uuid.UUID `json:"courierId,omitempty"`
	// CorrelationID ties the event to the request or sweep that caused it
	// (NFR7.1, D-047).
	CorrelationID string `json:"correlationId"`
	// Version is the errand's version, on errand.reward_set only: credit-service
	// ignores a reward_set older than one it has applied (D-057).
	Version *int `json:"version,omitempty"`
}

// newEvent builds an outbox event for o. occurredAt is the transition time;
// the relay may publish later.
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

// rewardSetEvent builds D-057's errand.reward_set for o as it stands: credit
// sets its hold to o's reward unless it has applied a later version.
func rewardSetEvent(o Order, occurredAt time.Time, correlationID string) *OutboxEvent {
	e := newEvent(uuid.New(), RoutingRewardSet, o, nil, occurredAt, correlationID)
	e.Payload.Version = &o.Version
	return e
}
