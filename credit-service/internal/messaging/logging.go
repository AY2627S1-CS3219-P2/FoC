// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The fields every consumer line about an event carries and the INFO
//   line written once an event is acknowledged (D-135).
// Author review: James Teo — reviewed and verified.

package messaging

import (
	"encoding/json"
	"log/slog"

	"foc/credit-service/internal/observability"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// deliveryLogger scopes log to one delivery: the request id it carries as its
// AMQP CorrelationId, and its routing key.
func deliveryLogger(log *slog.Logger, d amqp.Delivery) *slog.Logger {
	return log.With("request_id", d.CorrelationId, "routingKey", d.RoutingKey)
}

// withEventIDs adds whichever of eventId, errandId, userId and version body
// holds as valid values, read leniently so a line about a malformed event still
// names what it can. It decodes the body, so it runs only once the delivery has
// passed transport validation.
func withEventIDs(log *slog.Logger, body []byte) *slog.Logger {
	var ids struct {
		EventID  uuid.UUID `json:"eventId"`
		ErrandID uuid.UUID `json:"errandId"`
		UserID   uuid.UUID `json:"userId"`
		Version  *int64    `json:"version"`
	}
	_ = json.Unmarshal(body, &ids)

	var attrs []any
	if ids.EventID != uuid.Nil {
		attrs = append(attrs, "eventId", ids.EventID)
	}
	if ids.ErrandID != uuid.Nil {
		attrs = append(attrs, "errandId", ids.ErrandID)
	}
	if ids.UserID != uuid.Nil {
		attrs = append(attrs, "userId", ids.UserID)
	}
	if ids.Version != nil {
		attrs = append(attrs, "version", *ids.Version)
	}
	return log.With(attrs...)
}

// acknowledge acks d and writes the event's one INFO line once the broker has
// taken the ack.
func acknowledge(log *slog.Logger, d amqp.Delivery, msg string) {
	if err := d.Ack(false); err != nil {
		log.Warn("could not acknowledge event; the broker will redeliver it", "error", observability.Sanitize(err.Error()))
		return
	}
	log.Info(msg)
}
