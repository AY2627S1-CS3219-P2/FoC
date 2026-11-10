// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-11-04
// Scope: Collapsed settlement workflows (RefundReservation and PayoutReservation) into single atomic CTE operations.
// Eliminates multi-roundtrip transaction bloat, multi-account row-lock contention, and deadlocks.
// Author review: Reviewed by Seth

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RefundReservation executes the refund settlement transaction via a single atomic CTE.
// It conditionally transitions the reservation to REFUNDED, returns reserved credits back to available,
// and appends balanced ledger entries in one database operation.
func (r *PostgresRepository) RefundReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// Ensure requester account is initialized before executing the refund.
		if requesterID != nil && *requesterID != uuid.Nil {
			if err := ensureWalletInitialized(txCtx, tx, *requesterID, credit.InitialAllocationCredits, occurredAt); err != nil {
				return fmt.Errorf("ensure requester wallet for refund: %w", err)
			}
		}

		// Single Atomic CTE:
		// 1. Conditionally lock and transition the reservation to REFUNDED only if requester account is consistent and valid.
		// 2. Transfer reserved_credits back to available_credits on the requester account.
		// 3. Append two balanced ledger entries under eventID.
		const refundCTE = `
			WITH locked_res AS (
				UPDATE credit_reservations r
				SET status = 'REFUNDED',
				    updated_at = NOW()
				FROM credit_accounts a
				WHERE r.errand_id = $1
				  AND r.status = 'RESERVED'
				  AND ($2::uuid IS NULL OR r.requester_id = $2)
				  AND a.user_id = r.requester_id
				  AND a.reserved_credits >= r.amount
				  AND a.integrity_status != 'FLAGGED'
				RETURNING r.id, r.amount, r.requester_id
			),
			update_account AS (
				UPDATE credit_accounts a
				SET available_credits = a.available_credits + r.amount,
				    reserved_credits = a.reserved_credits - r.amount,
				    version = a.version + 1,
				    updated_at = NOW()
				FROM locked_res r
				WHERE a.user_id = r.requester_id
				  AND a.reserved_credits >= r.amount
				  AND a.integrity_status != 'FLAGGED'
				RETURNING a.user_id
			),
			insert_ledger AS (
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				SELECT
					r.requester_id, $1, $3::uuid, 'AVAILABLE'::text, r.amount, $4::transaction_reason, $5::timestamptz, NOW()
				FROM locked_res r
				JOIN update_account u ON u.user_id = r.requester_id
				UNION ALL
				SELECT
					r.requester_id, $1, $3::uuid, 'RESERVED'::text, -(r.amount::bigint), $4::transaction_reason, $5::timestamptz, NOW()
				FROM locked_res r
				JOIN update_account u ON u.user_id = r.requester_id
				RETURNING id
			)
			SELECT r.id, r.amount, r.requester_id
			FROM locked_res r
			JOIN update_account u ON u.user_id = r.requester_id;`

		var (
			resID       uuid.UUID
			resAmount   int64
			actualReqID uuid.UUID
		)
		err := tx.QueryRow(txCtx, refundCTE, errandID, requesterID, eventID, reason, occurredAt.UTC()).
			Scan(&resID, &resAmount, &actualReqID)

		if err == nil {
			// Fast path: atomically transitioned and settled!
			return nil
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "chk_accounts_available_credits_range" {
				return credit.ErrAccountIntegrityFlagged
			}
			return fmt.Errorf("execute atomic refund CTE: %w", err)
		}

		// Diagnostics path: CTE returned 0 rows. Query reservation state to return precise domain error.
		var (
			currStatus  credit.ReservationStatus
			currReqID   uuid.UUID
			currHold    int64
			currVersion int64
		)
		diagErr := tx.QueryRow(txCtx, `
			SELECT status, requester_id, amount, version
			FROM credit_reservations
			WHERE errand_id = $1`,
			errandID,
		).Scan(&currStatus, &currReqID, &currHold, &currVersion)

		if errors.Is(diagErr, pgx.ErrNoRows) {
			return credit.ErrReservationNotFound
		}
		if diagErr != nil {
			return fmt.Errorf("diagnose refund failure: %w", diagErr)
		}

		// Requester identity mismatch check
		if requesterID != nil && *requesterID != uuid.Nil && *requesterID != currReqID {
			return credit.ErrRequesterMismatch
		}

		// Lifecycle state checks
		if currStatus == credit.ReservationStatusRefunded {
			return nil // Idempotent duplicate delivery: already refunded
		}
		if currStatus == credit.ReservationStatusPaidOut || currStatus != credit.ReservationStatusReserved {
			return credit.ErrLifecycleViolation
		}

		// Check account integrity or balance mismatch if reservation was RESERVED
		var (
			accResCredits int64
			accIntegrity  credit.AccountIntegrityStatus
		)
		accErr := tx.QueryRow(txCtx, `
			SELECT reserved_credits, integrity_status
			FROM credit_accounts
			WHERE user_id = $1`,
			currReqID,
		).Scan(&accResCredits, &accIntegrity)
		if accErr != nil {
			return fmt.Errorf("diagnose requester account for refund: %w", accErr)
		}

		if accIntegrity == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}
		if accResCredits < currHold {
			_, _ = tx.Exec(txCtx, `
				UPDATE credit_accounts
				SET integrity_status = 'FLAGGED',
				    integrity_flagged_at = COALESCE(integrity_flagged_at, NOW()),
				    integrity_reason_code = 'RESERVED_PROJECTION_MISMATCH',
				    integrity_detail = 'reserved projection is smaller than reservation amount',
				    integrity_expected = $1,
				    integrity_observed = $2,
				    version = version + 1,
				    updated_at = NOW()
				WHERE user_id = $3`,
				currHold, accResCredits, currReqID,
			)
			return credit.ErrAccountIntegrityFlagged
		}

		return credit.ErrLifecycleViolation
	})
}

// PayoutReservation executes the completion payout settlement transaction via a single atomic CTE.
// It ensures courier wallet initialization in Step 1, then atomically locks the reservation,
// transfers credits between requester hold and courier available balance, returns leftover credits,
// marks the reservation PAID_OUT, and appends all audit ledger rows in one single database command.
func (r *PostgresRepository) PayoutReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, courierID uuid.UUID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error {
	// Pre-validations outside database transaction
	if amount == nil || *amount < 1 {
		return credit.ErrLifecycleViolation
	}
	if requesterID != nil && *requesterID == courierID {
		return credit.ErrSelfPayoutNotAllowed
	}
	payoutAmount := *amount

	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// Step 1: Ensure courier wallet exists (Universal Lazy Wallet Initialization)
		if err := ensureWalletInitialized(txCtx, tx, courierID, credit.InitialAllocationCredits, occurredAt.UTC()); err != nil {
			return fmt.Errorf("ensure courier wallet: %w", err)
		}

		// Single Atomic CTE:
		// 1. Lock current reservation and capture original_hold only if requester and courier accounts are consistent.
		// 2. Transition reservation to PAID_OUT and set amount to payoutAmount.
		// 3. Debit requester reserved credits by original_hold, returning leftover to available.
		// 4. Credit courier available credits by payoutAmount (enforcing MaxBalanceCredits).
		// 5. Append audit ledger rows atomically under eventID (and leftover return under deterministic SHA1).
		leftoverOpID := uuid.NewSHA1(eventID, []byte("reward_decreased"))

		const payoutCTE = `
			WITH current_res AS (
				SELECT r.id, r.amount AS original_hold, r.requester_id
				FROM credit_reservations r
				JOIN credit_accounts req ON req.user_id = r.requester_id
				JOIN credit_accounts cour ON cour.user_id = $4
				WHERE r.errand_id = $1
				  AND r.status = 'RESERVED'
				  AND ($2::uuid IS NULL OR r.requester_id = $2)
				  AND r.requester_id != $4
				  AND r.amount >= $3
				  AND req.reserved_credits >= r.amount
				  AND req.integrity_status != 'FLAGGED'
				  AND cour.available_credits + $3 <= $5
				  AND cour.integrity_status != 'FLAGGED'
				FOR UPDATE OF r
			),
			locked_res AS (
				UPDATE credit_reservations r
				SET status = 'PAID_OUT',
				    amount = $3,
				    updated_at = NOW()
				FROM current_res c
				WHERE r.id = c.id
				RETURNING r.id, c.original_hold, r.requester_id
			),
			debit_requester AS (
				UPDATE credit_accounts a
				SET reserved_credits = a.reserved_credits - r.original_hold,
				    available_credits = a.available_credits + (r.original_hold - $3),
				    version = a.version + 1,
				    updated_at = NOW()
				FROM locked_res r
				WHERE a.user_id = r.requester_id
				  AND a.reserved_credits >= r.original_hold
				  AND a.integrity_status != 'FLAGGED'
				RETURNING a.user_id
			),
			credit_courier AS (
				UPDATE credit_accounts c
				SET available_credits = c.available_credits + $3,
				    version = c.version + 1,
				    updated_at = NOW()
				FROM locked_res r
				JOIN debit_requester d ON d.user_id = r.requester_id
				WHERE c.user_id = $4
				  AND c.available_credits + $3 <= $5
				  AND c.integrity_status != 'FLAGGED'
				RETURNING c.user_id
			),
			insert_leftover_ledger AS (
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				SELECT
					r.requester_id, $1, $6::uuid, 'AVAILABLE'::text, (r.original_hold - $3), 'REWARD_DECREASED'::transaction_reason, $7::timestamptz, NOW()
				FROM locked_res r
				JOIN credit_courier cc ON true
				WHERE (r.original_hold - $3) > 0
				UNION ALL
				SELECT
					r.requester_id, $1, $6::uuid, 'RESERVED'::text, -((r.original_hold - $3)::bigint), 'REWARD_DECREASED'::transaction_reason, $7::timestamptz, NOW()
				FROM locked_res r
				JOIN credit_courier cc ON true
				WHERE (r.original_hold - $3) > 0
				RETURNING id
			),
			insert_payout_ledger AS (
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				SELECT
					r.requester_id, $1, $8::uuid, 'RESERVED'::text, -($3::bigint), 'ERRAND_COMPLETED_DEDUCTION'::transaction_reason, $7::timestamptz, NOW()
				FROM locked_res r
				JOIN credit_courier cc ON true
				UNION ALL
				SELECT
					$4, $1, $8::uuid, 'AVAILABLE'::text, $3, 'ERRAND_COMPLETED_PAYOUT'::transaction_reason, $7::timestamptz, NOW()
				FROM locked_res r
				JOIN credit_courier cc ON true
				RETURNING id
			)
			SELECT r.id, r.original_hold, r.requester_id
			FROM locked_res r
			JOIN debit_requester d ON d.user_id = r.requester_id
			JOIN credit_courier c ON c.user_id = $4;`

		var (
			resID        uuid.UUID
			originalHold int64
			actualReqID  uuid.UUID
		)
		err := tx.QueryRow(txCtx, payoutCTE,
			errandID, requesterID, payoutAmount, courierID,
			credit.MaxBalanceCredits, leftoverOpID, occurredAt.UTC(), eventID,
		).Scan(&resID, &originalHold, &actualReqID)

		if err == nil {
			// Fast path: completed, settled, and ledger recorded in 1 atomic roundtrip!
			return nil
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "chk_accounts_available_credits_range" {
				return credit.ErrAccountIntegrityFlagged
			}
			return fmt.Errorf("execute atomic payout CTE: %w", err)
		}

		// Diagnostics path: CTE returned 0 rows. Query reservation state to return precise domain error.
		var (
			currStatus credit.ReservationStatus
			currReqID  uuid.UUID
			currHold   int64
		)
		diagErr := tx.QueryRow(txCtx, `
			SELECT status, requester_id, amount
			FROM credit_reservations
			WHERE errand_id = $1`,
			errandID,
		).Scan(&currStatus, &currReqID, &currHold)

		if errors.Is(diagErr, pgx.ErrNoRows) {
			return credit.ErrReservationNotFound
		}
		if diagErr != nil {
			return fmt.Errorf("diagnose payout failure: %w", diagErr)
		}

		// Requester identity mismatch check
		if requesterID != nil && *requesterID != uuid.Nil && *requesterID != currReqID {
			return credit.ErrRequesterMismatch
		}

		// Self payout prohibition
		if currReqID == courierID {
			return credit.ErrSelfPayoutNotAllowed
		}

		// Payout amount above hold is a lifecycle violation
		if payoutAmount > currHold {
			return credit.ErrLifecycleViolation
		}

		// Lifecycle state checks
		if currStatus == credit.ReservationStatusPaidOut {
			return nil // Idempotent duplicate delivery: already paid out
		}
		if currStatus == credit.ReservationStatusRefunded || currStatus != credit.ReservationStatusReserved {
			return credit.ErrLifecycleViolation
		}

		// Inspect requester & courier account limits / flags
		var (
			reqResCredits int64
			reqIntegrity  credit.AccountIntegrityStatus
		)
		if err := tx.QueryRow(txCtx, `
			SELECT reserved_credits, integrity_status
			FROM credit_accounts WHERE user_id = $1`, currReqID,
		).Scan(&reqResCredits, &reqIntegrity); err != nil {
			return fmt.Errorf("diagnose requester account: %w", err)
		}

		var (
			courierAvailCredits int64
			courierIntegrity    credit.AccountIntegrityStatus
		)
		if err := tx.QueryRow(txCtx, `
			SELECT available_credits, integrity_status
			FROM credit_accounts WHERE user_id = $1`, courierID,
		).Scan(&courierAvailCredits, &courierIntegrity); err != nil {
			return fmt.Errorf("diagnose courier account: %w", err)
		}

		if reqIntegrity == credit.IntegrityFlagged || courierIntegrity == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}
		if reqResCredits < currHold {
			_, _ = tx.Exec(txCtx, `
				UPDATE credit_accounts
				SET integrity_status = 'FLAGGED',
				    integrity_flagged_at = COALESCE(integrity_flagged_at, NOW()),
				    integrity_reason_code = 'RESERVED_PROJECTION_MISMATCH',
				    integrity_detail = 'reserved projection is smaller than reservation amount',
				    integrity_expected = $1,
				    integrity_observed = $2,
				    version = version + 1,
				    updated_at = NOW()
				WHERE user_id = $3`,
				currHold, reqResCredits, currReqID,
			)
			return credit.ErrAccountIntegrityFlagged
		}
		if courierAvailCredits+payoutAmount > credit.MaxBalanceCredits {
			return credit.ErrCreditLimitExceeded
		}

		return credit.ErrLifecycleViolation
	})
}
