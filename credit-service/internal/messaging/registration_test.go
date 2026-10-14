// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-14
// Scope: Added unit tests for user.registered event payload handling.
// Author review: PENDING - reviewer to complete

package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRegistrationHandlerAllocatesInitialCredits(t *testing.T) {
	allocator := &allocatorStub{}
	handler := NewRegistrationHandler(allocator)

	body := []byte(`{
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

func TestRegistrationHandlerRejectsMalformedEvent(t *testing.T) {
	handler := NewRegistrationHandler(&allocatorStub{})

	err := handler.Handle(context.Background(), []byte(`{
		"eventId": "550e8400-e29b-41d4-a716-446655440000",
		"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"occurredAt": "2026-10-14T14:00:00Z",
		"extra": true
	}`))
	if !errors.Is(err, ErrMalformedEvent) {
		t.Fatalf("Handle error = %v, want %v", err, ErrMalformedEvent)
	}
}

func TestRegistrationHandlerReturnsRetryableAllocatorError(t *testing.T) {
	allocatorErr := errors.New("database unavailable")
	handler := NewRegistrationHandler(&allocatorStub{err: allocatorErr})

	err := handler.Handle(context.Background(), []byte(`{
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

type allocatorStub struct {
	userID     uuid.UUID
	occurredAt time.Time
	err        error
}

func (s *allocatorStub) AllocateInitialCredits(_ context.Context, userID uuid.UUID, occurredAt time.Time) error {
	s.userID = userID
	s.occurredAt = occurredAt
	return s.err
}
