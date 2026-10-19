// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL reconciliation repository with session advisory locks, keyset batch scanning, ledger replay, and version-checked account flagging per sourceOfTruth.md Phase 10.
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

// ReconciliationAdvisoryLockKey is the fixed 64-bit key used for session-level advisory locking across replicas.
// ASCII representation of 'CRED_REC'.
const ReconciliationAdvisoryLockKey int64 = 0x435245445F524543

type discrepancyFinding struct {
	reasonCode string
	detail     string
	expected   *float64
	observed   *float64
}

// AcquireReconciliationLock attempts to acquire the session-level advisory lock on a dedicated connection.
func (r *PostgresRepository) AcquireReconciliationLock(ctx context.Context) (func(), bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire connection for reconciliation lock: %w", err)
	}

	var acquired bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", ReconciliationAdvisoryLockKey).Scan(&acquired)
	if err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}

	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	unlock := func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", ReconciliationAdvisoryLockKey)
		conn.Release()
	}

	return unlock, true, nil
}

// ScanAccountsForReconciliation returns a batch of user IDs ordered by user_id ASC with keyset cursor advancement.
func (r *PostgresRepository) ScanAccountsForReconciliation(ctx context.Context, batchSize int, lastSeenUserID *uuid.UUID) ([]uuid.UUID, *uuid.UUID, error) {
	if batchSize <= 0 {
		batchSize = 500
	}

	rows, err := r.pool.Query(ctx, `
		SELECT user_id
		FROM credit_accounts
		WHERE ($1::UUID IS NULL OR user_id > $1)
		ORDER BY user_id ASC
		LIMIT $2`,
		lastSeenUserID, batchSize,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("scan accounts: %w", err)
	}
	defer rows.Close()

	var userIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, nil, fmt.Errorf("scan user id: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate accounts: %w", err)
	}

	var nextCursor *uuid.UUID
	if len(userIDs) == batchSize {
		lastID := userIDs[len(userIDs)-1]
		nextCursor = &lastID
	}

	return userIDs, nextCursor, nil
}

// ReconcileAccount verifies all financial invariants for an account using a REPEATABLE READ snapshot.
// If any invariant fails, it conditionally updates the account to FLAGGED verifying the observed version.
func (r *PostgresRepository) ReconcileAccount(ctx context.Context, userID uuid.UUID) (bool, error) {
	finding, observedVersion, err := r.auditAccountSnapshot(ctx, userID)
	if err != nil {
		return false, err
	}
	if finding == nil {
		return false, nil
	}

	return r.applyIntegrityFlag(ctx, userID, observedVersion, *finding)
}

func (r *PostgresRepository) auditAccountSnapshot(ctx context.Context, userID uuid.UUID) (*discrepancyFinding, int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("begin audit snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		version          int64
		availableCredits int64
		reservedCredits  int64
		integrityStatus  credit.AccountIntegrityStatus
	)
	err = tx.QueryRow(ctx, `
		SELECT version, available_credits, reserved_credits, integrity_status
		FROM credit_accounts
		WHERE user_id = $1`,
		userID,
	).Scan(&version, &availableCredits, &reservedCredits, &integrityStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("query account projection: %w", err)
	}

	// 1. Validate bucket bounds on stored projection
	if availableCredits < credit.MinBalanceCredits || availableCredits > credit.MaxBalanceCredits {
		fObs := float64(availableCredits)
		return &discrepancyFinding{
			reasonCode: "OUT_OF_BOUNDS_BALANCE",
			detail:     "available_credits exceeds approved account bucket bounds",
			observed:   &fObs,
		}, version, nil
	}
	if reservedCredits < credit.MinBalanceCredits || reservedCredits > credit.MaxBalanceCredits {
		fObs := float64(reservedCredits)
		return &discrepancyFinding{
			reasonCode: "OUT_OF_BOUNDS_BALANCE",
			detail:     "reserved_credits exceeds approved account bucket bounds",
			observed:   &fObs,
		}, version, nil
	}

	// 2. Compute ledger sums from credit_transactions using NUMERIC aggregation
	var (
		ledgerAvailable float64
		ledgerReserved  float64
	)
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN balance_bucket = 'AVAILABLE' THEN amount::NUMERIC ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN balance_bucket = 'RESERVED' THEN amount::NUMERIC ELSE 0 END), 0)
		FROM credit_transactions
		WHERE user_id = $1`,
		userID,
	).Scan(&ledgerAvailable, &ledgerReserved)
	if err != nil {
		return nil, 0, fmt.Errorf("aggregate ledger amounts: %w", err)
	}

	fAvailObs := float64(availableCredits)
	if ledgerAvailable != fAvailObs {
		return &discrepancyFinding{
			reasonCode: "AVAILABLE_PROJECTION_MISMATCH",
			detail:     "ledger available sum differs from account projection",
			expected:   &ledgerAvailable,
			observed:   &fAvailObs,
		}, version, nil
	}

	fResObs := float64(reservedCredits)
	if ledgerReserved != fResObs {
		return &discrepancyFinding{
			reasonCode: "RESERVED_PROJECTION_MISMATCH",
			detail:     "ledger reserved sum differs from account projection",
			expected:   &ledgerReserved,
			observed:   &fResObs,
		}, version, nil
	}

	// 3. Compare sum of active RESERVED reservations against reserved projection
	var activeReservationsSum float64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount::NUMERIC), 0)
		FROM credit_reservations
		WHERE requester_id = $1 AND status = 'RESERVED'`,
		userID,
	).Scan(&activeReservationsSum)
	if err != nil {
		return nil, 0, fmt.Errorf("aggregate active reservations: %w", err)
	}

	if activeReservationsSum != fResObs {
		return &discrepancyFinding{
			reasonCode: "RESERVED_PROJECTION_MISMATCH",
			detail:     "active reservations sum differs from reserved credits",
			expected:   &activeReservationsSum,
			observed:   &fResObs,
		}, version, nil
	}

	// 4. Replay errand-linked ledger history for all reservations where user is requester
	resRows, err := tx.Query(ctx, `
		SELECT errand_id, amount, status
		FROM credit_reservations
		WHERE requester_id = $1
		ORDER BY errand_id ASC`,
		userID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query reservations for replay: %w", err)
	}
	defer resRows.Close()

	type resRecord struct {
		errandID uuid.UUID
		amount   int64
		status   credit.ReservationStatus
	}
	var reservations []resRecord
	for resRows.Next() {
		var rr resRecord
		if err := resRows.Scan(&rr.errandID, &rr.amount, &rr.status); err != nil {
			return nil, 0, fmt.Errorf("scan reservation: %w", err)
		}
		reservations = append(reservations, rr)
	}
	if err := resRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate reservations: %w", err)
	}

	for _, res := range reservations {
		// Check pending work invariants
		var hasPendingRefund, hasPendingPayout bool
		err := tx.QueryRow(ctx, `
			SELECT
				EXISTS(SELECT 1 FROM pending_refunds WHERE errand_id = $1),
				EXISTS(SELECT 1 FROM pending_payouts WHERE errand_id = $1)`,
			res.errandID,
		).Scan(&hasPendingRefund, &hasPendingPayout)
		if err != nil {
			return nil, 0, fmt.Errorf("check pending work: %w", err)
		}

		if hasPendingRefund && hasPendingPayout {
			return &discrepancyFinding{
				reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
				detail:     fmt.Sprintf("both pending refund and pending payout exist for errand %s", res.errandID),
			}, version, nil
		}
		if (hasPendingRefund || hasPendingPayout) && res.status != credit.ReservationStatusReserved {
			return &discrepancyFinding{
				reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
				detail:     fmt.Sprintf("pending work exists for non-reserved errand %s (status %s)", res.errandID, res.status),
			}, version, nil
		}

		// Replay errand-linked transactions
		finding, err := r.replayErrandTransactions(ctx, tx, res.errandID, res.amount, res.status)
		if err != nil {
			return nil, 0, err
		}
		if finding != nil {
			return finding, version, nil
		}
	}

	// 5. Validate operation shapes for each distinct operation involving this user
	opRows, err := tx.Query(ctx, `
		SELECT DISTINCT operation_id
		FROM credit_transactions
		WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query user operations: %w", err)
	}
	defer opRows.Close()

	var opIDs []uuid.UUID
	for opRows.Next() {
		var opID uuid.UUID
		if err := opRows.Scan(&opID); err != nil {
			return nil, 0, fmt.Errorf("scan operation id: %w", err)
		}
		opIDs = append(opIDs, opID)
	}
	if err := opRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate operations: %w", err)
	}

	for _, opID := range opIDs {
		finding, err := r.validateOperationShape(ctx, tx, opID)
		if err != nil {
			return nil, 0, err
		}
		if finding != nil {
			return finding, version, nil
		}
	}

	return nil, version, nil
}

type ledgerRow struct {
	userID        uuid.UUID
	balanceBucket credit.BalanceBucket
	amount        int64
	reason        credit.TransactionReason
	errandID      *uuid.UUID
}

func (r *PostgresRepository) replayErrandTransactions(ctx context.Context, tx pgx.Tx, errandID uuid.UUID, storedAmount int64, status credit.ReservationStatus) (*discrepancyFinding, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, balance_bucket, amount, reason, errand_id
		FROM credit_transactions
		WHERE errand_id = $1
		ORDER BY created_at ASC, id ASC`,
		errandID,
	)
	if err != nil {
		return nil, fmt.Errorf("query errand transactions: %w", err)
	}
	defer rows.Close()

	var entries []ledgerRow
	for rows.Next() {
		var l ledgerRow
		if err := rows.Scan(&l.userID, &l.balanceBucket, &l.amount, &l.reason, &l.errandID); err != nil {
			return nil, fmt.Errorf("scan errand transaction: %w", err)
		}
		entries = append(entries, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate errand transactions: %w", err)
	}

	if len(entries) < 2 {
		return &discrepancyFinding{
			reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
			detail:     fmt.Sprintf("errand %s has fewer than 2 ledger entries", errandID),
		}, nil
	}

	var (
		reconstructedAmount int64
		reservationFound    bool
		refundCount         int
		payoutCount         int
	)

	// Group by operation in order
	type opGroup struct {
		reason credit.TransactionReason
		rows   []ledgerRow
	}
	var ops []opGroup
	rowMap := make(map[credit.TransactionReason]int)
	_ = rowMap

	for _, entry := range entries {
		if entry.balanceBucket == credit.BucketReserved {
			switch entry.reason {
			case credit.TransactionReasonErrandReservation:
				if reservationFound {
					return &discrepancyFinding{
						reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
						detail:     fmt.Sprintf("multiple ERRAND_RESERVATION operations found for errand %s", errandID),
					}, nil
				}
				reservationFound = true
				reconstructedAmount = entry.amount
			case credit.TransactionReasonRewardIncreased:
				reconstructedAmount += entry.amount
			case credit.TransactionReasonRewardDecreased:
				reconstructedAmount -= (-entry.amount) // entry.amount is negative in RESERVED bucket
			case credit.TransactionReasonErrandCancelled, credit.TransactionReasonErrandExpired, credit.TransactionReasonErrandFailed:
				refundCount++
			case credit.TransactionReasonErrandCompletedDeduct:
				payoutCount++
			}
		}
	}
	_ = ops

	if !reservationFound {
		return &discrepancyFinding{
			reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
			detail:     fmt.Sprintf("no ERRAND_RESERVATION operation found for errand %s", errandID),
		}, nil
	}

	if reconstructedAmount != storedAmount {
		fExp := float64(reconstructedAmount)
		fObs := float64(storedAmount)
		return &discrepancyFinding{
			reasonCode: "RESERVATION_AMOUNT_MISMATCH",
			detail:     fmt.Sprintf("reconstructed reservation amount %d differs from stored amount %d", reconstructedAmount, storedAmount),
			expected:   &fExp,
			observed:   &fObs,
		}, nil
	}

	switch status {
	case credit.ReservationStatusReserved:
		if refundCount > 0 || payoutCount > 0 {
			return &discrepancyFinding{
				reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
				detail:     fmt.Sprintf("RESERVED reservation %s has completed settlement ledger rows", errandID),
			}, nil
		}
	case credit.ReservationStatusRefunded:
		if refundCount != 1 || payoutCount > 0 {
			return &discrepancyFinding{
				reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
				detail:     fmt.Sprintf("REFUNDED reservation %s must have exactly 1 refund settlement, got %d refunds and %d payouts", errandID, refundCount, payoutCount),
			}, nil
		}
	case credit.ReservationStatusPaidOut:
		if payoutCount != 1 || refundCount > 0 {
			return &discrepancyFinding{
				reasonCode: "RESERVATION_LIFECYCLE_MISMATCH",
				detail:     fmt.Sprintf("PAID_OUT reservation %s must have exactly 1 payout settlement, got %d payouts and %d refunds", errandID, payoutCount, refundCount),
			}, nil
		}
	}

	return nil, nil
}

func (r *PostgresRepository) validateOperationShape(ctx context.Context, tx pgx.Tx, operationID uuid.UUID) (*discrepancyFinding, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, balance_bucket, amount, reason, errand_id
		FROM credit_transactions
		WHERE operation_id = $1`,
		operationID,
	)
	if err != nil {
		return nil, fmt.Errorf("query operation shape: %w", err)
	}
	defer rows.Close()

	var entries []ledgerRow
	for rows.Next() {
		var l ledgerRow
		if err := rows.Scan(&l.userID, &l.balanceBucket, &l.amount, &l.reason, &l.errandID); err != nil {
			return nil, fmt.Errorf("scan operation row: %w", err)
		}
		entries = append(entries, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operation rows: %w", err)
	}

	// 1. Initial allocation: exactly 1 row, AVAILABLE +100, no errandId, REGISTRATION_ALLOCATION
	if len(entries) == 1 {
		e := entries[0]
		if e.balanceBucket == credit.BucketAvailable && e.amount == credit.InitialAllocationCredits && e.reason == credit.TransactionReasonRegistrationAllocation && e.errandID == nil {
			return nil, nil
		}
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("operation %s is a 1-row non-zero movement that does not match REGISTRATION_ALLOCATION", operationID),
		}, nil
	}

	// All other valid operations must have exactly 2 rows summing to 0
	if len(entries) != 2 {
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("operation %s has %d rows (expected 2)", operationID, len(entries)),
		}, nil
	}

	e1, e2 := entries[0], entries[1]
	if e1.amount+e2.amount != 0 {
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("operation %s entries do not sum to 0 (%d + %d)", operationID, e1.amount, e2.amount),
		}, nil
	}

	if e1.errandID == nil || e2.errandID == nil || *e1.errandID != *e2.errandID {
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("operation %s entries do not share a matching errand ID", operationID),
		}, nil
	}

	// Case A: Payout (Requester ERRAND_COMPLETED_DEDUCTION in RESERVED, Courier ERRAND_COMPLETED_PAYOUT in AVAILABLE)
	if (e1.reason == credit.TransactionReasonErrandCompletedDeduct && e2.reason == credit.TransactionReasonErrandCompletedPayout) ||
		(e1.reason == credit.TransactionReasonErrandCompletedPayout && e2.reason == credit.TransactionReasonErrandCompletedDeduct) {
		if e1.userID == e2.userID {
			return &discrepancyFinding{
				reasonCode: "OPERATION_SHAPE_VIOLATION",
				detail:     fmt.Sprintf("payout operation %s has same user for requester and courier", operationID),
			}, nil
		}
		var dedRow, payRow ledgerRow
		if e1.reason == credit.TransactionReasonErrandCompletedDeduct {
			dedRow, payRow = e1, e2
		} else {
			dedRow, payRow = e2, e1
		}
		if dedRow.balanceBucket != credit.BucketReserved || dedRow.amount >= 0 ||
			payRow.balanceBucket != credit.BucketAvailable || payRow.amount <= 0 {
			return &discrepancyFinding{
				reasonCode: "OPERATION_SHAPE_VIOLATION",
				detail:     fmt.Sprintf("payout operation %s buckets or signs invalid", operationID),
			}, nil
		}
		return nil, nil
	}

	// Case B: Same-user operations (Reservation, RewardIncrease, RewardDecrease, Refund)
	if e1.userID != e2.userID {
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("operation %s spans different users without being a completion payout", operationID),
		}, nil
	}

	if e1.reason != e2.reason {
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("same-user operation %s has mismatched reasons (%s vs %s)", operationID, e1.reason, e2.reason),
		}, nil
	}

	switch e1.reason {
	case credit.TransactionReasonErrandReservation:
		var availRow, resRow ledgerRow
		if e1.balanceBucket == credit.BucketAvailable {
			availRow, resRow = e1, e2
		} else {
			availRow, resRow = e2, e1
		}
		if availRow.amount >= 0 || resRow.amount <= 0 || resRow.balanceBucket != credit.BucketReserved {
			return &discrepancyFinding{
				reasonCode: "OPERATION_SHAPE_VIOLATION",
				detail:     fmt.Sprintf("reservation operation %s has invalid bucket directions", operationID),
			}, nil
		}
		return nil, nil

	case credit.TransactionReasonRewardIncreased:
		var availRow, resRow ledgerRow
		if e1.balanceBucket == credit.BucketAvailable {
			availRow, resRow = e1, e2
		} else {
			availRow, resRow = e2, e1
		}
		if availRow.amount >= 0 || resRow.amount <= 0 || resRow.balanceBucket != credit.BucketReserved {
			return &discrepancyFinding{
				reasonCode: "OPERATION_SHAPE_VIOLATION",
				detail:     fmt.Sprintf("reward increase operation %s has invalid bucket directions", operationID),
			}, nil
		}
		return nil, nil

	case credit.TransactionReasonRewardDecreased:
		var availRow, resRow ledgerRow
		if e1.balanceBucket == credit.BucketAvailable {
			availRow, resRow = e1, e2
		} else {
			availRow, resRow = e2, e1
		}
		if availRow.amount <= 0 || resRow.amount >= 0 || resRow.balanceBucket != credit.BucketReserved {
			return &discrepancyFinding{
				reasonCode: "OPERATION_SHAPE_VIOLATION",
				detail:     fmt.Sprintf("reward decrease operation %s has invalid bucket directions", operationID),
			}, nil
		}
		return nil, nil

	case credit.TransactionReasonErrandCancelled, credit.TransactionReasonErrandExpired, credit.TransactionReasonErrandFailed:
		var availRow, resRow ledgerRow
		if e1.balanceBucket == credit.BucketAvailable {
			availRow, resRow = e1, e2
		} else {
			availRow, resRow = e2, e1
		}
		if availRow.amount <= 0 || resRow.amount >= 0 || resRow.balanceBucket != credit.BucketReserved {
			return &discrepancyFinding{
				reasonCode: "OPERATION_SHAPE_VIOLATION",
				detail:     fmt.Sprintf("refund operation %s has invalid bucket directions", operationID),
			}, nil
		}
		return nil, nil

	default:
		return &discrepancyFinding{
			reasonCode: "OPERATION_SHAPE_VIOLATION",
			detail:     fmt.Sprintf("operation %s has unapproved reason %s", operationID, e1.reason),
		}, nil
	}
}

func (r *PostgresRepository) applyIntegrityFlag(ctx context.Context, userID uuid.UUID, observedVersion int64, finding discrepancyFinding) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE credit_accounts
		SET integrity_status = 'FLAGGED',
		    integrity_flagged_at = COALESCE(integrity_flagged_at, NOW()),
		    integrity_reason_code = COALESCE(integrity_reason_code, $2),
		    integrity_detail = COALESCE(integrity_detail, $3),
		    integrity_expected = COALESCE(integrity_expected, $4),
		    integrity_observed = COALESCE(integrity_observed, $5),
		    version = version + 1,
		    updated_at = NOW()
		WHERE user_id = $1 AND version = $6`,
		userID, finding.reasonCode, finding.detail, finding.expected, finding.observed, observedVersion,
	)
	if err != nil {
		return false, fmt.Errorf("apply integrity flag: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}
