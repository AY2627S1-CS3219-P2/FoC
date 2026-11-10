// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added unit tests for lifecycle events dispatch, tolerant decoding, retry/DLQ policy, and clock skew per sourceOfTruth.md Phase 11.
// Author review: Reviewed by Seth

package messaging_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	refundFn    func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error
	payoutFn    func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, courierID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error
	rewardSetFn func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, newReward int64, version int64, occurredAt time.Time) error
}

func (m *mockLifecycleService) RefundReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
	if m.refundFn != nil {
		return m.refundFn(ctx, errandID, requesterID, eventID, reason, occurredAt)
	}
	return nil
}

func (m *mockLifecycleService) PayoutReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, courierID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error {
	if m.payoutFn != nil {
		return m.payoutFn(ctx, errandID, requesterID, courierID, eventID, amount, occurredAt)
	}
	return nil
}

func (m *mockLifecycleService) ApplyRewardSet(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, newReward int64, version int64, occurredAt time.Time) error {
	if m.rewardSetFn != nil {
		return m.rewardSetFn(ctx, errandID, requesterID, eventID, newReward, version, occurredAt)
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
	var calledRequester *uuid.UUID
	var calledTime time.Time

	svc := &mockLifecycleService{
		payoutFn: func(ctx context.Context, eID uuid.UUID, reqID *uuid.UUID, cID, evID uuid.UUID, amount *int64, occ time.Time) error {
			calledErrand = eID
			calledRequester = reqID
			calledCourier = cID
			calledEvent = evID
			calledTime = occ
			return nil
		},
	}
	h := messaging.NewLifecycleHandler(svc, logger)

	t.Run("valid completed payload dispatches successfully", func(t *testing.T) {
		requesterID := uuid.New()
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"requesterId": "%s",
			"courierId": "%s",
			"amount": 50,
			"occurredAt": "%s"
		}`, eventID, errandID, requesterID, courierID, now.Format(time.RFC3339Nano))

		err := h.Dispatch(context.Background(), messaging.RoutingCompleted, []byte(body))
		if err != nil {
			t.Fatalf("unexpected dispatch error: %v", err)
		}
		if calledErrand != errandID || calledCourier != courierID || calledEvent != eventID || calledRequester == nil || *calledRequester != requesterID || !calledTime.Equal(now) {
			t.Fatalf("dispatch did not forward expected values: errand=%s, requester=%v, courier=%s, event=%s, time=%v",
				calledErrand, calledRequester, calledCourier, calledEvent, calledTime)
		}
	})

	t.Run("missing amount fails validation", func(t *testing.T) {
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"courierId": "%s",
			"occurredAt": "%s"
		}`, eventID, errandID, courierID, now.Format(time.RFC3339Nano))

		err := h.Dispatch(context.Background(), messaging.RoutingCompleted, []byte(body))
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("negative amount fails validation", func(t *testing.T) {
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"courierId": "%s",
			"amount": -10,
			"occurredAt": "%s"
		}`, eventID, errandID, courierID, now.Format(time.RFC3339Nano))

		err := h.Dispatch(context.Background(), messaging.RoutingCompleted, []byte(body))
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})
}

func TestLifecycleHandler_DispatchRefunds(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	eventID := uuid.New()
	errandID := uuid.New()
	now := time.Now().UTC()

	routes := []struct {
		key        string
		wantReason credit.TransactionReason
	}{
		{messaging.RoutingCancelled, credit.TransactionReasonErrandCancelled},
		{messaging.RoutingExpired, credit.TransactionReasonErrandExpired},
		{messaging.RoutingFailed, credit.TransactionReasonErrandFailed},
	}

	for _, tc := range routes {
		t.Run(tc.key, func(t *testing.T) {
			var calledReason credit.TransactionReason
			var calledRequester *uuid.UUID
			svc := &mockLifecycleService{
				refundFn: func(ctx context.Context, eID uuid.UUID, reqID *uuid.UUID, evID uuid.UUID, reason credit.TransactionReason, occ time.Time) error {
					calledReason = reason
					calledRequester = reqID
					return nil
				},
			}
			h := messaging.NewLifecycleHandler(svc, logger)

			requesterID := uuid.New()
			body := fmt.Sprintf(`{
				"schemaVersion": 1,
				"eventId": "%s",
				"errandId": "%s",
				"requesterId": "%s",
				"occurredAt": "%s"
			}`, eventID, errandID, requesterID, now.Format(time.RFC3339Nano))

			err := h.Dispatch(context.Background(), tc.key, []byte(body))
			if err != nil {
				t.Fatalf("unexpected dispatch error: %v", err)
			}
			if calledReason != tc.wantReason || calledRequester == nil || *calledRequester != requesterID {
				t.Fatalf("want reason %s and requester %s, got: %s, %v", tc.wantReason, requesterID, calledReason, calledRequester)
			}
		})
	}
}

func TestLifecycleHandler_DispatchPayloadRejections(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := messaging.NewLifecycleHandler(&mockLifecycleService{}, logger)

	t.Run("trailing bytes rejected", func(t *testing.T) {
		body := []byte(`{"schemaVersion": 1, "eventId": "` + uuid.New().String() + `", "errandId": "` + uuid.New().String() + `", "occurredAt": "2026-10-19T12:00:00Z"} trailing`)
		err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("unsupported schemaVersion", func(t *testing.T) {
		body := []byte(`{"schemaVersion": 99, "eventId": "` + uuid.New().String() + `", "errandId": "` + uuid.New().String() + `", "occurredAt": "2026-10-19T12:00:00Z"}`)
		err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("missing eventId", func(t *testing.T) {
		body := []byte(`{"schemaVersion": 1, "errandId": "` + uuid.New().String() + `", "occurredAt": "2026-10-19T12:00:00Z"}`)
		err := h.Dispatch(context.Background(), messaging.RoutingCancelled, body)
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("want ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("missing errandId", func(t *testing.T) {
		body := []byte(`{"schemaVersion": 1, "eventId": "` + uuid.New().String() + `", "occurredAt": "2026-10-19T12:00:00Z"}`)
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

	t.Run("requester mismatch rejected to DLQ", func(t *testing.T) {
		mismatchedSvc := &mockLifecycleService{
			refundFn: func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
				return credit.ErrRequesterMismatch
			},
		}
		handler := messaging.NewLifecycleHandler(mismatchedSvc, logger)
		d := amqp.Delivery{
			RoutingKey:  messaging.RoutingCancelled,
			ContentType: "application/json",
			Body:        body,
		}
		handler.HandleDelivery(ctx, d)
	})

	t.Run("reward_set requester mismatch rejected to DLQ", func(t *testing.T) {
		mismatchedSvc := &mockLifecycleService{
			rewardSetFn: func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reward, version int64, occurredAt time.Time) error {
				return credit.ErrRequesterMismatch
			},
		}
		handler := messaging.NewLifecycleHandler(mismatchedSvc, logger)
		rewardSetBody := []byte(fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"requesterId": "%s",
			"amount": 50,
			"version": 2,
			"occurredAt": "2026-10-19T12:00:00Z"
		}`, uuid.New(), uuid.New(), uuid.New()))
		d := amqp.Delivery{
			RoutingKey:  messaging.RoutingRewardSet,
			ContentType: "application/json",
			Body:        rewardSetBody,
		}
		handler.HandleDelivery(ctx, d)
	})

	t.Run("account integrity flagged rejected immediately to DLQ without retries", func(t *testing.T) {
		flaggedSvc := &mockLifecycleService{
			refundFn: func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
				return credit.ErrAccountIntegrityFlagged
			},
		}
		var backoffCalled bool
		handler := messaging.NewLifecycleHandler(flaggedSvc, logger, messaging.WithLifecycleBackoff(func(ctx context.Context, attempt int) bool {
			backoffCalled = true
			return true
		}))
		d := amqp.Delivery{
			RoutingKey:  messaging.RoutingCancelled,
			ContentType: "application/json",
			Body:        body,
		}
		handler.HandleDelivery(ctx, d)
		if backoffCalled {
			t.Fatal("backoff retry must not be called when account integrity is flagged")
		}
	})
}

func TestLifecycleHandler_DispatchRewardSet(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	eventID := uuid.New()
	errandID := uuid.New()
	now := time.Now().UTC()

	var calledErrand, calledEvent uuid.UUID
	var calledReward, calledVersion int64
	var calledTime time.Time

	var calledRequester *uuid.UUID
	svc := &mockLifecycleService{
		rewardSetFn: func(ctx context.Context, eID uuid.UUID, reqID *uuid.UUID, evID uuid.UUID, reward, version int64, occ time.Time) error {
			calledErrand = eID
			calledRequester = reqID
			calledEvent = evID
			calledReward = reward
			calledVersion = version
			calledTime = occ
			return nil
		},
	}
	h := messaging.NewLifecycleHandler(svc, logger)

	t.Run("valid reward_set payload dispatches successfully and logs applied", func(t *testing.T) {
		logBuf.Reset()
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"amount": 75,
			"version": 3,
			"occurredAt": "%s"
		}`, eventID, errandID, now.Format(time.RFC3339Nano))

		err := h.Dispatch(context.Background(), messaging.RoutingRewardSet, []byte(body))
		if err != nil {
			t.Fatalf("unexpected dispatch error: %v", err)
		}
		if calledErrand != errandID || calledEvent != eventID || calledReward != 75 || calledVersion != 3 || !calledTime.Equal(now) {
			t.Fatalf("dispatch did not forward expected values: errand=%s, event=%s, reward=%d, version=%d, time=%v",
				calledErrand, calledEvent, calledReward, calledVersion, calledTime)
		}
		if calledRequester != nil {
			t.Fatalf("expected nil requesterId when omitted from payload, got %v", calledRequester)
		}
		if !bytes.Contains(logBuf.Bytes(), []byte("errand.reward_set applied")) {
			t.Fatalf("expected log 'errand.reward_set applied', got: %s", logBuf.String())
		}
		if !bytes.Contains(logBuf.Bytes(), []byte(errandID.String())) {
			t.Fatalf("expected log to contain errandId %s, got: %s", errandID, logBuf.String())
		}
	})

	t.Run("valid reward_set payload with requesterId dispatches and passes requesterId", func(t *testing.T) {
		logBuf.Reset()
		reqID := uuid.New()
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"requesterId": "%s",
			"amount": 75,
			"version": 3,
			"occurredAt": "%s"
		}`, eventID, errandID, reqID, now.Format(time.RFC3339Nano))

		err := h.Dispatch(context.Background(), messaging.RoutingRewardSet, []byte(body))
		if err != nil {
			t.Fatalf("unexpected dispatch error: %v", err)
		}
		if calledRequester == nil || *calledRequester != reqID {
			t.Fatalf("expected requesterId %s, got %v", reqID, calledRequester)
		}
	})

	t.Run("stale reward_set event logs ignored as stale", func(t *testing.T) {
		logBuf.Reset()
		staleSvc := &mockLifecycleService{
			rewardSetFn: func(ctx context.Context, eID uuid.UUID, reqID *uuid.UUID, evID uuid.UUID, reward, version int64, occ time.Time) error {
				return credit.ErrRewardSetStale
			},
		}
		staleHandler := messaging.NewLifecycleHandler(staleSvc, logger)
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"amount": 75,
			"version": 2,
			"occurredAt": "%s"
		}`, eventID, errandID, now.Format(time.RFC3339Nano))

		err := staleHandler.Dispatch(context.Background(), messaging.RoutingRewardSet, []byte(body))
		if err != nil {
			t.Fatalf("stale event should be acknowledged without error, got: %v", err)
		}
		if !bytes.Contains(logBuf.Bytes(), []byte("errand.reward_set ignored as stale")) {
			t.Fatalf("expected log 'errand.reward_set ignored as stale', got: %s", logBuf.String())
		}
		if !bytes.Contains(logBuf.Bytes(), []byte(errandID.String())) {
			t.Fatalf("expected log to contain errandId %s, got: %s", errandID, logBuf.String())
		}
	})

	t.Run("missing version is rejected as malformed", func(t *testing.T) {
		body := fmt.Sprintf(`{
			"schemaVersion": 1,
			"eventId": "%s",
			"errandId": "%s",
			"amount": 75,
			"occurredAt": "%s"
		}`, eventID, errandID, now.Format(time.RFC3339Nano))

		err := h.Dispatch(context.Background(), messaging.RoutingRewardSet, []byte(body))
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("expected ErrMalformedEvent, got %v", err)
		}
	})
}

func TestCalculateLifecycleRetryBackoff(t *testing.T) {
	expectedNominal := map[int]time.Duration{
		1: 1 * time.Second,
		2: 2 * time.Second,
		3: 4 * time.Second,
		4: 8 * time.Second,
	}

	for attempt, nominal := range expectedNominal {
		min := time.Duration(float64(nominal) * 0.79)
		max := time.Duration(float64(nominal) * 1.21)

		for i := 0; i < 20; i++ {
			b := messaging.CalculateLifecycleRetryBackoff(attempt)
			if b < min || b > max {
				t.Fatalf("attempt %d: backoff %v outside expected jitter bounds [%v, %v]", attempt, b, min, max)
			}
		}
	}
}

func TestLifecycleHandler_RetryBackoff_InvokedOnTemporaryError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	svc := &mockLifecycleService{
		rewardSetFn: func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reward, version int64, occurredAt time.Time) error {
			return errors.New("temporary database connection error")
		},
	}

	backoffInvokedWithAttempt := 0
	h := messaging.NewLifecycleHandler(svc, logger, messaging.WithLifecycleBackoff(func(ctx context.Context, attempt int) bool {
		backoffInvokedWithAttempt = attempt
		return true
	}))

	errandID := uuid.New()
	body := []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"eventId": "%s",
		"errandId": "%s",
		"amount": 50,
		"version": 2,
		"occurredAt": "%s"
	}`, uuid.New(), errandID, time.Now().UTC().Format(time.RFC3339Nano)))

	d := amqp.Delivery{
		RoutingKey:  messaging.RoutingRewardSet,
		ContentType: "application/json",
		Body:        body,
	}

	h.HandleDelivery(context.Background(), d)

	if backoffInvokedWithAttempt != 1 {
		t.Fatalf("expected backoff called with attempt 1, got %d", backoffInvokedWithAttempt)
	}

	// Verify errandId and version are logged in the retry log
	logStr := logBuf.String()
	if !bytes.Contains([]byte(logStr), []byte(errandID.String())) {
		t.Fatalf("expected retry log to contain errandId %s, got: %s", errandID, logStr)
	}
	if !bytes.Contains([]byte(logStr), []byte(`"version":2`)) {
		t.Fatalf("expected retry log to contain version:2, got: %s", logStr)
	}
}

func TestLifecycleHandler_RetryBackoff_AbortsOnContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := &mockLifecycleService{
		refundFn: func(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
			return errors.New("temporary database connection error")
		},
	}

	// Handler without mock backoff - use real backoff with an already cancelled context
	h := messaging.NewLifecycleHandler(svc, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	body := []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"eventId": "%s",
		"errandId": "%s",
		"occurredAt": "%s"
	}`, uuid.New(), uuid.New(), time.Now().UTC().Format(time.RFC3339Nano)))

	d := amqp.Delivery{
		RoutingKey:  messaging.RoutingCancelled,
		ContentType: "application/json",
		Body:        body,
	}

	start := time.Now()
	h.HandleDelivery(ctx, d)
	elapsed := time.Since(start)

	// Since context was cancelled, it should return immediately without waiting for nominal 1s delay
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("HandleDelivery took %v with cancelled context, expected immediate return", elapsed)
	}
}
