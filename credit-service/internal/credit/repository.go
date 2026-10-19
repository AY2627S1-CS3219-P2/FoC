// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Defined focused repository ports for credit-domain operations and background workers per sourceOfTruth.md.
// Author review: PENDING - reviewer to complete

package credit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// InitialAllocationRepository persists initial account creation and its ledger entry atomically.
type InitialAllocationRepository interface {
	AllocateInitialCredits(ctx context.Context, userID uuid.UUID, amount int64, occurredAt time.Time) error
}

// ReservationRepository manages reservation creation and reward adjustment transactions.
type ReservationRepository interface {
	// CreateReservation creates a new reservation atomically or returns the existing reservation if identical retry.
	// Returns created=true on new creation, or created=false on identical retry.
	CreateReservation(ctx context.Context, errandID uuid.UUID, requesterID uuid.UUID, amount int64) (*Reservation, bool, error)

	// AdjustReservationReward modifies an existing reservation's reward amount idempotently.
	AdjustReservationReward(ctx context.Context, errandID uuid.UUID, currentAmount int64, newAmount int64, operationID uuid.UUID) (*OperationResult, *Reservation, error)
}

// SettlementRepository manages refunds and payouts from asynchronous errand lifecycle events.
type SettlementRepository interface {
	// RefundReservation settles a cancelled, expired, or failed errand back to the requester.
	RefundReservation(ctx context.Context, errandID uuid.UUID, eventID uuid.UUID, reason TransactionReason, occurredAt time.Time) error

	// PayoutReservation settles a completed errand from the requester's hold to the courier's available balance.
	PayoutReservation(ctx context.Context, errandID uuid.UUID, courierID uuid.UUID, eventID uuid.UUID, occurredAt time.Time) error
}

// ReadRepository provides queries for authenticated user balance and paginated history reads.
type ReadRepository interface {
	// GetAccount retrieves the account projection for a user.
	GetAccount(ctx context.Context, userID uuid.UUID) (*Account, error)

	// ListTransactions retrieves cursor-paginated grouped financial operations for a user.
	ListTransactions(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*PaginatedTransactions, error)
}

// PendingRefundRepository claims and manages blocked pending refunds.
type PendingRefundRepository interface {
	ClaimDuePendingRefunds(ctx context.Context, limit int) ([]PendingRefund, error)
	ProcessPendingRefund(ctx context.Context, id uuid.UUID, retryInterval time.Duration) error
	RecordPendingRefundOverdue(ctx context.Context, id uuid.UUID, alertedAt time.Time) error
}

// PendingPayoutRepository claims and manages delayed pending payouts.
type PendingPayoutRepository interface {
	ClaimDuePendingPayouts(ctx context.Context, limit int) ([]PendingPayout, error)
	ProcessPendingPayout(ctx context.Context, id uuid.UUID, retryInterval time.Duration) error
	RecordPendingPayoutOverdue(ctx context.Context, id uuid.UUID, alertedAt time.Time) error
}

// ReconciliationRepository handles periodic audit scans and discrepancy detection.
type ReconciliationRepository interface {
	// AcquireReconciliationLock attempts to acquire a session-level advisory lock on a dedicated connection.
	// Returns an unlock function and true if acquired; false if another replica holds the lock.
	AcquireReconciliationLock(ctx context.Context) (func(), bool, error)

	// ScanAccountsForReconciliation returns a batch of user IDs advancing by keyset cursor.
	ScanAccountsForReconciliation(ctx context.Context, batchSize int, lastSeenUserID *uuid.UUID) ([]uuid.UUID, *uuid.UUID, error)

	// ReconcileAccount audit-checks an account against the ledger and active reservations, flagging on discrepancy.
	// Returns true if a discrepancy was flagged, or false if consistent or stale version was discarded.
	ReconcileAccount(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Repository combines the domain's persistence interfaces.
type Repository interface {
	InitialAllocationRepository
	ReservationRepository
	SettlementRepository
	ReadRepository
	PendingRefundRepository
	PendingPayoutRepository
	ReconciliationRepository
}
