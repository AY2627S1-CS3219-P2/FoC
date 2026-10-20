// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL reservation creation and reward adjustment transactions per sourceOfTruth.md Phases 4 & 5.
// Author review: PENDING - reviewer to complete

package repository

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ credit.ReservationRepository = (*PostgresRepository)(nil)

var errReservationRaceLost = errors.New("concurrent reservation creation race lost")

// advisoryLockKey converts a UUID into a deterministic int64 for PostgreSQL transaction advisory locking.
func advisoryLockKey(id uuid.UUID) int64 {
	return int64(binary.BigEndian.Uint64(id[0:8]) ^ binary.BigEndian.Uint64(id[8:16]))
}

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
				($1, $2, $3, 'AVAILABLE', -($4::bigint), 'ERRAND_RESERVATION', NULL, NOW()),
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

// AdjustReservationReward executes the reward adjustment transaction described in Phase 5.
func (r *PostgresRepository) AdjustReservationReward(ctx context.Context, errandID uuid.UUID, newReward int64, operationID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
	var resOp *credit.OperationResult
	var resRev *credit.Reservation

	err := r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Transaction-scoped PostgreSQL advisory lock derived from operationId.
		if _, err := tx.Exec(txCtx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey(operationID)); err != nil {
			return fmt.Errorf("acquire advisory lock: %w", err)
		}

		// 2. Look up operationId.
		var op credit.OperationResult
		err := tx.QueryRow(txCtx, `
			SELECT operation_id, operation_type, errand_id, current_amount, new_amount,
			       outcome_code, resulting_amount, authoritative_amount, created_at
			FROM credit_operation_results
			WHERE operation_id = $1`,
			operationID,
		).Scan(
			&op.OperationID, &op.OperationType, &op.ErrandID, &op.CurrentAmount, &op.NewAmount,
			&op.OutcomeCode, &op.ResultingAmount, &op.AuthoritativeAmount, &op.CreatedAt,
		)
		if err == nil {
			// Row exists. Compare stored fields directly.
			if op.ErrandID != errandID || op.NewAmount != newReward {
				return credit.ErrIdempotencyConflict
			}
			resOp = &op
			if op.OutcomeCode == credit.OutcomeSuccess {
				existing, err := findReservationByErrandID(txCtx, tx, errandID)
				if err != nil {
					return err
				}
				resRev = existing
				return nil
			}
			return mapOutcomeToError(op.OutcomeCode, op.AuthoritativeAmount)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lookup operation result: %w", err)
		}

		// 3. Lock the reservation by errandId.
		newAmount := newReward
		var reservation credit.Reservation
		err = tx.QueryRow(txCtx, `
			SELECT id, errand_id, requester_id, amount, status, created_at, updated_at
			FROM credit_reservations
			WHERE errand_id = $1
			FOR UPDATE`,
			errandID,
		).Scan(
			&reservation.ID, &reservation.ErrandID, &reservation.RequesterID, &reservation.Amount,
			&reservation.Status, &reservation.CreatedAt, &reservation.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Reservation not found is not persisted as an operation result.
				return credit.ErrReservationNotFound
			}
			return fmt.Errorf("lock reservation for adjust: %w", err)
		}
		currentAmount := reservation.Amount

		// 4. Require reservation status RESERVED.
		if reservation.Status != credit.ReservationStatusReserved {
			opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeReservationStateConflict, nil, nil)
			if err != nil {
				return err
			}
			resOp = opRecord
			return credit.ErrReservationStateConflict
		}

		// 5. If currentAmount == newAmount, persist successful result and return unchanged reservation (validated idempotent no-op).
		if currentAmount == newAmount {
			resulting := currentAmount
			opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeSuccess, &resulting, nil)
			if err != nil {
				return err
			}
			resOp = opRecord
			resRev = &reservation
			return nil
		}

		// 7. Otherwise, lock and verify the requester account.
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
			reservation.RequesterID,
		).Scan(&availableCredits, &reservedCredits, &version, &integrityStatus)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeWalletNotReady, nil, nil)
				if err != nil {
					return err
				}
				resOp = opRecord
				return credit.ErrWalletNotReady
			}
			return fmt.Errorf("lock requester account for adjust: %w", err)
		}

		if integrityStatus == credit.IntegrityFlagged {
			opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeAccountIntegrityFailure, nil, nil)
			if err != nil {
				return err
			}
			resOp = opRecord
			return credit.ErrAccountIntegrityFlagged
		}

		diff := newAmount - currentAmount
		var ledgerReason credit.TransactionReason

		if diff > 0 {
			// Increase: move positive diff from available to reserved
			if availableCredits < diff {
				opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeInsufficientCredits, nil, nil)
				if err != nil {
					return err
				}
				resOp = opRecord
				return credit.ErrInsufficientCredits
			}
			if reservedCredits+diff > credit.MaxBalanceCredits {
				opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeCreditLimitExceeded, nil, nil)
				if err != nil {
					return err
				}
				resOp = opRecord
				return credit.ErrCreditLimitExceeded
			}

			cmdTag, err := tx.Exec(txCtx, `
				UPDATE credit_accounts
				SET available_credits = available_credits - $1,
				    reserved_credits = reserved_credits + $1,
				    version = version + 1,
				    updated_at = NOW()
				WHERE user_id = $2 AND available_credits >= $1`,
				diff, reservation.RequesterID,
			)
			if err != nil || cmdTag.RowsAffected() != 1 {
				return fmt.Errorf("debit requester available credits for increase: %w", err)
			}
			ledgerReason = credit.TransactionReasonRewardIncreased
		} else {
			// Decrease: move positive difference from reserved to available
			absDiff := -diff
			if availableCredits+absDiff > credit.MaxBalanceCredits {
				opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeCreditLimitExceeded, nil, nil)
				if err != nil {
					return err
				}
				resOp = opRecord
				return credit.ErrCreditLimitExceeded
			}

			cmdTag, err := tx.Exec(txCtx, `
				UPDATE credit_accounts
				SET available_credits = available_credits + $1,
				    reserved_credits = reserved_credits - $1,
				    version = version + 1,
				    updated_at = NOW()
				WHERE user_id = $2 AND reserved_credits >= $1`,
				absDiff, reservation.RequesterID,
			)
			if err != nil || cmdTag.RowsAffected() != 1 {
				return fmt.Errorf("release requester reserved credits for decrease: %w", err)
			}
			ledgerReason = credit.TransactionReasonRewardDecreased
		}

		// 10. Update the same reservation row's amount and updated_at.
		err = tx.QueryRow(txCtx, `
			UPDATE credit_reservations
			SET amount = $1,
			    updated_at = NOW()
			WHERE id = $2
			RETURNING id, errand_id, requester_id, amount, status, created_at, updated_at`,
			newAmount, reservation.ID,
		).Scan(
			&reservation.ID, &reservation.ErrandID, &reservation.RequesterID, &reservation.Amount,
			&reservation.Status, &reservation.CreatedAt, &reservation.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("update reservation amount: %w", err)
		}

		// 11. Append two new ledger entries under operationId.
		var availDelta, resDelta int64
		if diff > 0 {
			availDelta = -diff
			resDelta = diff
		} else {
			availDelta = -diff // positive
			resDelta = diff    // negative
		}

		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'AVAILABLE', $4, $6, NULL, NOW()),
				($1, $2, $3, 'RESERVED', $5, $6, NULL, NOW())`,
			reservation.RequesterID, errandID, operationID, availDelta, resDelta, ledgerReason,
		)
		if err != nil {
			return fmt.Errorf("insert adjustment ledger entries: %w", err)
		}

		// 12. Persist the successful operation result.
		resulting := newAmount
		opRecord, err := persistOperationResult(txCtx, tx, operationID, errandID, currentAmount, newAmount, credit.OutcomeSuccess, &resulting, nil)
		if err != nil {
			return err
		}

		resOp = opRecord
		resRev = &reservation
		return nil
	})

	if err != nil {
		return resOp, nil, err
	}
	return resOp, resRev, nil
}

func persistOperationResult(ctx context.Context, tx pgx.Tx, operationID uuid.UUID, errandID uuid.UUID, currentAmount, newAmount int64, outcome credit.CreditOperationOutcome, resulting, authoritative *int64) (*credit.OperationResult, error) {
	var op credit.OperationResult
	err := tx.QueryRow(ctx, `
		INSERT INTO credit_operation_results (
			operation_id, operation_type, errand_id, current_amount, new_amount,
			outcome_code, resulting_amount, authoritative_amount, created_at
		)
		VALUES ($1, 'REWARD_ADJUSTMENT', $2, $3, $4, $5, $6, $7, NOW())
		RETURNING operation_id, operation_type, errand_id, current_amount, new_amount,
		          outcome_code, resulting_amount, authoritative_amount, created_at`,
		operationID, errandID, currentAmount, newAmount, outcome, resulting, authoritative,
	).Scan(
		&op.OperationID, &op.OperationType, &op.ErrandID, &op.CurrentAmount, &op.NewAmount,
		&op.OutcomeCode, &op.ResultingAmount, &op.AuthoritativeAmount, &op.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("persist operation result: %w", err)
	}
	return &op, nil
}

func mapOutcomeToError(outcome credit.CreditOperationOutcome, authoritativeAmount *int64) error {
	switch outcome {
	case credit.OutcomeSuccess:
		return nil
	case credit.OutcomeWalletNotReady:
		return credit.ErrWalletNotReady
	case credit.OutcomeInsufficientCredits:
		return credit.ErrInsufficientCredits
	case credit.OutcomeReservationStateConflict:
		return credit.ErrReservationStateConflict
	case credit.OutcomeAccountIntegrityFailure:
		return credit.ErrAccountIntegrityFlagged
	case credit.OutcomeCreditLimitExceeded:
		return credit.ErrCreditLimitExceeded
	default:
		return fmt.Errorf("stored operation failure: %s", outcome)
	}
}
