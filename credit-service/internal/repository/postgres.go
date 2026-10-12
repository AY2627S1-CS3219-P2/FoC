// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated F4.1 PostgreSQL adapter for idempotent initial credit allocation.
// Author review: PENDING - reviewer to complete

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
func (r *PostgresRepository) AllocateInitialCredits(ctx context.Context, userID uuid.UUID, amount int, occurredAt time.Time) (*credit.Account, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin initial allocation transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	account, inserted, err := insertInitialAccount(ctx, tx, userID, amount, occurredAt)
	if err != nil {
		return nil, err
	}
	if !inserted {
		account, err = selectAccountForUpdate(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit existing initial allocation transaction: %w", err)
		}
		return account, nil
	}

	if err := insertInitialAllocationTransaction(ctx, tx, userID, amount, occurredAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit initial allocation transaction: %w", err)
	}
	return account, nil
}

func insertInitialAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID, amount int, occurredAt time.Time) (*credit.Account, bool, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO credit_accounts (
			user_id, available_credits, reserved_credits, version, created_at, updated_at
		)
		VALUES ($1, $2, 0, 1, $3, $3)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING user_id, available_credits, reserved_credits, version, updated_at`,
		userID, amount, occurredAt,
	)
	account, err := scanAccount(row)
	if err == nil {
		return account, true, nil
	}
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("insert initial credit account: %w", err)
}

func selectAccountForUpdate(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*credit.Account, error) {
	account, err := scanAccount(tx.QueryRow(ctx, `
		SELECT user_id, available_credits, reserved_credits, version, updated_at
		FROM credit_accounts
		WHERE user_id = $1
		FOR UPDATE`,
		userID,
	))
	if err != nil {
		return nil, fmt.Errorf("select existing credit account: %w", err)
	}
	return account, nil
}

func insertInitialAllocationTransaction(ctx context.Context, tx pgx.Tx, userID uuid.UUID, amount int, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO credit_transactions (user_id, errand_id, amount, reason, created_at)
		VALUES ($1, NULL, $2, $3, $4)`,
		userID, amount, credit.TransactionReasonRegistrationAllocation, occurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert initial allocation transaction: %w", err)
	}
	return nil
}

func scanAccount(row pgx.Row) (*credit.Account, error) {
	var account credit.Account
	err := row.Scan(
		&account.UserID,
		&account.AvailableCredits,
		&account.ReservedCredits,
		&account.Version,
		&account.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &account, nil
}
