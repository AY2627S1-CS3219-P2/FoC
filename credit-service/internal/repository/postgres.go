// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated F4.1 PostgreSQL adapter for idempotent initial credit allocation.
// Author review: choppyback - rejected fetching and locking the existing account
// on duplicate registration events; changed duplicate handling to close the DB
// transaction and return success without inserting another transaction record.

package repository

import (
	"context"
	"fmt"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository persists credit-service aggregates in PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs a PostgreSQL credit repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// AllocateInitialCredits creates an account and registration-allocation ledger entry once.
func (r *PostgresRepository) AllocateInitialCredits(ctx context.Context, userID uuid.UUID, amount int, occurredAt time.Time) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin initial allocation transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	inserted, err := insertInitialAccount(ctx, tx, userID, amount, occurredAt)
	if err != nil {
		return err
	}
	if !inserted {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit initial allocation transaction (duplicate): %w", err)
		}
		return nil
	}

	if err := insertInitialAllocationTransaction(ctx, tx, userID, amount, occurredAt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit initial allocation transaction: %w", err)
	}
	return nil
}

func insertInitialAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID, amount int, occurredAt time.Time) (bool, error) {
	commandTag, err := tx.Exec(ctx, `
		INSERT INTO credit_accounts (
			user_id, available_credits, reserved_credits, version, created_at, updated_at
		)
		VALUES ($1, $2, 0, 1, $3, $3)
		ON CONFLICT (user_id) DO NOTHING`,
		userID, amount, occurredAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert initial credit account: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

func insertInitialAllocationTransaction(ctx context.Context, tx pgx.Tx, userID uuid.UUID, amount int, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO credit_transactions (
			user_id, errand_id, operation_id, balance_bucket, amount, reason, created_at
		)
		VALUES ($1, NULL, $2, 'AVAILABLE', $3, $4, $5)`,
		userID, uuid.New(), amount, credit.TransactionReasonRegistrationAllocation, occurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert initial allocation transaction: %w", err)
	}
	return nil
}
