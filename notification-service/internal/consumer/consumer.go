// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The RabbitMQ consumer on notification-service's own queue (D-140,
//   D-143). Structure follows chat-service/internal/consumer.
//   2026-11-10 (model: Fable 5): request_id, eventId and errandId on every
//   event line, an INFO line after each ack and an ERROR line when an event is
//   dead-lettered (D-135).
// Author review: PENDING — <reviewer to complete>

// Package consumer reads order lifecycle events from notification-service's own
// queue. It only ever reads: nothing here publishes, and no other service knows
// this consumer exists (D-140).
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"foc/notification-service/internal/notification"
)

const (
	// Queue is declared in rabbitmq/definitions.json (D-143).
	Queue       = "notification.errands"
	consumerTag = "notification-service.errands"
	prefetch    = 10

	// deliveryLimit is the queue's x-delivery-limit in rabbitmq/definitions.json;
	// consumer_test.go checks the two agree. The broker dead-letters an event
	// that is put back on the queue once its x-delivery-count has reached this.
	deliveryLimit = 5
	// headerDeliveryCount is how many times the broker has redelivered an
	// event. A quorum queue leaves it out of the first delivery.
	headerDeliveryCount = "x-delivery-count"

	// reconnectDelay is the pause before dialling the broker again.
	reconnectDelay = 5 * time.Second
	// retryDelay is the pause before a failed event goes back on the queue.
	// Without it, a short database outage would spend the queue's delivery
	// limit in moments and dead-letter events that only needed to wait.
	retryDelay = 2 * time.Second
)

// EventHandler stores what an event means. *notification.Service satisfies it.
type EventHandler interface {
	Handle(ctx context.Context, e notification.Event) error
}

// Consumer delivers the queue's events to a handler.
type Consumer struct {
	url        string
	handler    EventHandler
	log        *slog.Logger
	retryDelay time.Duration
}

// New returns a Consumer reading from the broker at url.
func New(url string, handler EventHandler, log *slog.Logger) *Consumer {
	return &Consumer{url: url, handler: handler, log: log, retryDelay: retryDelay}
}

// Run consumes until ctx ends, reconnecting whenever the broker or queue goes
// away, so the broker need not be up before this service (root AGENTS.md §5).
func (c *Consumer) Run(ctx context.Context) {
	for {
		err := c.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		c.log.WarnContext(ctx, "broker connection lost; will reconnect", "err", err)
		if !wait(ctx, reconnectDelay) {
			return
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("dial broker: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	if err := ch.Qos(prefetch, 0, false); err != nil {
		return fmt.Errorf("set prefetch: %w", err)
	}
	deliveries, err := ch.ConsumeWithContext(ctx, Queue, consumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", Queue, err)
	}
	c.log.InfoContext(ctx, "consuming events", "queue", Queue)

	for d := range deliveries {
		if err := c.settle(ctx, d, d); err != nil {
			return fmt.Errorf("settle delivery: %w", err)
		}
	}
	return errors.New("deliveries channel closed")
}

// settler is the part of amqp.Delivery that answers the broker.
type settler interface {
	Ack(multiple bool) error
	Nack(multiple, requeue bool) error
}

// settle handles one delivery and answers the broker. A success, or a type this
// service ignores, is acked. An event no retry can fix is dead-lettered at once.
// Anything else goes back on the queue, and the queue's delivery limit
// dead-letters it if it keeps failing (F6.2).
//
// Every line about the event carries its request id, routing key, eventId and
// errandId (D-135): INFO once it is acknowledged, WARN when it will be tried
// again, ERROR when it is dead-lettered.
func (c *Consumer) settle(ctx context.Context, d amqp.Delivery, s settler) error {
	log := c.log.With(eventAttrs(d)...)
	err := c.process(ctx, d.RoutingKey, d.Body)
	switch {
	case err == nil:
		if err := s.Ack(false); err != nil {
			log.WarnContext(ctx, "could not acknowledge the event; the broker will redeliver it", "err", err)
			return err
		}
		log.InfoContext(ctx, "event acknowledged")
		return nil
	case errors.Is(err, notification.ErrInvalidEvent):
		log.ErrorContext(ctx, "dead-lettering an unusable event", "err", err)
		return s.Nack(false, false)
	case deliveryCount(d) >= deliveryLimit:
		// Still put back on the queue: it is the broker that dead-letters it,
		// because this was its last delivery.
		log.ErrorContext(ctx, "event failed on its last delivery; the broker dead-letters it",
			"deliveryCount", deliveryCount(d), "err", err)
		wait(ctx, c.retryDelay)
		return s.Nack(false, true)
	default:
		log.WarnContext(ctx, "event failed; will retry", "deliveryCount", deliveryCount(d), "err", err)
		wait(ctx, c.retryDelay)
		return s.Nack(false, true)
	}
}

// eventAttrs names a delivery on every line about it: the request id it
// carries as its AMQP CorrelationId, its routing key, and its eventId and
// errandId, each only when the body holds it as a UUID. So a malformed event
// still names what it can, and nothing else from the body reaches the log.
func eventAttrs(d amqp.Delivery) []any {
	attrs := []any{"request_id", d.CorrelationId, "routingKey", d.RoutingKey}

	var ids struct {
		EventID  string `json:"eventId"`
		ErrandID string `json:"errandId"`
	}
	_ = json.Unmarshal(d.Body, &ids)
	if id, err := uuid.Parse(ids.EventID); err == nil {
		attrs = append(attrs, "eventId", id.String())
	}
	if id, err := uuid.Parse(ids.ErrandID); err == nil {
		attrs = append(attrs, "errandId", id.String())
	}
	return attrs
}

// deliveryCount is how many times the broker has redelivered d: 0 on its first
// delivery.
func deliveryCount(d amqp.Delivery) int64 {
	count, _ := d.Headers[headerDeliveryCount].(int64)
	return count
}

// payload is the JSON of an order lifecycle event (D-051).
type payload struct {
	EventID     uuid.UUID  `json:"eventId"`
	ErrandID    uuid.UUID  `json:"errandId"`
	RequesterID uuid.UUID  `json:"requesterId"`
	CourierID   *uuid.UUID `json:"courierId"`
	Amount      int        `json:"amount"`
	OccurredAt  time.Time  `json:"occurredAt"`
}

func (c *Consumer) process(ctx context.Context, routingKey string, body []byte) error {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("%w: %w", notification.ErrInvalidEvent, err)
	}
	return c.handler.Handle(ctx, notification.Event{
		ID:          p.EventID,
		Type:        routingKey,
		ErrandID:    p.ErrandID,
		RequesterID: p.RequesterID,
		CourierID:   p.CourierID,
		Amount:      p.Amount,
		OccurredAt:  p.OccurredAt,
	})
}

// wait pauses for d, reporting false if ctx ended first.
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
