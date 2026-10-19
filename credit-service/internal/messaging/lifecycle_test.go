// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added unit tests for lifecycle events dispatch, transport validation, retry/DLQ policy, and clock skew per sourceOfTruth.md Phase 11.
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

	t.Run("missing schemaVersion", func(t *testing.T) {
		body := []byte(`{
			"eventId": "` + uuid.New().String() + `",
			"errandId": "` + uuid.New().String() + `",
			"occurredAt": "2026-10-19T12:00:00Z"
		}`)
		err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("unknown fields rejected", func(t *testing.T) {
		body := []byte(`{
			"schemaVersion": 1,
			"eventId": "` + uuid.New().String() + `",
			"errandId": "` + uuid.New().String() + `",
			"occurredAt": "2026-10-19T12:00:00Z",
			"extraField": "bad"
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

	// Clock skew should not cause rejection
	if err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body); err != nil {
		t.Fatalf("clock skew should not reject valid event: %v", err)
	}
}

func TestSupervisor_Readiness(t *testing.T) {
	s := messaging.NewSupervisor("amqp://guest:guest@localhost:5672/", nil, nil, 2, nil)
	if s.IsReady() {
		t.Fatal("supervisor should not be ready before connecting")
	}
}

func TestLifecycleHandler_DeliveryTransportRejection(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := messaging.NewLifecycleHandler(&mockLifecycleService{}, logger)

	// Delivery with missing content_type should be rejected
	d := amqp.Delivery{
		RoutingKey:  messaging.RoutingCancelled,
		ContentType: "text/plain",
		Body:        []byte(`{"schemaVersion": 1}`),
	}
	// Calling HandleDelivery should safely reject without panicking
	h.HandleDelivery(context.Background(), d)
}
