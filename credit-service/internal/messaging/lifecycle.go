// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented lifecycle event consumer for errand.completed, errand.cancelled, errand.expired, and errand.failed with strict v1 validation and retry/DLQ policy per sourceOfTruth.md Phase 11.
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
)

var LifecycleRoutingKeys = []string{
	RoutingCompleted,
	RoutingCancelled,
	RoutingExpired,
	RoutingFailed,
}

// LifecycleService encapsulates the domain operations needed for errand lifecycle settlement.
type LifecycleService interface {
	RefundReservation(ctx context.Context, errandID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error
	PayoutReservation(ctx context.Context, errandID, courierID, eventID uuid.UUID, occurredAt time.Time) error
}

// LifecycleHandler handles errand lifecycle events.
type LifecycleHandler struct {
	service LifecycleService
	logger  *slog.Logger
}

// NewLifecycleHandler constructs a lifecycle consumer handler.
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
	OccurredAt    time.Time  `json:"occurredAt"`
}

type refundPayload struct {
	SchemaVersion int       `json:"schemaVersion"`
	EventID       uuid.UUID `json:"eventId"`
	ErrandID      uuid.UUID `json:"errandId"`
	OccurredAt    time.Time `json:"occurredAt"`
}

// Dispatch validates the event payload and delegates to the appropriate domain service method.
func (h *LifecycleHandler) Dispatch(ctx context.Context, routingKey string, body []byte) error {
	switch routingKey {
	case RoutingCompleted:
		var ev completedPayload
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("%w: decode %s: %v", ErrMalformedEvent, routingKey, err)
		}
		var trailing struct{}
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: trailing characters in JSON payload", ErrMalformedEvent)
		}

		if ev.SchemaVersion != 1 {
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
		return h.service.PayoutReservation(ctx, ev.ErrandID, *ev.CourierID, ev.EventID, ev.OccurredAt)

	case RoutingCancelled, RoutingExpired, RoutingFailed:
		var ev refundPayload
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("%w: decode %s: %v", ErrMalformedEvent, routingKey, err)
		}
		var trailing struct{}
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: trailing characters in JSON payload", ErrMalformedEvent)
		}

		if ev.SchemaVersion != 1 {
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
		return fmt.Errorf("%w: unsupported routing key %q", ErrMalformedEvent, routingKey)
	}
	return nil
}

func getDeliveryAttempt(d amqp.Delivery) int {
	if val, ok := d.Headers["x-delivery-count"]; ok {
		switch v := val.(type) {
		case int64:
			return int(v) + 1
		case int32:
			return int(v) + 1
		case int:
			return v + 1
		}
	}
	if xDeath, ok := d.Headers["x-death"].([]any); ok && len(xDeath) > 0 {
		if deathMap, ok := xDeath[0].(amqp.Table); ok {
			if count, ok := deathMap["count"].(int64); ok {
				return int(count) + 1
			}
		}
	}
	if d.Redelivered {
		return 2
	}
	return 1
}
