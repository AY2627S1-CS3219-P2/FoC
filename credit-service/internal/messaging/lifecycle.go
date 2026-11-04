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
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/observability"

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
	RefundReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error
	PayoutReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, courierID uuid.UUID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error
	ApplyRewardSet(ctx context.Context, errandID, eventID uuid.UUID, newReward int64, version int64, occurredAt time.Time) error
}

// LifecycleOption configures optional behavior of a LifecycleHandler.
type LifecycleOption func(*LifecycleHandler)

// WithLifecycleBackoff configures a custom retry backoff function.
func WithLifecycleBackoff(fn func(ctx context.Context, attempt int) bool) LifecycleOption {
	return func(h *LifecycleHandler) {
		h.backoffFn = fn
	}
}

// LifecycleHandler processes errand lifecycle events.
type LifecycleHandler struct {
	service   LifecycleService
	logger    *slog.Logger
	backoffFn func(ctx context.Context, attempt int) bool
}

// NewLifecycleHandler constructs a new LifecycleHandler.
func NewLifecycleHandler(service LifecycleService, logger *slog.Logger, opts ...LifecycleOption) *LifecycleHandler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &LifecycleHandler{
		service: service,
		logger:  logger,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// CalculateLifecycleRetryBackoff computes nominal 1, 2, 4, 8s backoff with +/-20% random jitter.
func CalculateLifecycleRetryBackoff(attempt int) time.Duration {
	var nominal time.Duration
	switch attempt {
	case 1:
		nominal = 1 * time.Second
	case 2:
		nominal = 2 * time.Second
	case 3:
		nominal = 4 * time.Second
	case 4:
		nominal = 8 * time.Second
	default:
		nominal = 8 * time.Second
	}
	jitterFactor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(nominal) * jitterFactor)
}

func (h *LifecycleHandler) waitRetryBackoff(ctx context.Context, attempt int) bool {
	if h.backoffFn != nil {
		return h.backoffFn(ctx, attempt)
	}
	delay := CalculateLifecycleRetryBackoff(attempt)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
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
	SchemaVersion int        `json:"schemaVersion"`
	EventID       uuid.UUID  `json:"eventId"`
	ErrandID      uuid.UUID  `json:"errandId"`
	RequesterID   *uuid.UUID `json:"requesterId,omitempty"`
	Amount        *int64     `json:"amount,omitempty"`
	OccurredAt    time.Time  `json:"occurredAt"`
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
	return h.dispatch(ctx, h.logger, routingKey, body)
}

// dispatch is Dispatch writing its lines to log, which HandleDelivery scopes to
// the delivery's request id.
func (h *LifecycleHandler) dispatch(ctx context.Context, log *slog.Logger, routingKey string, body []byte) error {
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
		if ev.Amount == nil || *ev.Amount < 1 || *ev.Amount > credit.MaxErrandReward {
			return fmt.Errorf("%w: completion amount is required and must be positive", ErrMalformedEvent)
		}
		if ev.OccurredAt.IsZero() {
			return fmt.Errorf("%w: occurredAt is required", ErrMalformedEvent)
		}

		checkClockSkew(log, ev.EventID, ev.ErrandID, ev.OccurredAt)

		if h.service == nil {
			return errors.New("lifecycle service is required")
		}
		return h.service.PayoutReservation(ctx, ev.ErrandID, ev.RequesterID, *ev.CourierID, ev.EventID, ev.Amount, ev.OccurredAt)

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

		checkClockSkew(log, ev.EventID, ev.ErrandID, ev.OccurredAt)

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
		return h.service.RefundReservation(ctx, ev.ErrandID, ev.RequesterID, ev.EventID, reason, ev.OccurredAt)

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

		checkClockSkew(log, ev.EventID, ev.ErrandID, ev.OccurredAt)

		if h.service == nil {
			return errors.New("lifecycle service is required")
		}
		err := h.service.ApplyRewardSet(ctx, ev.ErrandID, ev.EventID, ev.Amount, *ev.Version, ev.OccurredAt)
		if errors.Is(err, credit.ErrRewardSetStale) {
			log.Info("errand.reward_set ignored as stale",
				"routingKey", routingKey,
				"eventId", ev.EventID,
				"errandId", ev.ErrandID,
				"version", *ev.Version,
				"amount", ev.Amount,
			)
			return nil
		}
		if err != nil {
			return err
		}
		log.Info("errand.reward_set applied",
			"routingKey", routingKey,
			"eventId", ev.EventID,
			"errandId", ev.ErrandID,
			"version", *ev.Version,
			"amount", ev.Amount,
		)
		return nil

	default:
		return fmt.Errorf("%w: unsupported routing key %s", ErrMalformedEvent, routingKey)
	}
}

// HandleDelivery processes one AMQP message with transport validation, dispatch, and DLQ/retry rules.
func (h *LifecycleHandler) HandleDelivery(ctx context.Context, d amqp.Delivery) {
	log := deliveryLogger(h.logger, d)

	if err := validateTransport(d.RoutingKey, d.ContentType, d.ContentEncoding, d.Body, LifecycleRoutingKeys); err != nil {
		log.Warn("rejecting message with invalid transport to DLQ", "error", observability.Sanitize(err.Error()))
		_ = d.Nack(false, false)
		return
	}
	log = withEventIDs(log, d.Body)

	err := h.dispatch(ctx, h.logger.With("request_id", d.CorrelationId), d.RoutingKey, d.Body)
	if err == nil {
		acknowledge(log, d, "lifecycle event acknowledged")
		return
	}

	if errors.Is(err, ErrMalformedEvent) || errors.Is(err, credit.ErrLifecycleViolation) || errors.Is(err, credit.ErrSelfPayoutNotAllowed) || errors.Is(err, credit.ErrRequesterMismatch) {
		log.Warn("rejecting contract or lifecycle violation to DLQ", "error", observability.Sanitize(err.Error()))
		_ = d.Nack(false, false)
		return
	}

	attempt := getDeliveryAttempt(d)
	if errors.Is(err, credit.ErrReservationNotFound) {
		if d.RoutingKey == RoutingFailed {
			acknowledge(log, d, "errand.failed for unreserved errand acknowledged as no-op")
			return
		}
		if attempt >= 5 {
			log.Error("reservation not found retry attempts exhausted, dead-lettering to DLQ", "attempt", attempt, "error", observability.Sanitize(err.Error()))
			_ = d.Nack(false, false)
			return
		}
		log.Warn("reservation not found for lifecycle event, requeuing", "attempt", attempt)
		if !h.waitRetryBackoff(ctx, attempt) {
			return
		}
		_ = d.Nack(false, true)
		return
	}

	if attempt >= 5 {
		log.Error("temporary error retry attempts exhausted, dead-lettering to DLQ", "attempt", attempt, "error", observability.Sanitize(err.Error()))
		_ = d.Nack(false, false)
		return
	}
	log.Warn("retrying lifecycle event after temporary error", "attempt", attempt, "error", observability.Sanitize(err.Error()))
	if !h.waitRetryBackoff(ctx, attempt) {
		return
	}
	_ = d.Nack(false, true)
}

func checkClockSkew(log *slog.Logger, eventID, errandID uuid.UUID, occurredAt time.Time) {
	skew := time.Since(occurredAt)
	if math.Abs(skew.Minutes()) > 10 {
		log.Warn("implausible clock skew detected on event",
			"eventId", eventID,
			"errandId", errandID,
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
