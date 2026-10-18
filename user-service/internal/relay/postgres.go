// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: The relay's PostgreSQL store, adapted from order-service/internal/relay:
//   claims one due outbox_events row under a row lock held across the
//   publish, and records the outcome.
// Author review: James Teo — reviewed and verified.

package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SKIP LOCKED lets concurrent relays each take a different row instead of
// queueing behind one another.
const claimSQL = `
SELECT event_id, routing_key, payload, correlation_id, attempts
FROM outbox_events
WHERE published_at IS NULL AND available_at <= now()
ORDER BY available_at, created_at
LIMIT 1
FOR UPDATE SKIP LOCKED`

const markPublishedSQL = `UPDATE outbox_events SET published_at = now() WHERE event_id = $1`

const markFailedSQL = `
UPDATE outbox_events
SET attempts = attempts + 1, last_error = $2, available_at = now() + $3::interval
WHERE event_id = $1`

const (
	// maxLastError caps the error text kept on a row, in characters.
	maxLastError = 500
	// finishTimeout bounds the statements that close a claim when they run
	// detached from a cancelled context.
	finishTimeout = 5 * time.Second
)

// PostgresStore is the Store over user-service's outbox_events table.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a Store over the outbox_events table in pool's
// database.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// ClaimNext implements Store in one transaction. The row stays locked while
// publish runs, so a second relay skips it.
func (s *PostgresStore) ClaimNext(ctx context.Context, retryDelay time.Duration, publish func(ctx context.Context, r Record) error) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer rollback(ctx, tx)

	var r Record
	err = tx.QueryRow(ctx, claimSQL).Scan(&r.EventID, &r.RoutingKey, &r.Payload, &r.CorrelationID, &r.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit empty outbox claim: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim outbox row: %w", err)
	}

	if pubErr := publish(ctx, r); pubErr != nil {
		// A publish cut short because the relay is stopping is not a failed
		// attempt: rolling back leaves the row as it was for the next run.
		if ctx.Err() != nil {
			return false, fmt.Errorf("publish of outbox event %s interrupted: %w", r.EventID, ctx.Err())
		}
		if _, err := tx.Exec(ctx, markFailedSQL, r.EventID, lastError(pubErr), retryDelay); err != nil {
			return false, fmt.Errorf("record failed publish of outbox event %s: %w", r.EventID, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit failed publish of outbox event %s: %w", r.EventID, err)
		}
		return true, nil
	}

	// The broker has the event. Record that even if the relay began stopping
	// meanwhile, so a restart does not publish it a second time.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	if _, err := tx.Exec(fctx, markPublishedSQL, r.EventID); err != nil {
		return false, fmt.Errorf("mark outbox event %s published: %w", r.EventID, err)
	}
	if err := tx.Commit(fctx); err != nil {
		return false, fmt.Errorf("commit published outbox event %s: %w", r.EventID, err)
	}
	return true, nil
}

// rollback ends tx if it is still open; after Commit it does nothing. It runs
// detached from ctx so that a cancelled claim hands its connection back to the
// pool cleanly instead of having pgx discard it.
func rollback(ctx context.Context, tx pgx.Tx) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	_ = tx.Rollback(rctx)
}

// lastError is err's text as the last_error column can hold it: valid UTF-8
// with no NUL bytes, since PostgreSQL text rejects either (and a rejected write
// would leave the failure unrecorded and the row retried on every poll), cut
// to at most maxLastError characters.
func lastError(err error) string {
	s := strings.ToValidUTF8(err.Error(), "�")
	s = strings.ReplaceAll(s, "\x00", "")
	n := 0
	for i := range s {
		if n == maxLastError {
			return s[:i]
		}
		n++
	}
	return s
}
