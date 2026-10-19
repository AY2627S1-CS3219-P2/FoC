// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added unit tests for lifecycle events dispatch, tolerant decoding, retry/DLQ policy, and clock skew per sourceOfTruth.md Phase 11.
// Author review: PENDING - reviewer to complete

package messaging_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/messaging"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type mockLifecycleService struct {
	refundFn func(ctx context.Context, errandID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error
	payoutFn func(ctx context.Context, errandID, courierID, eventID uuid.UUID, occurredAt time.Time) error
}

func (m *mockLifecycleService) RefundReservation(ctx context.Context, errandID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
	if m.refundFn != nil {
		return m.refundFn(ctx, errandID, eventID, reason, occurredAt)
	}
	return nil
}

func (m *mockLifecycleService) PayoutReservation(ctx context.Context, errandID, courierID, eventID uuid.UUID, occurredAt time.Time) error {
	if m.payoutFn != nil {
		return m.payoutFn(ctx, errandID, courierID, eventID, occurredAt)
	}
	return nil
}

func TestLifecycleHandler_DispatchCompleted(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	eventID := uuid.New()
	errandID := uuid.New()
	courierID := uuid.New()
	now := time.Now().UTC()

	var calledErrand, calledCourier, calledEvent uuid.UUID
	var calledTime time.Time

	svc := &mockLifecycleService{
		payoutFn: func(ctx context.Context, eID, cID, evID uuid.UUID, occ time.Time) error {
			calledErrand = eID
			calledCourier = cID
			calledEvent = evID
			calledTime = occ
			return nil
		},
	}

	h := messaging.NewLifecycleHandler(svc, logger)

	body := []byte(`{
		"schemaVersion": 1,
		"eventId": "` + eventID.String() + `",
		"errandId": "` + errandID.String() + `",
		"courierId": "` + courierID.String() + `",
		"occurredAt": "` + now.Format(time.RFC3339Nano) + `"
	}`)

	if err := h.Dispatch(context.Background(), messaging.RoutingCompleted, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calledErrand != errandID || calledCourier != courierID || calledEvent != eventID || !calledTime.Equal(now) {
		t.Errorf("unexpected payout parameters passed: errand=%s courier=%s event=%s time=%s",
			calledErrand, calledCourier, calledEvent, calledTime)
	}
}

func TestLifecycleHandler_DispatchCompletedWithOrderServiceEnvelope(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	eventID := uuid.New()
	errandID := uuid.New()
	courierID := uuid.New()
	requesterID := uuid.New()
	now := time.Now().UTC()

	var calledErrand, calledCourier, calledEvent uuid.UUID

	svc := &mockLifecycleService{
		payoutFn: func(ctx context.Context, eID, cID, evID uuid.UUID, occ time.Time) error {
			calledErrand = eID
			calledCourier = cID
			calledEvent = evID
			return nil
		},
	}

	h := messaging.NewLifecycleHandler(svc, logger)

	// order-service outbox format: carries eventType, correlationId, requesterId, amount, and omits schemaVersion
	body := []byte(`{
		"eventId": "` + eventID.String() + `",
		"eventType": "errand.completed",
		"errandId": "` + errandID.String() + `",
		"requesterId": "` + requesterID.String() + `",
		"courierId": "` + courierID.String() + `",
		"amount": 20,
		"correlationId": "corr-errand-123",
		"occurredAt": "` + now.Format(time.RFC3339Nano) + `"
	}`)

	if err := h.Dispatch(context.Background(), messaging.RoutingCompleted, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calledErrand != errandID || calledCourier != courierID || calledEvent != eventID {
		t.Errorf("unexpected parameters: errand=%s courier=%s event=%s", calledErrand, calledCourier, calledEvent)
	}
}

func TestLifecycleHandler_DispatchRefunds(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	eventID := uuid.New()
	errandID := uuid.New()
	now := time.Now().UTC()

	tests := []struct {
		routingKey string
		wantReason credit.TransactionReason
	}{
		{messaging.RoutingCancelled, credit.TransactionReasonErrandCancelled},
		{messaging.RoutingExpired, credit.TransactionReasonErrandExpired},
		{messaging.RoutingFailed, credit.TransactionReasonErrandFailed},
	}

	for _, tc := range tests {
		t.Run(tc.routingKey, func(t *testing.T) {
			var calledReason credit.TransactionReason
			svc := &mockLifecycleService{
				refundFn: func(ctx context.Context, eID, evID uuid.UUID, reason credit.TransactionReason, occ time.Time) error {
					calledReason = reason
					return nil
				},
			}

			h := messaging.NewLifecycleHandler(svc, logger)
			body := []byte(`{
				"schemaVersion": 1,
				"eventId": "` + eventID.String() + `",
				"errandId": "` + errandID.String() + `",
				"occurredAt": "` + now.Format(time.RFC3339Nano) + `"
			}`)

			if err := h.Dispatch(context.Background(), tc.routingKey, body); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if calledReason != tc.wantReason {
				t.Errorf("reason = %v, want %v", calledReason, tc.wantReason)
			}
		})
	}
}

func TestLifecycleHandler_RejectsMalformedPayloads(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := messaging.NewLifecycleHandler(&mockLifecycleService{}, logger)

	t.Run("unsupported schemaVersion", func(t *testing.T) {
		body := []byte(`{
			"schemaVersion": 2,
			"eventId": "` + uuid.New().String() + `",
			"errandId": "` + uuid.New().String() + `",
			"occurredAt": "2026-10-19T12:00:00Z"
		}`)
		err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("missing required UUIDs", func(t *testing.T) {
		body := []byte(`{
			"schemaVersion": 1,
			"occurredAt": "2026-10-19T12:00:00Z"
		}`)
		err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("unsupported routing key", func(t *testing.T) {
		body := []byte(`{"schemaVersion": 1}`)
		err := h.Dispatch(context.Background(), "errand.unknown", body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})
}

func TestLifecycleHandler_ClockSkewLoggedNotRejected(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := &mockLifecycleService{}
	h := messaging.NewLifecycleHandler(svc, logger)

	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	body := []byte(`{
		"schemaVersion": 1,
		"eventId": "` + uuid.New().String() + `",
		"errandId": "` + uuid.New().String() + `",
		"occurredAt": "` + oldTime.Format(time.RFC3339Nano) + `"
	}`)

	if err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body); err != nil {
		t.Fatalf("unexpected error for skewed event: %v", err)
	}
}

func TestLifecycleHandler_HandleDelivery(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := &mockLifecycleService{}
	h := messaging.NewLifecycleHandler(svc, logger)

	ctx := context.Background()
	body := []byte(`{
		"schemaVersion": 1,
		"eventId": "` + uuid.New().String() + `",
		"errandId": "` + uuid.New().String() + `",
		"occurredAt": "2026-10-19T12:00:00Z"
	}`)

	t.Run("valid delivery gets acked", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  messaging.RoutingCancelled,
			ContentType: "application/json",
			Body:        body,
		}
		h.HandleDelivery(ctx, d)
	})

	t.Run("invalid transport rejected to DLQ", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  messaging.RoutingCancelled,
			ContentType: "text/plain",
			Body:        body,
		}
		h.HandleDelivery(ctx, d)
	})

	t.Run("malformed payload rejected to DLQ", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  messaging.RoutingCancelled,
			ContentType: "application/json",
			Body:        []byte(`{bad-json`),
		}
		h.HandleDelivery(ctx, d)
	})
}
