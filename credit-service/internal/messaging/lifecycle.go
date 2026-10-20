// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Updated errand lifecycle RabbitMQ consumers with transport validation, clock skew logging, and retry/DLQ policy per sourceOfTruth.md Phase 11.
// Author review: PENDING - reviewer to complete

package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	LifecycleQueue    = "credit.errand.lifecycle"
	LifecycleConsumer = "credit-service.lifecycle"

	RoutingCompleted = "errand.completed"
	RoutingCancelled = "errand.cancelled"
	RoutingExpired   = "errand.expired"
	RoutingFailed    = "errand.failed"
	RoutingRewardSet = "errand.reward_set"
)

var LifecycleRoutingKeys = []string{
	RoutingCompleted,
	RoutingCancelled,
	RoutingExpired,
	RoutingFailed,
	RoutingRewardSet,
}

// LifecycleService declares the domain actions needed by lifecycle consumers.
type LifecycleService interface {
	RefundReservation(ctx context.Context, errandID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error
	PayoutReservation(ctx context.Context, errandID, courierID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error
	ApplyRewardSet(ctx context.Context, errandID, eventID uuid.UUID, newReward int64, version int64, occurredAt time.Time) error
}

// LifecycleHandler processes errand lifecycle events.
type LifecycleHandler struct {
	service LifecycleService
	logger  *slog.Logger
}

// NewLifecycleHandler constructs a new LifecycleHandler.
func NewLifecycleHandler(service LifecycleService, logger *slog.Logger) *LifecycleHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &LifecycleHandler{
		service: service,
		logger:  logger,
	}
}

type completedPayload struct {
	SchemaVersion int        `json:"schemaVersion"`
	EventID       uuid.UUID  `json:"eventId"`
	ErrandID      uuid.UUID  `json:"errandId"`
	CourierID     *uuid.UUID `json:"courierId"`
	RequesterID   *uuid.UUID `json:"requesterId,omitempty"`
	Amount        *int64     `json:"amount,omitempty"`
	OccurredAt    time.Time  `json:"occurredAt"`
}

type refundPayload struct {
	SchemaVersion int       `json:"schemaVersion"`
	EventID       uuid.UUID `json:"eventId"`
	ErrandID      uuid.UUID `json:"errandId"`
	Amount        *int64    `json:"amount,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
}

type rewardSetPayload struct {
	SchemaVersion int        `json:"schemaVersion"`
	EventID       uuid.UUID  `json:"eventId"`
	ErrandID      uuid.UUID  `json:"errandId"`
	RequesterID   *uuid.UUID `json:"requesterId,omitempty"`
	Amount        int64      `json:"amount"`
	Version       *int64     `json:"version"`
	OccurredAt    time.Time  `json:"occurredAt"`
}

// Dispatch validates the event payload and delegates to the appropriate domain service method.
func (h *LifecycleHandler) Dispatch(ctx context.Context, routingKey string, body []byte) error {
	switch routingKey {
	case RoutingCompleted:
		var ev completedPayload
		dec := json.NewDecoder(bytes.NewReader(body))
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("%w: decode %s: %v", ErrMalformedEvent, routingKey, err)
		}
		var trailing struct{}
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: trailing characters in JSON payload", ErrMalformedEvent)
		}

		if ev.SchemaVersion != 0 && ev.SchemaVersion != 1 {
			return fmt.Errorf("%w: unsupported schemaVersion %d (want 1)", ErrMalformedEvent, ev.SchemaVersion)
		}
		if ev.EventID == uuid.Nil || ev.ErrandID == uuid.Nil || ev.CourierID == nil || *ev.CourierID == uuid.Nil {
			return fmt.Errorf("%w: missing required UUIDs", ErrMalformedEvent)
		}
		if ev.OccurredAt.IsZero() {
			return fmt.Errorf("%w: occurredAt is required", ErrMalformedEvent)
		}

		h.checkClockSkew(ev.EventID, ev.OccurredAt)

		if h.service == nil {
			return errors.New("lifecycle service is required")
		}
		return h.service.PayoutReservation(ctx, ev.ErrandID, *ev.CourierID, ev.EventID, ev.Amount, ev.OccurredAt)

	case RoutingCancelled, RoutingExpired, RoutingFailed:
		var ev refundPayload
		dec := json.NewDecoder(bytes.NewReader(body))
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("%w: decode %s: %v", ErrMalformedEvent, routingKey, err)
		}
		var trailing struct{}
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: trailing characters in JSON payload", ErrMalformedEvent)
		}

		if ev.SchemaVersion != 0 && ev.SchemaVersion != 1 {
			return fmt.Errorf("%w: unsupported schemaVersion %d (want 1)", ErrMalformedEvent, ev.SchemaVersion)
		}
		if ev.EventID == uuid.Nil || ev.ErrandID == uuid.Nil {
			return fmt.Errorf("%w: missing required UUIDs", ErrMalformedEvent)
		}
		if ev.OccurredAt.IsZero() {
			return fmt.Errorf("%w: occurredAt is required", ErrMalformedEvent)
		}

		h.checkClockSkew(ev.EventID, ev.OccurredAt)

		var reason credit.TransactionReason
		switch routingKey {
		case RoutingCancelled:
			reason = credit.TransactionReasonErrandCancelled
		case RoutingExpired:
			reason = credit.TransactionReasonErrandExpired
		case RoutingFailed:
			reason = credit.TransactionReasonErrandFailed
		}

		if h.service == nil {
			return errors.New("lifecycle service is required")
		}
		return h.service.RefundReservation(ctx, ev.ErrandID, ev.EventID, reason, ev.OccurredAt)

	case RoutingRewardSet:
		var ev rewardSetPayload
		dec := json.NewDecoder(bytes.NewReader(body))
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("%w: decode %s: %v", ErrMalformedEvent, routingKey, err)
		}
		var trailing struct{}
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: trailing characters in JSON payload", ErrMalformedEvent)
		}

		if ev.SchemaVersion != 0 && ev.SchemaVersion != 1 {
			return fmt.Errorf("%w: unsupported schemaVersion %d (want 1)", ErrMalformedEvent, ev.SchemaVersion)
		}
		if ev.EventID == uuid.Nil || ev.ErrandID == uuid.Nil {
			return fmt.Errorf("%w: missing required UUIDs", ErrMalformedEvent)
		}
		if ev.Amount <= 0 {
			return fmt.Errorf("%w: amount must be positive", ErrMalformedEvent)
		}
		if ev.Version == nil || *ev.Version <= 0 {
			return fmt.Errorf("%w: version must be positive", ErrMalformedEvent)
		}
		if ev.OccurredAt.IsZero() {
			return fmt.Errorf("%w: occurredAt is required", ErrMalformedEvent)
		}

		h.checkClockSkew(ev.EventID, ev.OccurredAt)

		if h.service == nil {
			return errors.New("lifecycle service is required")
		}
		return h.service.ApplyRewardSet(ctx, ev.ErrandID, ev.EventID, ev.Amount, *ev.Version, ev.OccurredAt)

	default:
		return fmt.Errorf("%w: unsupported routing key %s", ErrMalformedEvent, routingKey)
	}
}

// HandleDelivery processes one AMQP message with transport validation, dispatch, and DLQ/retry rules.
func (h *LifecycleHandler) HandleDelivery(ctx context.Context, d amqp.Delivery) {
	if err := validateTransport(d.RoutingKey, d.ContentType, d.ContentEncoding, d.Body, LifecycleRoutingKeys); err != nil {
		h.logger.Warn("rejecting message with invalid transport to DLQ", "routingKey", d.RoutingKey, "error", err)
		_ = d.Nack(false, false)
		return
	}

	err := h.Dispatch(ctx, d.RoutingKey, d.Body)
	if err == nil {
		_ = d.Ack(false)
		return
	}

	if errors.Is(err, ErrMalformedEvent) || errors.Is(err, credit.ErrLifecycleViolation) || errors.Is(err, credit.ErrSelfPayoutNotAllowed) {
		h.logger.Warn("rejecting contract or lifecycle violation to DLQ", "routingKey", d.RoutingKey, "error", err)
		_ = d.Nack(false, false)
		return
	}

	attempt := getDeliveryAttempt(d)
	if errors.Is(err, credit.ErrReservationNotFound) {
		if d.RoutingKey == RoutingFailed {
			h.logger.Info("errand.failed for unreserved errand acknowledged as no-op", "routingKey", d.RoutingKey)
			_ = d.Ack(false)
			return
		}
		if attempt >= 5 {
			h.logger.Error("reservation not found retry attempts exhausted, dead-lettering to DLQ", "routingKey", d.RoutingKey, "attempt", attempt, "error", err)
			_ = d.Nack(false, false)
			return
		}
		h.logger.Warn("reservation not found for lifecycle event, requeuing", "routingKey", d.RoutingKey, "attempt", attempt)
		_ = d.Nack(false, true)
		return
	}

	if attempt >= 5 {
		h.logger.Error("temporary error retry attempts exhausted, dead-lettering to DLQ", "routingKey", d.RoutingKey, "attempt", attempt, "error", err)
		_ = d.Nack(false, false)
		return
	}
	h.logger.Warn("retrying lifecycle event after temporary error", "routingKey", d.RoutingKey, "attempt", attempt, "error", err)
	_ = d.Nack(false, true)
}

func (h *LifecycleHandler) checkClockSkew(eventID uuid.UUID, occurredAt time.Time) {
	skew := time.Since(occurredAt)
	if math.Abs(skew.Minutes()) > 10 {
		h.logger.Warn("implausible clock skew detected on event",
			"eventId", eventID,
			"occurredAt", occurredAt,
			"receivedAt", time.Now().UTC(),
			"skew", skew.String(),
		)
	}
}

func validateTransport(routingKey, contentType, contentEncoding string, body []byte, allowedRoutingKeys []string) error {
	if len(body) > 16384 {
		return fmt.Errorf("%w: body size %d exceeds 16KiB limit", ErrMalformedEvent, len(body))
	}
	if !utf8.Valid(body) {
		return fmt.Errorf("%w: body is not valid UTF-8", ErrMalformedEvent)
	}
	if contentType != "application/json" {
		return fmt.Errorf("%w: invalid content_type %q, want application/json", ErrMalformedEvent, contentType)
	}
	if contentEncoding != "" && strings.ToLower(contentEncoding) != "utf-8" {
		return fmt.Errorf("%w: unsupported content_encoding %q", ErrMalformedEvent, contentEncoding)
	}
	validKey := false
	for _, k := range allowedRoutingKeys {
		if routingKey == k {
			validKey = true
			break
		}
	}
	if !validKey {
		return fmt.Errorf("%w: unrouted routing key %q", ErrMalformedEvent, routingKey)
	}
	return nil
}

func getDeliveryAttempt(d amqp.Delivery) int {
	if d.Headers == nil {
		return 1
	}
	val, ok := d.Headers["x-delivery-count"]
	if !ok {
		return 1
	}
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 1
	}
}
