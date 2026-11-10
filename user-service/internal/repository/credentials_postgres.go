// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Shared account-row lock through login/refresh credential persistence.
// Author review: ZI YANG - reviewed and verified

package repository

import (
	"context"
	"errors"
	"fmt"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WithAccountCredentials serializes credential issuance against role updates.
func (r *PostgresSessionRepository) WithAccountCredentials(ctx context.Context, uid uuid.UUID, issue func(*user.User, user.SessionRepository) error) error {
	if issue == nil {
		return errors.New("credential callback is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin credential transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	account, err := scanUser(tx.QueryRow(ctx, getUserByIDQuery+` FOR SHARE`, uid))
	if err != nil {
		return fmt.Errorf("lock credential account: %w", mapDatabaseError(err))
	}
	if err := issue(account, &PostgresSessionRepository{pool: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit credentials: %w", err)
	}
	return nil
}

// sessionDatabase is implemented by a pool and by a transaction (nested Begin
// uses a savepoint), keeping session persistence on the caller's connection.
type sessionDatabase interface {
	userInserter
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
}
