// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Defined distinguishable domain errors and typed amount mismatch error per sourceOfTruth.md.
// Author review: PENDING - reviewer to complete

package credit

import (
	"errors"
	"fmt"
)

// Distinguishable sentinel domain errors.
var (
	ErrInvalidRequest            = errors.New("invalid request")
	ErrInvalidUserID             = errors.New("invalid user ID")
	ErrInvalidErrandID           = errors.New("invalid errand ID")
	ErrInvalidOperationID        = errors.New("invalid operation ID")
	ErrInvalidRewardAmount       = errors.New("reward amount out of bounds")
	ErrWalletNotReady            = errors.New("wallet is not ready")
	ErrInsufficientCredits       = errors.New("insufficient available credits")
	ErrReservationNotFound       = errors.New("reservation not found")
	ErrReservationConflict       = errors.New("reservation conflicts with existing reservation")
	ErrReservationStateConflict  = errors.New("reservation in terminal or non-reservable state")
	ErrAccountIntegrityFlagged   = errors.New("account integrity flagged")
	ErrIdempotencyConflict       = errors.New("operation ID reused with conflicting parameters")
	ErrCreditLimitExceeded       = errors.New("credit bucket limit exceeded")
	ErrArithmeticOverflow        = errors.New("arithmetic calculation overflow")
	ErrLifecycleViolation        = errors.New("non-retryable lifecycle violation")
	ErrSelfPayoutNotAllowed      = errors.New("requester cannot be courier")
	ErrReservationAmountMismatch = errors.New("reservation amount mismatch")
	ErrServiceUnavailable        = errors.New("service unavailable")
)

// AmountMismatchError provides the authoritative current reservation amount when an adjust command mismatches.
type AmountMismatchError struct {
	CurrentAmount int64
}

func (e *AmountMismatchError) Error() string {
	return fmt.Sprintf("reservation amount mismatch: current amount is %d", e.CurrentAmount)
}

func (e *AmountMismatchError) Is(target error) bool {
	return target == ErrReservationAmountMismatch
}
