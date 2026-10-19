// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added CreateReservation service orchestration and repository port binding per sourceOfTruth.md Phase 4.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Service coordinates credit-domain operations.
type Service struct {
	initialAllocationRepo InitialAllocationRepository
	reservationRepo       ReservationRepository
	settlementRepo        SettlementRepository
	readRepo              ReadRepository
}

// NewService constructs a credit service with required ports.
func NewService(initialAllocationRepo InitialAllocationRepository, opts ...func(*Service)) *Service {
	s := &Service{initialAllocationRepo: initialAllocationRepo}
	if r, ok := initialAllocationRepo.(ReservationRepository); ok {
		s.reservationRepo = r
	}
	if sRepo, ok := initialAllocationRepo.(SettlementRepository); ok {
		s.settlementRepo = sRepo
	}
	if rRepo, ok := initialAllocationRepo.(ReadRepository); ok {
		s.readRepo = rRepo
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithReservationRepository injects a specific ReservationRepository.
func WithReservationRepository(repo ReservationRepository) func(*Service) {
	return func(s *Service) {
		s.reservationRepo = repo
	}
}

// AllocateInitialCredits handles F4.1 for a registered user event.
func (s *Service) AllocateInitialCredits(ctx context.Context, userID uuid.UUID, occurredAt time.Time) error {
	if userID == uuid.Nil {
		return ErrInvalidUserID
	}
	if occurredAt.IsZero() {
		return fmt.Errorf("registration event occurredAt is required")
	}
	if s.initialAllocationRepo == nil {
		return fmt.Errorf("initial allocation repository is required")
	}

	if err := s.initialAllocationRepo.AllocateInitialCredits(ctx, userID, InitialAllocationCredits, occurredAt.UTC()); err != nil {
		return fmt.Errorf("allocate initial credits: %w", err)
	}
	return nil
}

// CreateReservation validates parameters and requests reservation creation from the repository.
func (s *Service) CreateReservation(ctx context.Context, errandID, requesterID uuid.UUID, amount int64) (*Reservation, bool, error) {
	if errandID == uuid.Nil {
		return nil, false, ErrInvalidErrandID
	}
	if requesterID == uuid.Nil {
		return nil, false, ErrInvalidUserID
	}
	if err := ValidateRewardAmount(amount); err != nil {
		return nil, false, err
	}
	if s.reservationRepo == nil {
		return nil, false, errors.New("reservation repository is required")
	}

	return s.reservationRepo.CreateReservation(ctx, errandID, requesterID, amount)
}
