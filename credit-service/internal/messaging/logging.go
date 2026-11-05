// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The fields every consumer line about an event carries and the INFO
//   line written once an event is acknowledged (D-135).
// Author review: James Teo — reviewed and verified.

package messaging

import (
	"context"
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

type requestIDKey struct{}

// contextWithRequestID carries a delivery's request id through the EventRouter
// to the errand handlers, which receive only a context, routing key and body.
func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// requestLogger scopes log to the request id ctx carries, if any.
func requestLogger(ctx context.Context, log *slog.Logger) *slog.Logger {
	if requestID, ok := ctx.Value(requestIDKey{}).(string); ok {
		return log.With("request_id", requestID)
	}
	return log
}

// withEventIDs adds whichever of eventId, errandId, userId and version body
// holds as valid values, so a line about a malformed event still names what it
// can. Each id is parsed on its own: decoding straight into uuid.UUID would
// stop at the first malformed one and drop the rest. It decodes the body, so
// it runs only once the delivery has passed transport validation.
func withEventIDs(log *slog.Logger, body []byte) *slog.Logger {
	var ids struct {
		EventID  json.RawMessage `json:"eventId"`
		ErrandID json.RawMessage `json:"errandId"`
		UserID   json.RawMessage `json:"userId"`
		Version  *int64          `json:"version"`
	}
	_ = json.Unmarshal(body, &ids)

	var attrs []any
	for _, field := range []struct {
		key string
		raw json.RawMessage
	}{{"eventId", ids.EventID}, {"errandId", ids.ErrandID}, {"userId", ids.UserID}} {
		if id, ok := parseID(field.raw); ok {
			attrs = append(attrs, field.key, id)
		}
	}
	if ids.Version != nil {
		attrs = append(attrs, "version", *ids.Version)
	}
	return log.With(attrs...)
}

// parseID reports the non-nil UUID raw holds as a JSON string, if it holds one.
func parseID(raw json.RawMessage) (uuid.UUID, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
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
