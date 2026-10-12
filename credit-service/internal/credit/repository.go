// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated F4.1 repository port for idempotent initial allocation.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// InitialAllocationRepository persists initial account creation and its ledger entry atomically.
type InitialAllocationRepository interface {
	AllocateInitialCredits(ctx context.Context, userID uuid.UUID, amount int, occurredAt time.Time) error
}
