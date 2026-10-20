// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Unit tests for target-state reward adjustment validation, idempotency, and error propagation per sourceOfTruth.md Phase 5.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type adjustmentRepositoryStub struct {
	called      bool
	errandID    uuid.UUID
	newReward   int64
	operationID uuid.UUID
	opResult    *OperationResult
	reservation *Reservation
	err         error
}

func (s *adjustmentRepositoryStub) CreateReservation(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ int64) (*Reservation, bool, error) {
	return nil, false, nil
}

func (s *adjustmentRepositoryStub) AdjustReservationReward(_ context.Context, errandID uuid.UUID, newReward int64, operationID uuid.UUID) (*OperationResult, *Reservation, error) {
	s.called = true
	s.errandID = errandID
	s.newReward = newReward
	s.operationID = operationID
	return s.opResult, s.reservation, s.err
}

func TestAdjustReservationRewardValidations(t *testing.T) {
	repo := &adjustmentRepositoryStub{}
	service := NewService(nil, WithReservationRepository(repo))

	validErrandID := uuid.New()
	validOpID := uuid.New()
	validNew := int64(150)

	t.Run("rejects nil errand ID", func(t *testing.T) {
		_, _, err := service.AdjustReservationReward(context.Background(), uuid.Nil, validNew, validOpID)
		if !errors.Is(err, ErrInvalidErrandID) {
			t.Fatalf("expected ErrInvalidErrandID, got %v", err)
		}
	})

	t.Run("rejects nil operation ID", func(t *testing.T) {
		_, _, err := service.AdjustReservationReward(context.Background(), validErrandID, validNew, uuid.Nil)
		if !errors.Is(err, ErrInvalidOperationID) {
			t.Fatalf("expected ErrInvalidOperationID, got %v", err)
		}
	})

	t.Run("rejects zero new amount", func(t *testing.T) {
		_, _, err := service.AdjustReservationReward(context.Background(), validErrandID, 0, validOpID)
		if !errors.Is(err, ErrInvalidRewardAmount) {
			t.Fatalf("expected ErrInvalidRewardAmount, got %v", err)
		}
	})

	t.Run("rejects negative new amount", func(t *testing.T) {
		_, _, err := service.AdjustReservationReward(context.Background(), validErrandID, -5, validOpID)
		if !errors.Is(err, ErrInvalidRewardAmount) {
			t.Fatalf("expected ErrInvalidRewardAmount, got %v", err)
		}
	})

	t.Run("rejects new amount above max", func(t *testing.T) {
		_, _, err := service.AdjustReservationReward(context.Background(), validErrandID, MaxErrandReward+1, validOpID)
		if !errors.Is(err, ErrInvalidRewardAmount) {
			t.Fatalf("expected ErrInvalidRewardAmount, got %v", err)
		}
	})

	t.Run("rejects missing repository", func(t *testing.T) {
		svc := NewService(nil)
		_, _, err := svc.AdjustReservationReward(context.Background(), validErrandID, validNew, validOpID)
		if err == nil {
			t.Fatal("expected error when reservation repository is missing")
		}
	})
}

func TestAdjustReservationRewardDelegation(t *testing.T) {
	errandID := uuid.New()
	opID := uuid.New()
	currentAmount := int64(200)
	newAmount := int64(300)

	expectedOp := &OperationResult{
		OperationID:     opID,
		OperationType:   OperationTypeRewardAdjustment,
		ErrandID:        errandID,
		CurrentAmount:   currentAmount,
		NewAmount:       newAmount,
		OutcomeCode:     OutcomeSuccess,
		ResultingAmount: &newAmount,
		CreatedAt:       time.Now().UTC(),
	}
	expectedReservation := &Reservation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		RequesterID: uuid.New(),
		Amount:      newAmount,
		Status:      ReservationStatusReserved,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	repo := &adjustmentRepositoryStub{
		opResult:    expectedOp,
		reservation: expectedReservation,
	}
	service := NewService(nil, WithReservationRepository(repo))

	t.Run("successfully adjusts reward", func(t *testing.T) {
		op, res, err := service.AdjustReservationReward(context.Background(), errandID, newAmount, opID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !repo.called {
			t.Fatal("expected repository to be called")
		}
		if op != expectedOp {
			t.Fatalf("expected op %v, got %v", expectedOp, op)
		}
		if res != expectedReservation {
			t.Fatalf("expected res %v, got %v", expectedReservation, res)
		}
	})

	t.Run("propagates idempotency conflict error", func(t *testing.T) {
		repo.err = ErrIdempotencyConflict
		_, _, err := service.AdjustReservationReward(context.Background(), errandID, newAmount, opID)
		if !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
		}
	})

	t.Run("propagates insufficient credits error", func(t *testing.T) {
		repo.err = ErrInsufficientCredits
		_, _, err := service.AdjustReservationReward(context.Background(), errandID, newAmount, opID)
		if !errors.Is(err, ErrInsufficientCredits) {
			t.Fatalf("expected ErrInsufficientCredits, got %v", err)
		}
	})
}
