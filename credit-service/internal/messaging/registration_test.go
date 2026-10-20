// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Updated unit tests for user.registered event handling to remove Direct Reply-to and verify clean unidirectional event consumption.
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
		t.Fatalf("Handle returned error: %v", err)
	}
	if allocator.userID != uuid.MustParse("6ba7b810-9dad-41d1-80b4-00c04fd430c8") {
		t.Fatalf("userID = %s", allocator.userID)
	}
}

func TestRegistrationHandlerRejectsInvalidPayloads(t *testing.T) {
	handler := NewRegistrationHandler(&allocatorStub{})

	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "invalid json",
			body: []byte(`{"schemaVersion": 1,`),
		},
		{
			name: "unsupported schemaVersion",
			body: []byte(`{
				"schemaVersion": 2,
				"eventId": "550e8400-e29b-41d4-a716-446655440000",
				"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
				"occurredAt": "2026-10-14T14:00:00Z"
			}`),
		},
		{
			name: "missing eventId",
			body: []byte(`{
				"schemaVersion": 1,
				"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
				"occurredAt": "2026-10-14T14:00:00Z"
			}`),
		},
		{
			name: "missing userId",
			body: []byte(`{
				"schemaVersion": 1,
				"eventId": "550e8400-e29b-41d4-a716-446655440000",
				"occurredAt": "2026-10-14T14:00:00Z"
			}`),
		},
		{
			name: "missing occurredAt",
			body: []byte(`{
				"schemaVersion": 1,
				"eventId": "550e8400-e29b-41d4-a716-446655440000",
				"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8"
			}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.Handle(context.Background(), tt.body)
			if !errors.Is(err, ErrMalformedEvent) {
				t.Fatalf("Handle error = %v, want ErrMalformedEvent", err)
			}
		})
	}
}

func TestRegistrationHandlerReturnsRetryableErrors(t *testing.T) {
	allocatorErr := errors.New("database connection refused")
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

	t.Run("successful delivery", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  RoutingUserRegistered,
			ContentType: "application/json",
			Body:        validBody,
		}
		handler.HandleDelivery(ctx, d)
		if allocator.userID == uuid.Nil {
			t.Errorf("expected allocator to be invoked")
		}
	})

	t.Run("malformed message rejected to DLQ", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  RoutingUserRegistered,
			ContentType: "application/json",
			Body:        []byte(`{invalid-json`),
		}
		handler.HandleDelivery(ctx, d)
	})

	t.Run("invalid transport rejected to DLQ", func(t *testing.T) {
		d := amqp.Delivery{
			RoutingKey:  RoutingUserRegistered,
			ContentType: "text/plain",
			Body:        validBody,
		}
		handler.HandleDelivery(ctx, d)
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
