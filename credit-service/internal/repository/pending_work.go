// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL pending payout and refund repository adapters per sourceOfTruth.md Phase 8.
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
)

// ClaimDuePendingPayouts retrieves pending payout rows that are due for retry.
func (r *PostgresRepository) ClaimDuePendingPayouts(ctx context.Context, limit int) ([]credit.PendingPayout, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, errand_id, event_id, courier_id, occurred_at, first_seen_at, next_attempt_at,
		       attempt_count, last_attempt_at, blocked_reason, last_error, overdue_alerted_at
		FROM pending_payouts
		WHERE next_attempt_at <= NOW()
		ORDER BY next_attempt_at ASC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim due pending payouts: %w", err)
	}
	defer rows.Close()

	var result []credit.PendingPayout
	for rows.Next() {
		var p credit.PendingPayout
		if err := rows.Scan(
			&p.ID, &p.ErrandID, &p.EventID, &p.CourierID, &p.OccurredAt, &p.FirstSeenAt, &p.NextAttemptAt,
			&p.AttemptCount, &p.LastAttemptAt, &p.BlockedReason, &p.LastError, &p.OverdueAlertedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending payout: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending payouts: %w", err)
	}
	return result, nil
}

// RecordPendingPayoutOverdue marks that an overdue alert was emitted for a pending payout.
func (r *PostgresRepository) RecordPendingPayoutOverdue(ctx context.Context, id uuid.UUID, alertedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE pending_payouts
		SET overdue_alerted_at = $1
		WHERE id = $2 AND overdue_alerted_at IS NULL`,
		alertedAt.UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("record pending payout overdue: %w", err)
	}
	return nil
}

// ProcessPendingPayout executes one retry attempt for a pending payout inside a single transaction with FOR UPDATE SKIP LOCKED.
func (r *PostgresRepository) ProcessPendingPayout(ctx context.Context, id uuid.UUID, retryInterval time.Duration) error {
	if retryInterval <= 0 {
		retryInterval = 30 * time.Second
	}

	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Select the pending payout row with FOR UPDATE SKIP LOCKED.
		var (
			errandID      uuid.UUID
			eventID       uuid.UUID
			courierID     uuid.UUID
			occurredAt    time.Time
			attemptCount  int64
			blockedReason credit.PendingPayoutBlockedReason
		)
		err := tx.QueryRow(txCtx, `
			SELECT errand_id, event_id, courier_id, occurred_at, attempt_count, blocked_reason
			FROM pending_payouts
			WHERE id = $1 AND next_attempt_at <= NOW()
			FOR UPDATE SKIP LOCKED`,
			id,
		).Scan(&errandID, &eventID, &courierID, &occurredAt, &attemptCount, &blockedReason)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Already processed or locked by another worker replica.
				return nil
			}
			return fmt.Errorf("lock pending payout row: %w", err)
		}

		// 2. Lock the reservation.
		var res credit.Reservation
		err = tx.QueryRow(txCtx, `
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
			return fmt.Errorf("lock reservation for pending payout: %w", err)
		}

		if res.Status == credit.ReservationStatusPaidOut {
			// Already settled; delete stale pending row.
			_, _ = tx.Exec(txCtx, "DELETE FROM pending_payouts WHERE id = $1", id)
			return nil
		}
		if res.Status != credit.ReservationStatusReserved {
			return credit.ErrLifecycleViolation
		}

		// 3. Reject if pending refund exists.
		var pendingRefundExists int
		err = tx.QueryRow(txCtx, "SELECT 1 FROM pending_refunds WHERE errand_id = $1 FOR UPDATE", errandID).Scan(&pendingRefundExists)
		if err == nil {
			return credit.ErrLifecycleViolation
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check pending refunds: %w", err)
		}

		// 4. Reject self-payout.
		if res.RequesterID == courierID {
			return credit.ErrSelfPayoutNotAllowed
		}

		// 5. Lock accounts in ascending UUID order.
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
				return fmt.Errorf("lock requester: %w", err)
			}
			if err := lockCourier(); err != nil {
				return fmt.Errorf("lock courier: %w", err)
			}
		} else {
			if err := lockCourier(); err != nil {
				return fmt.Errorf("lock courier: %w", err)
			}
			if err := lockRequester(); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return credit.ErrWalletNotReady
				}
				return fmt.Errorf("lock requester: %w", err)
			}
		}

		// 6. Check for blockers:
		var currentBlocker string
		if !courierWalletFound {
			currentBlocker = "MISSING_WALLET"
		} else if reqIntegrity == credit.IntegrityFlagged || courierIntegrity == credit.IntegrityFlagged {
			currentBlocker = "ACCOUNT_FLAGGED"
		} else if reqRes < res.Amount {
			// Flag requester on projection mismatch
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
				return fmt.Errorf("flag requester on projection mismatch: %w", err)
			}
			currentBlocker = "ACCOUNT_FLAGGED"
		} else if courierAvail+res.Amount > credit.MaxBalanceCredits {
			currentBlocker = "AVAILABLE_LIMIT_EXCEEDED"
		}

		if currentBlocker != "" {
			// Continuing blocker: update attempt count and next_attempt_at before commit.
			retrySeconds := int(retryInterval.Seconds())
			if retrySeconds <= 0 {
				retrySeconds = 30
			}
			_, err = tx.Exec(txCtx, `
				UPDATE pending_payouts
				SET attempt_count = LEAST(attempt_count + 1, 9223372036854775807),
				    last_attempt_at = NOW(),
				    next_attempt_at = NOW() + ($1 * INTERVAL '1 second'),
				    blocked_reason = $2
				WHERE id = $3`,
				retrySeconds, currentBlocker, id,
			)
			if err != nil {
				return fmt.Errorf("update pending payout attempt metadata: %w", err)
			}
			return nil
		}

		// 7. Unblocked: execute settlement.
		cmdTag, err := tx.Exec(txCtx, `
			UPDATE credit_accounts
			SET reserved_credits = reserved_credits - $1,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $2 AND reserved_credits >= $1`,
			res.Amount, res.RequesterID,
		)
		if err != nil || cmdTag.RowsAffected() != 1 {
			return fmt.Errorf("debit requester reserved credits: %w", err)
		}

		cmdTag, err = tx.Exec(txCtx, `
			UPDATE credit_accounts
			SET available_credits = available_credits + $1,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $2 AND available_credits + $1 <= $3`,
			res.Amount, courierID, credit.MaxBalanceCredits,
		)
		if err != nil || cmdTag.RowsAffected() != 1 {
			return fmt.Errorf("credit courier available credits: %w", err)
		}

		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'RESERVED', -$4, 'ERRAND_COMPLETED_DEDUCTION', $5, NOW()),
				($6, $2, $3, 'AVAILABLE', $4, 'ERRAND_COMPLETED_PAYOUT', $5, NOW())`,
			res.RequesterID, errandID, eventID, res.Amount, occurredAt.UTC(), courierID,
		)
		if err != nil {
			return fmt.Errorf("insert payout ledger entries: %w", err)
		}

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

		_, err = tx.Exec(txCtx, "DELETE FROM pending_payouts WHERE id = $1", id)
		if err != nil {
			return fmt.Errorf("delete completed pending payout: %w", err)
		}

		return nil
	})
}

// ClaimDuePendingRefunds retrieves pending refund rows that are due for retry.
func (r *PostgresRepository) ClaimDuePendingRefunds(ctx context.Context, limit int) ([]credit.PendingRefund, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, errand_id, event_id, reason, occurred_at, first_seen_at, next_attempt_at,
		       attempt_count, last_attempt_at, last_error, overdue_alerted_at
		FROM pending_refunds
		WHERE next_attempt_at <= NOW()
		ORDER BY next_attempt_at ASC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim due pending refunds: %w", err)
	}
	defer rows.Close()

	var result []credit.PendingRefund
	for rows.Next() {
		var p credit.PendingRefund
		if err := rows.Scan(
			&p.ID, &p.ErrandID, &p.EventID, &p.Reason, &p.OccurredAt, &p.FirstSeenAt, &p.NextAttemptAt,
			&p.AttemptCount, &p.LastAttemptAt, &p.LastError, &p.OverdueAlertedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending refund: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending refunds: %w", err)
	}
	return result, nil
}

// RecordPendingRefundOverdue marks that an overdue alert was emitted for a pending refund.
func (r *PostgresRepository) RecordPendingRefundOverdue(ctx context.Context, id uuid.UUID, alertedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE pending_refunds
		SET overdue_alerted_at = $1
		WHERE id = $2 AND overdue_alerted_at IS NULL`,
		alertedAt.UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("record pending refund overdue: %w", err)
	}
	return nil
}

// ProcessPendingRefund executes one retry attempt for a pending refund inside a single transaction with FOR UPDATE SKIP LOCKED.
func (r *PostgresRepository) ProcessPendingRefund(ctx context.Context, id uuid.UUID, retryInterval time.Duration) error {
	if retryInterval <= 0 {
		retryInterval = 30 * time.Second
	}

	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Select the pending refund row with FOR UPDATE SKIP LOCKED.
		var (
			errandID     uuid.UUID
			eventID      uuid.UUID
			reason       credit.TransactionReason
			occurredAt   time.Time
			attemptCount int64
		)
		err := tx.QueryRow(txCtx, `
			SELECT errand_id, event_id, reason, occurred_at, attempt_count
			FROM pending_refunds
			WHERE id = $1 AND next_attempt_at <= NOW()
			FOR UPDATE SKIP LOCKED`,
			id,
		).Scan(&errandID, &eventID, &reason, &occurredAt, &attemptCount)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("lock pending refund row: %w", err)
		}

		// 2. Lock the reservation.
		var res credit.Reservation
		err = tx.QueryRow(txCtx, `
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
			return fmt.Errorf("lock reservation for pending refund: %w", err)
		}

		if res.Status == credit.ReservationStatusRefunded {
			_, _ = tx.Exec(txCtx, "DELETE FROM pending_refunds WHERE id = $1", id)
			return nil
		}
		if res.Status != credit.ReservationStatusReserved {
			return credit.ErrLifecycleViolation
		}

		// 3. Reject if pending payout exists.
		var pendingPayoutExists int
		err = tx.QueryRow(txCtx, "SELECT 1 FROM pending_payouts WHERE errand_id = $1 FOR UPDATE", errandID).Scan(&pendingPayoutExists)
		if err == nil {
			return credit.ErrLifecycleViolation
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check pending payouts: %w", err)
		}

		// 4. Lock requester account.
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
			return fmt.Errorf("lock requester for pending refund: %w", err)
		}

		// 5. Check if still blocked by integrity flag or projection mismatch.
		isFlagged := integrity == credit.IntegrityFlagged
		hasProjectionMismatch := resCredits < res.Amount

		if isFlagged || hasProjectionMismatch {
			if hasProjectionMismatch && !isFlagged {
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

			retrySeconds := int(retryInterval.Seconds())
			if retrySeconds <= 0 {
				retrySeconds = 30
			}
			_, err = tx.Exec(txCtx, `
				UPDATE pending_refunds
				SET attempt_count = LEAST(attempt_count + 1, 9223372036854775807),
				    last_attempt_at = NOW(),
				    next_attempt_at = NOW() + ($1 * INTERVAL '1 second')
				WHERE id = $2`,
				retrySeconds, id,
			)
			if err != nil {
				return fmt.Errorf("update pending refund attempt metadata: %w", err)
			}
			return nil
		}

		// 6. Unblocked: execute refund.
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
			return fmt.Errorf("refund available credit update failed: %w", err)
		}

		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'RESERVED', -$4, $5, $6, NOW()),
				($1, $2, $3, 'AVAILABLE', $4, $5, $6, NOW())`,
			res.RequesterID, errandID, eventID, res.Amount, reason, occurredAt.UTC(),
		)
		if err != nil {
			return fmt.Errorf("insert refund ledger entries: %w", err)
		}

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

		_, err = tx.Exec(txCtx, "DELETE FROM pending_refunds WHERE id = $1", id)
		if err != nil {
			return fmt.Errorf("delete completed pending refund: %w", err)
		}

		return nil
	})
}
