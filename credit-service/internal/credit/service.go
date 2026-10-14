// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated F4.1 service method for async registration allocation handling.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Service coordinates credit-domain operations.
type Service struct {
	initialAllocationRepo InitialAllocationRepository
}

// NewService constructs a credit service with required ports.
func NewService(initialAllocationRepo InitialAllocationRepository) *Service {
	return &Service{
		initialAllocationRepo: initialAllocationRepo,
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

	// call database method
	if err := s.initialAllocationRepo.AllocateInitialCredits(ctx, userID, InitialAllocationCredits, occurredAt.UTC()); err != nil {
		return fmt.Errorf("allocate initial credits: %w", err)
	}
	return nil
}
