// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated F4.1 credit domain models and constants from docs/requirements.md.
// Author review: PENDING - reviewer to complete

package credit

import (
	"time"

	"github.com/google/uuid"
)

// InitialAllocationCredits is the recorded F4.1 registration allocation amount.
const InitialAllocationCredits = 100

// Account is the credit balance aggregate for one user.
type Account struct {
	UserID           uuid.UUID
	AvailableCredits int
	ReservedCredits  int
	Version          int
	UpdatedAt        time.Time
}

// TransactionReason identifies why a ledger entry was created.
type TransactionReason string

const (
	// TransactionReasonRegistrationAllocation records the initial credit grant.
	TransactionReasonRegistrationAllocation TransactionReason = "REGISTRATION_ALLOCATION"
)
