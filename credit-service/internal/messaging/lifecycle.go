// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Updated errand lifecycle RabbitMQ consumers with transport validation, clock skew logging, and retry/DLQ policy per sourceOfTruth.md Phase 11.
// Author review: PENDING - reviewer to complete

package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ErrandsQueue     = "credit.errands"
	ErrandsConsumer  = "credit-service.errands"
	ErrandsDLQ       = "credit.errands.dlq"

	// Backward compatibility aliases
	LifecycleQueue    = ErrandsQueue
	LifecycleConsumer = ErrandsConsumer
	LifecycleDLQ      = ErrandsDLQ

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

// LifecycleHandler processes errand events using an EventRouter.
type LifecycleHandler struct {
	service   LifecycleService
	logger    *slog.Logger
	router    *EventRouter
	backoffFn func(ctx context.Context, attempt int) bool
}

// NewLifecycleHandler constructs a new LifecycleHandler.
func NewLifecycleHandler(service LifecycleService, logger *slog.Logger, opts ...LifecycleOption) *LifecycleHandler {
	if logger == nil {
		logger = slog.Default()
	}

	router := NewEventRouter(logger)
	if service != nil {
		payoutHandler := NewPayoutHandler(service, logger)
		refundHandler := NewRefundHandler(service, logger)
		rewardSetHandler := NewRewardSetHandler(service, logger)

		router.Register(RoutingCompleted, payoutHandler)
		router.Register(RoutingCancelled, refundHandler)
		router.Register(RoutingExpired, refundHandler)
		router.Register(RoutingFailed, refundHandler)
		router.Register(RoutingRewardSet, rewardSetHandler)
	}

	h := &LifecycleHandler{
		service: service,
		logger:  logger,
		router:  router,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// QueueName returns the primary queue name for errand events.
func (h *LifecycleHandler) QueueName() string {
	return ErrandsQueue
}

// DLQName returns the dead letter queue name for errand events.
func (h *LifecycleHandler) DLQName() string {
	return ErrandsDLQ
}

// ConsumerTag returns the AMQP consumer tag for the given worker.
func (h *LifecycleHandler) ConsumerTag(workerID int) string {
	return fmt.Sprintf("%s-%d", ErrandsConsumer, workerID)
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

// Dispatch validates the event payload and delegates to the appropriate domain service method via router.
func (h *LifecycleHandler) Dispatch(ctx context.Context, routingKey string, body []byte) error {
	if h.service == nil {
		return errors.New("lifecycle service is required")
	}
	return h.router.Dispatch(ctx, routingKey, body)
}

// HandleDelivery processes one AMQP message with transport validation, dispatch, and DLQ/retry rules.
func (h *LifecycleHandler) HandleDelivery(ctx context.Context, d amqp.Delivery) {
	if err := validateTransport(d.RoutingKey, d.ContentType, d.ContentEncoding, d.Body, LifecycleRoutingKeys); err != nil {
		h.logger.Warn("rejecting message with invalid transport to DLQ", "routingKey", d.RoutingKey, "error", err)
		_ = d.Nack(false, false)
		return
	}

	var meta struct {
		ErrandID uuid.UUID `json:"errandId"`
		Version  *int64    `json:"version"`
	}
	_ = json.Unmarshal(d.Body, &meta)

	var logAttrs []any
	logAttrs = append(logAttrs, "routingKey", d.RoutingKey)
	if meta.ErrandID != uuid.Nil {
		logAttrs = append(logAttrs, "errandId", meta.ErrandID)
	}
	if meta.Version != nil {
		logAttrs = append(logAttrs, "version", *meta.Version)
	}

	err := h.Dispatch(ctx, d.RoutingKey, d.Body)
	if err == nil {
		_ = d.Ack(false)
		return
	}

	if errors.Is(err, ErrMalformedEvent) || errors.Is(err, credit.ErrLifecycleViolation) || errors.Is(err, credit.ErrSelfPayoutNotAllowed) || errors.Is(err, credit.ErrRequesterMismatch) {
		attrs := append(logAttrs, "error", err)
		h.logger.Warn("rejecting contract or lifecycle violation to DLQ", attrs...)
		_ = d.Nack(false, false)
		return
	}

	attempt := getDeliveryAttempt(d)
	if errors.Is(err, credit.ErrReservationNotFound) {
		if d.RoutingKey == RoutingFailed {
			h.logger.Info("errand.failed for unreserved errand acknowledged as no-op", logAttrs...)
			_ = d.Ack(false)
			return
		}
		if attempt >= 5 {
			attrs := append(logAttrs, "attempt", attempt, "error", err)
			h.logger.Error("reservation not found retry attempts exhausted, dead-lettering to DLQ", attrs...)
			_ = d.Nack(false, false)
			return
		}
		attrs := append(logAttrs, "attempt", attempt)
		h.logger.Warn("reservation not found for lifecycle event, requeuing", attrs...)
		if !h.waitRetryBackoff(ctx, attempt) {
			return
		}
		_ = d.Nack(false, true)
		return
	}

	if attempt >= 5 {
		attrs := append(logAttrs, "attempt", attempt, "error", err)
		h.logger.Error("temporary error retry attempts exhausted, dead-lettering to DLQ", attrs...)
		_ = d.Nack(false, false)
		return
	}
	attrs := append(logAttrs, "attempt", attempt, "error", err)
	h.logger.Warn("retrying lifecycle event after temporary error", attrs...)
	if !h.waitRetryBackoff(ctx, attempt) {
		return
	}
	_ = d.Nack(false, true)
}

func (h *LifecycleHandler) checkClockSkew(eventID uuid.UUID, occurredAt time.Time) {
	checkClockSkew(h.logger, eventID, occurredAt)
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
