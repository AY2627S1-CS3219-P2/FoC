// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL refund settlement transaction per sourceOfTruth.md Phase 6.
// Author review: PENDING - reviewer to complete

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
				($1, $2, $3, 'RESERVED', -$4, $5, $6, NOW()),
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

// PayoutReservation is implemented in Phase 7.
func (r *PostgresRepository) PayoutReservation(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, _ time.Time) error {
	return errors.New("payout reservation not implemented yet")
}
