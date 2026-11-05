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
// If the account has not been materialized yet, it returns a virtual default account with initial credits in memory.
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
			now := time.Now().UTC()
			return &credit.Account{
				UserID:           userID,
				AvailableCredits: credit.InitialAllocationCredits,
				ReservedCredits:  0,
				Version:          1,
				IntegrityStatus:  credit.IntegrityConsistent,
				CreatedAt:        now,
				UpdatedAt:        now,
			}, nil
		}
		return nil, fmt.Errorf("get credit account: %w", err)
	}
	return &a, nil
}

// ListTransactions retrieves cursor-paginated grouped financial operations for an authenticated user.
// If the user's account has not been materialized in the database yet (and no cursor is supplied),
// it returns a virtual genesis registration allocation transaction in memory to remain consistent with GetAccount.
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
			MAX(errand_id::text)::uuid AS errand_id,
			MAX(reason::text)::transaction_reason AS reason,
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

	// If no committed transactions exist for this user on initial page (no cursor),
	// check if the account is unmaterialized. If unmaterialized, return the virtual
	// genesis allocation operation to match the virtual 100 available credits from GetAccount.
	if len(items) == 0 && (cursor == nil || *cursor == "") {
		var accountExists bool
		err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_accounts WHERE user_id = $1)", userID).Scan(&accountExists)
		if err != nil {
			return nil, fmt.Errorf("check account existence for virtual transaction: %w", err)
		}
		if !accountExists {
			now := time.Now().UTC()
			items = append(items, credit.TransactionOperation{
				OperationID:    uuid.NewSHA1(uuid.NameSpaceOID, []byte(userID.String()+"-genesis")),
				ErrandID:       nil,
				Reason:         credit.TransactionReasonRegistrationAllocation,
				AvailableDelta: credit.InitialAllocationCredits,
				ReservedDelta:  0,
				OccurredAt:     &now,
				CreatedAt:      now,
			})
		}
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
