// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Removed obsolete Direct Reply-to dead code; user.registered is handled as a standard unidirectional event.
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
	RegistrationQueue     = "credit.registration"
	RegistrationConsumer  = "credit-service.registration"
	RoutingUserRegistered = "user.registered"
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
	logger    *slog.Logger
}

// NewRegistrationHandler constructs a user.registered handler.
func NewRegistrationHandler(allocator InitialAllocator, opts ...func(*RegistrationHandler)) *RegistrationHandler {
	h := &RegistrationHandler{
		allocator: allocator,
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithLogger configures a structured logger for the registration handler.
func WithLogger(logger *slog.Logger) func(*RegistrationHandler) {
	return func(h *RegistrationHandler) {
		if logger != nil {
			h.logger = logger
		}
	}
}

type registrationEvent struct {
	SchemaVersion int       `json:"schemaVersion"`
	EventID       uuid.UUID `json:"eventId"`
	UserID        uuid.UUID `json:"userId"`
	OccurredAt    time.Time `json:"occurredAt"`
}

// Handle validates one user.registered payload and allocates initial credits.
func (h *RegistrationHandler) Handle(ctx context.Context, body []byte) error {
	var event registrationEvent

	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&event); err != nil {
		return fmt.Errorf("%w: decode user.registered: %v", ErrMalformedEvent, err)
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: multiple JSON values", ErrMalformedEvent)
	}

	if event.SchemaVersion != 0 && event.SchemaVersion != 1 {
		return fmt.Errorf("%w: unsupported schemaVersion %d (want 1)", ErrMalformedEvent, event.SchemaVersion)
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

// HandleDelivery processes an AMQP message, validates transport headers, invokes Handle, and acks/nacks.
func (h *RegistrationHandler) HandleDelivery(ctx context.Context, d amqp.Delivery) {
	if err := validateTransport(d.RoutingKey, d.ContentType, d.ContentEncoding, d.Body, []string{RoutingUserRegistered}); err != nil {
		h.logger.Warn("rejecting registration message with invalid transport to DLQ", "error", err)
		_ = d.Nack(false, false)
		return
	}

	if err := h.Handle(ctx, d.Body); err != nil {
		if errors.Is(err, ErrMalformedEvent) {
			h.logger.Warn("dropping malformed user.registered event to DLQ", "error", err)
			_ = d.Nack(false, false)
			return
		}
		attempt := getDeliveryAttempt(d)
		if attempt >= 5 {
			h.logger.Error("registration delivery attempts exhausted, dead-lettering to DLQ", "attempt", attempt, "error", err)
			_ = d.Nack(false, false)
			return
		}
		h.logger.Warn("retrying user.registered event after handler failure", "attempt", attempt, "error", err)
		_ = d.Nack(false, true)
		return
	}

	_ = d.Ack(false)
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

	deliveries, err := ch.ConsumeWithContext(ctx, RegistrationQueue, RegistrationConsumer, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", RegistrationQueue, err)
	}

	for delivery := range deliveries {
		handler.HandleDelivery(ctx, delivery)
	}

	return ctx.Err()
}
