// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-14
// Scope: Added user.registered event handling for initial credit allocation.
// Author review: PENDING - reviewer to complete

// Package messaging adapts broker events into credit-service use cases.
package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	registrationQueue    = "credit.registration"
	registrationConsumer = "credit-service.registration"
)

// ErrMalformedEvent marks a message that cannot be retried successfully.
var ErrMalformedEvent = errors.New("messaging: malformed event")

// InitialAllocator is the credit use case needed by the user.registered handler.
type InitialAllocator interface {
	AllocateInitialCredits(context.Context, uuid.UUID, time.Time) error
}

// RegistrationHandler handles user.registered events.
type RegistrationHandler struct {
	allocator InitialAllocator
}

// NewRegistrationHandler constructs a user.registered handler.
func NewRegistrationHandler(allocator InitialAllocator) *RegistrationHandler {
	return &RegistrationHandler{allocator: allocator}
}

// Handle validates one user.registered payload and allocates initial credits.
func (h *RegistrationHandler) Handle(ctx context.Context, body []byte) error {
	var event struct {
		EventID    uuid.UUID `json:"eventId"`
		UserID     uuid.UUID `json:"userId"`
		OccurredAt time.Time `json:"occurredAt"`
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return fmt.Errorf("%w: decode user.registered: %v", ErrMalformedEvent, err)
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: multiple JSON values", ErrMalformedEvent)
	}
	if event.EventID == uuid.Nil {
		return fmt.Errorf("%w: eventId is required", ErrMalformedEvent)
	}
	if event.UserID == uuid.Nil {
		return fmt.Errorf("%w: userId is required", ErrMalformedEvent)
	}
	if event.OccurredAt.IsZero() {
		return fmt.Errorf("%w: occurredAt is required", ErrMalformedEvent)
	}
	if h.allocator == nil {
		return errors.New("registration allocator is required")
	}

	if err := h.allocator.AllocateInitialCredits(ctx, event.UserID, event.OccurredAt); err != nil {
		if errors.Is(err, credit.ErrInvalidUserID) {
			return fmt.Errorf("%w: %v", ErrMalformedEvent, err)
		}
		return fmt.Errorf("handle user.registered %s: %w", event.EventID, err)
	}
	return nil
}

// RunRegistrationConsumer consumes credit.registration messages until ctx ends.
func RunRegistrationConsumer(ctx context.Context, amqpURL string, handler *RegistrationHandler, logger *slog.Logger) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("dial RabbitMQ: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("set RabbitMQ qos: %w", err)
	}

	deliveries, err := ch.ConsumeWithContext(ctx, registrationQueue, registrationConsumer, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", registrationQueue, err)
	}

	for delivery := range deliveries {
		if err := handler.Handle(ctx, delivery.Body); err != nil {
			if errors.Is(err, ErrMalformedEvent) {
				logger.Warn("dropping malformed user.registered event", "error", err)
				_ = delivery.Nack(false, false)
				continue
			}
			logger.Error("retrying user.registered event after handler failure", "error", err)
			_ = delivery.Nack(false, true)
			continue
		}
		_ = delivery.Ack(false)
	}

	return ctx.Err()
}
