// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for reservation creation validation, identical retry, conflicts, and error propagation per sourceOfTruth.md Phase 4.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type reservationRepositoryStub struct {
	called      bool
	errandID    uuid.UUID
	requesterID uuid.UUID
	amount      int64
	reservation *Reservation
	created     bool
	err         error
}

func (s *reservationRepositoryStub) CreateReservation(_ context.Context, errandID uuid.UUID, requesterID uuid.UUID, amount int64) (*Reservation, bool, error) {
	s.called = true
	s.errandID = errandID
	s.requesterID = requesterID
	s.amount = amount
	return s.reservation, s.created, s.err
}

func (s *reservationRepositoryStub) AdjustReservationReward(_ context.Context, _ uuid.UUID, _ int64, _ int64, _ uuid.UUID) (*OperationResult, *Reservation, error) {
	return nil, nil, nil
}

func TestCreateReservationValidations(t *testing.T) {
	repo := &reservationRepositoryStub{}
	service := NewService(nil, WithReservationRepository(repo))

	validErrandID := uuid.New()
	validRequesterID := uuid.New()
	validAmount := int64(100)

	t.Run("rejects nil errand ID", func(t *testing.T) {
		_, _, err := service.CreateReservation(context.Background(), uuid.Nil, validRequesterID, validAmount)
		if !errors.Is(err, ErrInvalidErrandID) {
			t.Fatalf("expected ErrInvalidErrandID, got %v", err)
		}
	})

	t.Run("rejects nil requester ID", func(t *testing.T) {
		_, _, err := service.CreateReservation(context.Background(), validErrandID, uuid.Nil, validAmount)
		if !errors.Is(err, ErrInvalidUserID) {
			t.Fatalf("expected ErrInvalidUserID, got %v", err)
		}
	})

	t.Run("rejects zero amount", func(t *testing.T) {
		_, _, err := service.CreateReservation(context.Background(), validErrandID, validRequesterID, 0)
		if !errors.Is(err, ErrInvalidRewardAmount) {
			t.Fatalf("expected ErrInvalidRewardAmount, got %v", err)
		}
	})

	t.Run("rejects negative amount", func(t *testing.T) {
		_, _, err := service.CreateReservation(context.Background(), validErrandID, validRequesterID, -50)
		if !errors.Is(err, ErrInvalidRewardAmount) {
			t.Fatalf("expected ErrInvalidRewardAmount, got %v", err)
		}
	})

	t.Run("rejects amount above maximum", func(t *testing.T) {
		_, _, err := service.CreateReservation(context.Background(), validErrandID, validRequesterID, MaxErrandReward+1)
		if !errors.Is(err, ErrInvalidRewardAmount) {
			t.Fatalf("expected ErrInvalidRewardAmount, got %v", err)
		}
	})

	t.Run("rejects missing repository", func(t *testing.T) {
		svc := NewService(nil)
		_, _, err := svc.CreateReservation(context.Background(), validErrandID, validRequesterID, validAmount)
		if err == nil {
			t.Fatal("expected error when reservation repository is missing")
		}
	})
}

func TestCreateReservationHappyPath(t *testing.T) {
	errandID := uuid.New()
	requesterID := uuid.New()
	amount := int64(500)
	expectedRes := &Reservation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		RequesterID: requesterID,
		Amount:      amount,
		Status:      ReservationStatusReserved,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	repo := &reservationRepositoryStub{
		reservation: expectedRes,
		created:     true,
	}
	service := NewService(nil, WithReservationRepository(repo))

	res, created, err := service.CreateReservation(context.Background(), errandID, requesterID, amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatalf("expected created = true for new reservation")
	}
	if res != expectedRes {
		t.Fatalf("expected reservation %+v, got %+v", expectedRes, res)
	}
	if !repo.called || repo.errandID != errandID || repo.requesterID != requesterID || repo.amount != amount {
		t.Fatalf("repository called with wrong args: %+v", repo)
	}
}

func TestCreateReservationIdenticalRetry(t *testing.T) {
	errandID := uuid.New()
	requesterID := uuid.New()
	amount := int64(500)
	existingRes := &Reservation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		RequesterID: requesterID,
		Amount:      amount,
		Status:      ReservationStatusReserved,
		CreatedAt:   time.Now().Add(-time.Hour),
		UpdatedAt:   time.Now().Add(-time.Hour),
	}

	repo := &reservationRepositoryStub{
		reservation: existingRes,
		created:     false,
	}
	service := NewService(nil, WithReservationRepository(repo))

	res, created, err := service.CreateReservation(context.Background(), errandID, requesterID, amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatalf("expected created = false for identical retry")
	}
	if res != existingRes {
		t.Fatalf("expected existing reservation, got %+v", res)
	}
}

func TestCreateReservationErrorPropagation(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{"wallet not ready", ErrWalletNotReady},
		{"insufficient credits", ErrInsufficientCredits},
		{"reservation conflict", ErrReservationConflict},
		{"reservation state conflict", ErrReservationStateConflict},
		{"integrity flagged", ErrAccountIntegrityFlagged},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &reservationRepositoryStub{err: tc.err}
			service := NewService(nil, WithReservationRepository(repo))

			_, _, err := service.CreateReservation(context.Background(), uuid.New(), uuid.New(), 100)
			if !errors.Is(err, tc.err) {
				t.Fatalf("expected error %v, got %v", tc.err, err)
			}
		})
	}
}
