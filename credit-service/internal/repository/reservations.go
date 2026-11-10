// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-11-03
// Scope: Removed credit_operation_results dependency; reward adjustment and reward_set idempotency backed by credit_transactions ledger and reservation version. Lazy wallet initialization on reservation creation.
// Author review: Reviewed by Seth

package repository

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CreateReservation executes the idempotent reservation transaction described in Phase 4.
func (r *PostgresRepository) CreateReservation(ctx context.Context, errandID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error) {
	var (
		res     *credit.Reservation
		created bool
	)

	err := r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// 1. Transaction-scoped PostgreSQL advisory lock derived from errandId.
		if _, err := tx.Exec(txCtx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey(errandID)); err != nil {
			return fmt.Errorf("acquire advisory lock: %w", err)
		}

		// 2. Look up reservation by errandId.
		existing, err := findReservationByErrandID(txCtx, tx, errandID)
		if err != nil {
			return err
		}

		// 3. If row exists:
		if existing != nil {
			if existing.RequesterID != requesterID || existing.Amount != amount {
				return credit.ErrReservationConflict
			}
			if existing.Status != credit.ReservationStatusReserved {
				return credit.ErrReservationStateConflict
			}
			res = existing
			created = false
			return nil
		}

		// 4. Ensure requester wallet exists (lazy initialization) and lock it.
		if err := ensureWalletInitialized(txCtx, tx, requesterID, credit.InitialAllocationCredits, time.Now().UTC()); err != nil {
			return fmt.Errorf("ensure requester wallet initialized: %w", err)
		}

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
			return fmt.Errorf("lock requester account: %w", err)
		}

		// 5. Account integrity checks.
		if integrityStatus == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}
		if availableCredits < amount {
			return credit.ErrInsufficientCredits
		}
		if reservedCredits+amount > credit.MaxBalanceCredits {
			return credit.ErrCreditLimitExceeded
		}

		// 6. Update credit_accounts.
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
			return fmt.Errorf("update credit account balances: %w", err)
		}
		if cmdTag.RowsAffected() != 1 {
			return credit.ErrInsufficientCredits
		}

		// 7. Insert credit_reservations row.
		var newRes credit.Reservation
		err = tx.QueryRow(txCtx, `
			INSERT INTO credit_reservations (
				errand_id, requester_id, amount, status, version, created_at, updated_at
			)
			VALUES ($1, $2, $3, 'RESERVED', 1, NOW(), NOW())
			RETURNING id, errand_id, requester_id, amount, status, version, created_at, updated_at`,
			errandID, requesterID, amount,
		).Scan(
			&newRes.ID, &newRes.ErrandID, &newRes.RequesterID, &newRes.Amount,
			&newRes.Status, &newRes.Version, &newRes.CreatedAt, &newRes.UpdatedAt,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return credit.ErrReservationConflict
			}
			return fmt.Errorf("insert reservation: %w", err)
		}

		// 8. Generate new operationId and append two balanced ledger rows.
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
			return fmt.Errorf("insert reservation ledger entries: %w", err)
		}

		res = &newRes
		created = true
		return nil
	})

	if err != nil {
		return nil, false, err
	}
	return res, created, nil
}

// AdjustReservationReward executes the reward adjustment transaction.
// Idempotency is backed by the immutable credit_transactions ledger and reservation version.
func (r *PostgresRepository) AdjustReservationReward(ctx context.Context, errandID uuid.UUID, requesterID uuid.UUID, newReward int64, currVersion int64, operationID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
	if currVersion < 1 || currVersion == math.MaxInt64 {
		return nil, nil, fmt.Errorf("%w: currVersion cannot advance", credit.ErrInvalidRequest)
	}
	targetVersion := currVersion + 1
	var resRev *credit.Reservation

	err := r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		resRev = nil

		// 1. Transaction-scoped PostgreSQL advisory lock derived from operationId.
		if _, err := tx.Exec(txCtx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey(operationID)); err != nil {
			return fmt.Errorf("acquire advisory lock: %w", err)
		}

		// 2. Check if this operationId was already committed in the ledger.
		var (
			existingErrandID   uuid.UUID
			existingUserID     uuid.UUID
			existingResVersion *int64
		)
		err := tx.QueryRow(txCtx, `
			SELECT errand_id, user_id, reservation_version
			FROM credit_transactions
			WHERE operation_id = $1 AND balance_bucket = 'RESERVED'
			LIMIT 1`,
			operationID,
		).Scan(&existingErrandID, &existingUserID, &existingResVersion)
		if err == nil {
			// Operation already committed. Verify it was for the exact same errand, requester, and target version.
			if existingErrandID != errandID || existingUserID != requesterID {
				return credit.ErrIdempotencyConflict
			}
			if existingResVersion != nil && *existingResVersion != targetVersion {
				return credit.ErrIdempotencyConflict
			}
			existing, err := findReservationByErrandID(txCtx, tx, errandID)
			if err != nil {
				return err
			}
			if existing == nil {
				return credit.ErrReservationNotFound
			}
			resRev = existing
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lookup operation in ledger: %w", err)
		}

		// 3. Lock the reservation by errandId.
		newAmount := newReward
		var reservation credit.Reservation
		err = tx.QueryRow(txCtx, `
			SELECT id, errand_id, requester_id, amount, status, version, created_at, updated_at
			FROM credit_reservations
			WHERE errand_id = $1
			FOR UPDATE`,
			errandID,
		).Scan(
			&reservation.ID, &reservation.ErrandID, &reservation.RequesterID, &reservation.Amount,
			&reservation.Status, &reservation.Version, &reservation.CreatedAt, &reservation.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrReservationNotFound
			}
			return fmt.Errorf("lock reservation for adjust: %w", err)
		}
		if reservation.RequesterID != requesterID {
			return credit.ErrReservationNotFound
		}
		currentAmount := reservation.Amount

		// 4. Require reservation status RESERVED.
		if reservation.Status != credit.ReservationStatusReserved {
			return credit.ErrReservationStateConflict
		}
		// Reject stale requests from an earlier order state.
		// Requests where currVersion >= reservation.Version are accepted, allowing raises after non-credit edits.
		if currVersion < reservation.Version {
			return credit.ErrReservationConflict
		}

		// 5. Reject zero-delta adjustments: credit ledger operations require non-zero deltas.
		if currentAmount == newAmount {
			return fmt.Errorf("%w: newReward matches current reward; no adjustment needed", credit.ErrInvalidRequest)
		}

		// 6. Otherwise, lock and verify the requester account.
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
				return credit.ErrWalletNotReady
			}
			return fmt.Errorf("lock requester account for adjust: %w", err)
		}

		if integrityStatus == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}

		diff := newAmount - currentAmount
		var ledgerReason credit.TransactionReason

		if diff > 0 {
			// Increase: move positive diff from available to reserved
			if availableCredits < diff {
				return credit.ErrInsufficientCredits
			}
			if reservedCredits+diff > credit.MaxBalanceCredits {
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

		// 7. Update the same reservation row's amount and version.
		err = tx.QueryRow(txCtx, `
			UPDATE credit_reservations
			SET amount = $1,
			    version = $3,
			    updated_at = NOW()
			WHERE id = $2
			RETURNING id, errand_id, requester_id, amount, status, version, created_at, updated_at`,
			newAmount, reservation.ID, targetVersion,
		).Scan(
			&reservation.ID, &reservation.ErrandID, &reservation.RequesterID, &reservation.Amount,
			&reservation.Status, &reservation.Version, &reservation.CreatedAt, &reservation.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("update reservation amount: %w", err)
		}

		// 8. Append two new ledger entries under operationId.
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
				user_id, errand_id, operation_id, balance_bucket, amount, reason, reservation_version, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'AVAILABLE', $4, $6, $7, NULL, NOW()),
				($1, $2, $3, 'RESERVED', $5, $6, $7, NULL, NOW())`,
			reservation.RequesterID, errandID, operationID, availDelta, resDelta, ledgerReason, targetVersion,
		)
		if err != nil {
			return fmt.Errorf("insert adjustment ledger entries: %w", err)
		}

		resRev = &reservation
		return nil
	})

	if err != nil {
		return nil, nil, err
	}
	return nil, resRev, nil
}

// ApplyRewardSet applies an outbox errand.reward_set event to reconcile hold with committed decreases (D-057, D-102).
func (r *PostgresRepository) ApplyRewardSet(ctx context.Context, errandID uuid.UUID, requesterID *uuid.UUID, eventID uuid.UUID, newReward, version int64, occurredAt time.Time) error {
	return r.withTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// Lock the reservation
		var reservation credit.Reservation
		err := tx.QueryRow(txCtx, `
			SELECT id, errand_id, requester_id, amount, status, version, created_at, updated_at
			FROM credit_reservations
			WHERE errand_id = $1
			FOR UPDATE`, errandID,
		).Scan(
			&reservation.ID, &reservation.ErrandID, &reservation.RequesterID, &reservation.Amount,
			&reservation.Status, &reservation.Version, &reservation.CreatedAt, &reservation.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrReservationNotFound
			}
			return fmt.Errorf("lock reservation for reward_set: %w", err)
		}

		// Requester identity mismatch check
		if requesterID != nil && *requesterID != uuid.Nil && *requesterID != reservation.RequesterID {
			return credit.ErrRequesterMismatch
		}

		// Terminal state: ignore as safe no-op
		if reservation.Status != credit.ReservationStatusReserved {
			return nil
		}

		// A successful PATCH with this eventID already made the financial move in the ledger.
		var appliedCount int
		err = tx.QueryRow(txCtx, `
			SELECT COUNT(*)
			FROM credit_transactions
			WHERE operation_id = $1 AND errand_id = $2`, eventID, errandID,
		).Scan(&appliedCount)
		if err == nil && appliedCount > 0 {
			return nil
		}

		// Older events cannot change a hold already set for a newer order state.
		if version < reservation.Version {
			return credit.ErrRewardSetStale
		}

		diff := newReward - reservation.Amount
		if diff > 0 {
			return credit.ErrLifecycleViolation
		}
		if diff == 0 {
			_, err := tx.Exec(txCtx, `
				UPDATE credit_reservations
				SET version = $1, updated_at = NOW()
				WHERE id = $2`,
				version, reservation.ID,
			)
			return err
		}

		// Lock requester account
		var (
			availableCredits int64
			reservedCredits  int64
			accVersion       int64
			integrityStatus  credit.AccountIntegrityStatus
		)
		err = tx.QueryRow(txCtx, `
			SELECT available_credits, reserved_credits, version, integrity_status
			FROM credit_accounts
			WHERE user_id = $1
			FOR UPDATE`,
			reservation.RequesterID,
		).Scan(&availableCredits, &reservedCredits, &accVersion, &integrityStatus)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credit.ErrWalletNotReady
			}
			return fmt.Errorf("lock requester account for reward_set: %w", err)
		}

		if integrityStatus == credit.IntegrityFlagged {
			return credit.ErrAccountIntegrityFlagged
		}

		absDiff := -diff
		if reservedCredits < absDiff {
			return credit.ErrInsufficientCredits
		}
		if availableCredits+absDiff > credit.MaxBalanceCredits {
			return credit.ErrCreditLimitExceeded
		}

		// Move absDiff from reserved to available
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
			return fmt.Errorf("release requester reserved credits for reward_set: %w", err)
		}

		// Update reservation amount and version
		_, err = tx.Exec(txCtx, `
			UPDATE credit_reservations
			SET amount = $1,
			    version = $2,
			    updated_at = NOW()
			WHERE id = $3`,
			newReward, version, reservation.ID,
		)
		if err != nil {
			return fmt.Errorf("update reservation for reward_set: %w", err)
		}

		// Append balanced ledger entries
		_, err = tx.Exec(txCtx, `
			INSERT INTO credit_transactions (
				user_id, errand_id, operation_id, balance_bucket, amount, reason, reservation_version, occurred_at, created_at
			)
			VALUES
				($1, $2, $3, 'AVAILABLE', $4, 'REWARD_DECREASED', $6, $5, NOW()),
				($1, $2, $3, 'RESERVED', -($4::bigint), 'REWARD_DECREASED', $6, $5, NOW())`,
			reservation.RequesterID, errandID, eventID, absDiff, occurredAt.UTC(), version,
		)
		if err != nil {
			return fmt.Errorf("insert reward_set ledger entries: %w", err)
		}

		return nil
	})
}

func findReservationByErrandID(ctx context.Context, tx pgx.Tx, errandID uuid.UUID) (*credit.Reservation, error) {
	var res credit.Reservation
	err := tx.QueryRow(ctx, `
		SELECT id, errand_id, requester_id, amount, status, version, created_at, updated_at
		FROM credit_reservations
		WHERE errand_id = $1`,
		errandID,
	).Scan(
		&res.ID, &res.ErrandID, &res.RequesterID, &res.Amount,
		&res.Status, &res.Version, &res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find reservation by errand ID: %w", err)
	}
	return &res, nil
}

func advisoryLockKey(id uuid.UUID) int64 {
	return int64(binary.BigEndian.Uint64(id[:8]))
}
