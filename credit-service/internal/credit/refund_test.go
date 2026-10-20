// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for refund validation, lifecycle error handling, and delegation per sourceOfTruth.md Phase 6.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type settlementRepositoryStub struct {
	called     bool
	errandID   uuid.UUID
	courierID  uuid.UUID
	eventID    uuid.UUID
	reason     TransactionReason
	occurredAt time.Time
	err        error
}

func (s *settlementRepositoryStub) RefundReservation(_ context.Context, errandID uuid.UUID, eventID uuid.UUID, reason TransactionReason, occurredAt time.Time) error {
	s.called = true
	s.errandID = errandID
	s.eventID = eventID
	s.reason = reason
	s.occurredAt = occurredAt
	return s.err
}

func (s *settlementRepositoryStub) PayoutReservation(_ context.Context, errandID uuid.UUID, courierID uuid.UUID, eventID uuid.UUID, _ *int64, occurredAt time.Time) error {
	s.called = true
	s.errandID = errandID
	s.courierID = courierID
	s.eventID = eventID
	s.occurredAt = occurredAt
	return s.err
}

func TestRefundReservationValidations(t *testing.T) {
	repo := &settlementRepositoryStub{}
	service := NewService(nil, WithSettlementRepository(repo))

	validErrandID := uuid.New()
	validEventID := uuid.New()
	validOccurredAt := time.Now().UTC()
	validReason := TransactionReasonErrandCancelled

	t.Run("rejects nil errand ID", func(t *testing.T) {
		err := service.RefundReservation(context.Background(), uuid.Nil, validEventID, validReason, validOccurredAt)
		if !errors.Is(err, ErrInvalidErrandID) {
			t.Fatalf("expected ErrInvalidErrandID, got %v", err)
		}
	})

	t.Run("rejects nil event ID", func(t *testing.T) {
		err := service.RefundReservation(context.Background(), validErrandID, uuid.Nil, validReason, validOccurredAt)
		if !errors.Is(err, ErrInvalidOperationID) {
			t.Fatalf("expected ErrInvalidOperationID, got %v", err)
		}
	})

	t.Run("rejects zero occurredAt", func(t *testing.T) {
		err := service.RefundReservation(context.Background(), validErrandID, validEventID, validReason, time.Time{})
		if err == nil {
			t.Fatal("expected error for zero occurredAt")
		}
	})

	t.Run("rejects non-refund reason", func(t *testing.T) {
		invalidReasons := []TransactionReason{
			TransactionReasonErrandReservation,
			TransactionReasonRewardIncreased,
			TransactionReasonRewardDecreased,
			TransactionReasonErrandCompletedPayout,
			TransactionReasonErrandCompletedDeduct,
			TransactionReasonRegistrationAllocation,
		}
		for _, r := range invalidReasons {
			err := service.RefundReservation(context.Background(), validErrandID, validEventID, r, validOccurredAt)
			if !errors.Is(err, ErrLifecycleViolation) {
				t.Fatalf("expected ErrLifecycleViolation for reason %v, got %v", r, err)
			}
		}
	})

	t.Run("rejects missing repository", func(t *testing.T) {
		svc := NewService(nil)
		err := svc.RefundReservation(context.Background(), validErrandID, validEventID, validReason, validOccurredAt)
		if err == nil {
			t.Fatal("expected error when settlement repository is missing")
		}
	})
}

func TestRefundReservationDelegation(t *testing.T) {
	errandID := uuid.New()
	eventID := uuid.New()
	occurredAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("delegates valid refund reasons", func(t *testing.T) {
		reasons := []TransactionReason{
			TransactionReasonErrandCancelled,
			TransactionReasonErrandExpired,
			TransactionReasonErrandFailed,
		}
		for _, reason := range reasons {
			repo := &settlementRepositoryStub{}
			service := NewService(nil, WithSettlementRepository(repo))

			err := service.RefundReservation(context.Background(), errandID, eventID, reason, occurredAt)
			if err != nil {
				t.Fatalf("unexpected error for reason %s: %v", reason, err)
			}
			if !repo.called {
				t.Fatalf("expected repo to be called for reason %s", reason)
			}
			if repo.errandID != errandID || repo.eventID != eventID || repo.reason != reason || !repo.occurredAt.Equal(occurredAt) {
				t.Fatalf("mismatched repo arguments: got errand=%v, event=%v, reason=%v, occurred=%v",
					repo.errandID, repo.eventID, repo.reason, repo.occurredAt)
			}
		}
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		testErrors := []error{
			ErrReservationNotFound,
			ErrLifecycleViolation,
			ErrWalletNotReady,
			ErrServiceUnavailable,
		}
		for _, targetErr := range testErrors {
			repo := &settlementRepositoryStub{err: targetErr}
			service := NewService(nil, WithSettlementRepository(repo))

			err := service.RefundReservation(context.Background(), errandID, eventID, TransactionReasonErrandCancelled, occurredAt)
			if !errors.Is(err, targetErr) {
				t.Fatalf("expected error %v, got %v", targetErr, err)
			}
		}
	})
}
