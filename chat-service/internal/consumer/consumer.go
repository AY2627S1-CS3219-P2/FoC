package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ChatQueue       = "chat.errands"
	ChatConsumerTag = "chat-service.errands"
)

type ErrandLifecycleEvent struct {
	EventID       uuid.UUID  `json:"eventId"`
	EventType     string     `json:"eventType"`
	ErrandID      uuid.UUID  `json:"errandId"`
	RequesterID   uuid.UUID  `json:"requesterId"`
	Amount        int        `json:"amount"`
	OccurredAt    time.Time  `json:"occurredAt"`
	CourierID     *uuid.UUID `json:"courierId,omitempty"`
	CorrelationID string     `json:"correlationId"`
}

type LifecycleHandler struct {
	service *chat.Service
}

func NewLifecycleHandler(service *chat.Service) *LifecycleHandler {
	return &LifecycleHandler{service: service}
}

func (h *LifecycleHandler) Handle(ctx context.Context, routingKey string, body []byte) error {
	var event ErrandLifecycleEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("malformed json: %w", err)
	}
	if event.ErrandID == uuid.Nil {
		return errors.New("errandId is required")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}

	key := routingKey
	if key == "" {
		key = event.EventType
	}

	switch key {
	case "errand.accepted":
		if event.CourierID == nil || *event.CourierID == uuid.Nil {
			return errors.New("courierId is required for errand.accepted")
		}
		_, err := h.service.CreateOrResumeConversation(ctx, event.ErrandID, event.RequesterID, *event.CourierID, event.OccurredAt)
		return err

	case "errand.withdrawn":
		if event.CourierID == nil || *event.CourierID == uuid.Nil {
			return errors.New("courierId is required for errand.withdrawn")
		}
		return h.service.MarkCourierWithdrawn(ctx, event.ErrandID, *event.CourierID, event.OccurredAt)

	case "errand.completed":
		return h.service.MarkErrandCompleted(ctx, event.ErrandID, event.OccurredAt)

	case "errand.cancelled", "errand.expired":
		return h.service.MarkErrandTerminated(ctx, event.ErrandID, event.OccurredAt)

	default:
		// Ignore events this consumer is not concerned with
		return nil
	}
}

func (h *LifecycleHandler) HandleDelivery(ctx context.Context, d amqp.Delivery) {
	err := h.Handle(ctx, d.RoutingKey, d.Body)
	if err != nil {
		log.Printf("error handling amqp delivery (key: %s): %v", d.RoutingKey, err)
		// Check delivery count if available from headers
		var count int64 = 0
		if xCount, ok := d.Headers["x-delivery-count"]; ok {
			if c, ok := xCount.(int64); ok {
				count = c
			}
		}
		if count >= 5 {
			log.Printf("delivery count %d exceeded, dead-lettering", count)
			_ = d.Nack(false, false)
			return
		}
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

func RunConsumer(ctx context.Context, amqpURL string, handler *LifecycleHandler) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("dial amqp: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open amqp channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	if err := ch.Qos(10, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}

	deliveries, err := ch.ConsumeWithContext(
		ctx,
		ChatQueue,
		ChatConsumerTag,
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("consume %s: %w", ChatQueue, err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("amqp deliveries channel closed")
			}
			handler.HandleDelivery(ctx, d)
		}
	}
}
