// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL read repository queries for account projection and cursor-paginated grouped transactions per sourceOfTruth.md Phase 9.
// Author review: PENDING - reviewer to complete

package repository

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type cursorPayload struct {
	V           int       `json:"v"`
	CreatedAt   time.Time `json:"createdAt"`
	OperationID uuid.UUID `json:"operationId"`
}

// GetAccount retrieves the account projection for a user.
func (r *PostgresRepository) GetAccount(ctx context.Context, userID uuid.UUID) (*credit.Account, error) {
	var a credit.Account
	err := r.pool.QueryRow(ctx, `
		SELECT user_id, available_credits, reserved_credits, version, integrity_status,
		       integrity_flagged_at, integrity_reason_code, integrity_detail,
		       integrity_expected, integrity_observed, created_at, updated_at
		FROM credit_accounts
		WHERE user_id = $1`,
		userID,
	).Scan(
		&a.UserID, &a.AvailableCredits, &a.ReservedCredits, &a.Version, &a.IntegrityStatus,
		&a.IntegrityFlaggedAt, &a.IntegrityReasonCode, &a.IntegrityDetail,
		&a.IntegrityExpected, &a.IntegrityObserved, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, credit.ErrWalletNotReady
		}
		return nil, fmt.Errorf("get credit account: %w", err)
	}
	return &a, nil
}

// ListTransactions retrieves cursor-paginated grouped financial operations for an authenticated user.
func (r *PostgresRepository) ListTransactions(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*credit.PaginatedTransactions, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var (
		cursorCreatedAt   *time.Time
		cursorOperationID *uuid.UUID
	)

	if cursor != nil && *cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(*cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid cursor base64url encoding", credit.ErrInvalidRequest)
		}

		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()

		var p cursorPayload
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("%w: invalid cursor payload json", credit.ErrInvalidRequest)
		}
		if dec.More() {
			return nil, fmt.Errorf("%w: cursor contains trailing characters", credit.ErrInvalidRequest)
		}
		if p.V != 1 || p.CreatedAt.IsZero() || p.OperationID == uuid.Nil {
			return nil, fmt.Errorf("%w: cursor has invalid fields or version", credit.ErrInvalidRequest)
		}

		utcTime := p.CreatedAt.UTC()
		cursorCreatedAt = &utcTime
		cursorOperationID = &p.OperationID
	}

	fetchLimit := limit + 1
	rows, err := r.pool.Query(ctx, `
		SELECT
			operation_id,
			MAX(errand_id) AS errand_id,
			MAX(reason) AS reason,
			COALESCE(SUM(CASE WHEN balance_bucket = 'AVAILABLE' THEN amount ELSE 0 END), 0)::BIGINT AS available_delta,
			COALESCE(SUM(CASE WHEN balance_bucket = 'RESERVED' THEN amount ELSE 0 END), 0)::BIGINT AS reserved_delta,
			MAX(occurred_at) AS occurred_at,
			MIN(created_at) AS created_at,
			COUNT(DISTINCT reason) AS distinct_reasons
		FROM credit_transactions
		WHERE user_id = $1
		GROUP BY operation_id
		HAVING ($2::TIMESTAMPTZ IS NULL OR (MIN(created_at) < $2 OR (MIN(created_at) = $2 AND operation_id < $3)))
		ORDER BY created_at DESC, operation_id DESC
		LIMIT $4`,
		userID, cursorCreatedAt, cursorOperationID, fetchLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("list user transactions: %w", err)
	}
	defer rows.Close()

	var items []credit.TransactionOperation
	for rows.Next() {
		var (
			op              credit.TransactionOperation
			distinctReasons int
		)
		if err := rows.Scan(
			&op.OperationID, &op.ErrandID, &op.Reason, &op.AvailableDelta, &op.ReservedDelta,
			&op.OccurredAt, &op.CreatedAt, &distinctReasons,
		); err != nil {
			return nil, fmt.Errorf("scan transaction operation: %w", err)
		}
		if distinctReasons > 1 {
			return nil, fmt.Errorf("%w: multiple distinct transaction reasons in one operation for user %s",
				credit.ErrAccountIntegrityFlagged, userID)
		}
		items = append(items, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transaction operations: %w", err)
	}

	var nextCursor *string
	if len(items) > limit {
		lastItem := items[limit-1]
		payload := cursorPayload{
			V:           1,
			CreatedAt:   lastItem.CreatedAt.UTC(),
			OperationID: lastItem.OperationID,
		}
		payloadBytes, _ := json.Marshal(payload)
		encoded := base64.RawURLEncoding.EncodeToString(payloadBytes)
		nextCursor = &encoded
		items = items[:limit]
	}

	return &credit.PaginatedTransactions{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}
