// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-11-03
// Scope: Streamlined settlements to 3-table schema: 2-step atomic lazy wallet initialization on payout, direct refund settlement, and removed pending tables.
// Author review: Reviewed by Seth

package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RefundReservation executes the refund settlement transaction.
func (r *PostgresRepository) RefundReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Lock the reservation by errandId.
		var res credit.Reservation
		err := tx.QueryRow(txCtx, `
			SELECT id, errand_id, requester_id, amount, status, created_at, updated_at
			FROM credit_reservations
			WHERE errand_id = $1
			FOR UPDATE`,
			errandID,
		).Scan(
			&res.ID, &res.ErrandID, &res.RequesterID, &res.Amount,
			&res.Status, &res.CreatedAt, &res.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrReservationNotFound
			}
			return fmt.Errorf("lock reservation for refund: %w", err)
		}

		if requesterID != nil && *requesterID != uuid.Nil && *requesterID != res.RequesterID {
			return credit.ErrRequesterMismatch
		}

		// 2. If it is already REFUNDED, acknowledge the duplicate.
		if res.Status == credit.ReservationStatusRefunded {
			return nil
		}

		// 3. If it is PAID_OUT, return a non-retryable lifecycle violation.
		if res.Status == credit.ReservationStatusPaidOut {
			return credit.ErrLifecycleViolation
		}

		// If reservation is not in RESERVED status, reject as lifecycle violation.
		if res.Status != credit.ReservationStatusReserved {
			return credit.ErrLifecycleViolation
		}

		// 4. Lock and verify the requester account.
		var (
			availCredits int64
			resCredits   int64
			version      int64
			integrity    credit.AccountIntegrityStatus
		)
		err = tx.QueryRow(txCtx, `
			SELECT available_credits, reserved_credits, version, integrity_status
			FROM credit_accounts
			WHERE user_id = $1
			FOR UPDATE`,
			res.RequesterID,
		).Scan(&availCredits, &resCredits, &version, &integrity)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrWalletNotReady
			}
			return fmt.Errorf("lock requester account for refund: %w", err)
		}

		// 5. If the requester has an integrity flag or insufficient reserved credits, reject.
		if integrity == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}
		if resCredits < res.Amount {
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
				res.Amount, resCredits, res.RequesterID,
			)
			return credit.ErrAccountIntegrityFlagged
		}

		// 6. Move the stored amount from reserved to available.
		cmdTag, err := tx.Exec(txCtx, `
			UPDATE credit_accounts
			SET available_credits = available_credits + $1,
			    reserved_credits = reserved_credits - $1,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $2 AND reserved_credits >= $1`,
			res.Amount, res.RequesterID,
		)
		if err != nil || cmdTag.RowsAffected() != 1 {
			return fmt.Errorf("refund credit account balance update failed: %w", err)
		}

		// 7. Use lifecycle event's eventId as operationId and append two new refund ledger entries.
		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'RESERVED', -($4::bigint), $5, $6, NOW()),
				($1, $2, $3, 'AVAILABLE', $4, $5, $6, NOW())`,
			res.RequesterID, errandID, eventID, res.Amount, reason, occurredAt.UTC(),
		)
		if err != nil {
			return fmt.Errorf("insert refund ledger entries: %w", err)
		}

		// 8. Mark the reservation REFUNDED.
		_, err = tx.Exec(txCtx, `
			UPDATE credit_reservations
			SET status = 'REFUNDED',
			    updated_at = NOW()
			WHERE id = $1`,
			res.ID,
		)
		if err != nil {
			return fmt.Errorf("update reservation status to REFUNDED: %w", err)
		}

		return nil
	})
}

// PayoutReservation executes the completion payout settlement transaction.
func (r *PostgresRepository) PayoutReservation(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, courierID uuid.UUID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error {
	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Lock the reservation.
		var res credit.Reservation
		err := tx.QueryRow(txCtx, `
			SELECT id, errand_id, requester_id, amount, status, created_at, updated_at
			FROM credit_reservations
			WHERE errand_id = $1
			FOR UPDATE`,
			errandID,
		).Scan(
			&res.ID, &res.ErrandID, &res.RequesterID, &res.Amount,
			&res.Status, &res.CreatedAt, &res.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrReservationNotFound
			}
			return fmt.Errorf("lock reservation for payout: %w", err)
		}

		if requesterID != nil && *requesterID != uuid.Nil && *requesterID != res.RequesterID {
			return credit.ErrRequesterMismatch
		}

		// 2. If it is already PAID_OUT, treat incoming completion as a duplicate and return success.
		if res.Status == credit.ReservationStatusPaidOut {
			return nil
		}

		// 3. Return a non-retryable lifecycle violation if it is REFUNDED.
		if res.Status == credit.ReservationStatusRefunded {
			return credit.ErrLifecycleViolation
		}
		if res.Status != credit.ReservationStatusReserved {
			return credit.ErrLifecycleViolation
		}

		// 4. Compare reservation's requesterId with the event's courierId. If equal, return self-payout violation.
		if res.RequesterID == courierID {
			return credit.ErrSelfPayoutNotAllowed
		}

		// The completion amount is authoritative.
		if amount == nil || *amount < 1 || *amount > res.Amount {
			return credit.ErrLifecycleViolation
		}
		payoutAmount := *amount
		leftover := res.Amount - payoutAmount

		// Step 1: Ensure courier wallet exists (Lazy Wallet Initialization).
		// If courier does not have an account yet, create it with 100 available credits and record REGISTRATION_ALLOCATION.
		cmdTag, err := tx.Exec(txCtx, `
			INSERT INTO credit_accounts (
				user_id, available_credits, reserved_credits, version, created_at, updated_at
			)
			VALUES ($1, $2, 0, 1, $3, $3)
			ON CONFLICT (user_id) DO NOTHING`,
			courierID, credit.InitialAllocationCredits, occurredAt.UTC(),
		)
		if err != nil {
			return fmt.Errorf("ensure courier wallet: %w", err)
		}
		if cmdTag.RowsAffected() == 1 {
			// Record the registration allocation ledger entry with its own fresh operation ID.
			regOpID := uuid.New()
			_, err = tx.Exec(txCtx, `
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				VALUES ($1, NULL, $2, 'AVAILABLE', $3, 'REGISTRATION_ALLOCATION', $4, $4)`,
				courierID, regOpID, credit.InitialAllocationCredits, occurredAt.UTC(),
			)
			if err != nil {
				return fmt.Errorf("insert lazy registration allocation transaction: %w", err)
			}
		}

		// Step 2: Lock requester and courier accounts in ascending UUID order to prevent deadlock.
		var (
			reqAvail, reqRes, reqVersion int64
			reqIntegrity                 credit.AccountIntegrityStatus

			courierAvail, courierRes, courierVersion int64
			courierIntegrity                         credit.AccountIntegrityStatus
		)

		orderRequesterFirst := bytes.Compare(res.RequesterID[:], courierID[:]) < 0

		lockRequester := func() error {
			return tx.QueryRow(txCtx, `
				SELECT available_credits, reserved_credits, version, integrity_status
				FROM credit_accounts
				WHERE user_id = $1
				FOR UPDATE`,
				res.RequesterID,
			).Scan(&reqAvail, &reqRes, &reqVersion, &reqIntegrity)
		}

		lockCourier := func() error {
			return tx.QueryRow(txCtx, `
				SELECT available_credits, reserved_credits, version, integrity_status
				FROM credit_accounts
				WHERE user_id = $1
				FOR UPDATE`,
				courierID,
			).Scan(&courierAvail, &courierRes, &courierVersion, &courierIntegrity)
		}

		if orderRequesterFirst {
			if err := lockRequester(); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return credit.ErrWalletNotReady
				}
				return fmt.Errorf("lock requester account: %w", err)
			}
			if err := lockCourier(); err != nil {
				return fmt.Errorf("lock courier account: %w", err)
			}
		} else {
			if err := lockCourier(); err != nil {
				return fmt.Errorf("lock courier account: %w", err)
			}
			if err := lockRequester(); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return credit.ErrWalletNotReady
				}
				return fmt.Errorf("lock requester account: %w", err)
			}
		}

		// Verify account integrity and limits.
		if reqIntegrity == credit.IntegrityFlagged || courierIntegrity == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}
		if reqRes < res.Amount {
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
				res.Amount, reqRes, res.RequesterID,
			)
			return credit.ErrAccountIntegrityFlagged
		}
		if courierAvail+payoutAmount > credit.MaxBalanceCredits {
			return credit.ErrCreditLimitExceeded
		}

		// Subtract stored amount from requester reserved credits, returning any leftover to available (D-059).
		cmdTag, err = tx.Exec(txCtx, `
			UPDATE credit_accounts
			SET reserved_credits = reserved_credits - $1,
			    available_credits = available_credits + $2,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $3 AND reserved_credits >= $1`,
			res.Amount, leftover, res.RequesterID,
		)
		if err != nil || cmdTag.RowsAffected() != 1 {
			return fmt.Errorf("payout requester reserved debit failed: %w", err)
		}

		// Add payoutAmount to courier available credits.
		cmdTag, err = tx.Exec(txCtx, `
			UPDATE credit_accounts
			SET available_credits = available_credits + $1,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $2 AND available_credits + $1 <= $3`,
			payoutAmount, courierID, credit.MaxBalanceCredits,
		)
		if err != nil || cmdTag.RowsAffected() != 1 {
			return fmt.Errorf("payout courier available credit failed: %w", err)
		}

		// Ledger entries:
		if leftover > 0 {
			leftoverOpID := uuid.NewSHA1(eventID, []byte("reward_decreased"))
			_, err = tx.Exec(txCtx, `
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				VALUES
					($1, $2, $3, 'AVAILABLE', $4, 'REWARD_DECREASED', $5, NOW()),
					($1, $2, $3, 'RESERVED', -($4::bigint), 'REWARD_DECREASED', $5, NOW())`,
				res.RequesterID, errandID, leftoverOpID, leftover, occurredAt.UTC(),
			)
			if err != nil {
				return fmt.Errorf("insert leftover return ledger entries: %w", err)
			}
		}

		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'RESERVED', -($4::bigint), 'ERRAND_COMPLETED_DEDUCTION', $5, NOW()),
				($6, $2, $3, 'AVAILABLE', $4, 'ERRAND_COMPLETED_PAYOUT', $5, NOW())`,
			res.RequesterID, errandID, eventID, payoutAmount, occurredAt.UTC(), courierID,
		)
		if err != nil {
			return fmt.Errorf("insert payout ledger entries: %w", err)
		}

		// Mark reservation PAID_OUT and update stored amount to payoutAmount.
		_, err = tx.Exec(txCtx, `
			UPDATE credit_reservations
			SET status = 'PAID_OUT',
			    amount = $2,
			    updated_at = NOW()
			WHERE id = $1`,
			res.ID, payoutAmount,
		)
		if err != nil {
			return fmt.Errorf("update reservation status to PAID_OUT: %w", err)
		}

		return nil
	})
}
