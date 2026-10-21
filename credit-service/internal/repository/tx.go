// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented PostgreSQL transaction runner with lock_timeout, statement_timeout, and bounded 3-attempt retries for deadlock/serialization errors.
// Author review: PENDING - reviewer to complete

package repository

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxTxAttempts    = 3
	defaultTxTimeout = 10 * time.Second
)

// isRetryableTxError checks for PostgreSQL SQLSTATEs 40001 (serialization), 40P01 (deadlock), and 55P03 (lock_timeout).
func isRetryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01" || pgErr.Code == "55P03"
	}
	return false
}

func retryDelay(attempt int) time.Duration {
	var base time.Duration
	if attempt == 1 {
		base = 25 * time.Millisecond
	} else {
		base = 75 * time.Millisecond
	}
	// ±20% jitter
	jitterFrac := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(base) * jitterFrac)
}

func (r *PostgresRepository) withTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	var lastErr error
	for attempt := 1; attempt <= maxTxAttempts; attempt++ {
		err := r.execTxOnce(ctx, fn)
		if err == nil {
			return nil
		}
		if !isRetryableTxError(err) {
			return err
		}
		lastErr = err
		if attempt < maxTxAttempts {
			delay := retryDelay(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("%w: transaction retry limit exceeded: %v", credit.ErrServiceUnavailable, lastErr)
}

func (r *PostgresRepository) execTxOnce(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	txCtx, cancel := context.WithTimeout(ctx, defaultTxTimeout)
	defer cancel()

	tx, err := r.pool.BeginTx(txCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(txCtx)
	}()

	// Ordinary financial transactions set lock_timeout to 1s and statement_timeout to 5s.
	if _, err := tx.Exec(txCtx, "SET LOCAL lock_timeout = '1s'; SET LOCAL statement_timeout = '5s'"); err != nil {
		return err
	}

	if err := fn(txCtx, tx); err != nil {
		return err
	}

	return tx.Commit(txCtx)
}
