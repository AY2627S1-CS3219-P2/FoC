// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated F4.1 unit tests for async registration allocation service behavior.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceAllocateInitialCredits(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	occurredAt := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	repository := &allocationRepositoryStub{}
	service := NewService(repository)

	if err := service.AllocateInitialCredits(context.Background(), userID, occurredAt); err != nil {
		t.Fatalf("AllocateInitialCredits returned error: %v", err)
	}
	if repository.userID != userID {
		t.Fatalf("repository user ID = %s, want %s", repository.userID, userID)
	}
	if repository.amount != InitialAllocationCredits {
		t.Fatalf("repository amount = %d, want %d", repository.amount, InitialAllocationCredits)
	}
	if !repository.occurredAt.Equal(occurredAt) {
		t.Fatalf("repository occurredAt = %s, want %s", repository.occurredAt, occurredAt)
	}
}

func TestServiceAllocateInitialCreditsRejectsMissingUserID(t *testing.T) {
	repository := &allocationRepositoryStub{}
	service := NewService(repository)

	err := service.AllocateInitialCredits(context.Background(), uuid.Nil, time.Now())
	if !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("AllocateInitialCredits error = %v, want %v", err, ErrInvalidUserID)
	}
	if repository.called {
		t.Fatal("repository should not be called for invalid user ID")
	}
}

func TestServiceAllocateInitialCreditsWrapsRepositoryError(t *testing.T) {
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	repositoryErr := errors.New("repository unavailable")
	service := NewService(&allocationRepositoryStub{err: repositoryErr})
	occurredAt := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	err := service.AllocateInitialCredits(context.Background(), userID, occurredAt)
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("AllocateInitialCredits error = %v, want wrapped %v", err, repositoryErr)
	}
}

type allocationRepositoryStub struct {
	called     bool
	userID     uuid.UUID
	amount     int
	occurredAt time.Time
	err        error
}

func (s *allocationRepositoryStub) AllocateInitialCredits(_ context.Context, userID uuid.UUID, amount int, occurredAt time.Time) error {
	s.called = true
	s.userID = userID
	s.amount = amount
	s.occurredAt = occurredAt
	return s.err
}
