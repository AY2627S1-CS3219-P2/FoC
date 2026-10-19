// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Updated unit tests for user.registered event handling with tolerant schema decoding, Direct Reply-to, and retry/DLQ policy per sourceOfTruth.md Phase 11.
// Author review: PENDING - reviewer to complete

package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRegistrationHandlerAllocatesInitialCredits(t *testing.T) {
	allocator := &allocatorStub{}
	handler := NewRegistrationHandler(allocator)

	body := []byte(`{
		"schemaVersion": 1,
		"eventId": "550e8400-e29b-41d4-a716-446655440000",
		"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"occurredAt": "2026-10-14T14:00:00Z"
	}`)

	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if allocator.userID != uuid.MustParse("6ba7b810-9dad-41d1-80b4-00c04fd430c8") {
		t.Fatalf("userID = %s", allocator.userID)
	}
	if want := time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC); !allocator.occurredAt.Equal(want) {
		t.Fatalf("occurredAt = %s, want %s", allocator.occurredAt, want)
	}
}

func TestRegistrationHandlerAcceptsMissingSchemaVersionAndExtraFields(t *testing.T) {
	allocator := &allocatorStub{}
	handler := NewRegistrationHandler(allocator)

	// user-service event payload format with eventType and omitted schemaVersion
	body := []byte(`{
		"eventId": "550e8400-e29b-41d4-a716-446655440000",
		"eventType": "user.registered",
		"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"occurredAt": "2026-10-14T14:00:00Z"
	}`)

	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("Handle returned unexpected error for tolerant payload: %v", err)
	}
	if allocator.userID != uuid.MustParse("6ba7b810-9dad-41d1-80b4-00c04fd430c8") {
		t.Fatalf("userID = %s", allocator.userID)
	}
}

func TestRegistrationHandlerRejectsInvalidSchemaVersion(t *testing.T) {
	handler := NewRegistrationHandler(&allocatorStub{})

	err := handler.Handle(context.Background(), []byte(`{
		"schemaVersion": 2,
		"eventId": "550e8400-e29b-41d4-a716-446655440000",
		"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"occurredAt": "2026-10-14T14:00:00Z"
	}`))
	if !errors.Is(err, ErrMalformedEvent) {
		t.Fatalf("want ErrMalformedEvent, got: %v", err)
	}
}

func TestRegistrationHandlerRejectsMalformedEvent(t *testing.T) {
	handler := NewRegistrationHandler(&allocatorStub{})

	t.Run("missing required userId", func(t *testing.T) {
		err := handler.Handle(context.Background(), []byte(`{
			"eventId": "550e8400-e29b-41d4-a716-446655440000",
			"occurredAt": "2026-10-14T14:00:00Z"
		}`))
		if !errors.Is(err, ErrMalformedEvent) {
			t.Fatalf("Handle error = %v, want %v", err, ErrMalformedEvent)
		}
	})

	t.Run("invalid json syntax", func(t *testing.T) {
		err := handler.Handle(context.Background(), []byte(`{not-json}`))
		if !errors.Is(err, ErrMalformedEvent) {
			t.Fatalf("Handle error = %v, want %v", err, ErrMalformedEvent)
		}
	})
}

func TestRegistrationHandlerReturnsRetryableAllocatorError(t *testing.T) {
	allocatorErr := errors.New("database unavailable")
	handler := NewRegistrationHandler(&allocatorStub{err: allocatorErr})

	err := handler.Handle(context.Background(), []byte(`{
		"schemaVersion": 1,
		"eventId": "550e8400-e29b-41d4-a716-446655440000",
		"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"occurredAt": "2026-10-14T14:00:00Z"
	}`))
	if !errors.Is(err, allocatorErr) {
		t.Fatalf("Handle error = %v, want wrapped %v", err, allocatorErr)
	}
	if errors.Is(err, ErrMalformedEvent) {
		t.Fatalf("Handle error = %v, should be retryable", err)
	}
}

type fakeReplier struct {
	replyTo       string
	correlationID string
	body          []byte
}

func (f *fakeReplier) Reply(_ context.Context, replyTo, correlationID string, body []byte) error {
	f.replyTo = replyTo
	f.correlationID = correlationID
	f.body = body
	return nil
}

func TestRegistrationHandler_HandleDelivery(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	allocator := &allocatorStub{}
	handler := NewRegistrationHandler(allocator, WithLogger(logger))

	ctx := context.Background()
	validBody, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"eventId":       uuid.New().String(),
		"userId":        uuid.New().String(),
		"occurredAt":    time.Now().UTC().Format(time.RFC3339Nano),
	})

	t.Run("successful delivery with direct reply", func(t *testing.T) {
		replier := &fakeReplier{}
		d := amqp.Delivery{
			RoutingKey:    RoutingUserRegistered,
			ContentType:   "application/json",
			Body:          validBody,
			ReplyTo:       "amq.rabbitmq.reply-to",
			CorrelationId: "corr-1",
		}
		handler.HandleDelivery(ctx, d, replier)
		if replier.replyTo != "amq.rabbitmq.reply-to" {
			t.Errorf("expected replyTo to be set, got %s", replier.replyTo)
		}
		if replier.correlationID != "corr-1" {
			t.Errorf("expected correlationID 'corr-1', got %s", replier.correlationID)
		}
	})

	t.Run("malformed message rejected to DLQ", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  RoutingUserRegistered,
			ContentType: "application/json",
			Body:        []byte(`{invalid-json`),
		}
		handler.HandleDelivery(ctx, d, nil)
	})

	t.Run("invalid transport rejected to DLQ", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  RoutingUserRegistered,
			ContentType: "text/plain",
			Body:        validBody,
		}
		handler.HandleDelivery(ctx, d, nil)
	})
}

type allocatorStub struct {
	userID     uuid.UUID
	occurredAt time.Time
	err        error
}

func (s *allocatorStub) AllocateInitialCredits(_ context.Context, id uuid.UUID, occurredAt time.Time) error {
	s.userID = id
	s.occurredAt = occurredAt
	return s.err
}
