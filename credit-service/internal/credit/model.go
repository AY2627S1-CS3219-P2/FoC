// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Completed Credit Service domain models, enums, limits, and checked arithmetic helpers per sourceOfTruth.md Phase 3.
// Author review: PENDING - reviewer to complete

package credit

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// Constants for business rules and product limits from sourceOfTruth.md.
const (
	InitialAllocationCredits int64 = 100
	MinErrandReward          int64 = 1
	MaxErrandReward          int64 = 1_000_000
	MinBalanceCredits        int64 = 0
	MaxBalanceCredits        int64 = 1_000_000_000
)

// AccountIntegrityStatus indicates whether an account projection matches the ledger.
type AccountIntegrityStatus string

const (
	IntegrityConsistent AccountIntegrityStatus = "CONSISTENT"
	IntegrityFlagged    AccountIntegrityStatus = "FLAGGED"
)

// Account is the credit balance aggregate for one user.
type Account struct {
	UserID              uuid.UUID
	AvailableCredits    int64
	ReservedCredits     int64
	Version             int64
	IntegrityStatus     AccountIntegrityStatus
	IntegrityFlaggedAt  *time.Time
	IntegrityReasonCode *string
	IntegrityDetail     *string
	IntegrityExpected   *float64
	IntegrityObserved   *float64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ReservationStatus represents the financial lifecycle state of one reservation.
type ReservationStatus string

const (
	ReservationStatusReserved ReservationStatus = "RESERVED"
	ReservationStatusPaidOut  ReservationStatus = "PAID_OUT"
	ReservationStatusRefunded ReservationStatus = "REFUNDED"
)

// Reservation represents credit reserved for one errand.
type Reservation struct {
	ID          uuid.UUID
	ErrandID    uuid.UUID
	RequesterID uuid.UUID
	Amount      int64
	Status      ReservationStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BalanceBucket specifies which balance bucket is affected by a transaction.
type BalanceBucket string

const (
	BucketAvailable BalanceBucket = "AVAILABLE"
	BucketReserved  BalanceBucket = "RESERVED"
)

// TransactionReason identifies why a ledger entry was created.
type TransactionReason string

const (
	TransactionReasonRegistrationAllocation TransactionReason = "REGISTRATION_ALLOCATION"
	TransactionReasonErrandReservation      TransactionReason = "ERRAND_RESERVATION"
	TransactionReasonErrandFailed           TransactionReason = "ERRAND_FAILED"
	TransactionReasonErrandCompletedPayout  TransactionReason = "ERRAND_COMPLETED_PAYOUT"
	TransactionReasonErrandCompletedDeduct  TransactionReason = "ERRAND_COMPLETED_DEDUCTION"
	TransactionReasonErrandCancelled        TransactionReason = "ERRAND_CANCELLED"
	TransactionReasonErrandExpired          TransactionReason = "ERRAND_EXPIRED"
	TransactionReasonRewardIncreased        TransactionReason = "REWARD_INCREASED"
	TransactionReasonRewardDecreased        TransactionReason = "REWARD_DECREASED"
)

// CreditTransaction is one append-only ledger entry.
type CreditTransaction struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	ErrandID      *uuid.UUID
	OperationID   uuid.UUID
	BalanceBucket BalanceBucket
	Amount        int64
	Reason        TransactionReason
	OccurredAt    *time.Time
	CreatedAt     time.Time
}

// TransactionOperation represents grouped ledger movements under one operation for user-facing history.
type TransactionOperation struct {
	OperationID    uuid.UUID
	ErrandID       *uuid.UUID
	Reason         TransactionReason
	AvailableDelta int64
	ReservedDelta  int64
	OccurredAt     *time.Time
	CreatedAt      time.Time
}

// PaginatedTransactions contains a page of grouped transaction operations and the next keyset cursor.
type PaginatedTransactions struct {
	Items      []TransactionOperation
	NextCursor *string
}

// CreditOperationType identifies the idempotent command type for operation results.
type CreditOperationType string

const (
	OperationTypeRewardAdjustment CreditOperationType = "REWARD_ADJUSTMENT"
)

// CreditOperationOutcome stores the deterministic outcome of an idempotent command.
type CreditOperationOutcome string

const (
	OutcomeSuccess                   CreditOperationOutcome = "SUCCESS"
	OutcomeWalletNotReady            CreditOperationOutcome = "WALLET_NOT_READY"
	OutcomeInsufficientCredits       CreditOperationOutcome = "INSUFFICIENT_CREDITS"
	OutcomeReservationStateConflict  CreditOperationOutcome = "RESERVATION_STATE_CONFLICT"
	OutcomeReservationAmountMismatch CreditOperationOutcome = "RESERVATION_AMOUNT_MISMATCH"
	OutcomeAccountIntegrityFailure   CreditOperationOutcome = "ACCOUNT_INTEGRITY_FAILURE"
	OutcomeCreditLimitExceeded       CreditOperationOutcome = "CREDIT_LIMIT_EXCEEDED"
)

// OperationResult is a persisted idempotency record for reward adjustments.
type OperationResult struct {
	OperationID         uuid.UUID
	OperationType       CreditOperationType
	ErrandID            uuid.UUID
	CurrentAmount       int64
	NewAmount           int64
	OutcomeCode         CreditOperationOutcome
	ResultingAmount     *int64
	AuthoritativeAmount *int64
	CreatedAt           time.Time
}

// PendingRefund preserves a refund that cannot proceed due to account integrity flags.
type PendingRefund struct {
	ID               uuid.UUID
	ErrandID         uuid.UUID
	EventID          uuid.UUID
	Reason           TransactionReason
	OccurredAt       time.Time
	FirstSeenAt      time.Time
	NextAttemptAt    time.Time
	AttemptCount     int64
	LastAttemptAt    *time.Time
	LastError        *string
	OverdueAlertedAt *time.Time
}

// PendingPayoutBlockedReason describes why a payout was delayed.
type PendingPayoutBlockedReason string

const (
	BlockedReasonMissingWallet          PendingPayoutBlockedReason = "MISSING_WALLET"
	BlockedReasonAccountFlagged         PendingPayoutBlockedReason = "ACCOUNT_FLAGGED"
	BlockedReasonAvailableLimitExceeded PendingPayoutBlockedReason = "AVAILABLE_LIMIT_EXCEEDED"
)

// PendingPayout preserves a completion payout when the courier wallet is missing or accounts are blocked.
type PendingPayout struct {
	ID               uuid.UUID
	ErrandID         uuid.UUID
	EventID          uuid.UUID
	CourierID        uuid.UUID
	OccurredAt       time.Time
	FirstSeenAt      time.Time
	NextAttemptAt    time.Time
	AttemptCount     int64
	LastAttemptAt    *time.Time
	BlockedReason    PendingPayoutBlockedReason
	LastError        *string
	OverdueAlertedAt *time.Time
}

// CheckAdd performs overflow-checked integer addition.
func CheckAdd(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrArithmeticOverflow
	}
	return a + b, nil
}

// CheckSub performs overflow-checked integer subtraction.
func CheckSub(a, b int64) (int64, error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, ErrArithmeticOverflow
	}
	return a - b, nil
}

// ValidateRewardAmount verifies that a proposed reward is within authorized bounds.
func ValidateRewardAmount(amount int64) error {
	if amount < MinErrandReward || amount > MaxErrandReward {
		return ErrInvalidRewardAmount
	}
	return nil
}

// ValidateBucketBalance verifies that an account balance bucket is within bounds.
func ValidateBucketBalance(balance int64) error {
	if balance < MinBalanceCredits || balance > MaxBalanceCredits {
		return ErrCreditLimitExceeded
	}
	return nil
}
