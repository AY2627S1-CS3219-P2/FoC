// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL reservation creation transaction per sourceOfTruth.md Phase 4.
// Author review: PENDING - reviewer to complete

package repository

import (
	"context"
	"errors"
	"fmt"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errReservationRaceLost = errors.New("concurrent reservation creation race lost")

// CreateReservation executes the idempotent reservation creation transaction described in Phase 4.
func (r *PostgresRepository) CreateReservation(ctx context.Context, errandID uuid.UUID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error) {
	var result *credit.Reservation
	var created bool

	err := r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Read by errandId. If a committed reservation already exists, classify it without locking requester account.
		existing, err := findReservationByErrandID(txCtx, tx, errandID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.Status == credit.ReservationStatusReserved {
				if existing.RequesterID == requesterID && existing.Amount == amount {
					result = existing
					created = false
					return nil
				}
				return credit.ErrReservationConflict
			}
			return credit.ErrReservationStateConflict
		}

		// 2. If no reservation exists, lock the requester account first.
		var (
			availableCredits int64
			reservedCredits  int64
			version          int64
			integrityStatus  credit.AccountIntegrityStatus
		)
		err = tx.QueryRow(txCtx, `
			SELECT available_credits, reserved_credits, version, integrity_status
			FROM credit_accounts
			WHERE user_id = $1
			FOR UPDATE`,
			requesterID,
		).Scan(&availableCredits, &reservedCredits, &version, &integrityStatus)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrWalletNotReady
			}
			return fmt.Errorf("lock requester account: %w", err)
		}

		// 3. Reject a FLAGGED account as an integrity failure.
		if integrityStatus == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}

		// 4. Insert the RESERVED reservation with ON CONFLICT (errand_id) DO NOTHING RETURNING ...
		var res credit.Reservation
		err = tx.QueryRow(txCtx, `
			INSERT INTO credit_reservations (
				errand_id, requester_id, amount, status, created_at, updated_at
			)
			VALUES ($1, $2, $3, 'RESERVED', NOW(), NOW())
			ON CONFLICT (errand_id) DO NOTHING
			RETURNING id, errand_id, requester_id, amount, status, created_at, updated_at`,
			errandID, requesterID, amount,
		).Scan(
			&res.ID, &res.ErrandID, &res.RequesterID, &res.Amount,
			&res.Status, &res.CreatedAt, &res.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// 5. If the insert loses a concurrent race, roll back this attempt.
				return errReservationRaceLost
			}
			return fmt.Errorf("insert reservation: %w", err)
		}

		// 6. Use a conditional check to ensure available credits are at least requested amount.
		if availableCredits < amount {
			return credit.ErrInsufficientCredits
		}

		// 7. Move the amount from available to reserved and increment account version.
		cmdTag, err := tx.Exec(txCtx, `
			UPDATE credit_accounts
			SET available_credits = available_credits - $1,
			    reserved_credits = reserved_credits + $1,
			    version = version + 1,
			    updated_at = NOW()
			WHERE user_id = $2 AND available_credits >= $1`,
			amount, requesterID,
		)
		if err != nil {
			return fmt.Errorf("update requester balances: %w", err)
		}
		if cmdTag.RowsAffected() != 1 {
			return credit.ErrInsufficientCredits
		}

		// 8. Generate one internal operation ID and insert the AVAILABLE negative and RESERVED positive ledger rows under it.
		opID := uuid.New()
		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'AVAILABLE', -$4, 'ERRAND_RESERVATION', NULL, NOW()),
				($1, $2, $3, 'RESERVED', $4, 'ERRAND_RESERVATION', NULL, NOW())`,
			requesterID, errandID, opID, amount,
		)
		if err != nil {
			return fmt.Errorf("insert reservation transactions: %w", err)
		}

		result = &res
		created = true
		return nil
	})

	// If concurrent race was lost, classify the winner in a fresh read transaction
	if errors.Is(err, errReservationRaceLost) {
		return r.classifyCommittedReservation(ctx, errandID, requesterID, amount)
	}
	if err != nil {
		return nil, false, err
	}
	return result, created, nil
}

func (r *PostgresRepository) classifyCommittedReservation(ctx context.Context, errandID uuid.UUID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error) {
	var result *credit.Reservation
	err := r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		existing, err := findReservationByErrandID(txCtx, tx, errandID)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("%w: concurrent reservation rolled back", credit.ErrServiceUnavailable)
		}
		if existing.Status == credit.ReservationStatusReserved {
			if existing.RequesterID == requesterID && existing.Amount == amount {
				result = existing
				return nil
			}
			return credit.ErrReservationConflict
		}
		return credit.ErrReservationStateConflict
	})
	if err != nil {
		return nil, false, err
	}
	return result, false, nil
}

func findReservationByErrandID(ctx context.Context, tx pgx.Tx, errandID uuid.UUID) (*credit.Reservation, error) {
	var res credit.Reservation
	err := tx.QueryRow(ctx, `
		SELECT id, errand_id, requester_id, amount, status, created_at, updated_at
		FROM credit_reservations
		WHERE errand_id = $1`,
		errandID,
	).Scan(
		&res.ID, &res.ErrandID, &res.RequesterID, &res.Amount,
		&res.Status, &res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find reservation by errand ID: %w", err)
	}
	return &res, nil
}
