// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for completion payout validation, error propagation, and delegation per sourceOfTruth.md Phase 7.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPayoutReservationValidations(t *testing.T) {
	repo := &settlementRepositoryStub{}
	service := NewService(nil, WithSettlementRepository(repo))

	validErrandID := uuid.New()
	validCourierID := uuid.New()
	validEventID := uuid.New()
	validOccurredAt := time.Now().UTC()

	t.Run("rejects nil errand ID", func(t *testing.T) {
		err := service.PayoutReservation(context.Background(), uuid.Nil, validCourierID, validEventID, nil, validOccurredAt)
		if !errors.Is(err, ErrInvalidErrandID) {
			t.Fatalf("expected ErrInvalidErrandID, got %v", err)
		}
	})

	t.Run("rejects nil courier ID", func(t *testing.T) {
		err := service.PayoutReservation(context.Background(), validErrandID, uuid.Nil, validEventID, nil, validOccurredAt)
		if !errors.Is(err, ErrInvalidUserID) {
			t.Fatalf("expected ErrInvalidUserID, got %v", err)
		}
	})

	t.Run("rejects nil event ID", func(t *testing.T) {
		err := service.PayoutReservation(context.Background(), validErrandID, validCourierID, uuid.Nil, nil, validOccurredAt)
		if !errors.Is(err, ErrInvalidOperationID) {
			t.Fatalf("expected ErrInvalidOperationID, got %v", err)
		}
	})

	t.Run("rejects zero occurredAt", func(t *testing.T) {
		err := service.PayoutReservation(context.Background(), validErrandID, validCourierID, validEventID, nil, time.Time{})
		if err == nil {
			t.Fatal("expected error for zero occurredAt")
		}
	})

	t.Run("rejects missing repository", func(t *testing.T) {
		svc := NewService(nil)
		err := svc.PayoutReservation(context.Background(), validErrandID, validCourierID, validEventID, nil, validOccurredAt)
		if err == nil {
			t.Fatal("expected error when settlement repository is missing")
		}
	})
}

func TestPayoutReservationDelegation(t *testing.T) {
	errandID := uuid.New()
	courierID := uuid.New()
	eventID := uuid.New()
	occurredAt := time.Date(2026, 10, 19, 13, 30, 0, 0, time.UTC)

	t.Run("delegates valid completion payout", func(t *testing.T) {
		repo := &settlementRepositoryStub{}
		service := NewService(nil, WithSettlementRepository(repo))

		err := service.PayoutReservation(context.Background(), errandID, courierID, eventID, nil, occurredAt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !repo.called {
			t.Fatal("expected repo to be called")
		}
		if repo.errandID != errandID || repo.courierID != courierID || repo.eventID != eventID || !repo.occurredAt.Equal(occurredAt) {
			t.Fatalf("mismatched repo arguments: got errand=%v, courier=%v, event=%v, occurred=%v",
				repo.errandID, repo.courierID, repo.eventID, repo.occurredAt)
		}
	})

	t.Run("propagates self-payout error", func(t *testing.T) {
		repo := &settlementRepositoryStub{err: ErrSelfPayoutNotAllowed}
		service := NewService(nil, WithSettlementRepository(repo))

		err := service.PayoutReservation(context.Background(), errandID, courierID, eventID, nil, occurredAt)
		if !errors.Is(err, ErrSelfPayoutNotAllowed) {
			t.Fatalf("expected ErrSelfPayoutNotAllowed, got %v", err)
		}
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		testErrors := []error{
			ErrReservationNotFound,
			ErrLifecycleViolation,
			ErrWalletNotReady,
			ErrCreditLimitExceeded,
			ErrServiceUnavailable,
		}
		for _, targetErr := range testErrors {
			repo := &settlementRepositoryStub{err: targetErr}
			service := NewService(nil, WithSettlementRepository(repo))

			err := service.PayoutReservation(context.Background(), errandID, courierID, eventID, nil, occurredAt)
			if !errors.Is(err, targetErr) {
				t.Fatalf("expected error %v, got %v", targetErr, err)
			}
		}
	})
}
