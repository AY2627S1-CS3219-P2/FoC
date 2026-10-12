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
	clock                 func() time.Time
}

// NewService constructs a credit service with required ports.
func NewService(initialAllocationRepo InitialAllocationRepository) *Service {
	return &Service{
		initialAllocationRepo: initialAllocationRepo,
		clock:                 time.Now,
	}
}

// AllocateInitialCredits handles F4.1 for a registered user event.
func (s *Service) AllocateInitialCredits(ctx context.Context, userID uuid.UUID) (*Account, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}
	if s.initialAllocationRepo == nil {
		return nil, fmt.Errorf("initial allocation repository is required")
	}
	account, err := s.initialAllocationRepo.AllocateInitialCredits(ctx, userID, InitialAllocationCredits, s.clock().UTC())
	if err != nil {
		return nil, fmt.Errorf("allocate initial credits: %w", err)
	}
	return account, nil
}
