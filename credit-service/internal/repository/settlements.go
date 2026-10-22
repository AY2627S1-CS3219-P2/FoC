// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL refund and completion payout settlement transactions per sourceOfTruth.md Phases 6 & 7.
// Author review: PENDING - reviewer to complete

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
	"github.com/jackc/pgx/v5/pgconn"
)

// RefundReservation executes the refund settlement transaction described in Phase 6.
func (r *PostgresRepository) RefundReservation(ctx context.Context, errandID uuid.UUID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
	switch reason {
	case credit.TransactionReasonErrandCancelled, credit.TransactionReasonErrandExpired, credit.TransactionReasonErrandFailed:
	default:
		return credit.ErrLifecycleViolation
	}

	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Look up and lock the reservation by errandId.
		var res credit.Reservation
		err := tx.QueryRow(txCtx, `
			SELECT id, errand_id, requester_id, amount, status, version, created_at, updated_at
			FROM credit_reservations
			WHERE errand_id = $1
			FOR UPDATE`,
			errandID,
		).Scan(
			&res.ID, &res.ErrandID, &res.RequesterID, &res.Amount,
			&res.Status, &res.Version, &res.CreatedAt, &res.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrReservationNotFound
			}
			return fmt.Errorf("lock reservation for refund: %w", err)
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

		// 4. If pending payout work exists, return a non-retryable lifecycle violation.
		var pendingPayoutExists int
		err = tx.QueryRow(txCtx, `
			SELECT 1 FROM pending_payouts WHERE errand_id = $1 FOR UPDATE`,
			errandID,
		).Scan(&pendingPayoutExists)
		if err == nil {
			return credit.ErrLifecycleViolation
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check pending payouts: %w", err)
		}

		// 5. Lock and verify the requester account.
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

		// 6. If the requester is FLAGGED, or its stored reserved projection is smaller than the reservation amount:
		// leave balances and reservation status unchanged, insert or verify durable pending-refund work, commit that work, and return a delayed outcome.
		isFlagged := integrity == credit.IntegrityFlagged
		hasProjectionMismatch := resCredits < res.Amount

		if isFlagged || hasProjectionMismatch {
			if hasProjectionMismatch && !isFlagged {
				// Projection mismatch flags the account, increments its version, and records a reconciliation finding.
				_, err = tx.Exec(txCtx, `
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
				if err != nil {
					return fmt.Errorf("flag account on projection mismatch: %w", err)
				}
			}

			// Insert or verify pending_refunds work.
			var (
				existingEventID uuid.UUID
				existingReason  credit.TransactionReason
			)
			err = tx.QueryRow(txCtx, `
				SELECT event_id, reason
				FROM pending_refunds
				WHERE errand_id = $1
				FOR UPDATE`,
				errandID,
			).Scan(&existingEventID, &existingReason)
			if err == nil {
				// A new event for an errand that already has pending refund work must match its event identity and reason;
				// otherwise it is a non-retryable lifecycle conflict.
				if existingEventID == eventID && existingReason == reason {
					return nil
				}
				return credit.ErrLifecycleViolation
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("lookup existing pending refund: %w", err)
			}

			// Insert durable pending refund row.
			_, err = tx.Exec(txCtx, `
				INSERT INTO pending_refunds (
					errand_id, event_id, reason, occurred_at, first_seen_at, next_attempt_at, attempt_count
				)
				VALUES ($1, $2, $3, $4, NOW(), NOW(), 0)`,
				errandID, eventID, reason, occurredAt.UTC(),
			)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					// Unique violation on event_id or errand_id from concurrent race
					return credit.ErrLifecycleViolation
				}
				return fmt.Errorf("insert pending refund: %w", err)
			}

			// Delayed outcome committed.
			return nil
		}

		// 7. Move the stored amount from reserved to available.
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

		// 8. Use lifecycle event's eventId as operationId and append two new refund ledger entries.
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

		// 9. Mark the reservation REFUNDED and delete any pending-refund row.
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

		_, err = tx.Exec(txCtx, `
			DELETE FROM pending_refunds
			WHERE errand_id = $1`,
			errandID,
		)
		if err != nil {
			return fmt.Errorf("delete pending refund: %w", err)
		}

		// 10. Commit before acknowledging RabbitMQ message (handled by withTx).
		return nil
	})
}

// PayoutReservation executes the completion payout settlement transaction described in Phase 7.
func (r *PostgresRepository) PayoutReservation(ctx context.Context, errandID uuid.UUID, courierID uuid.UUID, eventID uuid.UUID, amount *int64, occurredAt time.Time) error {
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

		// 4. Return a non-retryable lifecycle violation if pending refund work exists.
		var pendingRefundExists int
		err = tx.QueryRow(txCtx, `
			SELECT 1 FROM pending_refunds WHERE errand_id = $1 FOR UPDATE`,
			errandID,
		).Scan(&pendingRefundExists)
		if err == nil {
			return credit.ErrLifecycleViolation
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check pending refunds: %w", err)
		}

		// 5. Compare reservation's requesterId with the event's courierId. If equal, return self-payout violation.
		if res.RequesterID == courierID {
			return credit.ErrSelfPayoutNotAllowed
		}

		// D-059: Determine payout amount and any surplus hold returned to requester.
		// Implemented to settle by event amount and return surplus hold; can be revised later if domain boundary contracts evolve.
		payoutAmount := res.Amount
		if amount != nil && *amount > 0 && *amount < res.Amount {
			payoutAmount = *amount
		}
		leftover := res.Amount - payoutAmount

		// 6. Lock requester and courier accounts in ascending UUID order.
		var (
			reqAvail, reqRes, reqVersion int64
			reqIntegrity                 credit.AccountIntegrityStatus

			courierAvail, courierRes, courierVersion int64
			courierIntegrity                         credit.AccountIntegrityStatus
			courierWalletFound                       bool
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
			err := tx.QueryRow(txCtx, `
				SELECT available_credits, reserved_credits, version, integrity_status
				FROM credit_accounts
				WHERE user_id = $1
				FOR UPDATE`,
				courierID,
			).Scan(&courierAvail, &courierRes, &courierVersion, &courierIntegrity)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					courierWalletFound = false
					return nil
				}
				return err
			}
			courierWalletFound = true
			return nil
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

		// 7. Check blockers:
		// If either account is FLAGGED, requester reserved projection is smaller than reservation amount,
		// or adding amount would exceed courier available-bucket limit (or courier wallet missing),
		// preserve or create pending payout work and return delayed outcome.
		var blockedReason string
		if !courierWalletFound {
			blockedReason = "MISSING_WALLET"
		} else if reqIntegrity == credit.IntegrityFlagged || courierIntegrity == credit.IntegrityFlagged {
			blockedReason = "ACCOUNT_FLAGGED"
		} else if reqRes < res.Amount {
			// Projection mismatch flags the affected account, increments version, records finding.
			_, err = tx.Exec(txCtx, `
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
			if err != nil {
				return fmt.Errorf("flag requester account on projection mismatch: %w", err)
			}
			blockedReason = "ACCOUNT_FLAGGED"
		} else if courierAvail+payoutAmount > credit.MaxBalanceCredits {
			blockedReason = "AVAILABLE_LIMIT_EXCEEDED"
		}

		// The reservation lock serializes settlement; a row lock here can deadlock with the worker.
		var (
			existingEventID      uuid.UUID
			existingCourierID    uuid.UUID
			existingPayoutAmount int64
			existingOccurred     time.Time
		)
		err = tx.QueryRow(txCtx, `
			SELECT event_id, courier_id, payout_amount, occurred_at
			FROM pending_payouts
			WHERE errand_id = $1`,
			errandID,
		).Scan(&existingEventID, &existingCourierID, &existingPayoutAmount, &existingOccurred)
		if err == nil {
			if existingEventID != eventID || existingCourierID != courierID || existingPayoutAmount != payoutAmount || !existingOccurred.Equal(occurredAt.UTC()) {
				return credit.ErrLifecycleViolation
			}
			if blockedReason != "" {
				return nil
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lookup existing pending payout: %w", err)
		}

		if blockedReason != "" {

			// Insert pending payout row.
			_, err = tx.Exec(txCtx, `
				INSERT INTO pending_payouts (
					errand_id, event_id, courier_id, payout_amount, occurred_at, first_seen_at, next_attempt_at, attempt_count, blocked_reason
				)
				VALUES ($1, $2, $3, $4, $5, NOW(), NOW(), 0, $6)`,
				errandID, eventID, courierID, payoutAmount, occurredAt.UTC(), blockedReason,
			)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					return credit.ErrLifecycleViolation
				}
				return fmt.Errorf("insert pending payout: %w", err)
			}

			// Delayed outcome committed.
			return nil
		}

		// 8. Subtract stored amount from requester reserved credits, returning any leftover to available (D-059).
		cmdTag, err := tx.Exec(txCtx, `
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

		// 9. Add payoutAmount to courier available credits.
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

		// 10. Use completion eventId as operationId and append requester deduction, courier payout, and any surplus return under it.
		if leftover > 0 {
			_, err = tx.Exec(txCtx, `
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				VALUES
					($1, $2, $3, 'RESERVED', -($4::bigint), 'ERRAND_COMPLETED_DEDUCTION', $5, NOW()),
					($1, $2, $3, 'AVAILABLE', $6, 'REWARD_DECREASED', $5, NOW()),
					($7, $2, $3, 'AVAILABLE', $8, 'ERRAND_COMPLETED_PAYOUT', $5, NOW())`,
				res.RequesterID, errandID, eventID, res.Amount, occurredAt.UTC(), leftover, courierID, payoutAmount,
			)
		} else {
			_, err = tx.Exec(txCtx, `
				INSERT INTO credit_transactions (
					user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
				)
				VALUES
					($1, $2, $3, 'RESERVED', -($4::bigint), 'ERRAND_COMPLETED_DEDUCTION', $5, NOW()),
					($6, $2, $3, 'AVAILABLE', $4, 'ERRAND_COMPLETED_PAYOUT', $5, NOW())`,
				res.RequesterID, errandID, eventID, res.Amount, occurredAt.UTC(), courierID,
			)
		}
		if err != nil {
			return fmt.Errorf("insert payout ledger entries: %w", err)
		}

		// 11. Mark reservation PAID_OUT.
		_, err = tx.Exec(txCtx, `
			UPDATE credit_reservations
			SET status = 'PAID_OUT',
			    updated_at = NOW()
			WHERE id = $1`,
			res.ID,
		)
		if err != nil {
			return fmt.Errorf("update reservation status to PAID_OUT: %w", err)
		}

		// 12. Delete any pending payout row for the errand.
		_, err = tx.Exec(txCtx, `
			DELETE FROM pending_payouts
			WHERE errand_id = $1`,
			errandID,
		)
		if err != nil {
			return fmt.Errorf("delete pending payout: %w", err)
		}

		// 13. Commit before acknowledging completion message (handled by withTx).
		return nil
	})
}
