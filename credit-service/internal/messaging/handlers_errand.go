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
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
)

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

func checkClockSkew(logger *slog.Logger, eventID uuid.UUID, occurredAt time.Time) {
	skew := time.Since(occurredAt)
	if math.Abs(skew.Minutes()) > 10 {
		logger.Warn("implausible clock skew detected on event",
			"eventId", eventID,
			"occurredAt", occurredAt,
			"receivedAt", time.Now().UTC(),
			"skew", skew.String(),
		)
	}
}

// PayoutHandler handles errand.completed events.
type PayoutHandler struct {
	service LifecycleService
	logger  *slog.Logger
}

// NewPayoutHandler constructs a new PayoutHandler.
func NewPayoutHandler(service LifecycleService, logger *slog.Logger) *PayoutHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PayoutHandler{service: service, logger: logger}
}

// Handle validates and processes an errand.completed event.
func (h *PayoutHandler) Handle(ctx context.Context, routingKey string, body []byte) error {
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

	checkClockSkew(h.logger, ev.EventID, ev.OccurredAt)

	if h.service == nil {
		return errors.New("lifecycle service is required")
	}
	return h.service.PayoutReservation(ctx, ev.ErrandID, ev.RequesterID, *ev.CourierID, ev.EventID, ev.Amount, ev.OccurredAt)
}

// RefundHandler handles errand refund events (errand.cancelled, errand.expired, errand.failed).
type RefundHandler struct {
	service LifecycleService
	logger  *slog.Logger
}

// NewRefundHandler constructs a new RefundHandler.
func NewRefundHandler(service LifecycleService, logger *slog.Logger) *RefundHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RefundHandler{service: service, logger: logger}
}

// Handle validates and processes errand cancellation, expiration, and failure events.
func (h *RefundHandler) Handle(ctx context.Context, routingKey string, body []byte) error {
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

	checkClockSkew(h.logger, ev.EventID, ev.OccurredAt)

	var reason credit.TransactionReason
	switch routingKey {
	case RoutingCancelled:
		reason = credit.TransactionReasonErrandCancelled
	case RoutingExpired:
		reason = credit.TransactionReasonErrandExpired
	case RoutingFailed:
		reason = credit.TransactionReasonErrandFailed
	default:
		return fmt.Errorf("%w: unsupported refund routing key %s", ErrMalformedEvent, routingKey)
	}

	if h.service == nil {
		return errors.New("lifecycle service is required")
	}
	return h.service.RefundReservation(ctx, ev.ErrandID, ev.RequesterID, ev.EventID, reason, ev.OccurredAt)
}

// RewardSetHandler handles errand.reward_set events.
type RewardSetHandler struct {
	service LifecycleService
	logger  *slog.Logger
}

// NewRewardSetHandler constructs a new RewardSetHandler.
func NewRewardSetHandler(service LifecycleService, logger *slog.Logger) *RewardSetHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RewardSetHandler{service: service, logger: logger}
}

// Handle validates and processes errand.reward_set events.
func (h *RewardSetHandler) Handle(ctx context.Context, routingKey string, body []byte) error {
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

	checkClockSkew(h.logger, ev.EventID, ev.OccurredAt)

	if h.service == nil {
		return errors.New("lifecycle service is required")
	}
	err := h.service.ApplyRewardSet(ctx, ev.ErrandID, ev.EventID, ev.Amount, *ev.Version, ev.OccurredAt)
	if errors.Is(err, credit.ErrRewardSetStale) {
		h.logger.Info("errand.reward_set ignored as stale",
			"routingKey", routingKey,
			"errandId", ev.ErrandID,
			"version", *ev.Version,
			"amount", ev.Amount,
		)
		return nil
	}
	if err != nil {
		return err
	}
	h.logger.Info("errand.reward_set applied",
		"routingKey", routingKey,
		"errandId", ev.ErrandID,
		"version", *ev.Version,
		"amount", ev.Amount,
	)
	return nil
}
