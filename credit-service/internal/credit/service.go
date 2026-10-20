// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added CreateReservation, AdjustReservationReward, RefundReservation, PayoutReservation, GetAccount, and ListTransactions service orchestration per sourceOfTruth.md.
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

// WithSettlementRepository injects a specific SettlementRepository.
func WithSettlementRepository(repo SettlementRepository) func(*Service) {
	return func(s *Service) {
		s.settlementRepo = repo
	}
}

// WithReadRepository injects a specific ReadRepository.
func WithReadRepository(repo ReadRepository) func(*Service) {
	return func(s *Service) {
		s.readRepo = repo
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

// AdjustReservationReward validates parameters and requests reward adjustment from the repository.
func (s *Service) AdjustReservationReward(ctx context.Context, errandID uuid.UUID, newReward int64, operationID uuid.UUID) (*OperationResult, *Reservation, error) {
	if errandID == uuid.Nil {
		return nil, nil, ErrInvalidErrandID
	}
	if operationID == uuid.Nil {
		return nil, nil, ErrInvalidOperationID
	}
	if err := ValidateRewardAmount(newReward); err != nil {
		return nil, nil, err
	}
	if s.reservationRepo == nil {
		return nil, nil, errors.New("reservation repository is required")
	}

	return s.reservationRepo.AdjustReservationReward(ctx, errandID, newReward, operationID)
}

// RefundReservation validates refund parameters and requests refund settlement from the repository.
func (s *Service) RefundReservation(ctx context.Context, errandID, eventID uuid.UUID, reason TransactionReason, occurredAt time.Time) error {
	if errandID == uuid.Nil {
		return ErrInvalidErrandID
	}
	if eventID == uuid.Nil {
		return ErrInvalidOperationID
	}
	if occurredAt.IsZero() {
		return errors.New("refund event occurredAt is required")
	}
	switch reason {
	case TransactionReasonErrandCancelled, TransactionReasonErrandExpired, TransactionReasonErrandFailed:
	default:
		return ErrLifecycleViolation
	}
	if s.settlementRepo == nil {
		return errors.New("settlement repository is required")
	}

	return s.settlementRepo.RefundReservation(ctx, errandID, eventID, reason, occurredAt)
}

// PayoutReservation validates completion payout parameters and requests payout settlement from the repository.
func (s *Service) PayoutReservation(ctx context.Context, errandID, courierID, eventID uuid.UUID, occurredAt time.Time) error {
	if errandID == uuid.Nil {
		return ErrInvalidErrandID
	}
	if courierID == uuid.Nil {
		return ErrInvalidUserID
	}
	if eventID == uuid.Nil {
		return ErrInvalidOperationID
	}
	if occurredAt.IsZero() {
		return errors.New("completion event occurredAt is required")
	}
	if s.settlementRepo == nil {
		return errors.New("settlement repository is required")
	}

	return s.settlementRepo.PayoutReservation(ctx, errandID, courierID, eventID, occurredAt)
}

// GetAccount retrieves the account projection for a user.
func (s *Service) GetAccount(ctx context.Context, userID uuid.UUID) (*Account, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}
	if s.readRepo == nil {
		return nil, errors.New("read repository is required")
	}
	return s.readRepo.GetAccount(ctx, userID)
}

// ListTransactions retrieves cursor-paginated grouped operations for a user.
func (s *Service) ListTransactions(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*PaginatedTransactions, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}
	if s.readRepo == nil {
		return nil, errors.New("read repository is required")
	}
	return s.readRepo.ListTransactions(ctx, userID, limit, cursor)
}
