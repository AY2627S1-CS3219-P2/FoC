// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The RabbitMQ consumer on notification-service's own queue (D-140,
//   D-143). Structure follows chat-service/internal/consumer.
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
func (c *Consumer) settle(ctx context.Context, d amqp.Delivery, s settler) error {
	err := c.process(ctx, d.RoutingKey, d.Body)
	switch {
	case err == nil:
		return s.Ack(false)
	case errors.Is(err, notification.ErrInvalidEvent):
		c.log.ErrorContext(ctx, "dead-lettering an unusable event", "key", d.RoutingKey, "err", err)
		return s.Nack(false, false)
	default:
		c.log.WarnContext(ctx, "event failed; will retry", "key", d.RoutingKey, "err", err)
		wait(ctx, c.retryDelay)
		return s.Nack(false, true)
	}
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
